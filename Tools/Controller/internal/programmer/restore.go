package programmer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ValidatedBackup struct {
	Manifest       BackupManifest
	ManifestPath   string
	ManifestSHA256 string
	Directory      string
	Files          map[string]ValidatedBackupFile
}

type ValidatedBackupFile struct {
	BackupFile
	Path string
}

// ValidateBackupManifest re-hashes every referenced artifact and validates
// both flash and EEPROM as Intel HEX. It accepts no symlinks or paths outside
// the operation's backup root.
func ValidateBackupManifest(manifestPath string) (ValidatedBackup, error) {
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return ValidatedBackup{}, fmt.Errorf("read backup manifest: %w", err)
	}
	var manifest BackupManifest
	decoder := json.NewDecoder(strings.NewReader(string(content)))
	if err := decoder.Decode(&manifest); err != nil {
		return ValidatedBackup{}, fmt.Errorf("decode backup manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ValidatedBackup{}, errors.New("backup manifest contains trailing JSON")
		}
		return ValidatedBackup{}, fmt.Errorf("decode backup manifest trailing data: %w", err)
	}
	if manifest.Schema != 1 {
		return ValidatedBackup{}, fmt.Errorf("unsupported backup manifest schema %d", manifest.Schema)
	}
	if manifest.Status != "complete" {
		return ValidatedBackup{}, fmt.Errorf("backup status is %q, not complete", manifest.Status)
	}
	if manifest.CreatedAt.IsZero() || manifest.CompletedAt.IsZero() ||
		manifest.CompletedAt.Before(manifest.CreatedAt) {
		return ValidatedBackup{}, errors.New("backup manifest timestamps are invalid")
	}
	if strings.TrimSpace(manifest.MCU) == "" {
		return ValidatedBackup{}, errors.New("backup manifest MCU is empty")
	}
	if len(manifest.Errors) != 0 {
		return ValidatedBackup{}, errors.New("complete backup manifest contains errors")
	}
	if manifest.ApplicationPackedTimestamp != "" {
		if !currentIdentitySchema(manifest.ApplicationIdentitySchema) {
			return ValidatedBackup{}, errors.New("packed firmware timestamp requires compact identity schema 3")
		}
		packed, parseErr := strconv.ParseUint(manifest.ApplicationPackedTimestamp, 16, 32)
		if parseErr != nil || len(manifest.ApplicationPackedTimestamp) != 8 {
			return ValidatedBackup{}, errors.New("packed firmware timestamp must be eight hexadecimal digits")
		}
		decoded, decodeErr := DecodeFirmwareTimestamp(uint32(packed))
		if decodeErr != nil {
			return ValidatedBackup{}, decodeErr
		}
		if manifest.ApplicationTimestamp != decoded.Compact {
			return ValidatedBackup{}, fmt.Errorf(
				"firmware timestamp display %q differs from packed value %q",
				manifest.ApplicationTimestamp, decoded.Compact,
			)
		}
	}
	directory, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return ValidatedBackup{}, err
	}
	operationRoot := filepath.Dir(directory)
	backupRoot := operationRoot
	if filepath.Base(operationRoot) == "operations" {
		backupRoot = filepath.Dir(operationRoot)
	}
	result := ValidatedBackup{
		Manifest: manifest, ManifestPath: manifestPath,
		ManifestSHA256: sha256Hex(content), Directory: directory,
		Files: make(map[string]ValidatedBackupFile),
	}
	seenNames := make(map[string]bool)
	for _, entry := range manifest.Files {
		if entry.Kind != "flash" && entry.Kind != "eeprom" && entry.Kind != "metadata" {
			return ValidatedBackup{}, fmt.Errorf("unknown backup artifact kind %q", entry.Kind)
		}
		if _, exists := result.Files[entry.Kind]; exists {
			return ValidatedBackup{}, fmt.Errorf("duplicate backup artifact kind %q", entry.Kind)
		}
		if strings.TrimSpace(entry.Name) == "" || seenNames[entry.Name] {
			return ValidatedBackup{}, fmt.Errorf("invalid or duplicate backup artifact name %q", entry.Name)
		}
		seenNames[entry.Name] = true
		expectedHash, hashErr := normalizeRequiredSHA256(entry.SHA256)
		if hashErr != nil {
			return ValidatedBackup{}, fmt.Errorf("artifact %q SHA-256: %w", entry.Name, hashErr)
		}
		if entry.Bytes <= 0 {
			return ValidatedBackup{}, fmt.Errorf("artifact %q is empty", entry.Name)
		}
		relative := entry.RelativePath
		if relative == "" {
			relative = entry.Name
		}
		if filepath.IsAbs(relative) {
			return ValidatedBackup{}, fmt.Errorf("artifact %q uses an absolute path", entry.Name)
		}
		resolved, resolveErr := filepath.Abs(filepath.Join(directory, filepath.FromSlash(relative)))
		if resolveErr != nil {
			return ValidatedBackup{}, resolveErr
		}
		containmentRoot := directory
		if entry.Storage == "content-addressed" {
			containmentRoot = filepath.Join(backupRoot, "firmware", "sha256")
			if !strings.Contains(entry.Name, expectedHash) {
				return ValidatedBackup{}, fmt.Errorf(
					"content-addressed artifact name %q does not include SHA-256", entry.Name,
				)
			}
		} else if entry.Storage != "" && entry.Storage != "operation" {
			return ValidatedBackup{}, fmt.Errorf("artifact %q has unknown storage %q", entry.Name, entry.Storage)
		}
		if !pathWithin(containmentRoot, resolved) {
			return ValidatedBackup{}, fmt.Errorf("artifact %q escapes backup storage root", entry.Name)
		}
		info, statErr := os.Lstat(resolved)
		if statErr != nil {
			return ValidatedBackup{}, fmt.Errorf("inspect artifact %q: %w", entry.Name, statErr)
		}
		if !info.Mode().IsRegular() || info.Size() != entry.Bytes {
			return ValidatedBackup{}, fmt.Errorf("artifact %q type or size mismatch", entry.Name)
		}
		actual, fileErr := backupFile(resolved, entry.Kind)
		if fileErr != nil {
			return ValidatedBackup{}, fmt.Errorf("hash artifact %q: %w", entry.Name, fileErr)
		}
		if actual.SHA256 != expectedHash {
			return ValidatedBackup{}, fmt.Errorf("artifact %q SHA-256 mismatch", entry.Name)
		}
		if entry.Kind == "flash" || entry.Kind == "eeprom" {
			document, parseErr := LoadIntelHex(resolved)
			if parseErr != nil {
				return ValidatedBackup{}, fmt.Errorf("artifact %q: %w", entry.Name, parseErr)
			}
			limit := ATmega328PFlashSize
			if entry.Kind == "eeprom" {
				limit = PCControllerEEPROMBytes
			}
			for address := range document.Image.data {
				if address >= limit {
					return ValidatedBackup{}, fmt.Errorf(
						"artifact %q address 0x%X exceeds %s size",
						entry.Name, address, entry.Kind,
					)
				}
			}
		}
		result.Files[entry.Kind] = ValidatedBackupFile{BackupFile: entry, Path: resolved}
	}
	for _, required := range []string{"flash", "eeprom", "metadata"} {
		if _, exists := result.Files[required]; !exists {
			return ValidatedBackup{}, fmt.Errorf("complete backup lacks %s artifact", required)
		}
	}
	if !manifest.MetadataAvailable {
		return ValidatedBackup{}, errors.New("complete backup marks programmer metadata unavailable")
	}
	flash := result.Files["flash"]
	if manifest.Reference == "" || !strings.Contains(manifest.Reference, flash.SHA256) {
		return ValidatedBackup{}, errors.New("backup reference does not include flash SHA-256")
	}
	return result, nil
}

