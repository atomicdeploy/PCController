package programmer

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"pccontroller.local/controller/internal/native"
)

const (
	DefaultPersistentMenuPageCount = 14
	DefaultVisibleMenuMask         = uint16(1<<DefaultPersistentMenuPageCount) - 1
	defaultEEPROMCompileArtifact   = "safe-default-eeprom.hex"
)

// GenerateDefaultEEPROMIntelHex returns a complete, immediately usable
// ATmega328P EEPROM image. Its deployment baseline keeps heat-producing and
// user-facing outputs off while initializing every current settings field.
func GenerateDefaultEEPROMIntelHex() ([]byte, error) {
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	settings := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
	values := settings[:EEPROMSettingsValueBytes]
	factory := native.DefaultSettings()
	values[0] = factory.Flags
	values[1] = factory.LightMode
	values[2] = factory.OnBrightness
	values[3] = factory.OffBrightness
	values[4] = factory.DisplayBrightness
	values[5] = factory.StatusBrightness
	values[6] = factory.OutputPersistence
	binary.LittleEndian.PutUint16(values[7:9], factory.StreamPeriodMS)
	for index := 9; index < 17; index++ {
		values[index] = 0 // eight safe, off user-PWM defaults
	}
	values[17] = factory.DefaultPage
	values[18] = factory.ExtendedFlags
	binary.LittleEndian.PutUint16(values[19:21], DefaultVisibleMenuMask)
	copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
	values[28] = factory.DisplayClosedBrightness |
		(factory.MotionExitHoldSeconds << 3)
	values[29] = factory.RelayRestoreMask
	values[30] = factory.MotionBreakMSValue
	for index := 31; index < len(values); index++ {
		values[index] = 0 // valid empty board name plus deterministic padding
	}
	settings[EEPROMSettingsValueBytes] = avrCRC8(values)

	// A new board starts with a valid empty 20-record learned-RF store.
	header := data[EEPROMRemoteHeaderAddress : EEPROMRemoteHeaderAddress+4]
	binary.LittleEndian.PutUint16(header[0:2], 0x4C52)
	header[2] = EEPROMRemoteRecordSize
	header[3] = EEPROMRemoteCapacity
	for id := byte(0); id < EEPROMRemoteCapacity; id++ {
		start := EEPROMRemoteEntriesAddress + uint32(id)*EEPROMRemoteRecordBytes
		record := data[start : start+EEPROMRemoteRecordBytes]
		for index := range record {
			record[index] = 0
		}
		// Eleven zero value bytes have CRC-8 zero, so the twelfth remains zero.
	}

	// The rich LED palette and effects are owned by Go and installed in the
	// same factory image as settings/remotes. AVR only retains a tiny fallback.
	profiles := native.DefaultStatusProfiles(native.FactoryStatusBrightness)
	for condition, profile := range profiles {
		descriptor, err := native.StatusProfileSetPayload(byte(condition), profile)
		if err != nil {
			return nil, fmt.Errorf("encode factory status profile %d: %w", condition, err)
		}
		start := EEPROMStatusProfileAddress + uint32(condition)*EEPROMStatusProfileRecordBytes
		record := data[start : start+EEPROMStatusProfileRecordBytes]
		copy(record, descriptor[1:])
		record[EEPROMStatusProfileBytes] = avrCRC8(record[:EEPROMStatusProfileBytes])
	}

	image := &IntelHexImage{data: make(map[uint32]byte, PCControllerEEPROMBytes)}
	for address, value := range data {
		image.data[uint32(address)] = value
	}
	return image.Canonical()
}

// WriteDefaultEEPROMIntelHex publishes a user-requested factory image without
// overwriting an existing backup/artifact path.
func WriteDefaultEEPROMIntelHex(outputPath string) error {
	content, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create factory EEPROM image: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(outputPath)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write factory EEPROM image: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync factory EEPROM image: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close factory EEPROM image: %w", err)
	}
	remove = false
	return nil
}

// stageDefaultEEPROMCompileArtifact publishes the canonical deployment EEPROM
// image beside the application and bootloader outputs.
func stageDefaultEEPROMCompileArtifact(outputDir string) (string, error) {
	content, err := GenerateDefaultEEPROMIntelHex()
	if err != nil {
		return "", fmt.Errorf("generate safe default EEPROM: %w", err)
	}
	path := filepath.Join(outputDir, defaultEEPROMCompileArtifact)
	if err := writeFileAtomicReplace(path, content, 0o644); err != nil {
		return "", fmt.Errorf("publish safe default EEPROM: %w", err)
	}
	return path, nil
}
