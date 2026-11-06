package sfx

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pccontroller.local/desktop/assets"
	"pccontroller.local/desktop/internal/config"
)

func TestCompressedImportsNormalizeAndRemainPlayableAfterRestart(t *testing.T) {
	for _, format := range []string{"mp3", "flac"} {
		t.Run(format, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", "chime."+format))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			newManager := func(notifications config.NotificationsConfig) *Manager {
				manager, err := NewManager(Options{ConfigPath: path, Notifications: notifications,
					Playback: func(wave []byte) error { _, err := ValidateWave(wave); return err }})
				if err != nil {
					t.Fatal(err)
				}
				return manager
			}
			manager := newManager(config.NotificationsConfig{})
			// Deliberately misleading extension: the importer must use bytes.
			imported, err := manager.Import("My chime.wav", source)
			if err != nil {
				t.Fatal(err)
			}
			if imported.Wave.Duration <= 0 || imported.Wave.Duration > MaxDuration {
				t.Fatal(imported)
			}
			second, err := manager.Import("renamed."+format, source)
			if err != nil || second.ID != imported.ID {
				t.Fatalf("deduplication: %+v %v", second, err)
			}
			selection := config.NotificationsConfig{SoundSource: config.SoundSourceCustom,
				SoundFile: imported.ID, SoundName: imported.Name}
			reloaded := newManager(selection)
			asset, err := reloaded.Resolve(CueMessage)
			if err != nil {
				t.Fatal(err)
			}
			if !asset.Custom || asset.Name != "My chime.wav" || asset.ID != assetID(asset.Data) {
				t.Fatal(asset)
			}
			if bytes.Equal(asset.Data, source) {
				t.Fatal("compressed bytes reached WAV playback")
			}
			if err := reloaded.Play(CueOn); err != nil {
				t.Fatal(err)
			}
			if format == "flac" {
				original, _ := assets.Files.ReadFile("sfx/on.wav")
				if !bytes.Equal(asset.Data, original) {
					t.Fatal("lossless import changed PCM or canonical WAV")
				}
			}
		})
	}
}

func TestCompressedImportsRejectLongAndMalformedWithoutChangingSelection(t *testing.T) {
	manager, err := NewManager(Options{ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Playback: func([]byte) error { t.Fatal("import must not play audio"); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := manager.Resolve(CueOn)
	for _, format := range []string{"mp3", "flac"} {
		short, err := os.ReadFile(filepath.Join("testdata", "chime."+format))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := manager.Import("truncated."+format, short[:len(short)-1]); err == nil {
			t.Fatalf("accepted truncated %s", format)
		}
		source, err := os.ReadFile(filepath.Join("testdata", "long."+format))
		if err != nil {
			t.Fatal(err)
		}
		for _, data := range [][]byte{source, source[:12]} {
			if _, err := manager.Import("cue."+format, data); err == nil {
				t.Fatalf("accepted long/malformed %s", format)
			}
		}
	}
	if _, err := manager.Import("huge.mp3", make([]byte, MaxWaveBytes+1)); !errors.Is(err, ErrWaveTooLarge) {
		t.Fatal(err)
	}
	after, _ := manager.Resolve(CueOn)
	if after.ID != before.ID || after.Custom {
		t.Fatal("failed import changed selection")
	}
	if entries, _ := os.ReadDir(manager.storeDir); len(entries) != 0 {
		t.Fatal("failed import persisted an asset")
	}
}

func TestCompressedImportFileAndPCMExpansionBounds(t *testing.T) {
	manager, err := NewManager(Options{ConfigPath: filepath.Join(t.TempDir(), "config.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.ImportFile(filepath.Join("testdata", "chime.mp3")); err != nil {
		t.Fatal(err)
	}
	for _, format := range [][3]int{{96000, 2, 32}, {8000, 1, 24}} {
		pcm, err := newPCMImport(format[0], format[1], format[2])
		if err != nil {
			t.Fatal(err)
		}
		if err := pcm.append(make([]byte, pcm.limit+1)); err == nil {
			t.Fatal("accepted excess expansion")
		}
		if err := pcm.append(make([]byte, 3*(format[2]/8)*format[1])); err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateWave(pcm.wave()); err != nil {
			t.Fatal(err)
		}
	}
}