// DecodeBackupEEPROM verifies the complete backup first and then decodes its
// offline EEPROM artifact. It is intentionally distinct from live board RPC.
func DecodeBackupEEPROM(manifestPath string) (OfflineEEPROMDecode, error) {
	backup, err := ValidateBackupManifest(manifestPath)
	if err != nil {
		return OfflineEEPROMDecode{}, err
	}
	return DecodeOfflineEEPROMHex(backup.Files["eeprom"].Path)
}

type RestoreComponent string

const (
	RestoreFlash  RestoreComponent = "flash"
	RestoreEEPROM RestoreComponent = "eeprom"
)

type RestorePlanOptions struct {
	Method      Method
	Port        string
	Programmer  string
	MCU         string
	Components  []RestoreComponent
	Avrdude     string
	AvrdudeConf string
	ArduinoCLI  string
}

type RestoreStep struct {
	Component RestoreComponent `json:"component"`
	Path      string           `json:"path"`
	SHA256    string           `json:"sha256"`
	Options   Options          `json:"-"`
}

type RestorePlan struct {
	ManifestPath   string        `json:"manifest_path"`
	ManifestSHA256 string        `json:"manifest_sha256"`
	Reference      string        `json:"reference"`
	Method         Method        `json:"method"`
	Steps          []RestoreStep `json:"steps"`
}

