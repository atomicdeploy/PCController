package programmer

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/ownedstorage"
	"pccontroller.local/controller/internal/pathguard"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	HostDataDirectoryEnvironment = "PCCONTROLLER_DATA_DIR"
	// ToolchainDirectoryEnvironment pins managed Arduino/AVR dependencies to
	// one durable machine-wide-in-practice location instead of a source tree.
	ToolchainDirectoryEnvironment = "PCCONTROLLER_TOOLCHAIN_DIR"
)

type HostDataPaths struct {
	DataDir          string `json:"data_dir"`
	BackupsDir       string `json:"backups_dir"`
	BackupOperations string `json:"backup_operations_dir"`
	FirmwareBlobsDir string `json:"firmware_blobs_dir"`
	BoardSettingsDir string `json:"board_settings_dir"`
	ToolchainDir     string `json:"toolchain_dir"`
	StateDir         string `json:"state_dir"`
	LogsDir          string `json:"logs_dir"`
	LastSessionPath  string `json:"last_session_path"`
}

func DefaultHostDataPaths() (HostDataPaths, error) {
	dataDirectory, err := defaultHostDataDirectory(
		runtime.GOOS, os.Getenv,
		func() (string, error) { return os.UserHomeDir() },
		func() (string, error) { return os.UserConfigDir() },
	)
	if err != nil {
		return HostDataPaths{}, err
	}
	paths, err := HostDataPathsFor(dataDirectory)
	if err != nil {
		return HostDataPaths{}, err
	}
	toolchainDirectory, err := defaultToolchainDirectory(paths.ToolchainDir, os.Getenv)
	if err != nil {
		return HostDataPaths{}, err
	}
	paths.ToolchainDir = toolchainDirectory
	return paths, nil
}

func defaultToolchainDirectory(fallback string, lookup func(string) string) (string, error) {
	for _, name := range []string{ToolchainDirectoryEnvironment} {
		if override := strings.TrimSpace(lookup(name)); override != "" {
			if !filepath.IsAbs(override) {
				return "", fmt.Errorf("%s must be an absolute path", name)
			}
			return filepath.Clean(override), nil
		}
	}
	return fallback, nil
}

func HostDataPathsFor(dataDirectory string) (HostDataPaths, error) {
	dataDirectory = strings.TrimSpace(dataDirectory)
	if dataDirectory == "" || !filepath.IsAbs(dataDirectory) {
		return HostDataPaths{}, errors.New("host data directory must be an absolute path")
	}
	var err error
	dataDirectory, err = pathguard.ResolveAbsolute(dataDirectory)
	if err != nil {
		return HostDataPaths{}, fmt.Errorf("resolve host data directory: %w", err)
	}
	backups := filepath.Join(dataDirectory, "backups")
	state := filepath.Join(dataDirectory, "state")
	return HostDataPaths{
		DataDir:          dataDirectory,
		BackupsDir:       backups,
		BackupOperations: filepath.Join(backups, "operations"),
		FirmwareBlobsDir: filepath.Join(backups, "firmware", "sha256"),
		BoardSettingsDir: filepath.Join(backups, "board-settings", "sha256"),
		ToolchainDir:     filepath.Join(dataDirectory, "tools", "toolchain"),
		StateDir:         state,
		LogsDir:          filepath.Join(dataDirectory, "logs"),
		LastSessionPath:  filepath.Join(state, "last-session.json"),
	}, nil
}

func EnsureHostDataPaths(paths HostDataPaths) error {
	if err := ownedstorage.Ensure(paths.DataDir); err != nil {
		return fmt.Errorf("establish host data ownership: %w", err)
	}
	for _, directory := range []string{
		paths.DataDir, paths.BackupsDir, paths.BackupOperations,
		paths.FirmwareBlobsDir, paths.BoardSettingsDir, paths.ToolchainDir,
		paths.StateDir, paths.LogsDir,
	} {
		if strings.TrimSpace(directory) == "" || !filepath.IsAbs(directory) {
			return fmt.Errorf("invalid host data path %q", directory)
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create host data directory %q: %w", directory, err)
		}
	}
	return nil
}

