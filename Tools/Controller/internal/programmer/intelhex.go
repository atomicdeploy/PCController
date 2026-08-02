package programmer

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	ATmega328PFlashSize                  uint32 = 32 * 1024
	ATmega328PConservativeBootloaderBase uint32 = 30 * 1024
)

// IntelHexImage is a validated, sparse Intel HEX address space. Callers can
// inspect bytes, but mutation is deliberately confined to guarded patch plans.
type IntelHexImage struct {
	data         map[uint32]byte
	startSegment *uint32
	startLinear  *uint32
}

type IntelHexSegment struct {
	Start        uint32 `json:"start"`
	EndExclusive uint64 `json:"end_exclusive"`
	Bytes        uint32 `json:"bytes"`
}

type IntelHexInspection struct {
	HasData         bool              `json:"has_data"`
	DataBytes       uint32            `json:"data_bytes"`
	MinimumAddress  uint32            `json:"minimum_address,omitempty"`
	MaximumAddress  uint32            `json:"maximum_address,omitempty"`
	Segments        []IntelHexSegment `json:"segments"`
	CanonicalSHA256 string            `json:"canonical_sha256"`
}

type IntelHexDocument struct {
	Path         string
	SourceBytes  int64
	SourceSHA256 string
	Records      uint32
	Image        *IntelHexImage
	Inspection   IntelHexInspection
}

// ParseIntelHex validates checksums, record shapes, address arithmetic, EOF,
// and overlapping data. It supports all standard Intel HEX address/start
// records while rejecting ambiguous duplicate bytes.
func ParseIntelHex(input io.Reader) (*IntelHexImage, error) {
	if input == nil {
		return nil, errors.New("parse Intel HEX: nil input")
	}
	image := &IntelHexImage{data: make(map[uint32]byte)}
	scanner := bufio.NewScanner(input)
	// Intel records are normally short, but permit a generous validated line.
	scanner.Buffer(make([]byte, 1024), 64*1024)
	var base uint64
	eofSeen := false
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if eofSeen {
			return nil, fmt.Errorf("Intel HEX line %d: data after EOF", lineNumber)
		}
		if !strings.HasPrefix(line, ":") {
			return nil, fmt.Errorf("Intel HEX line %d: missing ':'", lineNumber)
		}
		raw, err := hex.DecodeString(line[1:])
		if err != nil {
			return nil, fmt.Errorf("Intel HEX line %d: decode: %w", lineNumber, err)
		}
		if len(raw) < 5 {
			return nil, fmt.Errorf("Intel HEX line %d: record is too short", lineNumber)
		}
		count := int(raw[0])
		if len(raw) != count+5 {
			return nil, fmt.Errorf(
				"Intel HEX line %d: byte count %d does not match record length %d",
				lineNumber, count, len(raw)-5,
			)
		}
		var checksum byte
		for _, value := range raw {
			checksum += value
		}
		if checksum != 0 {
			return nil, fmt.Errorf("Intel HEX line %d: checksum mismatch", lineNumber)
		}
		address := uint16(raw[1])<<8 | uint16(raw[2])
		recordType := raw[3]
		data := raw[4 : 4+count]
		switch recordType {
		case 0x00:
			absolute := base + uint64(address)
			if absolute+uint64(len(data)) > uint64(^uint32(0))+1 {
				return nil, fmt.Errorf("Intel HEX line %d: address overflow", lineNumber)
			}
			for index, value := range data {
				location := uint32(absolute + uint64(index))
				if _, exists := image.data[location]; exists {
					return nil, fmt.Errorf(
						"Intel HEX line %d: overlapping data at 0x%08X",
						lineNumber, location,
					)
				}
				image.data[location] = value
			}
		case 0x01:
			if count != 0 || address != 0 {
				return nil, fmt.Errorf("Intel HEX line %d: malformed EOF", lineNumber)
			}
			eofSeen = true
		case 0x02:
			if count != 2 || address != 0 {
				return nil, fmt.Errorf(
					"Intel HEX line %d: malformed extended segment address",
					lineNumber,
				)
			}
			base = uint64(uint16(data[0])<<8|uint16(data[1])) << 4
		case 0x03:
			if count != 4 || address != 0 {
				return nil, fmt.Errorf("Intel HEX line %d: malformed start segment", lineNumber)
			}
			value := uint32(data[0])<<24 | uint32(data[1])<<16 |
				uint32(data[2])<<8 | uint32(data[3])
			if image.startSegment != nil && *image.startSegment != value {
				return nil, fmt.Errorf("Intel HEX line %d: conflicting start segment", lineNumber)
			}
			image.startSegment = uint32Pointer(value)
		case 0x04:
			if count != 2 || address != 0 {
				return nil, fmt.Errorf(
					"Intel HEX line %d: malformed extended linear address",
					lineNumber,
				)
			}
			base = uint64(uint16(data[0])<<8|uint16(data[1])) << 16
		case 0x05:
			if count != 4 || address != 0 {
				return nil, fmt.Errorf("Intel HEX line %d: malformed start linear", lineNumber)
			}
			value := uint32(data[0])<<24 | uint32(data[1])<<16 |
				uint32(data[2])<<8 | uint32(data[3])
			if image.startLinear != nil && *image.startLinear != value {
				return nil, fmt.Errorf("Intel HEX line %d: conflicting start linear", lineNumber)
			}
			image.startLinear = uint32Pointer(value)
		default:
			return nil, fmt.Errorf(
				"Intel HEX line %d: unsupported record type 0x%02X",
				lineNumber, recordType,
			)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Intel HEX: %w", err)
	}
	if !eofSeen {
		return nil, errors.New("Intel HEX: missing EOF record")
	}
	return image, nil
}

