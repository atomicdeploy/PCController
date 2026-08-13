package programmer

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

const (
	firmwareManifestName      = "firmware-manifest.json"
	urbootApplicationCapacity = generatedBoardApplicationBytes
	atmega328PEEPROMCapacity  = generatedBoardEEPROMBytes
	firmwareManifestFormat    = "pccontroller-avr-firmware-manifest/v1"
)

type compileManifest struct {
	Format       string                       `json:"format"`
	GeneratedUTC time.Time                    `json:"generatedUtc"`
	Target       compileManifestTarget        `json:"target"`
	Source       compileManifestSource        `json:"source"`
	EEPROMLayout compileManifestEEPROMLayout  `json:"eepromLayout"`
	StackBudget  compileManifestStackBudget   `json:"stackBudget"`
	PatchRegions []compileManifestPatchRegion `json:"patchRegions"`
	Artifacts    []compileManifestArtifact    `json:"artifacts"`
}

type compileManifestEEPROMLayout struct {
	Schema                 byte   `json:"schema"`
	SettingsAddress        uint32 `json:"settingsAddress"`
	SettingsStagingAddress uint32 `json:"settingsStagingAddress"`
	SettingsBankBytes      uint32 `json:"settingsBankBytes"`
	SettingsBankCount      byte   `json:"settingsBankCount"`
	ControllerBytes        uint32 `json:"controllerBytes"`
	BoardNameBytes         uint32 `json:"boardNameBytes"`
	RecordBytes            uint32 `json:"recordBytes"`
	Generation             string `json:"generation"`
	TemperatureRoleAddress uint32 `json:"temperatureRoleAddress"`
	TemperatureRoleBytes   uint32 `json:"temperatureRoleBytes"`
	AudioCueAddress         uint32 `json:"audioCueAddress"`
	AudioCueBytes           uint32 `json:"audioCueBytes"`
	Checksum               string `json:"checksum"`
}

type compileManifestPatchRegion struct {
	Name   string                      `json:"name"`
	Start  uint32                      `json:"start"`
	Length uint32                      `json:"length"`
	Schema uint8                       `json:"schema"`
	Magic  string                      `json:"magic"`
	Fields []compileManifestPatchField `json:"fields"`
}

type compileManifestPatchField struct {
	Name     string `json:"name"`
	Offset   uint32 `json:"offset"`
	Length   uint32 `json:"length"`
	Encoding string `json:"encoding"`
}

type compileManifestTarget struct {
	FQBN                  string `json:"fqbn"`
	MCU                   string `json:"mcu"`
	ClockHz               uint32 `json:"clockHz"`
	Bootloader            string `json:"bootloader"`
	Baud                  int    `json:"baud"`
	ApplicationLimitBytes uint32 `json:"applicationLimitBytes"`
	FlashBytes            uint32 `json:"flashBytes"`
	EEPROMBytes           uint32 `json:"eepromBytes"`
}

type compileManifestSource struct {
	SHA256          string `json:"sha256"`
	Files           int    `json:"files"`
	BuildHash       string `json:"buildHash"`
	PackedTimestamp string `json:"packedTimestamp"`
	BuildTimestamp  string `json:"buildTimestamp,omitempty"`
}

type compileManifestRange struct {
	Start uint32 `json:"start"`
	End   uint32 `json:"end"`
}

type compileManifestArtifact struct {
	Path           string                 `json:"path"`
	Role           string                 `json:"role"`
	ContainerBytes int64                  `json:"containerBytes"`
	SHA256         string                 `json:"sha256"`
	CapacityBytes  uint32                 `json:"capacityBytes"`
	FreeBytes      uint32                 `json:"freeBytes"`
	UsagePercent   float64                `json:"usagePercent"`
	Records        uint32                 `json:"records"`
	DataBytes      uint32                 `json:"dataBytes"`
	Ranges         []compileManifestRange `json:"ranges"`
	StartAddress   *uint32                `json:"startAddress"`
	EndAddress     *uint32                `json:"endAddress"`
}

