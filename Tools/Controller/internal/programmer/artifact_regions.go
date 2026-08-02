package programmer

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ManifestRegionInspection is a hardware-free verification report for every
// named memory domain declared by a compile manifest.
type ManifestRegionInspection struct {
	ManifestPath   string                     `json:"manifest_path"`
	ManifestSHA256 string                     `json:"manifest_sha256"`
	Format         string                     `json:"format"`
	Target         ManifestInspectionTarget   `json:"target"`
	Regions        []ManifestRegionDescriptor `json:"regions"`
}

// ManifestInspectionTarget is the memory layout used to derive region bounds.
type ManifestInspectionTarget struct {
	MCU                   string `json:"mcu"`
	ApplicationLimitBytes uint32 `json:"application_limit_bytes"`
	FlashBytes            uint32 `json:"flash_bytes"`
	EEPROMBytes           uint32 `json:"eeprom_bytes"`
}

// ManifestRegionDescriptor binds a validated region to the exact artifact and
// hashes both the source container and its dense 0xFF-filled address range.
type ManifestRegionDescriptor struct {
	Name                 string `json:"name"`
	Kind                 string `json:"kind"`
	Memory               string `json:"memory"`
	Start                uint32 `json:"start"`
	Length               uint32 `json:"length"`
	PresentBytes         uint32 `json:"present_bytes"`
	Complete             bool   `json:"complete"`
	ArtifactRole         string `json:"artifact_role"`
	ArtifactPath         string `json:"artifact_path"`
	ArtifactSHA256       string `json:"artifact_sha256"`
	ArtifactImageSHA256  string `json:"artifact_image_sha256"`
	RegionSHA256         string `json:"region_sha256"`
	BoundsValidated      bool   `json:"bounds_validated"`
	ChecksumValidated    bool   `json:"checksum_validated"`
	ManifestHashMatched  bool   `json:"manifest_hash_matched"`
	DeclaredMagicMatched bool   `json:"declared_magic_matched,omitempty"`
}

type validatedManifestArtifact struct {
	declaration compileManifestArtifact
	path        string
	document    *IntelHexDocument
}

