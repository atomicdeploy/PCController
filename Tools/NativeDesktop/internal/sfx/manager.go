package sfx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"rayanlamp.local/app/assets"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/winfx"
)

// Cue identifies one semantic Rayan Lamp sound.
type Cue string

const (
	CueOn      Cue = "on"
	CueOff     Cue = "off"
	CueMessage Cue = "message"
	CueError   Cue = "error"

	customStoreDirectory = "sfx"
)

var (
	ErrUnknownCue       = errors.New("sfx: unknown cue")
	ErrCustomAsset      = errors.New("sfx: custom sound asset is unavailable")
	ErrInvalidSelection = errors.New("sfx: invalid sound selection")
)

type bundledDefinition struct {
	cue  Cue
	path string
	name string
}

var bundledDefinitions = []bundledDefinition{
	{cue: CueOn, path: "sfx/on.wav", name: "Lamp on"},
	{cue: CueOff, path: "sfx/off.wav", name: "Lamp off"},
	{cue: CueMessage, path: "sfx/message.wav", name: "Lamp message"},
	{cue: CueError, path: "sfx/error.wav", name: "Lamp error"},
}

// PlaybackFunc plays a complete validated PCM WAV byte stream. The production
// implementation blocks in WinMM, making the caller's worker goroutine own the
// byte slice for the complete SND_MEMORY playback.
type PlaybackFunc func(wave []byte) error

// Options configures one process-local sound manager.
type Options struct {
	ConfigPath    string
	Notifications config.NotificationsConfig
	Playback      PlaybackFunc
}

// Asset is an immutable sound snapshot suitable for an authenticated HTTP
// response or another read-only consumer. Data is always a defensive copy.
type Asset struct {
	ID     string
	Name   string
	Cue    Cue
	Custom bool
	Wave   WaveInfo
	Data   []byte
}

// ImportedAsset identifies one content-addressed user sound. Persist ID and
// Name in NotificationsConfig before selecting the custom source.
type ImportedAsset struct {
	ID   string
	Name string
	Wave WaveInfo
}

// Descriptor is public metadata for one bundled sound.
type Descriptor struct {
	ID   string
	Name string
	Cue  Cue
	Wave WaveInfo
}

type cachedAsset struct {
	id   string
	name string
	wave WaveInfo
	data []byte
}

type selection struct {
	source string
	file   string
	name   string
}

// Manager resolves one atomic sound selection against immutable bundled data
// and a content-addressed per-user store.
type Manager struct {
	storeDir string
	playback PlaybackFunc
	bundled  map[Cue]*cachedAsset

	mu       sync.RWMutex
	selected selection
	custom   map[string]*cachedAsset
	importMu sync.Mutex
}

var (
	bundledOnce        sync.Once
	bundledShared      map[Cue]*cachedAsset
	bundledSharedErr   error
	bundledDescriptors []Descriptor
)

// NewManager validates the bundled catalog, resolves the per-user asset store,
// and atomically applies the initial sound selection.
func NewManager(options Options) (*Manager, error) {
	configPath, err := config.ResolvePath(options.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("sfx: resolve config path: %w", err)
	}
	bundled, err := sharedBundledCatalog()
	if err != nil {
		return nil, err
	}
	playback := options.Playback
	if playback == nil {
		playback = winfx.PlayWaveMemory
	}
	manager := &Manager{
		storeDir: filepath.Join(
			filepath.Dir(configPath),
			customStoreDirectory,
		),
		playback: playback,
		bundled:  bundled,
		custom:   make(map[string]*cachedAsset),
	}
	if err := manager.ApplyConfig(options.Notifications); err != nil {
		// A stale or externally removed custom asset must not make application
		// startup silent or fatal. Publish the complete bundled selection and
		// return the usable manager alongside a typed warning for the host log.
		manager.selected = selection{source: config.SoundSourceBundled}
		return manager, err
	}
	return manager, nil
}

