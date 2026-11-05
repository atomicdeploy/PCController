package sfx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/sfxgen"
)

func TestManagerResolvesAndPlaysBundledCatalogBytes(t *testing.T) {
	t.Parallel()
	var (
		playedMu sync.Mutex
		played   []byte
	)
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Notifications: config.NotificationsConfig{
			SoundSource: config.SoundSourceBundled,
		},
		Playback: func(wave []byte) error {
			playedMu.Lock()
			played = append([]byte(nil), wave...)
			playedMu.Unlock()
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	catalog := BundledCatalog()
	if len(catalog) != 4 {
		t.Fatalf("bundled catalog length = %d, want 4", len(catalog))
	}
	seen := make(map[Cue]bool)
	for _, descriptor := range catalog {
		seen[descriptor.Cue] = true
		asset, resolveErr := manager.Resolve(descriptor.Cue)
		if resolveErr != nil {
			t.Fatalf("resolve %s: %v", descriptor.Cue, resolveErr)
		}
		if asset.Custom ||
			asset.ID != descriptor.ID ||
			asset.Name != descriptor.Name ||
			asset.Wave != descriptor.Wave {
			t.Fatalf("resolved %s = %+v", descriptor.Cue, asset)
		}
		if got := assetID(asset.Data); got != descriptor.ID {
			t.Fatalf("%s data hash = %s, want %s", descriptor.Cue, got, descriptor.ID)
		}
		asset.Data[0] ^= 0xff
		again, resolveErr := manager.Resolve(descriptor.Cue)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if got := assetID(again.Data); got != descriptor.ID {
			t.Fatal("Resolve exposed mutable cached bundled data")
		}
	}
	for _, cue := range []Cue{CueOn, CueOff, CueMessage, CueError} {
		if !seen[cue] {
			t.Fatalf("bundled catalog is missing %s", cue)
		}
	}

	expected, err := manager.Resolve(CueMessage)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Play(CueMessage); err != nil {
		t.Fatal(err)
	}
	playedMu.Lock()
	defer playedMu.Unlock()
	if !bytes.Equal(played, expected.Data) {
		t.Fatal("Play did not pass the exact resolved bytes to the backend")
	}
}

func TestEmbeddedBundledCatalogMatchesDeterministicGenerator(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Playback:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, generated := range sfxgen.GenerateCatalog() {
		asset, resolveErr := manager.Resolve(Cue(generated.Cue))
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if !bytes.Equal(asset.Data, generated.Data) {
			t.Fatalf(
				"embedded %s WAV differs from deterministic generator",
				generated.Cue,
			)
		}
	}
}

func TestManagerImportsCustomSoundOnceAndAppliesImmediately(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	var played []byte
	manager, err := NewManager(Options{
		ConfigPath: configPath,
		Notifications: config.NotificationsConfig{
			SoundSource: config.SoundSourceBundled,
		},
		Playback: func(wave []byte) error {
			played = append([]byte(nil), wave...)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := sfxgen.Generate("message")
	if err != nil {
		t.Fatal(err)
	}
	pristine := append([]byte(nil), custom...)
	imported, err := manager.Import(`C:\Sounds\Studio chime.wav`, custom)
	if err != nil {
		t.Fatal(err)
	}
	if imported.ID != assetID(pristine) ||
		imported.Name != "Studio chime.wav" {
		t.Fatalf("imported asset = %+v", imported)
	}
	custom[0] ^= 0xff

	second, err := manager.Import("Renamed.wav", pristine)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != imported.ID {
		t.Fatalf("second import ID = %s, want %s", second.ID, imported.ID)
	}
	entries, err := os.ReadDir(filepath.Join(root, customStoreDirectory))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != imported.ID+".wav" {
		t.Fatalf("stored entries = %+v", entries)
	}

	selection := config.NotificationsConfig{
		SoundSource: config.SoundSourceCustom,
		SoundFile:   imported.ID,
		SoundName:   "My shared cue",
	}
	if err := manager.ApplyConfig(selection); err != nil {
		t.Fatal(err)
	}
	for _, cue := range []Cue{CueOn, CueOff, CueMessage, CueError} {
		asset, resolveErr := manager.Resolve(cue)
		if resolveErr != nil {
			t.Fatal(resolveErr)
		}
		if !asset.Custom ||
			asset.Cue != cue ||
			asset.ID != imported.ID ||
			asset.Name != selection.SoundName ||
			!bytes.Equal(asset.Data, pristine) {
			t.Fatalf("custom %s asset = %+v", cue, asset)
		}
	}
	if err := manager.Play(CueOn); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(played, pristine) {
		t.Fatal("custom playback bytes differ from imported bytes")
	}

	reloaded, err := NewManager(Options{
		ConfigPath:    configPath,
		Notifications: selection,
		Playback:      func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := reloaded.Resolve(CueError)
	if err != nil {
		t.Fatal(err)
	}
	if asset.ID != imported.ID ||
		asset.Name != selection.SoundName ||
		!bytes.Equal(asset.Data, pristine) {
		t.Fatalf("reloaded custom asset = %+v", asset)
	}
}

func TestManagerRetainsOnlySelectedCustomSoundAndReloadsHistory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	var played []byte
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(root, "config.json"),
		Playback: func(wave []byte) error {
			played = append([]byte(nil), wave...)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	base, err := sfxgen.Generate("message")
	if err != nil {
		t.Fatal(err)
	}
	const assetCount = 20
	ids := make([]string, 0, assetCount)
	waves := make(map[string][]byte, assetCount)
	for index := 0; index < assetCount; index++ {
		wave := append([]byte(nil), base...)
		// Mutating PCM sample bytes changes the content identity without
		// invalidating the RIFF/WAVE structure.
		wave[len(wave)-1] = byte(index)
		wave[len(wave)-2] = byte(index * 7)
		imported, importErr := manager.Import(
			fmt.Sprintf("cue-%02d.wav", index),
			wave,
		)
		if importErr != nil {
			t.Fatalf("import %d: %v", index, importErr)
		}
		ids = append(ids, imported.ID)
		waves[imported.ID] = wave
		if got := len(manager.custom); got != 0 {
			t.Fatalf(
				"unselected import %d left %d resident custom assets",
				index,
				got,
			)
		}
	}

	for index, id := range ids {
		if err := manager.ApplyConfig(config.NotificationsConfig{
			SoundSource: config.SoundSourceCustom,
			SoundFile:   id,
			SoundName:   fmt.Sprintf("Selected %d", index),
		}); err != nil {
			t.Fatalf("select %d: %v", index, err)
		}
		if got := len(manager.custom); got != 1 {
			t.Fatalf("selected %d left %d resident custom assets", index, got)
		}
		if manager.custom[id] == nil {
			t.Fatalf("selected %d asset %s is not resident", index, id)
		}
		resolved, resolveErr := manager.Resolve(CueOn)
		if resolveErr != nil {
			t.Fatalf("resolve %d: %v", index, resolveErr)
		}
		if !bytes.Equal(resolved.Data, waves[id]) {
			t.Fatalf("resolved %d bytes differ from imported content", index)
		}
	}

	// The first ID was evicted many selections ago. Selecting it again must
	// transparently reload the exact durable bytes used for playback.
	firstID := ids[0]
	if err := manager.ApplyConfig(config.NotificationsConfig{
		SoundSource: config.SoundSourceCustom,
		SoundFile:   firstID,
		SoundName:   "Historical selection",
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Play(CueError); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(played, waves[firstID]) {
		t.Fatal("historical custom playback differs after disk reload")
	}

	if err := manager.ApplyConfig(config.NotificationsConfig{
		SoundSource: config.SoundSourceBundled,
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(manager.custom); got != 0 {
		t.Fatalf("bundled selection retained %d custom assets", got)
	}
}

func TestApplyConfigFailureRetainsPreviousSelectionAndStartupFallsBack(t *testing.T) {
	t.Parallel()
	configPath := filepath.Join(t.TempDir(), "config.json")
	manager, err := NewManager(Options{
		ConfigPath: configPath,
		Notifications: config.NotificationsConfig{
			SoundSource: config.SoundSourceBundled,
		},
		Playback: func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := manager.Resolve(CueOn)
	if err != nil {
		t.Fatal(err)
	}
	missing := config.NotificationsConfig{
		SoundSource: config.SoundSourceCustom,
		SoundFile: "0123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
		SoundName: "Missing",
	}
	if err := manager.ApplyConfig(missing); !errors.Is(err, ErrCustomAsset) {
		t.Fatalf("ApplyConfig missing error = %v", err)
	}
	after, err := manager.Resolve(CueOn)
	if err != nil {
		t.Fatal(err)
	}
	if before.ID != after.ID || after.Custom {
		t.Fatal("failed ApplyConfig changed the active bundled selection")
	}

	fallback, warning := NewManager(Options{
		ConfigPath:    configPath,
		Notifications: missing,
		Playback:      func([]byte) error { return nil },
	})
	if fallback == nil || !errors.Is(warning, ErrCustomAsset) {
		t.Fatalf("NewManager missing = (%v, %v)", fallback, warning)
	}
	resolved, err := fallback.Resolve(CueMessage)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Custom {
		t.Fatal("startup warning did not fall back to bundled sounds")
	}
}

func TestReconcileConfigRepairsUnavailableCustomSelectionToBundled(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Playback:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	wave, err := sfxgen.Generate("message")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := manager.Import("available.wav", wave)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ApplyConfig(config.NotificationsConfig{
		SoundSource: config.SoundSourceCustom,
		SoundFile:   imported.ID,
		SoundName:   imported.Name,
	}); err != nil {
		t.Fatal(err)
	}

	missing := config.NotificationsConfig{
		Sound:       true,
		SoundSource: config.SoundSourceCustom,
		SoundFile: "0123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
		SoundName: "Missing.wav",
	}
	repaired, warning := manager.ReconcileConfig(missing)
	if !errors.Is(warning, ErrCustomAsset) {
		t.Fatalf("ReconcileConfig warning = %v", warning)
	}
	if repaired.SoundSource != config.SoundSourceBundled ||
		repaired.SoundFile != missing.SoundFile ||
		repaired.SoundName != missing.SoundName ||
		!repaired.Sound {
		t.Fatalf("repaired notifications = %+v", repaired)
	}
	resolved, err := manager.Resolve(CueOn)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Custom || resolved.ID == imported.ID {
		t.Fatal("reconciled manager retained the older custom selection")
	}
}

func TestPreflightNotificationsRepairsMalformedPublicMetadata(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Playback:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	repaired, warning := manager.PreflightNotifications(
		config.NotificationsConfig{
			SoundSource: config.SoundSourceCustom,
			SoundFile:   "NOT-A-SHA",
			SoundName:   "Bad\nname",
		},
	)
	if !errors.Is(warning, ErrInvalidSelection) {
		t.Fatalf("PreflightNotifications warning = %v", warning)
	}
	if repaired.SoundSource != config.SoundSourceBundled ||
		repaired.SoundFile != "" ||
		repaired.SoundName != "" {
		t.Fatalf("repaired notifications = %+v", repaired)
	}
}

func TestPreflightAtRepairsMissingCustomAssetBeforeComposition(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	missing := config.NotificationsConfig{
		SoundSource: config.SoundSourceCustom,
		SoundFile: "0123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
		SoundName: "Missing.wav",
	}
	effective, warning := PreflightAt(
		filepath.Join(root, "config.json"),
		missing,
	)
	if !errors.Is(warning, ErrCustomAsset) {
		t.Fatalf("PreflightAt warning = %v", warning)
	}
	if effective.SoundSource != config.SoundSourceBundled ||
		effective.SoundFile != missing.SoundFile ||
		effective.SoundName != missing.SoundName {
		t.Fatalf("effective notifications = %+v", effective)
	}
}

func TestImportRejectsInvalidAndConflictingStoredData(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(root, "config.json"),
		Playback:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Import("invalid.wav", []byte("not wave")); !errors.Is(err, ErrInvalidWave) {
		t.Fatalf("invalid import error = %v", err)
	}
	wave, err := sfxgen.Generate("on")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(wave)
	id := hex.EncodeToString(sum[:])
	store := filepath.Join(root, customStoreDirectory)
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(store, id+".wav"),
		[]byte("corrupt"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Import("on.wav", wave); !errors.Is(err, ErrCustomAsset) {
		t.Fatalf("conflicting import error = %v", err)
	}
}

func TestManagerRejectsUnknownCueAndInvalidSelection(t *testing.T) {
	t.Parallel()
	manager, err := NewManager(Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Playback:   func([]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Resolve("alarm"); !errors.Is(err, ErrUnknownCue) {
		t.Fatalf("unknown cue error = %v", err)
	}
	if err := manager.Play("alarm"); !errors.Is(err, ErrUnknownCue) {
		t.Fatalf("unknown playback cue error = %v", err)
	}
	if err := manager.ApplyConfig(config.NotificationsConfig{
		SoundSource: "remote",
	}); !errors.Is(err, ErrInvalidSelection) {
		t.Fatalf("invalid selection error = %v", err)
	}
}