func LoadIntelHex(path string) (*IntelHexDocument, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Intel HEX %q: %w", path, err)
	}
	image, err := ParseIntelHex(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("parse Intel HEX %q: %w", path, err)
	}
	inspection, err := image.Inspect()
	if err != nil {
		return nil, err
	}
	return &IntelHexDocument{
		Path:         path,
		SourceBytes:  int64(len(content)),
		SourceSHA256: sha256Hex(content),
		Records:      countIntelHexRecords(content),
		Image:        image,
		Inspection:   inspection,
	}, nil
}

func countIntelHexRecords(content []byte) uint32 {
	var records uint32
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) != 0 {
			records++
		}
	}
	return records
}

func (image *IntelHexImage) Byte(address uint32) (byte, bool) {
	if image == nil {
		return 0, false
	}
	value, ok := image.data[address]
	return value, ok
}

// BytesAt requires every byte in the requested interval to be present. This
// strict behavior prevents sparse HEX holes from becoming implicit 0x00/0xFF
// values during settings decoding or patch verification.
func (image *IntelHexImage) BytesAt(start, length uint32) ([]byte, error) {
	if image == nil {
		return nil, errors.New("Intel HEX image is nil")
	}
	if uint64(start)+uint64(length) > uint64(^uint32(0))+1 {
		return nil, errors.New("Intel HEX byte interval overflows address space")
	}
	result := make([]byte, length)
	for offset := uint32(0); offset < length; offset++ {
		value, ok := image.data[start+offset]
		if !ok {
			return nil, fmt.Errorf("Intel HEX has no byte at 0x%08X", start+offset)
		}
		result[offset] = value
	}
	return result, nil
}

func (image *IntelHexImage) Inspect() (IntelHexInspection, error) {
	canonical, err := image.Canonical()
	if err != nil {
		return IntelHexInspection{}, err
	}
	addresses := image.sortedAddresses()
	inspection := IntelHexInspection{
		DataBytes:       uint32(len(addresses)),
		Segments:        []IntelHexSegment{},
		CanonicalSHA256: sha256Hex(canonical),
	}
	if len(addresses) == 0 {
		return inspection, nil
	}
	inspection.HasData = true
	inspection.MinimumAddress = addresses[0]
	inspection.MaximumAddress = addresses[len(addresses)-1]
	start := addresses[0]
	previous := start
	for _, address := range addresses[1:] {
		if address != previous+1 {
			inspection.Segments = append(inspection.Segments, IntelHexSegment{
				Start: start, EndExclusive: uint64(previous) + 1, Bytes: previous - start + 1,
			})
			start = address
		}
		previous = address
	}
	inspection.Segments = append(inspection.Segments, IntelHexSegment{
		Start: start, EndExclusive: uint64(previous) + 1, Bytes: previous - start + 1,
	})
	return inspection, nil
}