// PreflightAt validates a notification selection against the store beside the
// supplied configuration path. It is intended for process composition before
// the application core is constructed or the winning primary persists config.
func PreflightAt(
	configPath string,
	notifications config.NotificationsConfig,
) (config.NotificationsConfig, error) {
	manager, managerWarning := NewManager(Options{
		ConfigPath:    configPath,
		Notifications: notifications,
		Playback:      func([]byte) error { return nil },
	})
	if manager == nil {
		return repairNotifications(notifications), managerWarning
	}
	effective, warning := manager.PreflightNotifications(notifications)
	if warning != nil {
		return effective, warning
	}
	return effective, managerWarning
}

// PreflightNotifications resolves a candidate selection without publishing it.
// A missing, corrupt, or malformed custom selection is repaired to the bundled
// source and returned with a warning. Callers must use the returned value even
// when err is non-nil so persisted/public configuration cannot claim "custom"
// while playback is using a different asset.
func (m *Manager) PreflightNotifications(
	notifications config.NotificationsConfig,
) (config.NotificationsConfig, error) {
	if m == nil {
		return repairNotifications(notifications),
			errors.New("sfx: manager is nil")
	}
	next, err := normalizeSelection(notifications)
	if err != nil {
		return repairNotifications(notifications), err
	}
	notifications.SoundSource = next.source
	notifications.SoundFile = next.file
	notifications.SoundName = next.name
	if next.source == config.SoundSourceCustom {
		if _, err := m.loadCustom(next.file); err != nil {
			return repairNotifications(notifications), err
		}
	}
	return notifications, nil
}

// ApplyConfig validates and preloads a custom asset before publishing the new
// selection, so concurrent Resolve and Play calls observe either the complete
// old selection or the complete new one.
func (m *Manager) ApplyConfig(notifications config.NotificationsConfig) error {
	if m == nil {
		return errors.New("sfx: manager is nil")
	}
	next, err := normalizeSelection(notifications)
	if err != nil {
		return err
	}
	var selectedCustom *cachedAsset
	if next.source == config.SoundSourceCustom {
		selectedCustom, err = m.loadCustom(next.file)
		if err != nil {
			return err
		}
	}

	m.mu.Lock()
	m.selected = next
	// The content-addressed store is authoritative and every historical asset
	// can be reloaded by ID. Keep only the active custom WAV resident so
	// importing or selecting many different files cannot grow the process
	// heap for its entire lifetime.
	clear(m.custom)
	if selectedCustom != nil {
		m.custom[next.file] = selectedCustom
	}
	m.mu.Unlock()
	return nil
}

// ReconcileConfig preflights and atomically publishes the effective selection.
// Invalid custom settings become a complete bundled selection instead of
// leaving the manager on an older custom asset. The repaired notification
// configuration is returned alongside the original warning.
func (m *Manager) ReconcileConfig(
	notifications config.NotificationsConfig,
) (config.NotificationsConfig, error) {
	effective, warning := m.PreflightNotifications(notifications)
	if err := m.ApplyConfig(effective); err != nil {
		return effective, errors.Join(warning, err)
	}
	return effective, warning
}

// Resolve returns the exact selected WAV bytes for cue. A custom selection
// intentionally maps the same content-addressed asset to all semantic cues.
func (m *Manager) Resolve(cue Cue) (Asset, error) {
	if m == nil {
		return Asset{}, errors.New("sfx: manager is nil")
	}
	normalized, err := normalizeCue(cue)
	if err != nil {
		return Asset{}, err
	}
	cached, custom, name, err := m.resolveCached(normalized)
	if err != nil {
		return Asset{}, err
	}
	data := append([]byte(nil), cached.data...)
	if name == "" {
		name = cached.name
	}
	return Asset{
		ID:     cached.id,
		Name:   name,
		Cue:    normalized,
		Custom: custom,
		Wave:   cached.wave,
		Data:   data,
	}, nil
}

// Play synchronously plays cue from an immutable cached byte slice. Callers
// should invoke it from an existing audio/UI worker rather than the UI thread.
func (m *Manager) Play(cue Cue) error {
	if m == nil {
		return errors.New("sfx: manager is nil")
	}
	normalized, err := normalizeCue(cue)
	if err != nil {
		return err
	}
	cached, _, _, err := m.resolveCached(normalized)
	if err != nil {
		return err
	}
	if m.playback == nil {
		return errors.New("sfx: playback is unavailable")
	}
	return m.playback(cached.data)
}

