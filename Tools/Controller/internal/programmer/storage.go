package programmer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const HostDataDirectoryEnvironment = "PCCONTROLLER_DATA_DIR"

type HostDataPaths struct {
	DataDir          string `json:"data_dir"`
	BackupsDir       string `json:"backups_dir"`
	BackupOperations string `json:"backup_operations_dir"`
	FirmwareBlobsDir string `json:"firmware_blobs_dir"`
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
	return HostDataPathsFor(dataDirectory)
}

func HostDataPathsFor(dataDirectory string) (HostDataPaths, error) {
	dataDirectory = filepath.Clean(strings.TrimSpace(dataDirectory))
	if dataDirectory == "" || dataDirectory == "." || !filepath.IsAbs(dataDirectory) {
		return HostDataPaths{}, errors.New("host data directory must be an absolute path")
	}
	backups := filepath.Join(dataDirectory, "backups")
	state := filepath.Join(dataDirectory, "state")
	return HostDataPaths{
		DataDir:          dataDirectory,
		BackupsDir:       backups,
		BackupOperations: filepath.Join(backups, "operations"),
		FirmwareBlobsDir: filepath.Join(backups, "firmware", "sha256"),
		StateDir:         state,
		LogsDir:          filepath.Join(dataDirectory, "logs"),
		LastSessionPath:  filepath.Join(state, "last-session.json"),
	}, nil
}

func EnsureHostDataPaths(paths HostDataPaths) error {
	for _, directory := range []string{
		paths.DataDir, paths.BackupsDir, paths.BackupOperations,
		paths.FirmwareBlobsDir, paths.StateDir, paths.LogsDir,
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
		return filepath.Join(base, "PCController"), nil
	case "darwin":
		base, err := home()
		if err != nil {
			return "", fmt.Errorf("locate home directory: %w", err)
		}
		return filepath.Join(base, "Library", "Application Support", "PCController"), nil
	default:
		base := strings.TrimSpace(lookup("XDG_DATA_HOME"))
		if base == "" {
			homeDirectory, err := home()
			if err != nil {
				return "", fmt.Errorf("locate home directory: %w", err)
			}
			base = filepath.Join(homeDirectory, ".local", "share")
		}
		return filepath.Join(base, "pccontroller"), nil
	}
}

type FirmwareBlob struct {
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

type FirmwareTimestamp struct {
	Packed    uint32    `json:"packed"`
	BuildTime time.Time `json:"build_time"`
	Compact   string    `json:"compact"`
}

// DecodeFirmwareTimestampSchema2 decodes the firmware identity's packed local
// build date/time: Y(7), M(4), D(5), h(5), m(6), seconds/2(5).
func DecodeFirmwareTimestampSchema2(packed uint32) (FirmwareTimestamp, error) {
	year := 2000 + int((packed>>25)&0x7F)
	month := time.Month((packed >> 21) & 0x0F)
	day := int((packed >> 16) & 0x1F)
	hour := int((packed >> 11) & 0x1F)
	minute := int((packed >> 5) & 0x3F)
	second := int(packed&0x1F) * 2
	if month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59 {
		return FirmwareTimestamp{}, fmt.Errorf("invalid schema-2 firmware timestamp 0x%08X", packed)
	}
	value := time.Date(year, month, day, hour, minute, second, 0, time.UTC)
	if value.Year() != year || value.Month() != month || value.Day() != day {
		return FirmwareTimestamp{}, fmt.Errorf("invalid schema-2 firmware calendar date 0x%08X", packed)
	}
	return FirmwareTimestamp{
		Packed: packed, BuildTime: value, Compact: value.Format("060102150405"),
	}, nil
}