// Canonical writes sorted 16-byte data records, standard type-04 addressing,
// uppercase hexadecimal, LF endings, retained start records, and one EOF.
func (image *IntelHexImage) Canonical() ([]byte, error) {
	if image == nil {
		return nil, errors.New("canonical Intel HEX: nil image")
	}
	addresses := image.sortedAddresses()
	var output bytes.Buffer
	var currentHigh uint32
	haveHigh := false
	for index := 0; index < len(addresses); {
		start := addresses[index]
		high := start >> 16
		if high != 0 && (!haveHigh || high != currentHigh) {
			writeIntelHexRecord(&output, 0, 0x04, []byte{byte(high >> 8), byte(high)})
			currentHigh = high
			haveHigh = true
		}
		data := make([]byte, 0, 16)
		for index < len(addresses) && len(data) < 16 {
			address := addresses[index]
			if address>>16 != high || address != start+uint32(len(data)) {
				break
			}
			data = append(data, image.data[address])
			index++
		}
		writeIntelHexRecord(&output, uint16(start), 0x00, data)
	}
	if image.startSegment != nil {
		value := *image.startSegment
		writeIntelHexRecord(&output, 0, 0x03, []byte{
			byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value),
		})
	}
	if image.startLinear != nil {
		value := *image.startLinear
		writeIntelHexRecord(&output, 0, 0x05, []byte{
			byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value),
		})
	}
	writeIntelHexRecord(&output, 0, 0x01, nil)
	return output.Bytes(), nil
}

type FlashLayout struct {
	FlashSize      uint32
	BootloaderBase uint32
}

func DefaultATmega328PFlashLayout() FlashLayout {
	return FlashLayout{
		FlashSize: ATmega328PFlashSize,
		// Reserve the largest common ATmega328P boot section by default.
		// A caller may provide a fuse-derived, smaller boundary explicitly.
		BootloaderBase: ATmega328PConservativeBootloaderBase,
	}
}

type NamedPatchRegion struct {
	Name   string
	Start  uint32
	Length uint32
}

type RegionPatch struct {
	Region      string
	Offset      uint32
	ExpectedOld []byte
	Replacement []byte
}

type IntelHexPatchPlan struct {
	// SourceSHA256 is mandatory and hashes the exact source file, not merely
	// its decoded address space. This prevents silently patching a rebuilt HEX.
	SourceSHA256        string
	Regions             []NamedPatchRegion
	Patches             []RegionPatch
	BootloaderWhitelist []string
}

type AppliedRegionPatch struct {
	Region  string `json:"region"`
	Address uint32 `json:"address"`
	Length  uint32 `json:"length"`
	OldHex  string `json:"old_hex"`
	NewHex  string `json:"new_hex"`
}

type IntelHexPatchResult struct {
	SourcePath        string               `json:"source_path"`
	OutputPath        string               `json:"output_path"`
	SourceSHA256      string               `json:"source_sha256"`
	OutputSHA256      string               `json:"output_sha256"`
	BeforeImageSHA256 string               `json:"before_image_sha256"`
	AfterImageSHA256  string               `json:"after_image_sha256"`
	Patches           []AppliedRegionPatch `json:"patches"`
}