// InspectManifestRegions strictly validates a current compile manifest and
// all artifacts beside it before exposing application, bootloader, EEPROM,
// and declared metadata regions. No serial/programmer access occurs.
func InspectManifestRegions(manifestPath string) (ManifestRegionInspection, error) {
	var report ManifestRegionInspection
	manifestPath = filepath.Clean(strings.TrimSpace(manifestPath))
	if manifestPath == "" || manifestPath == "." {
		return report, errors.New("firmware region inspection requires a manifest path")
	}
	absolute, err := filepath.Abs(manifestPath)
	if err != nil {
		return report, fmt.Errorf("resolve firmware manifest: %w", err)
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return report, fmt.Errorf("read firmware manifest: %w", err)
	}
	decoder := json.NewDecoder(bufio.NewReader(bytes.NewReader(content)))
	decoder.DisallowUnknownFields()
	var manifest compileManifest
	if err := decoder.Decode(&manifest); err != nil {
		return report, fmt.Errorf("decode firmware manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return report, errors.New("firmware manifest contains trailing JSON")
		}
		return report, fmt.Errorf("decode firmware manifest trailing data: %w", err)
	}
	if manifest.Format != firmwareManifestFormat {
		return report, fmt.Errorf("unsupported firmware manifest format %q", manifest.Format)
	}
	if manifest.GeneratedUTC.IsZero() {
		return report, errors.New("firmware manifest has no generation timestamp")
	}
	if manifest.Target.ApplicationLimitBytes == 0 || manifest.Target.FlashBytes == 0 ||
		manifest.Target.EEPROMBytes == 0 ||
		manifest.Target.ApplicationLimitBytes >= manifest.Target.FlashBytes {
		return report, errors.New("firmware manifest declares invalid memory bounds")
	}
	if manifest.Target.ApplicationLimitBytes != urbootApplicationCapacity ||
		manifest.Target.FlashBytes != ATmega328PFlashSize ||
		manifest.Target.EEPROMBytes != atmega328PEEPROMCapacity {
		return report, errors.New("firmware manifest memory bounds do not match the current ATmega328P/Urboot target")
	}
	if !strings.EqualFold(strings.TrimSpace(manifest.Target.MCU), "atmega328p") {
		return report, fmt.Errorf("firmware manifest targets unsupported MCU %q", manifest.Target.MCU)
	}
	if _, err := normalizeRequiredSHA256(manifest.Source.SHA256); err != nil {
		return report, fmt.Errorf("firmware manifest source SHA-256: %w", err)
	}
	if manifest.Source.Files <= 0 {
		return report, errors.New("firmware manifest source file count must be positive")
	}
	declaredBuildHash, err := parseManifestHex32(manifest.Source.BuildHash, "build hash")
	if err != nil {
		return report, err
	}
	declaredTimestamp, err := parseManifestHex32(manifest.Source.PackedTimestamp, "packed timestamp")
	if err != nil {
		return report, err
	}

	artifacts, err := validateManifestArtifacts(absolute, manifest)
	if err != nil {
		return report, err
	}
	application, err := exactlyOneManifestArtifact(artifacts, "application")
	if err != nil {
		return report, err
	}
	merged, err := exactlyOneManifestArtifact(artifacts, "flash+bootloader")
	if err != nil {
		return report, err
	}
	eeprom, err := preferredEEPROMManifestArtifact(artifacts)
	if err != nil {
		return report, err
	}

	report = ManifestRegionInspection{
		ManifestPath: absolute, ManifestSHA256: sha256Hex(content), Format: manifest.Format,
		Target: ManifestInspectionTarget{
			MCU:                   manifest.Target.MCU,
			ApplicationLimitBytes: manifest.Target.ApplicationLimitBytes,
			FlashBytes:            manifest.Target.FlashBytes,
			EEPROMBytes:           manifest.Target.EEPROMBytes,
		},
		Regions: make([]ManifestRegionDescriptor, 0, 3+len(manifest.PatchRegions)),
	}
	applicationRegion, err := inspectManifestRegion(
		"application", "application", "flash", 0,
		manifest.Target.ApplicationLimitBytes, application, "",
	)
	if err != nil {
		return ManifestRegionInspection{}, err
	}
	bootloaderRegion, err := inspectManifestRegion(
		"bootloader", "bootloader", "flash", manifest.Target.ApplicationLimitBytes,
		manifest.Target.FlashBytes-manifest.Target.ApplicationLimitBytes, merged, "",
	)
	if err != nil {
		return ManifestRegionInspection{}, err
	}
	eepromRegion, err := inspectManifestRegion(
		"eeprom", "eeprom", "eeprom", 0, manifest.Target.EEPROMBytes, eeprom, "",
	)
	if err != nil {
		return ManifestRegionInspection{}, err
	}
	if applicationRegion.PresentBytes == 0 || bootloaderRegion.PresentBytes == 0 ||
		eepromRegion.PresentBytes == 0 {
		return ManifestRegionInspection{}, errors.New("manifest application, bootloader, and EEPROM regions must each contain data")
	}
	report.Regions = append(report.Regions, applicationRegion, bootloaderRegion, eepromRegion)

	seenMetadata := make(map[string]bool, len(manifest.PatchRegions))
	haveFirmwareIdentity := false
	for index, region := range manifest.PatchRegions {
		region.Name = strings.TrimSpace(region.Name)
		if region.Name == "" || region.Length == 0 {
			return ManifestRegionInspection{}, fmt.Errorf("metadata region %d requires a name and non-zero length", index)
		}
		if seenMetadata[region.Name] {
			return ManifestRegionInspection{}, fmt.Errorf("duplicate metadata region %q", region.Name)
		}
		seenMetadata[region.Name] = true
		if uint64(region.Start)+uint64(region.Length) > uint64(manifest.Target.ApplicationLimitBytes) {
			return ManifestRegionInspection{}, fmt.Errorf("metadata region %q exceeds the declared application region", region.Name)
		}
		if err := validateManifestPatchFields(region); err != nil {
			return ManifestRegionInspection{}, err
		}
		metadata, inspectErr := inspectManifestRegion(
			region.Name, "metadata", "flash", region.Start, region.Length,
			application, region.Magic,
		)
		if inspectErr != nil {
			return ManifestRegionInspection{}, inspectErr
		}
		if !metadata.Complete {
			return ManifestRegionInspection{}, fmt.Errorf("metadata region %q is sparse", region.Name)
		}
		if region.Name == "firmware-identity" {
			haveFirmwareIdentity = true
			if region.Start != FirmwareIdentityAddress || region.Length != FirmwareIdentityLength ||
				region.Schema != FirmwareIdentitySchema || region.Magic != "PCI1" {
				return ManifestRegionInspection{}, errors.New("firmware-identity declaration differs from the current compact identity layout")
			}
			identity, readErr := application.document.Image.BytesAt(region.Start, region.Length)
			if readErr != nil {
				return ManifestRegionInspection{}, readErr
			}
			if binary.LittleEndian.Uint32(identity[4:8]) != declaredBuildHash ||
				binary.LittleEndian.Uint32(identity[8:12]) != declaredTimestamp {
				return ManifestRegionInspection{}, errors.New("firmware-identity bytes differ from manifest source metadata")
			}
		}
		report.Regions = append(report.Regions, metadata)
	}
	if len(manifest.PatchRegions) == 0 {
		return ManifestRegionInspection{}, errors.New("firmware manifest declares no metadata regions")
	}
	if !haveFirmwareIdentity {
		return ManifestRegionInspection{}, errors.New("firmware manifest does not declare the current firmware-identity metadata region")
	}
	return report, nil
}