// clearCompileManifest prevents a failed compile from leaving an earlier
// success manifest beside partially replaced Arduino artifacts.
func clearCompileManifest(outputDir string) error {
	path := filepath.Join(outputDir, firmwareManifestName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale firmware manifest: %w", err)
	}
	return nil
}

// writeCompileManifest validates every generated Intel HEX artifact and only
// then atomically publishes metadata derived from the bytes now on disk.
func writeCompileManifest(
	options Options,
	identity CompileIdentity,
	stackBudget compileManifestStackBudget,
) (string, error) {
	if _, err := stageDefaultEEPROMCompileArtifact(identity.OutputDir); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(identity.OutputDir)
	if err != nil {
		return "", fmt.Errorf("read firmware output directory: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if extension == ".hex" || extension == ".eep" {
			paths = append(paths, filepath.Join(identity.OutputDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return "", errors.New("successful Arduino compile produced no Intel HEX artifacts")
	}

	artifacts := make([]compileManifestArtifact, 0, len(paths))
	haveApplication := false
	applicationPath := ""
	for _, path := range paths {
		artifact, inspectErr := inspectCompileArtifact(path, identity.SourceRoot)
		if inspectErr != nil {
			return "", inspectErr
		}
		if artifact.Role == "application" {
			haveApplication = true
			applicationPath = path
		}
		artifacts = append(artifacts, artifact)
	}
	if !haveApplication {
		return "", errors.New("successful Arduino compile produced no application HEX")
	}
	identityBytes, err := readFirmwareIdentityBytes(applicationPath)
	if err != nil {
		return "", err
	}
	if got := binary.LittleEndian.Uint32(identityBytes[0:4]); got != FirmwareIdentityMagic {
		return "", fmt.Errorf("firmware identity magic at 0x%X is 0x%08X, require 0x%08X", FirmwareIdentityAddress, got, FirmwareIdentityMagic)
	}
	if got := binary.LittleEndian.Uint32(identityBytes[4:8]); got != identity.SourceHash {
		return "", fmt.Errorf("firmware identity source hash is 0x%08X, require 0x%08X", got, identity.SourceHash)
	}
	if got := binary.LittleEndian.Uint32(identityBytes[8:12]); got != identity.PackedTimestamp {
		return "", fmt.Errorf("firmware identity timestamp is 0x%08X, require 0x%08X", got, identity.PackedTimestamp)
	}

	buildTimestamp := ""
	if decoded, decodeErr := DecodeFirmwareTimestamp(identity.PackedTimestamp); decodeErr == nil {
		buildTimestamp = decoded.Compact
	}
	manifest := compileManifest{
		Format: firmwareManifestFormat, GeneratedUTC: time.Now().UTC(),
		Target: compileManifestTarget{
			FQBN: options.FQBN, MCU: generatedBoardMCU, ClockHz: generatedBoardClockHz,
			Bootloader: generatedBoardBootloader, Baud: generatedBoardBaud,
			ApplicationLimitBytes: urbootApplicationCapacity,
			FlashBytes:            ATmega328PFlashSize, EEPROMBytes: atmega328PEEPROMCapacity,
		},
		Source: compileManifestSource{
			SHA256: identity.SourceSHA256, Files: identity.SourceFiles,
			BuildHash:       fmt.Sprintf("%08X", identity.SourceHash),
			PackedTimestamp: fmt.Sprintf("%08X", identity.PackedTimestamp),
			BuildTimestamp:  buildTimestamp,
		},
		EEPROMLayout: compileManifestEEPROMLayout{
			Schema: EEPROMSettingsRecordSchema, SettingsAddress: EEPROMSettingsAddress,
			SettingsStagingAddress: EEPROMSettingsStagingAddress,
			SettingsBankBytes:      EEPROMSettingsBankBytes, SettingsBankCount: EEPROMSettingsBankCount,
			ControllerBytes:        EEPROMSettingsControllerBytes,
			BoardNameBytes:         1 + native.MaximumBoardNameLength,
			RecordBytes:            EEPROMSettingsRecordBytes,
			Generation:             "board-name metadata high nibble, modulo 16; delta 1..7 is newer",
			TemperatureRoleAddress: EEPROMTemperatureRoleAddress,
			TemperatureRoleBytes:   EEPROMTemperatureRoleBytes,
			AudioCueAddress:         EEPROMAudioCueAddress,
			AudioCueBytes:           EEPROMAudioCueRecordBytes,
			Checksum:               "CRC-8/ATM (poly 0x07)",
		},
		StackBudget:  stackBudget,
		PatchRegions: firmwareIdentityManifestRegions(),
		Artifacts:    artifacts,
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode firmware manifest: %w", err)
	}
	encoded = append(encoded, '\n')
	path := filepath.Join(identity.OutputDir, firmwareManifestName)
	if err := writeFileAtomicReplace(path, encoded, 0o644); err != nil {
		return "", fmt.Errorf("publish firmware manifest: %w", err)
	}
	return path, nil
}

func readFirmwareIdentityBytes(path string) ([]byte, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return nil, err
	}
	value, err := document.Image.BytesAt(FirmwareIdentityAddress, FirmwareIdentityLength)
	if err != nil {
		return nil, fmt.Errorf("read declared firmware identity region: %w", err)
	}
	return value, nil
}

func firmwareIdentityManifestRegions() []compileManifestPatchRegion {
	return []compileManifestPatchRegion{{
		Name: "firmware-identity", Start: FirmwareIdentityAddress,
		Length: FirmwareIdentityLength, Schema: FirmwareIdentitySchema,
		Magic: "PCI1",
		Fields: []compileManifestPatchField{
			{Name: "magic", Offset: 0, Length: 4, Encoding: "ascii-little-endian"},
			{Name: "source_hash", Offset: 4, Length: 4, Encoding: "uint32-little-endian"},
			{Name: "packed_timestamp", Offset: 8, Length: 4, Encoding: "uint32-little-endian"},
		},
	}}
}

func inspectCompileArtifact(path, sourceRoot string) (compileManifestArtifact, error) {
	name := strings.ToLower(filepath.Base(path))
	document, err := LoadIntelHex(path)
	if err != nil {
		return compileManifestArtifact{}, err
	}
	role := "application"
	capacity := urbootApplicationCapacity
	switch {
	case name == defaultEEPROMCompileArtifact:
		role, capacity = "default-eeprom", atmega328PEEPROMCapacity
	case strings.HasSuffix(name, ".eep"):
		role, capacity = "eeprom", atmega328PEEPROMCapacity
	case strings.Contains(name, "with_bootloader"):
		role, capacity = "flash+bootloader", ATmega328PFlashSize
	}
	inspection := document.Inspection
	if inspection.DataBytes > capacity ||
		(inspection.HasData && inspection.MaximumAddress >= capacity) {
		return compileManifestArtifact{}, fmt.Errorf(
			"firmware artifact %s exceeds %s capacity %d bytes",
			filepath.Base(path), role, capacity,
		)
	}
	displayPath, err := filepath.Rel(sourceRoot, path)
	if err != nil || strings.HasPrefix(displayPath, "..") {
		displayPath = path
	} else {
		displayPath = filepath.ToSlash(displayPath)
	}
	ranges := make([]compileManifestRange, 0, len(inspection.Segments))
	for _, segment := range inspection.Segments {
		ranges = append(ranges, compileManifestRange{
			Start: segment.Start, End: uint32(segment.EndExclusive - 1),
		})
	}
	var startAddress, endAddress *uint32
	if inspection.HasData {
		start, end := inspection.MinimumAddress, inspection.MaximumAddress
		startAddress, endAddress = &start, &end
	}
	usage := math.Round(float64(inspection.DataBytes)*10_000/float64(capacity)) / 100
	return compileManifestArtifact{
		Path: displayPath, Role: role, ContainerBytes: document.SourceBytes,
		SHA256: document.SourceSHA256, CapacityBytes: capacity,
		FreeBytes: capacity - inspection.DataBytes, UsagePercent: usage,
		Records: document.Records, DataBytes: inspection.DataBytes, Ranges: ranges,
		StartAddress: startAddress, EndAddress: endAddress,
	}, nil
}