func defaultHostDataDirectory(
	goos string,
	lookup func(string) string,
	home func() (string, error),
	config func() (string, error),
) (string, error) {
	if override := strings.TrimSpace(lookup(HostDataDirectoryEnvironment)); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s must be an absolute path", HostDataDirectoryEnvironment)
		}
		return filepath.Clean(override), nil
	}
	switch goos {
	case "windows":
		base := strings.TrimSpace(lookup("LOCALAPPDATA"))
		if base == "" {
			var err error
			base, err = config()
			if err != nil {
				return "", fmt.Errorf("locate Windows application data: %w", err)
			}
		}
		return filepath.Join(base, productidentity.ConfigDirectory), nil
	case "darwin":
		base, err := home()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		return filepath.Join(base, "Library", "Application Support", productidentity.ConfigDirectory), nil
	default:
		base := strings.TrimSpace(lookup("XDG_DATA_HOME"))
		if base == "" {
			homeDirectory, err := home()
			if err != nil {
				return "", fmt.Errorf("locate home directory: %w", err)
			}
			base = filepath.Join(homeDirectory, ".local", "share")
		}
		return filepath.Join(base, strings.ToLower(productidentity.ConfigDirectory)), nil
	}
}

type FirmwareBlob struct {
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	Path         string `json:"path"`
	Reference    string `json:"reference"`
	Deduplicated bool   `json:"deduplicated"`
}

type MetadataBlob struct {
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	Path         string `json:"path"`
	Reference    string `json:"reference"`
	Deduplicated bool   `json:"deduplicated"`
}

// StoreFirmwareBlob publishes one immutable, content-addressed firmware file.
// The hard-link publish is atomic and O_EXCL-like: concurrent writers either
// create the same final name once or verify and reuse the winner.
func StoreFirmwareBlob(blobRoot, sourcePath string) (FirmwareBlob, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return FirmwareBlob{}, fmt.Errorf("read firmware blob source: %w", err)
	}
	if _, err := ParseIntelHex(strings.NewReader(string(content))); err != nil {
		return FirmwareBlob{}, fmt.Errorf("firmware blob is not valid Intel HEX: %w", err)
	}
	hash := sha256Hex(content)
	reference := "firmware-sha256-" + hash + ".hex"
	directory := filepath.Join(blobRoot, hash[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return FirmwareBlob{}, fmt.Errorf("create firmware blob directory: %w", err)
	}
	destination := filepath.Join(directory, reference)
	if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); verifyErr == nil && existing {
		return FirmwareBlob{
			SHA256: hash, Bytes: int64(len(content)), Path: destination,
			Reference: reference, Deduplicated: true,
		}, nil
	}
	temporary, err := os.CreateTemp(directory, ".firmware-*.tmp")
	if err != nil {
		return FirmwareBlob{}, fmt.Errorf("create firmware blob temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return FirmwareBlob{}, err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return FirmwareBlob{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return FirmwareBlob{}, err
	}
	if err := temporary.Close(); err != nil {
		return FirmwareBlob{}, err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); !existing || verifyErr != nil {
			return FirmwareBlob{}, fmt.Errorf("publish firmware blob: %w", err)
		}
		return FirmwareBlob{
			SHA256: hash, Bytes: int64(len(content)), Path: destination,
			Reference: reference, Deduplicated: true,
		}, nil
	}
	return FirmwareBlob{
		SHA256: hash, Bytes: int64(len(content)), Path: destination,
		Reference: reference,
	}, nil
}