func parseManifestHex32(value, field string) (uint32, error) {
	value = strings.TrimSpace(value)
	if len(value) != 8 {
		return 0, fmt.Errorf("firmware manifest %s must contain exactly 8 hexadecimal digits", field)
	}
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("firmware manifest %s must be hexadecimal: %w", field, err)
	}
	return uint32(parsed), nil
}

func validateManifestArtifacts(
	manifestPath string,
	manifest compileManifest,
) ([]validatedManifestArtifact, error) {
	if len(manifest.Artifacts) == 0 {
		return nil, errors.New("firmware manifest declares no artifacts")
	}
	result := make([]validatedManifestArtifact, 0, len(manifest.Artifacts))
	seenPath := make(map[string]bool, len(manifest.Artifacts))
	for index, declaration := range manifest.Artifacts {
		declaration.Role = strings.TrimSpace(declaration.Role)
		path, err := resolveManifestArtifactPath(manifestPath, declaration.Path)
		if err != nil {
			return nil, fmt.Errorf("manifest artifact %d: %w", index, err)
		}
		key := strings.ToLower(filepath.Clean(path))
		if seenPath[key] {
			return nil, fmt.Errorf("manifest declares artifact %q more than once", declaration.Path)
		}
		seenPath[key] = true
		document, err := LoadIntelHex(path)
		if err != nil {
			return nil, err
		}
		if err := validateManifestArtifactDeclaration(declaration, document); err != nil {
			return nil, fmt.Errorf("manifest artifact %q: %w", declaration.Path, err)
		}
		result = append(result, validatedManifestArtifact{
			declaration: declaration, path: path, document: document,
		})
	}
	return result, nil
}

func resolveManifestArtifactPath(manifestPath, declared string) (string, error) {
	declared = filepath.Clean(strings.TrimSpace(declared))
	if declared == "" || declared == "." {
		return "", errors.New("artifact path is empty")
	}
	if filepath.IsAbs(declared) {
		return declared, nil
	}
	// Generated artifacts are intentionally portable as one bundle: the
	// manifest is authoritative and every named image lives beside it.
	candidate := filepath.Join(filepath.Dir(manifestPath), filepath.Base(declared))
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
		return candidate, nil
	}
	return "", fmt.Errorf("artifact %q is not a regular file beside its manifest", declared)
}

func validateManifestArtifactDeclaration(
	declaration compileManifestArtifact,
	document *IntelHexDocument,
) error {
	if declaration.Role == "" || declaration.CapacityBytes == 0 {
		return errors.New("role and capacity are required")
	}
	expectedCapacity := uint32(0)
	switch declaration.Role {
	case "application":
		expectedCapacity = urbootApplicationCapacity
	case "flash+bootloader":
		expectedCapacity = ATmega328PFlashSize
	case "eeprom", "default-eeprom":
		expectedCapacity = atmega328PEEPROMCapacity
	default:
		return fmt.Errorf("unknown artifact role %q", declaration.Role)
	}
	if declaration.CapacityBytes != expectedCapacity {
		return fmt.Errorf("capacity %d does not match role capacity %d", declaration.CapacityBytes, expectedCapacity)
	}
	if document.SourceBytes != declaration.ContainerBytes ||
		!strings.EqualFold(document.SourceSHA256, declaration.SHA256) {
		return errors.New("container size or SHA-256 differs from manifest")
	}
	inspection := document.Inspection
	if inspection.DataBytes != declaration.DataBytes ||
		declaration.FreeBytes != declaration.CapacityBytes-declaration.DataBytes {
		return errors.New("data/free byte counts differ from manifest")
	}
	usage := math.Round(float64(inspection.DataBytes)*10_000/float64(declaration.CapacityBytes)) / 100
	if math.Abs(usage-declaration.UsagePercent) > 0.000001 {
		return errors.New("usage percentage differs from manifest")
	}
	if inspection.DataBytes > declaration.CapacityBytes ||
		(inspection.HasData && inspection.MaximumAddress >= declaration.CapacityBytes) {
		return errors.New("artifact data exceeds declared capacity")
	}
	if !matchingManifestRanges(declaration.Ranges, inspection.Segments) ||
		!matchingManifestEndpoints(declaration, inspection) {
		return errors.New("address ranges differ from manifest")
	}
	return nil
}

func matchingManifestRanges(declared []compileManifestRange, actual []IntelHexSegment) bool {
	if len(declared) != len(actual) {
		return false
	}
	for index := range declared {
		if declared[index].Start != actual[index].Start ||
			uint64(declared[index].End)+1 != actual[index].EndExclusive {
			return false
		}
	}
	return true
}

