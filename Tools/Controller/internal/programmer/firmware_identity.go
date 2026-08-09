package programmer

import (
	"encoding/binary"
	"fmt"
)

const (
	// FirmwareIdentityAddress occupies the final record below the selected
	// stock-core application ceiling. A different generated target policy can
	// therefore move both the linker section and guarded patch region together.
	FirmwareIdentityAddress uint32 = urbootApplicationCapacity - FirmwareIdentityLength
	FirmwareIdentityLength  uint32 = 12
	FirmwareIdentityMagic   uint32 = 0x31494350
	FirmwareIdentitySchema  uint8  = 1
)

// FirmwareIdentity describes the immutable build identity embedded in flash.
type FirmwareIdentity struct {
	Address         uint32 `json:"address"`
	Length          uint32 `json:"length"`
	Schema          uint8  `json:"schema"`
	Magic           string `json:"magic"`
	SourceHash      uint32 `json:"source_hash"`
	SourceHashHex   string `json:"source_hash_hex"`
	PackedTimestamp uint32 `json:"packed_timestamp"`
	TimestampHex    string `json:"packed_timestamp_hex"`
	BuildTimestamp  string `json:"build_timestamp,omitempty"`
}

// InspectFirmwareIdentity loads and validates the declared identity record.
func InspectFirmwareIdentity(path string) (FirmwareIdentity, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return FirmwareIdentity{}, err
	}
	return inspectFirmwareIdentityImage(document.Image)
}

func inspectFirmwareIdentityImage(image *IntelHexImage) (FirmwareIdentity, error) {
	bytes, err := image.BytesAt(FirmwareIdentityAddress, FirmwareIdentityLength)
	if err != nil {
		return FirmwareIdentity{}, fmt.Errorf("read firmware identity: %w", err)
	}
	magic := binary.LittleEndian.Uint32(bytes[0:4])
	if magic != FirmwareIdentityMagic {
		return FirmwareIdentity{}, fmt.Errorf(
			"firmware identity magic at 0x%X is 0x%08X, require 0x%08X",
			FirmwareIdentityAddress, magic, FirmwareIdentityMagic,
		)
	}
	result := FirmwareIdentity{
		Address: FirmwareIdentityAddress, Length: FirmwareIdentityLength,
		Schema: FirmwareIdentitySchema, Magic: "PCI1",
		SourceHash:      binary.LittleEndian.Uint32(bytes[4:8]),
		PackedTimestamp: binary.LittleEndian.Uint32(bytes[8:12]),
	}
	result.SourceHashHex = fmt.Sprintf("%08X", result.SourceHash)
	result.TimestampHex = fmt.Sprintf("%08X", result.PackedTimestamp)
	if decoded, decodeErr := DecodeFirmwareTimestamp(result.PackedTimestamp); decodeErr == nil {
		result.BuildTimestamp = decoded.Compact
	}
	return result, nil
}

// PatchFirmwareIdentity creates a guarded, non-overwriting HEX artifact with
// a replacement source hash and packed build timestamp.
func PatchFirmwareIdentity(
	sourcePath, outputPath, sourceSHA256 string,
	sourceHash, packedTimestamp uint32,
) (IntelHexPatchResult, error) {
	document, err := LoadIntelHex(sourcePath)
	if err != nil {
		return IntelHexPatchResult{}, err
	}
	current, err := document.Image.BytesAt(FirmwareIdentityAddress, FirmwareIdentityLength)
	if err != nil {
		return IntelHexPatchResult{}, fmt.Errorf("read firmware identity for patch: %w", err)
	}
	if binary.LittleEndian.Uint32(current[0:4]) != FirmwareIdentityMagic {
		return IntelHexPatchResult{}, fmt.Errorf("firmware identity patch rejected: PCI1 magic is absent")
	}
	replacement := append([]byte(nil), current...)
	binary.LittleEndian.PutUint32(replacement[4:8], sourceHash)
	binary.LittleEndian.PutUint32(replacement[8:12], packedTimestamp)
	return ApplyIntelHexPatchPlan(
		sourcePath, outputPath,
		FlashLayout{FlashSize: ATmega328PFlashSize, BootloaderBase: urbootApplicationCapacity},
		IntelHexPatchPlan{
			SourceSHA256: sourceSHA256,
			Regions: []NamedPatchRegion{{
				Name: "firmware-identity", Start: FirmwareIdentityAddress,
				Length: FirmwareIdentityLength,
			}},
			Patches: []RegionPatch{{
				Region: "firmware-identity", ExpectedOld: current,
				Replacement: replacement,
			}},
		},
	)
}
