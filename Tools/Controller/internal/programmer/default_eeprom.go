package programmer

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
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
	values[0] = 0   // audible, sensors ordered, motion always allowed
	values[1] = 0   // enclosure illumination off until explicitly enabled
	values[2] = 128 // open illumination brightness
	values[3] = 0   // closed illumination brightness
	values[4] = 5   // open-enclosure TM1637 brightness
	values[5] = 128 // status RGB brightness
	values[6] = 0   // no output restore; all motion/relays/PWM start safe and off
	binary.LittleEndian.PutUint16(values[7:9], 500)
	for index := 9; index < 17; index++ {
		values[index] = 0 // eight safe, off user-PWM defaults
	}
	values[17] = 0 // Status is the deterministic default page
	values[18] = 0 // no last-page save; default colors/decimals
	binary.LittleEndian.PutUint16(values[19:21], DefaultVisibleMenuMask)
	copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
	values[28] = 0 // closed-enclosure TM1637 fully off
	values[29] = 0 // no relays are selected for optional startup restoration
	values[30] = 1 // minimum safe break-before-direction interval in milliseconds
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

	image := &IntelHexImage{data: make(map[uint32]byte, PCControllerEEPROMBytes)}
	for address, value := range data {
		image.data[uint32(address)] = value
	}
	return image.Canonical()
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