// StoreEEPROMBlob publishes one immutable full EEPROM image by content hash.
// Operation manifests reference this single copy, so repeated captures never
// consume duplicate storage while each capture keeps its own timestamp.
func StoreEEPROMBlob(blobRoot, sourcePath string) (EEPROMBlob, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return EEPROMBlob{}, fmt.Errorf("read EEPROM blob source: %w", err)
	}
	document, err := ParseIntelHex(strings.NewReader(string(content)))
	if err != nil {
		return EEPROMBlob{}, fmt.Errorf("EEPROM blob is not valid Intel HEX: %w", err)
	}
	if err := requireFullEEPROMImage(document); err != nil {
		return EEPROMBlob{}, fmt.Errorf("EEPROM blob is incomplete: %w", err)
	}
	hash := sha256Hex(content)
	reference := "eeprom-sha256-" + hash + ".hex"
	directory := filepath.Join(blobRoot, hash[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return EEPROMBlob{}, fmt.Errorf("create EEPROM blob directory: %w", err)
	}
	destination := filepath.Join(directory, reference)
	if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); verifyErr == nil && existing {
		return EEPROMBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference, Deduplicated: true}, nil
	}
	temporary, err := os.CreateTemp(directory, ".eeprom-*.tmp")
	if err != nil {
		return EEPROMBlob{}, fmt.Errorf("create EEPROM blob temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return EEPROMBlob{}, err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return EEPROMBlob{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return EEPROMBlob{}, err
	}
	if err := temporary.Close(); err != nil {
		return EEPROMBlob{}, err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); !existing || verifyErr != nil {
			return EEPROMBlob{}, fmt.Errorf("publish EEPROM blob: %w", err)
		}
		return EEPROMBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference, Deduplicated: true}, nil
	}
	return EEPROMBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference}, nil
}

// StoreMetadataBlob publishes the programmer/fuse report by content hash.
// Operation timestamps remain in the manifest; identical hardware reports do
// not create duplicate payload files.
func StoreMetadataBlob(blobRoot, sourcePath string) (MetadataBlob, error) {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return MetadataBlob{}, fmt.Errorf("read metadata blob source: %w", err)
	}
	if len(content) == 0 {
		return MetadataBlob{}, errors.New("metadata blob is empty")
	}
	hash := sha256Hex(content)
	reference := "metadata-sha256-" + hash + ".txt"
	directory := filepath.Join(blobRoot, hash[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return MetadataBlob{}, fmt.Errorf("create metadata blob directory: %w", err)
	}
	destination := filepath.Join(directory, reference)
	if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); verifyErr == nil && existing {
		return MetadataBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference, Deduplicated: true}, nil
	}
	temporary, err := os.CreateTemp(directory, ".metadata-*.tmp")
	if err != nil {
		return MetadataBlob{}, fmt.Errorf("create metadata blob temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return MetadataBlob{}, err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return MetadataBlob{}, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return MetadataBlob{}, err
	}
	if err := temporary.Close(); err != nil {
		return MetadataBlob{}, err
	}
	if err := os.Link(temporaryPath, destination); err != nil {
		if existing, verifyErr := verifyBlob(destination, hash, int64(len(content))); !existing || verifyErr != nil {
			return MetadataBlob{}, fmt.Errorf("publish metadata blob: %w", err)
		}
		return MetadataBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference, Deduplicated: true}, nil
	}
	return MetadataBlob{SHA256: hash, Bytes: int64(len(content)), Path: destination, Reference: reference}, nil
}

func verifyBlob(path, expectedHash string, expectedBytes int64) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != expectedBytes {
		return true, errors.New("content-addressed firmware blob has wrong type or size")
	}
	file, err := backupFile(path, "flash")
	if err != nil {
		return true, err
	}
	if file.SHA256 != expectedHash {
		return true, errors.New("content-addressed firmware blob hash mismatch")
	}
	return true, nil
}

