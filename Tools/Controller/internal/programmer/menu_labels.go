package programmer

import "fmt"

const eepromMenuLabelsInvalidCommit byte = 0

// EEPROMByteWrite is one ordered byte operation in a power-loss-safe EEPROM
// record update. The menu-label plan invalidates first and commits last, so an
// interrupted update can only render the firmware's safe fallback.
type EEPROMByteWrite struct {
	Address uint32
	Value   byte
}

func menuLabelsCRC(labels []byte) byte {
	crc := EEPROMMenuLabelsFormatMarker
	for _, label := range labels {
		crc = avrCRC8Update(crc, label)
	}
	return crc
}

func validMenuLabelByte(value byte) bool {
	return value >= ' ' && value <= '~'
}

func validateMenuLabels(labels []byte) error {
	if len(labels) != int(EEPROMMenuLabelBytes) {
		return fmt.Errorf("menu labels are %d bytes, require %d", len(labels), EEPROMMenuLabelBytes)
	}
	for index, value := range labels {
		if !validMenuLabelByte(value) {
			return fmt.Errorf("menu label byte %d is not printable ASCII", index)
		}
	}
	return nil
}

// menuLabelsWritePlan defines the only safe order for a bytewise update:
// invalidate the old commit, write the payload, publish the versioned CRC
// header, then write the commit marker last. Factory-image generation applies
// the same plan so its final representation is identical. Any future running-
// board updater must execute the returned operations in this exact order.
func menuLabelsWritePlan(labels []byte) ([]EEPROMByteWrite, error) {
	if err := validateMenuLabels(labels); err != nil {
		return nil, err
	}
	crc := menuLabelsCRC(labels)
	plan := make([]EEPROMByteWrite, 0, 1+len(labels)+2)
	plan = append(plan, EEPROMByteWrite{
		Address: EEPROMMenuLabelsCommitAddress,
		Value:   eepromMenuLabelsInvalidCommit,
	})
	for offset, value := range labels {
		plan = append(plan, EEPROMByteWrite{
			Address: EEPROMMenuLabelsAddress + uint32(offset),
			Value:   value,
		})
	}
	plan = append(plan,
		EEPROMByteWrite{Address: EEPROMMenuLabelsCRCAddress, Value: crc},
		EEPROMByteWrite{Address: EEPROMMenuLabelsCommitAddress, Value: EEPROMMenuLabelsFormatMarker},
	)
	return plan, nil
}

func applyMenuLabelsWritePlan(data []byte, labels []byte) error {
	plan, err := menuLabelsWritePlan(labels)
	if err != nil {
		return err
	}
	for _, write := range plan {
		if write.Address >= uint32(len(data)) {
			return fmt.Errorf("menu-label EEPROM write 0x%04X exceeds image size %d", write.Address, len(data))
		}
		data[write.Address] = write.Value
	}
	return nil
}

// validMenuLabelsRecord is deliberately byte-for-byte equivalent to the AVR
// reader: versioned final commit marker, printable payload, then CRC-8/ATM
// seeded with that format marker and applied to all fixed-width label bytes.
func validMenuLabelsRecord(data []byte) bool {
	if uint32(len(data)) < EEPROMMenuLabelsEnd ||
		data[EEPROMMenuLabelsCommitAddress] != EEPROMMenuLabelsFormatMarker {
		return false
	}
	labels := data[EEPROMMenuLabelsAddress:EEPROMMenuLabelsCommitAddress]
	if err := validateMenuLabels(labels); err != nil {
		return false
	}
	return data[EEPROMMenuLabelsCRCAddress] == menuLabelsCRC(labels)
}
