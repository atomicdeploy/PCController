package programmer

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	firmwareManifestName      = "firmware-manifest.json"
	urbootApplicationCapacity = uint32(32_384)
	atmega328PEEPROMCapacity  = uint32(1_024)
	firmwareManifestFormat    = "pccontroller-avr-firmware-manifest/v1"
)

type compileManifest struct {
	Format       string                     `json:"format"`
	GeneratedUTC time.Time                  `json:"generatedUtc"`
	Target       compileManifestTarget      `json:"target"`
	Source       compileManifestSource      `json:"source"`
	StackBudget  compileManifestStackBudget `json:"stackBudget"`
	Artifacts    []compileManifestArtifact  `json:"artifacts"`
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
	for _, path := range paths {
		artifact, inspectErr := inspectCompileArtifact(path, identity.SourceRoot)
		if inspectErr != nil {
			return "", inspectErr
		}
		if artifact.Role == "application" {
			haveApplication = true
		}
		artifacts = append(artifacts, artifact)
	}
	if !haveApplication {
		return "", errors.New("successful Arduino compile produced no application HEX")
	}

	buildTimestamp := ""
	if decoded, decodeErr := DecodeFirmwareTimestampSchema2(identity.PackedTimestamp); decodeErr == nil {
		buildTimestamp = decoded.Compact
	}
	manifest := compileManifest{
		Format: firmwareManifestFormat, GeneratedUTC: time.Now().UTC(),
		Target: compileManifestTarget{
			FQBN: options.FQBN, MCU: "atmega328p", ClockHz: 16_000_000,
			Bootloader: "UART0 Urboot/urclock", Baud: 115200,
			ApplicationLimitBytes: urbootApplicationCapacity,
			FlashBytes:            ATmega328PFlashSize, EEPROMBytes: atmega328PEEPROMCapacity,
		},
		Source: compileManifestSource{
			SHA256: identity.SourceSHA256, Files: identity.SourceFiles,
			BuildHash:       fmt.Sprintf("%08X", identity.SourceHash),
			PackedTimestamp: fmt.Sprintf("%08X", identity.PackedTimestamp),
			BuildTimestamp:  buildTimestamp,
		},
		StackBudget: stackBudget,
		Artifacts:   artifacts,
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

func inspectCompileArtifact(path, sourceRoot string) (compileManifestArtifact, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return compileManifestArtifact{}, err
	}
	name := strings.ToLower(filepath.Base(path))
	role := "application"
	capacity := urbootApplicationCapacity
	switch {
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
		DataBytes: inspection.DataBytes, Ranges: ranges,
		StartAddress: startAddress, EndAddress: endAddress,
	}, nil
}
