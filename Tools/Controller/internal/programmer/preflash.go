package programmer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"pccontroller.local/controller/internal/deployment"
)

type FlashOperation func(context.Context, string, io.Writer) error
type PostBackupOperation func(context.Context, AutomaticPreflashResult, io.Writer) error

type AutomaticPreflashOptions struct {
	FirmwarePath       string
	Backup             Options
	DataPaths          HostDataPaths
	Deployment         string
	ReinitializeEEPROM bool
	// AfterBackup runs after the selected capture policy, before firmware is
	// reinspected or written. EEPROM-reset callers require a raw backup here to arm durable
	// board-side programming state without contaminating the original backup.
	AfterBackup PostBackupOperation
}

type AutomaticPreflashResult struct {
	FirmwarePath    string   `json:"firmware_path"`
	FirmwareSHA256  string   `json:"firmware_sha256"`
	Deployment      string   `json:"deployment"`
	BackupSkipped   bool     `json:"backup_skipped"`
	BackupDirectory string   `json:"backup_directory,omitempty"`
	BackupManifest  string   `json:"backup_manifest,omitempty"`
	BackupReference string   `json:"backup_reference,omitempty"`
	BackupComplete  bool     `json:"backup_complete"`
	Flashed         bool     `json:"flashed"`
	Warnings        []string `json:"warnings,omitempty"`
}

// AutomaticBackupThenFlash protects production writes with a complete backup.
// Explicit development uploads skip only new archival capture. EEPROM resets
// always require capture; target integrity and write verification are unchanged.
func AutomaticBackupThenFlash(
	ctx context.Context,
	options AutomaticPreflashOptions,
	runner CommandRunner,
	flash FlashOperation,
	output io.Writer,
) (AutomaticPreflashResult, error) {
	result := AutomaticPreflashResult{FirmwarePath: options.FirmwarePath}
	classification, err := deployment.Normalize(options.Deployment)
	if err != nil {
		return result, err
	}
	result.Deployment = classification
	if runner == nil {
		return result, errors.New("automatic pre-flash backup requires a command runner")
	}
	if flash == nil {
		return result, errors.New("automatic pre-flash backup requires a flash operation")
	}
	document, err := LoadIntelHex(options.FirmwarePath)
	if err != nil {
		return result, fmt.Errorf("inspect firmware before backup: %w", err)
	}
	for address := range document.Image.data {
		if address >= ATmega328PFlashSize {
			return result, fmt.Errorf("firmware address 0x%X exceeds ATmega328P flash", address)
		}
	}
	result.FirmwareSHA256 = document.SourceSHA256

	backupOptions := options.Backup
	backupOptions.Operation = OperationBackup
	if backupOptions.Method == "" {
		backupOptions.Method = MethodUrclock
	}
	switch backupOptions.Method {
	case MethodUrclock:
		if strings.TrimSpace(backupOptions.Port) == "" {
			return result, errors.New("automatic Urclock backup requires a serial port")
		}
	case MethodUSBasp:
		// Explicit MethodUSBasp selection chooses the advanced recovery path.
	default:
		return result, fmt.Errorf(
			"automatic pre-flash backup supports Urclock or USBasp, got %q",
			backupOptions.Method,
		)
	}
	if !deployment.RequiresBackup(classification, options.ReinitializeEEPROM) {
		result.BackupSkipped = true
		result.Warnings = append(result.Warnings, "explicit development deployment: no new archival backup; existing backups retained")
	} else {
		paths := options.DataPaths
		if strings.TrimSpace(backupOptions.OutputPath) == "" {
			if strings.TrimSpace(paths.DataDir) == "" {
				paths, err = DefaultHostDataPaths()
				if err != nil {
					return result, err
				}
			}
			if err := EnsureHostDataPaths(paths); err != nil {
				return result, err
			}
			backupOptions.OutputPath = paths.BackupsDir
		}
		backupDirectory, backupErr := BackupWithRunner(
			ctx, backupOptions, output, runner,
		)
		result.BackupDirectory = backupDirectory
		if backupDirectory != "" {
			result.BackupManifest = joinManifestPath(backupDirectory)
		}
		if backupErr == nil {
			validated, validateErr := ValidateBackupManifest(result.BackupManifest)
			if validateErr == nil {
				result.BackupComplete = true
				result.BackupReference = validated.Manifest.Reference
			} else {
				backupErr = fmt.Errorf("validate automatic backup: %w", validateErr)
			}
		}
		if !result.BackupComplete {
			if backupErr == nil {
				backupErr = errors.New("automatic backup did not produce a complete validated manifest")
			}
			return result, fmt.Errorf("refusing to flash without complete backup: %w", backupErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("pre-flash operation canceled after backup: %w", err)
	}
	if options.AfterBackup != nil {
		if err := options.AfterBackup(ctx, result, output); err != nil {
			return result, fmt.Errorf("post-backup programming preparation: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("pre-flash operation canceled after post-backup preparation: %w", err)
	}
	finalDocument, err := LoadIntelHex(options.FirmwarePath)
	if err != nil {
		return result, fmt.Errorf("reinspect firmware after backup: %w", err)
	}
	if finalDocument.SourceSHA256 != result.FirmwareSHA256 {
		return result, fmt.Errorf(
			"refusing to flash: firmware changed during backup (was %s, now %s)",
			result.FirmwareSHA256, finalDocument.SourceSHA256,
		)
	}
	if err := flash(ctx, options.FirmwarePath, output); err != nil {
		return result, fmt.Errorf("flash operation after backup: %w", err)
	}
	result.Flashed = true
	return result, nil
}

func joinManifestPath(directory string) string {
	return filepath.Join(directory, "manifest.json")
}