func matchingManifestEndpoints(
	declaration compileManifestArtifact,
	inspection IntelHexInspection,
) bool {
	if !inspection.HasData {
		return declaration.StartAddress == nil && declaration.EndAddress == nil
	}
	return declaration.StartAddress != nil && declaration.EndAddress != nil &&
		*declaration.StartAddress == inspection.MinimumAddress &&
		*declaration.EndAddress == inspection.MaximumAddress
}

func exactlyOneManifestArtifact(
	artifacts []validatedManifestArtifact,
	role string,
) (validatedManifestArtifact, error) {
	var matches []validatedManifestArtifact
	for _, artifact := range artifacts {
		if artifact.declaration.Role == role {
			matches = append(matches, artifact)
		}
	}
	if len(matches) != 1 {
		return validatedManifestArtifact{}, fmt.Errorf("firmware manifest requires exactly one %q artifact, found %d", role, len(matches))
	}
	return matches[0], nil
}

func preferredEEPROMManifestArtifact(
	artifacts []validatedManifestArtifact,
) (validatedManifestArtifact, error) {
	defaults := make([]validatedManifestArtifact, 0, 1)
	fallbacks := make([]validatedManifestArtifact, 0, 1)
	for _, artifact := range artifacts {
		switch artifact.declaration.Role {
		case "default-eeprom":
			defaults = append(defaults, artifact)
		case "eeprom":
			fallbacks = append(fallbacks, artifact)
		}
	}
	if len(defaults) == 1 {
		return defaults[0], nil
	}
	if len(defaults) > 1 {
		return validatedManifestArtifact{}, errors.New("firmware manifest declares multiple default EEPROM artifacts")
	}
	if len(fallbacks) != 1 {
		return validatedManifestArtifact{}, fmt.Errorf("firmware manifest requires one EEPROM artifact, found %d", len(fallbacks))
	}
	return fallbacks[0], nil
}

func inspectManifestRegion(
	name, kind, memory string,
	start, length uint32,
	artifact validatedManifestArtifact,
	magic string,
) (ManifestRegionDescriptor, error) {
	dense := make([]byte, length)
	for index := range dense {
		dense[index] = 0xFF
	}
	var present uint32
	for address, value := range artifact.document.Image.data {
		if address < start || uint64(address) >= uint64(start)+uint64(length) {
			continue
		}
		dense[address-start] = value
		present++
	}
	magicMatched := false
	if magic != "" {
		if uint32(len(magic)) > length {
			return ManifestRegionDescriptor{}, fmt.Errorf("metadata region %q magic exceeds its bounds", name)
		}
		actual, err := artifact.document.Image.BytesAt(start, uint32(len(magic)))
		if err != nil {
			return ManifestRegionDescriptor{}, fmt.Errorf("metadata region %q magic: %w", name, err)
		}
		magicMatched = string(actual) == magic
		if !magicMatched {
			return ManifestRegionDescriptor{}, fmt.Errorf("metadata region %q magic is %q, require %q", name, string(actual), magic)
		}
	}
	digest := sha256.Sum256(dense)
	return ManifestRegionDescriptor{
		Name: name, Kind: kind, Memory: memory, Start: start, Length: length,
		PresentBytes: present, Complete: present == length,
		ArtifactRole: artifact.declaration.Role, ArtifactPath: artifact.path,
		ArtifactSHA256:      artifact.document.SourceSHA256,
		ArtifactImageSHA256: artifact.document.Inspection.CanonicalSHA256,
		RegionSHA256:        hex.EncodeToString(digest[:]), BoundsValidated: true,
		ChecksumValidated: true, ManifestHashMatched: true,
		DeclaredMagicMatched: magicMatched,
	}, nil
}

func validateManifestPatchFields(region compileManifestPatchRegion) error {
	seen := make(map[string]bool, len(region.Fields))
	fields := append([]compileManifestPatchField(nil), region.Fields...)
	sort.Slice(fields, func(left, right int) bool { return fields[left].Offset < fields[right].Offset })
	var previousEnd uint64
	for index, field := range fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" || field.Length == 0 || strings.TrimSpace(field.Encoding) == "" {
			return fmt.Errorf("metadata region %q field %d is incomplete", region.Name, index)
		}
		if seen[field.Name] {
			return fmt.Errorf("metadata region %q repeats field %q", region.Name, field.Name)
		}
		seen[field.Name] = true
		end := uint64(field.Offset) + uint64(field.Length)
		if end > uint64(region.Length) {
			return fmt.Errorf("metadata region %q field %q exceeds its bounds", region.Name, field.Name)
		}
		if index != 0 && uint64(field.Offset) < previousEnd {
			return fmt.Errorf("metadata region %q fields overlap at %q", region.Name, field.Name)
		}
		previousEnd = end
	}
	return nil
}