// ApplyIntelHexPatchPlan validates the complete plan before writing a new
// canonical HEX artifact. Existing output files are never overwritten; this
// makes the same-directory temporary-file rename an atomic create on Windows
// and Unix and preserves every prior flash artifact.
func ApplyIntelHexPatchPlan(
	sourcePath, outputPath string,
	layout FlashLayout,
	plan IntelHexPatchPlan,
) (IntelHexPatchResult, error) {
	var result IntelHexPatchResult
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(outputPath) == "" {
		return result, errors.New("Intel HEX patch requires source and output paths")
	}
	sourceAbsolute, err := filepath.Abs(sourcePath)
	if err != nil {
		return result, fmt.Errorf("resolve source path: %w", err)
	}
	outputAbsolute, err := filepath.Abs(outputPath)
	if err != nil {
		return result, fmt.Errorf("resolve output path: %w", err)
	}
	if strings.EqualFold(filepath.Clean(sourceAbsolute), filepath.Clean(outputAbsolute)) {
		return result, errors.New("Intel HEX patch output must differ from source")
	}
	if _, err := os.Stat(outputAbsolute); err == nil {
		return result, fmt.Errorf("Intel HEX patch output already exists: %s", outputAbsolute)
	} else if !errors.Is(err, os.ErrNotExist) {
		return result, fmt.Errorf("inspect Intel HEX patch output: %w", err)
	}
	if layout.FlashSize == 0 || layout.BootloaderBase > layout.FlashSize {
		return result, errors.New("invalid flash layout")
	}
	if len(plan.Patches) == 0 {
		return result, errors.New("Intel HEX patch plan contains no patches")
	}
	expectedSourceHash, err := normalizeRequiredSHA256(plan.SourceSHA256)
	if err != nil {
		return result, fmt.Errorf("patch source SHA-256: %w", err)
	}
	document, err := LoadIntelHex(sourceAbsolute)
	if err != nil {
		return result, err
	}
	if document.SourceSHA256 != expectedSourceHash {
		return result, fmt.Errorf(
			"patch source SHA-256 mismatch: got %s, require %s",
			document.SourceSHA256, expectedSourceHash,
		)
	}
	for address := range document.Image.data {
		if address >= layout.FlashSize {
			return result, fmt.Errorf(
				"Intel HEX address 0x%08X is outside flash size 0x%X",
				address, layout.FlashSize,
			)
		}
	}
	regions, err := validateNamedRegions(layout, plan.Regions)
	if err != nil {
		return result, err
	}
	whitelist := make(map[string]bool, len(plan.BootloaderWhitelist))
	for _, name := range plan.BootloaderWhitelist {
		name = strings.TrimSpace(name)
		if _, exists := regions[name]; !exists {
			return result, fmt.Errorf("bootloader whitelist names unknown region %q", name)
		}
		whitelist[name] = true
	}
	clone := document.Image.clone()
	claimed := make(map[uint32]string)
	applied := make([]AppliedRegionPatch, 0, len(plan.Patches))
	for index, patch := range plan.Patches {
		patch.Region = strings.TrimSpace(patch.Region)
		region, ok := regions[patch.Region]
		if !ok {
			return result, fmt.Errorf("patch %d names unknown region %q", index, patch.Region)
		}
		if len(patch.ExpectedOld) == 0 {
			return result, fmt.Errorf("patch %d has an empty byte sequence", index)
		}
		if len(patch.ExpectedOld) != len(patch.Replacement) {
			return result, fmt.Errorf(
				"patch %d must preserve size: expected-old=%d replacement=%d",
				index, len(patch.ExpectedOld), len(patch.Replacement),
			)
		}
		end := uint64(patch.Offset) + uint64(len(patch.ExpectedOld))
		if end > uint64(region.Length) {
			return result, fmt.Errorf("patch %d exceeds region %q", index, region.Name)
		}
		address := region.Start + patch.Offset
		if uint64(address)+uint64(len(patch.ExpectedOld)) > uint64(layout.FlashSize) {
			return result, fmt.Errorf("patch %d exceeds flash", index)
		}
		if layout.BootloaderBase < layout.FlashSize &&
			uint64(address)+uint64(len(patch.ExpectedOld)) > uint64(layout.BootloaderBase) &&
			!whitelist[region.Name] {
			return result, fmt.Errorf(
				"patch %d enters bootloader range at 0x%X; explicitly whitelist region %q",
				index, layout.BootloaderBase, region.Name,
			)
		}
		actual, readErr := clone.BytesAt(address, uint32(len(patch.ExpectedOld)))
		if readErr != nil {
			return result, fmt.Errorf("patch %d expected-old read: %w", index, readErr)
		}
		if !bytes.Equal(actual, patch.ExpectedOld) {
			return result, fmt.Errorf(
				"patch %d expected-old mismatch at 0x%X: got %X require %X",
				index, address, actual, patch.ExpectedOld,
			)
		}
		for offset := range patch.Replacement {
			location := address + uint32(offset)
			if owner, exists := claimed[location]; exists {
				return result, fmt.Errorf(
					"patch %d overlaps patch region %q at 0x%X",
					index, owner, location,
				)
			}
			claimed[location] = region.Name
			clone.data[location] = patch.Replacement[offset]
		}
		applied = append(applied, AppliedRegionPatch{
			Region: region.Name, Address: address,
			Length: uint32(len(patch.Replacement)),
			OldHex: strings.ToUpper(hex.EncodeToString(actual)),
			NewHex: strings.ToUpper(hex.EncodeToString(patch.Replacement)),
		})
	}
	canonical, err := clone.Canonical()
	if err != nil {
		return result, err
	}
	if err := atomicCreateFile(outputAbsolute, canonical, 0o600); err != nil {
		return result, fmt.Errorf("write patched Intel HEX: %w", err)
	}
	return IntelHexPatchResult{
		SourcePath: sourceAbsolute, OutputPath: outputAbsolute,
		SourceSHA256: document.SourceSHA256, OutputSHA256: sha256Hex(canonical),
		BeforeImageSHA256: document.Inspection.CanonicalSHA256,
		AfterImageSHA256:  sha256Hex(canonical),
		Patches:           applied,
	}, nil
}