// PlanSafeRestore validates every artifact at planning time. Execution remains
// an explicit caller decision, so the TUI/CLI can show the exact hashes first.
func PlanSafeRestore(manifestPath string, options RestorePlanOptions) (RestorePlan, error) {
	backup, err := ValidateBackupManifest(manifestPath)
	if err != nil {
		return RestorePlan{}, err
	}
	if options.Method == "" {
		options.Method = MethodUrclock
	}
	if options.MCU == "" {
		options.MCU = "atmega328p"
	}
	if !strings.EqualFold(options.MCU, backup.Manifest.MCU) {
		return RestorePlan{}, fmt.Errorf(
			"restore MCU %q differs from backup MCU %q", options.MCU, backup.Manifest.MCU,
		)
	}
	switch options.Method {
	case MethodUrclock:
		if strings.TrimSpace(options.Port) == "" {
			return RestorePlan{}, errors.New("Urclock restore requires a serial port")
		}
	case MethodUSBasp:
		// Explicit MethodUSBasp selection chooses the advanced recovery path.
	default:
		return RestorePlan{}, fmt.Errorf("safe restore supports Urclock or USBasp, got %q", options.Method)
	}
	components := options.Components
	if len(components) == 0 {
		components = []RestoreComponent{RestoreFlash, RestoreEEPROM}
	}
	seen := make(map[RestoreComponent]bool)
	plan := RestorePlan{
		ManifestPath:   backup.ManifestPath,
		ManifestSHA256: backup.ManifestSHA256,
		Reference:      backup.Manifest.Reference, Method: options.Method,
	}
	for _, component := range components {
		if seen[component] {
			return RestorePlan{}, fmt.Errorf("duplicate restore component %q", component)
		}
		seen[component] = true
		file, exists := backup.Files[string(component)]
		if !exists {
			return RestorePlan{}, fmt.Errorf("backup lacks restore component %q", component)
		}
		stepOptions := Options{
			Method: options.Method, Port: options.Port, Programmer: options.Programmer,
			MCU: options.MCU, Avrdude: options.Avrdude,
			AvrdudeConf: options.AvrdudeConf, ArduinoCLI: options.ArduinoCLI,
			HexPath: file.Path,
		}
		switch component {
		case RestoreFlash:
			stepOptions.Operation = OperationWriteFlash
		case RestoreEEPROM:
			stepOptions.Operation = OperationWriteEEPROM
			stepOptions.ConfirmEEPROMWrite = true
		default:
			return RestorePlan{}, fmt.Errorf("unknown restore component %q", component)
		}
		plan.Steps = append(plan.Steps, RestoreStep{
			Component: component, Path: file.Path, SHA256: file.SHA256,
			Options: stepOptions,
		})
	}
	return plan, nil
}

// VerifyRestorePlan re-hashes files immediately before an executor uses them,
// closing the time-of-check/time-of-use gap for a long-lived confirmation UI.
func VerifyRestorePlan(plan RestorePlan) error {
	manifestContent, err := os.ReadFile(plan.ManifestPath)
	if err != nil {
		return err
	}
	if sha256Hex(manifestContent) != plan.ManifestSHA256 {
		return errors.New("restore manifest changed after planning")
	}
	for _, step := range plan.Steps {
		file, err := backupFile(step.Path, string(step.Component))
		if err != nil {
			return err
		}
		if file.SHA256 != step.SHA256 {
			return fmt.Errorf("restore %s artifact changed after planning", step.Component)
		}
	}
	return nil
}

type CommandRunner interface {
	Run(context.Context, Command, io.Writer) error
}

type CommandRunnerFunc func(context.Context, Command, io.Writer) error

func (function CommandRunnerFunc) Run(
	ctx context.Context,
	command Command,
	output io.Writer,
) error {
	return function(ctx, command, output)
}

func pathWithin(root, candidate string) bool {
	root = filepath.Clean(root)
	candidate = filepath.Clean(candidate)
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