// PruneExpiredBackups removes only operation manifests whose explicit
// retention deadline passed. The just-created operation and the most recent
// predecessor are always protected. Content-addressed payload blobs are
// retained because another operation may still reference them.
func PruneExpiredBackups(root string, now time.Time, currentDirectory string) ([]string, error) {
	operations := filepath.Join(root, "operations")
	entries, err := os.ReadDir(operations)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	type candidate struct {
		directory string
		manifest  BackupManifest
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(operations, entry.Name())
		content, readErr := os.ReadFile(filepath.Join(directory, "manifest.json"))
		if readErr != nil {
			continue
		}
		var manifest BackupManifest
		if json.Unmarshal(content, &manifest) != nil || manifest.CreatedAt.IsZero() {
			continue
		}
		candidates = append(candidates, candidate{directory: directory, manifest: manifest})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].manifest.CreatedAt.After(candidates[j].manifest.CreatedAt)
	})
	protected := map[string]bool{}
	if absolute, absErr := filepath.Abs(currentDirectory); absErr == nil {
		protected[filepath.Clean(absolute)] = true
	}
	for _, item := range candidates {
		absolute, _ := filepath.Abs(item.directory)
		if !protected[filepath.Clean(absolute)] {
			protected[filepath.Clean(absolute)] = true // newest predecessor
			break
		}
	}
	removed := make([]string, 0)
	for _, item := range candidates {
		absolute, _ := filepath.Abs(item.directory)
		if protected[filepath.Clean(absolute)] || item.manifest.RetainUntil.IsZero() || !now.After(item.manifest.RetainUntil) {
			continue
		}
		if err := os.RemoveAll(item.directory); err != nil {
			return removed, err
		}
		removed = append(removed, item.directory)
	}
	if len(removed) != 0 {
		if err := pruneUnreferencedBackupBlobs(root); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func pruneUnreferencedBackupBlobs(root string) error {
	referenced := map[string]bool{}
	operations := filepath.Join(root, "operations")
	entries, err := os.ReadDir(operations)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(operations, entry.Name())
		content, readErr := os.ReadFile(filepath.Join(directory, "manifest.json"))
		if readErr != nil {
			return fmt.Errorf("retain content blobs because backup manifest %q is unreadable: %w", entry.Name(), readErr)
		}
		var manifest BackupManifest
		if unmarshalErr := json.Unmarshal(content, &manifest); unmarshalErr != nil {
			return fmt.Errorf("retain content blobs because backup manifest %q is invalid: %w", entry.Name(), unmarshalErr)
		}
		for _, file := range manifest.Files {
			if !strings.HasPrefix(file.Storage, "content-addressed") {
				continue
			}
			path := filepath.Clean(filepath.Join(directory, filepath.FromSlash(file.RelativePath)))
			if absolute, absErr := filepath.Abs(path); absErr == nil {
				referenced[filepath.Clean(absolute)] = true
			}
		}
	}
	for _, directory := range []string{
		filepath.Join(root, "firmware", "sha256"),
		filepath.Join(root, "eeprom", "sha256"),
		filepath.Join(root, "metadata", "sha256"),
	} {
		walkErr := filepath.Walk(directory, func(path string, info os.FileInfo, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("refuse to prune non-regular backup blob %q", path)
			}
			absolute, absErr := filepath.Abs(path)
			if absErr != nil {
				return absErr
			}
			if !referenced[filepath.Clean(absolute)] {
				return os.Remove(path)
			}
			return nil
		})
		if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
			return walkErr
		}
	}
	return nil
}

type FirmwareTimestamp struct {
	Packed    uint32    `json:"packed"`
	BuildTime time.Time `json:"build_time"`
	Compact   string    `json:"compact"`
}

// DecodeFirmwareTimestamp decodes the firmware identity's packed local
// build date/time: Y(7), M(4), D(5), h(5), m(6), seconds/2(5).
func DecodeFirmwareTimestamp(packed uint32) (FirmwareTimestamp, error) {
	year := 2000 + int((packed>>25)&0x7F)
	month := time.Month((packed >> 21) & 0x0F)
	day := int((packed >> 16) & 0x1F)
	hour := int((packed >> 11) & 0x1F)
	minute := int((packed >> 5) & 0x3F)
	second := int(packed&0x1F) * 2
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return FirmwareTimestamp{}, fmt.Errorf("invalid packed firmware timestamp 0x%08X", packed)
	}
	value := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return FirmwareTimestamp{}, fmt.Errorf("invalid packed firmware calendar date 0x%08X", packed)
	}
	return FirmwareTimestamp{
		Packed: packed, BuildTime: value, Compact: value.Format("060102150405"),
	}, nil
}