func validateNamedRegions(
	layout FlashLayout,
	input []NamedPatchRegion,
) (map[string]NamedPatchRegion, error) {
	regions := make(map[string]NamedPatchRegion, len(input))
	for index, region := range input {
		region.Name = strings.TrimSpace(region.Name)
		if region.Name == "" || region.Length == 0 {
			return nil, fmt.Errorf("patch region %d requires a name and non-zero length", index)
		}
		if _, exists := regions[region.Name]; exists {
			return nil, fmt.Errorf("duplicate patch region %q", region.Name)
		}
		if uint64(region.Start)+uint64(region.Length) > uint64(layout.FlashSize) {
			return nil, fmt.Errorf("patch region %q exceeds flash", region.Name)
		}
		for _, existing := range regions {
			regionEnd := uint64(region.Start) + uint64(region.Length)
			existingEnd := uint64(existing.Start) + uint64(existing.Length)
			if uint64(region.Start) < existingEnd && uint64(existing.Start) < regionEnd {
				return nil, fmt.Errorf(
					"patch regions %q and %q overlap", region.Name, existing.Name,
				)
			}
		}
		regions[region.Name] = region
	}
	return regions, nil
}

func (image *IntelHexImage) clone() *IntelHexImage {
	clone := &IntelHexImage{data: make(map[uint32]byte, len(image.data))}
	for address, value := range image.data {
		clone.data[address] = value
	}
	if image.startSegment != nil {
		clone.startSegment = uint32Pointer(*image.startSegment)
	}
	if image.startLinear != nil {
		clone.startLinear = uint32Pointer(*image.startLinear)
	}
	return clone
}

func (image *IntelHexImage) sortedAddresses() []uint32 {
	addresses := make([]uint32, 0, len(image.data))
	for address := range image.data {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(left, right int) bool {
		return addresses[left] < addresses[right]
	})
	return addresses
}

func writeIntelHexRecord(output *bytes.Buffer, address uint16, recordType byte, data []byte) {
	record := make([]byte, 0, len(data)+5)
	record = append(record, byte(len(data)), byte(address>>8), byte(address), recordType)
	record = append(record, data...)
	var sum byte
	for _, value := range record {
		sum += value
	}
	record = append(record, byte(-sum))
	output.WriteByte(':')
	output.WriteString(strings.ToUpper(hex.EncodeToString(record)))
	output.WriteByte('\n')
}

func atomicCreateFile(path string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".pccontroller-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// A hard-link publish is atomic and refuses an existing destination on both
	// Windows and Unix; os.Rename could replace a file on Unix after our check.
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func normalizeRequiredSHA256(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != sha256.Size*2 {
		return "", errors.New("must contain exactly 64 hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", errors.New("must be hexadecimal")
	}
	return value, nil
}

func sha256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func uint32Pointer(value uint32) *uint32 { return &value }