// ImportFile validates and imports a regular WAV file without loading more
// than MaxWaveBytes+1 into memory.
func (m *Manager) ImportFile(path string) (ImportedAsset, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ImportedAsset{}, errors.New("sfx: custom sound path is empty")
	}
	file, err := os.Open(path)
	if err != nil {
		return ImportedAsset{}, fmt.Errorf("sfx: open custom sound: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return ImportedAsset{}, fmt.Errorf("sfx: inspect custom sound: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ImportedAsset{}, errors.New("sfx: custom sound is not a regular file")
	}
	if info.Size() > MaxWaveBytes {
		return ImportedAsset{}, ErrWaveTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxWaveBytes+1))
	if err != nil {
		return ImportedAsset{}, fmt.Errorf("sfx: read custom sound: %w", err)
	}
	return m.Import(filepath.Base(path), data)
}

// Import stores a valid PCM WAV at most once under its lowercase SHA-256 ID.
// Caller-owned bytes are never retained.
func (m *Manager) Import(name string, wave []byte) (ImportedAsset, error) {
	if m == nil {
		return ImportedAsset{}, errors.New("sfx: manager is nil")
	}
	info, err := ValidateWave(wave)
	if err != nil {
		return ImportedAsset{}, err
	}
	id := assetID(wave)
	displayName := normalizeDisplayName(name)
	data := append([]byte(nil), wave...)

	m.importMu.Lock()
	defer m.importMu.Unlock()
	if err := os.MkdirAll(m.storeDir, 0o700); err != nil {
		return ImportedAsset{}, fmt.Errorf(
			"sfx: create custom sound store: %w",
			err,
		)
	}
	target := filepath.Join(m.storeDir, id+".wav")
	if current, readErr := readBoundedFile(target); readErr == nil {
		if !bytes.Equal(current, data) {
			return ImportedAsset{}, fmt.Errorf(
				"%w: stored asset %s does not match its ID",
				ErrCustomAsset,
				id,
			)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return ImportedAsset{}, fmt.Errorf(
			"sfx: inspect stored custom sound: %w",
			readErr,
		)
	} else if err := writeNewAsset(target, data); err != nil {
		return ImportedAsset{}, err
	}

	// Import establishes durable content-addressed storage, not a second
	// in-memory catalog. ApplyConfig loads and retains the asset if it becomes
	// the active selection.
	return ImportedAsset{ID: id, Name: displayName, Wave: info}, nil
}

// BundledCatalog returns stable metadata without exposing mutable bundled
// buffers.
func BundledCatalog() []Descriptor {
	_, _ = sharedBundledCatalog()
	return append([]Descriptor(nil), bundledDescriptors...)
}

func sharedBundledCatalog() (map[Cue]*cachedAsset, error) {
	bundledOnce.Do(func() {
		bundledShared = make(map[Cue]*cachedAsset, len(bundledDefinitions))
		bundledDescriptors = make(
			[]Descriptor,
			0,
			len(bundledDefinitions),
		)
		for _, definition := range bundledDefinitions {
			data, err := assets.Files.ReadFile(definition.path)
			if err != nil {
				bundledSharedErr = fmt.Errorf(
					"sfx: read bundled %s cue: %w",
					definition.cue,
					err,
				)
				return
			}
			wave, err := ValidateWave(data)
			if err != nil {
				bundledSharedErr = fmt.Errorf(
					"sfx: validate bundled %s cue: %w",
					definition.cue,
					err,
				)
				return
			}
			cached := &cachedAsset{
				id:   assetID(data),
				name: definition.name,
				wave: wave,
				data: data,
			}
			bundledShared[definition.cue] = cached
			bundledDescriptors = append(
				bundledDescriptors,
				Descriptor{
					ID:   cached.id,
					Name: cached.name,
					Cue:  definition.cue,
					Wave: cached.wave,
				},
			)
		}
	})
	if bundledSharedErr != nil {
		return nil, bundledSharedErr
	}
	return bundledShared, nil
}

func (m *Manager) resolveCached(
	cue Cue,
) (*cachedAsset, bool, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.selected.source == config.SoundSourceCustom {
		custom := m.custom[m.selected.file]
		if custom == nil {
			return nil, false, "", fmt.Errorf(
				"%w: %s",
				ErrCustomAsset,
				m.selected.file,
			)
		}
		return custom, true, m.selected.name, nil
	}
	bundled := m.bundled[cue]
	if bundled == nil {
		return nil, false, "", fmt.Errorf("%w %q", ErrUnknownCue, cue)
	}
	return bundled, false, bundled.name, nil
}

func (m *Manager) loadCustom(id string) (*cachedAsset, error) {
	m.mu.RLock()
	cached := m.custom[id]
	m.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	if !validAssetID(id) {
		return nil, fmt.Errorf("%w: invalid custom asset ID", ErrInvalidSelection)
	}

	path := filepath.Join(m.storeDir, id+".wav")
	data, err := readBoundedFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrCustomAsset, id, err)
	}
	if actual := assetID(data); actual != id {
		return nil, fmt.Errorf(
			"%w: stored asset hash is %s, want %s",
			ErrCustomAsset,
			actual,
			id,
		)
	}
	wave, err := ValidateWave(data)
	if err != nil {
		return nil, fmt.Errorf("%w %s: %w", ErrCustomAsset, id, err)
	}
	loaded := &cachedAsset{
		id:   id,
		name: "Custom sound",
		wave: wave,
		data: data,
	}
	// Do not cache a candidate merely because it was preflighted. ApplyConfig
	// atomically publishes both the selection and its sole resident custom
	// asset after validation succeeds.
	return loaded, nil
}

func normalizeSelection(
	notifications config.NotificationsConfig,
) (selection, error) {
	source := strings.ToLower(strings.TrimSpace(notifications.SoundSource))
	if source == "" {
		source = config.SoundSourceBundled
	}
	if source != config.SoundSourceBundled &&
		source != config.SoundSourceCustom {
		return selection{}, fmt.Errorf(
			"%w: source must be bundled or custom",
			ErrInvalidSelection,
		)
	}
	file := strings.TrimSpace(notifications.SoundFile)
	if file != "" && !validAssetID(file) {
		return selection{}, fmt.Errorf(
			"%w: sound file must be a lowercase SHA-256 asset ID",
			ErrInvalidSelection,
		)
	}
	if source == config.SoundSourceCustom && file == "" {
		return selection{}, fmt.Errorf(
			"%w: custom source requires an asset ID",
			ErrInvalidSelection,
		)
	}
	name := strings.TrimSpace(notifications.SoundName)
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return selection{}, fmt.Errorf(
			"%w: sound name contains control characters",
			ErrInvalidSelection,
		)
	}
	return selection{source: source, file: file, name: name}, nil
}

func repairNotifications(
	notifications config.NotificationsConfig,
) config.NotificationsConfig {
	notifications.SoundSource = config.SoundSourceBundled
	notifications.SoundFile = strings.TrimSpace(notifications.SoundFile)
	if !validAssetID(notifications.SoundFile) {
		notifications.SoundFile = ""
	}
	notifications.SoundName = strings.TrimSpace(notifications.SoundName)
	if strings.IndexFunc(notifications.SoundName, unicode.IsControl) >= 0 {
		notifications.SoundName = ""
	}
	return notifications
}

func normalizeCue(cue Cue) (Cue, error) {
	normalized := Cue(strings.ToLower(strings.TrimSpace(string(cue))))
	switch normalized {
	case CueOn, CueOff, CueMessage, CueError:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w %q", ErrUnknownCue, cue)
	}
}

func normalizeDisplayName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "Custom sound.wav"
	}
	runes := []rune(name)
	if len(runes) > 255 {
		name = string(runes[:255])
	}
	return name
}

func assetID(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func validAssetID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func readBoundedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > MaxWaveBytes {
		return nil, ErrWaveTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxWaveBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxWaveBytes {
		return nil, ErrWaveTooLarge
	}
	return data, nil
}

func writeNewAsset(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(
		directory,
		"."+filepath.Base(path)+".*.tmp",
	)
	if err != nil {
		return fmt.Errorf("sfx: create temporary custom sound: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sfx: secure temporary custom sound: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sfx: write temporary custom sound: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sfx: flush temporary custom sound: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("sfx: close temporary custom sound: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("sfx: install custom sound: %w", err)
	}
	removeTemporary = false
	return nil
}
