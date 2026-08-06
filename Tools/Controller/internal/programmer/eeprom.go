package programmer

import (
	"encoding/binary"
	"fmt"
	"strings"

	"pccontroller.local/controller/internal/native"
)

const (
	PCControllerEEPROMBytes        uint32 = generatedBoardEEPROMBytes
	EEPROMSettingsAddress          uint32 = 32
	EEPROMSettingsValueBytes       uint32 = 40
	EEPROMSettingsRecordBytes      uint32 = EEPROMSettingsValueBytes + 1
	EEPROMRemoteHeaderAddress      uint32 = 80
	EEPROMRemoteEntriesAddress     uint32 = 84
	EEPROMRemoteRecordSize         byte   = 12
	EEPROMRemoteCapacity           byte   = 20
	EEPROMRemoteRecordBytes        uint32 = 12
	EEPROMResetJournalAddress      uint32 = 336
	EEPROMResetJournalSlots        byte   = 64
	EEPROMResetJournalRecordSize   uint32 = 6
	EEPROMStatusProfileAddress     uint32 = EEPROMResetJournalAddress + uint32(EEPROMResetJournalSlots)*EEPROMResetJournalRecordSize
	EEPROMStatusProfileCount       byte   = 19
	EEPROMStatusProfileBytes       uint32 = 12
	EEPROMStatusProfileRecordBytes uint32 = EEPROMStatusProfileBytes + 1
)

type OfflineEEPROMDecode struct {
	SourceKind   string                    `json:"source_kind"`
	SourcePath   string                    `json:"source_path"`
	SourceSHA256 string                    `json:"source_sha256"`
	Layout       string                    `json:"layout"`
	Settings     OfflineSettingsDecode     `json:"settings"`
	Remotes      OfflineRemoteStoreDecode  `json:"remotes"`
	ResetJournal OfflineResetJournalDecode `json:"reset_journal"`
}

// ControllerSettings is the current semantic MCU settings/name layout. Alpha
// versions replace this profile directly instead of carrying migrations.
type ControllerSettings struct {
	Flags                     byte    `json:"flags"`
	Silent                    bool    `json:"silent"`
	ProgrammingMode           bool    `json:"programming_mode"`
	SwapTemperatureSensors    bool    `json:"swap_temperature_sensors"`
	MotionDoorPolicy          byte    `json:"motion_door_policy"`
	DoorAudioEnabled          bool    `json:"door_audio_enabled"`
	RelayAudioEnabled         bool    `json:"relay_audio_enabled"`
	MotionBreakMS             uint16  `json:"motion_break_ms"`
	IlluminationMode          byte    `json:"illumination_mode"`
	IlluminationOnBrightness  byte    `json:"illumination_on_brightness"`
	IlluminationOffBrightness byte    `json:"illumination_off_brightness"`
	DisplayBrightness         byte    `json:"display_brightness"`
	StatusBrightness          byte    `json:"status_brightness"`
	OutputPersistence         byte    `json:"output_persistence"`
	StreamPeriodMS            uint16  `json:"stream_period_ms"`
	UserPWM                   [8]byte `json:"user_pwm"`
	DefaultMenuPage           byte    `json:"default_menu_page"`
	MenuFlags                 byte    `json:"menu_flags"`
	SaveLastMenuPage          bool    `json:"save_last_menu_page"`
	StatusColor               byte    `json:"status_color"`
	VoltageDecimals           byte    `json:"voltage_decimals"`
	CurrentDecimals           byte    `json:"current_decimals"`
	VisibleMenuMask           uint16  `json:"visible_menu_mask"`
	MenuOrder                 [7]byte `json:"menu_order_packed"`
	DisplayClosedBrightness   byte    `json:"display_closed_brightness"`
	MotionExitHoldSeconds     byte    `json:"motion_exit_hold_seconds"`
	RelayRestoreMask          byte    `json:"relay_restore_mask"`
	BoardName                 string  `json:"board_name"`
}

type OfflineSettingsDecode struct {
	Present          bool               `json:"present"`
	Supported        bool               `json:"supported"`
	Valid            bool               `json:"valid"`
	Format           string             `json:"format"`
	ValueBytes       uint32             `json:"value_bytes"`
	Issue            string             `json:"issue,omitempty"`
	StoredChecksum   byte               `json:"stored_checksum"`
	ComputedChecksum byte               `json:"computed_checksum"`
	Values           ControllerSettings `json:"values"`
}

type OfflineRemoteRecord struct {
	ID               byte   `json:"id"`
	Present          bool   `json:"present"`
	Occupied         bool   `json:"occupied"`
	Valid            bool   `json:"valid"`
	Issue            string `json:"issue,omitempty"`
	Code             uint32 `json:"code"`
	Bits             byte   `json:"bits"`
	Protocol         byte   `json:"protocol"`
	PulseMicros      uint16 `json:"pulse_micros"`
	ActionKind       byte   `json:"action_kind"`
	ActionValue      byte   `json:"action_value"`
	Behavior         byte   `json:"behavior"`
	StoredChecksum   byte   `json:"stored_checksum"`
	ComputedChecksum byte   `json:"computed_checksum"`
}

type OfflineRemoteStoreDecode struct {
	Present      bool                  `json:"present"`
	Valid        bool                  `json:"valid"`
	Issue        string                `json:"issue,omitempty"`
	Magic        uint16                `json:"magic"`
	RecordBytes  byte                  `json:"record_bytes"`
	Capacity     byte                  `json:"capacity"`
	ValidCount   byte                  `json:"valid_count"`
	InvalidCount byte                  `json:"invalid_count"`
	Slots        []OfflineRemoteRecord `json:"slots"`
}

type OfflineResetJournalRecord struct {
	Slot             byte   `json:"slot"`
	Present          bool   `json:"present"`
	Occupied         bool   `json:"occupied"`
	Valid            bool   `json:"valid"`
	Count            uint32 `json:"count"`
	StoredChecksum   byte   `json:"stored_checksum"`
	ComputedChecksum byte   `json:"computed_checksum"`
	Marker           byte   `json:"marker"`
}

type OfflineResetJournalDecode struct {
	Present        bool                        `json:"present"`
	Complete       bool                        `json:"complete"`
	Valid          bool                        `json:"valid"`
	ValidRecords   byte                        `json:"valid_records"`
	InvalidRecords byte                        `json:"invalid_records"`
	NewestSlot     *byte                       `json:"newest_slot,omitempty"`
	NewestCount    *uint32                     `json:"newest_count,omitempty"`
	Slots          []OfflineResetJournalRecord `json:"slots"`
}

// DecodeOfflineEEPROMHex explicitly decodes a file snapshot. It never queries
// or mutates a live board, which keeps forensic restore data separate from the
// native protocol's current settings response.
func DecodeOfflineEEPROMHex(path string) (OfflineEEPROMDecode, error) {
	document, err := LoadIntelHex(path)
	if err != nil {
		return OfflineEEPROMDecode{}, err
	}
	for address := range document.Image.data {
		if address >= PCControllerEEPROMBytes {
			return OfflineEEPROMDecode{}, fmt.Errorf(
				"EEPROM HEX address 0x%X exceeds ATmega328P EEPROM", address,
			)
		}
	}
	decoded := OfflineEEPROMDecode{
		SourceKind: "offline-eeprom-hex",
		SourcePath: path, SourceSHA256: document.SourceSHA256,
		Layout: "settings-name-unversioned-40/rf-record12-cap20/reset-journal-336",
	}
	decoded.Settings = decodeOfflineSettings(document.Image)
	decoded.Remotes = decodeOfflineRemotes(document.Image)
	decoded.ResetJournal = decodeOfflineResetJournal(document.Image)
	return decoded, nil
}

func decodeOfflineSettings(image *IntelHexImage) OfflineSettingsDecode {
	record, err := image.BytesAt(
		EEPROMSettingsAddress,
		EEPROMSettingsRecordBytes,
	)
	if err != nil {
		_, present := image.data[EEPROMSettingsAddress]
		return OfflineSettingsDecode{
			Present: present,
			Format:  "current/unversioned-40+crc8",
			Issue: fmt.Sprintf(
				"unsupported settings layout: require 40 value bytes plus CRC-8 at EEPROM 0x%04X..0x%04X: %v",
				EEPROMSettingsAddress,
				EEPROMSettingsAddress+EEPROMSettingsRecordBytes-1,
				err,
			),
		}
	}
	return decodeOfflineSettingsRecord(record)
}

func decodeOfflineSettingsRecord(record []byte) OfflineSettingsDecode {
	result := OfflineSettingsDecode{
		Present:          true,
		Supported:        true,
		Format:           "current/unversioned-40+crc8",
		ValueBytes:       EEPROMSettingsValueBytes,
		StoredChecksum:   record[len(record)-1],
		ComputedChecksum: avrCRC8(record[:len(record)-1]),
	}
	settings := record[:len(record)-1]
	values := ControllerSettings{
		Flags:                     settings[0],
		IlluminationMode:          settings[1],
		IlluminationOnBrightness:  settings[2],
		IlluminationOffBrightness: settings[3],
		DisplayBrightness:         settings[4], StatusBrightness: settings[5],
		OutputPersistence: settings[6],
		StreamPeriodMS:    binary.LittleEndian.Uint16(settings[7:9]),
		DefaultMenuPage:   settings[17], MenuFlags: settings[18],
	}
	copy(values.UserPWM[:], settings[9:17])
	values.Silent = values.Flags&0x01 != 0
	values.ProgrammingMode = values.Flags&0x02 != 0
	values.SwapTemperatureSensors = values.Flags&0x04 != 0
	values.MotionDoorPolicy = (values.Flags >> 3) & 0x03
	values.DoorAudioEnabled = values.Flags&0x20 == 0
	values.RelayAudioEnabled = values.Flags&0x40 == 0
	values.MotionBreakMS = uint16(settings[30])
	values.SaveLastMenuPage = values.MenuFlags&0x01 != 0
	values.StatusColor = (values.MenuFlags >> 1) & 0x07
	values.VoltageDecimals = decodeDecimalBits((values.MenuFlags >> 4) & 0x03)
	values.CurrentDecimals = decodeDecimalBits((values.MenuFlags >> 6) & 0x03)
	values.VisibleMenuMask = binary.LittleEndian.Uint16(settings[19:21])
	copy(values.MenuOrder[:], settings[21:28])
	displayOptions := settings[28]
	values.DisplayClosedBrightness = displayOptions & 0x07
	values.MotionExitHoldSeconds = displayOptions >> 3
	if values.MotionExitHoldSeconds == 0 {
		values.MotionExitHoldSeconds = 2
	}
	values.RelayRestoreMask = settings[29]
	nameLength := settings[31]
	if nameLength <= native.MaximumBoardNameLength {
		values.BoardName = string(settings[32 : 32+nameLength])
	}
	result.Values = values

	var issues []string
	if result.StoredChecksum != result.ComputedChecksum {
		issues = append(issues, "CRC-8 mismatch")
	}
	if values.IlluminationMode > 2 {
		issues = append(issues, "illumination mode exceeds 2")
	}
	if values.DisplayBrightness > 7 {
		issues = append(issues, "display brightness exceeds 7")
	}
	if values.OutputPersistence&^0x0F != 0 {
		issues = append(issues, "output persistence flags exceed 0x0F")
	}
	if values.DefaultMenuPage >= 14 {
		issues = append(issues, "default menu page exceeds 13")
	}
	if values.StreamPeriodMS != 0 && values.StreamPeriodMS < 100 {
		issues = append(issues, "non-zero stream period is below 100 ms")
	}
	if values.MotionBreakMS == 0 {
		issues = append(issues, "motion break is outside 1..255 ms")
	}
	if nameLength > native.MaximumBoardNameLength {
		issues = append(issues, "board name exceeds 8 bytes")
	} else if err := native.ValidateBoardName(values.BoardName); err != nil {
		issues = append(issues, err.Error())
	}
	issues = append(issues, validateOfflineMenuLayout(values)...)
	result.Valid = len(issues) == 0
	result.Issue = strings.Join(issues, "; ")
	return result
}

func validateOfflineMenuLayout(values ControllerSettings) []string {
	const allPages uint16 = 0x3FFF
	var issues []string
	if values.VisibleMenuMask == 0 || values.VisibleMenuMask&^allPages != 0 {
		issues = append(issues, "visible menu mask is empty or exceeds pages 0..13")
	} else if values.VisibleMenuMask&(uint16(1)<<values.DefaultMenuPage) == 0 {
		issues = append(issues, "default menu page is hidden")
	}
	var seen uint16
	for rank := byte(0); rank < 14; rank++ {
		packed := values.MenuOrder[rank>>1]
		page := packed & 0x0F
		if rank&1 != 0 {
			page = packed >> 4
		}
		if page >= 14 {
			issues = append(issues, fmt.Sprintf("menu-order rank %d has invalid page %d", rank, page))
			continue
		}
		bit := uint16(1) << page
		if seen&bit != 0 {
			issues = append(issues, fmt.Sprintf("menu-order page %d is duplicated", page))
			continue
		}
		seen |= bit
	}
	if seen != allPages {
		issues = append(issues, "menu order is not a permutation of pages 0..13")
	}
	return issues
}

func decodeOfflineRemotes(image *IntelHexImage) OfflineRemoteStoreDecode {
	result := OfflineRemoteStoreDecode{
		Slots: make([]OfflineRemoteRecord, 0, EEPROMRemoteCapacity),
	}
	header, err := image.BytesAt(EEPROMRemoteHeaderAddress, 4)
	if err != nil {
		result.Issue = err.Error()
	} else {
		result.Present = true
		result.Magic = binary.LittleEndian.Uint16(header[0:2])
		result.RecordBytes = header[2]
		result.Capacity = header[3]
	}
	var storeIssues []string
	presentSlots := byte(0)
	if result.Present {
		if result.Magic != 0x4C52 {
			storeIssues = append(storeIssues, fmt.Sprintf("magic=0x%04X, require 0x4C52", result.Magic))
		}
		if result.RecordBytes != EEPROMRemoteRecordSize {
			storeIssues = append(storeIssues, fmt.Sprintf(
				"record_bytes=%d, require %d",
				result.RecordBytes,
				EEPROMRemoteRecordSize,
			))
		}
		if result.Capacity != EEPROMRemoteCapacity {
			storeIssues = append(storeIssues, fmt.Sprintf("capacity=%d, require %d", result.Capacity, EEPROMRemoteCapacity))
		}
	}
	for id := byte(0); id < EEPROMRemoteCapacity; id++ {
		address := EEPROMRemoteEntriesAddress + uint32(id)*EEPROMRemoteRecordBytes
		record, readErr := image.BytesAt(address, EEPROMRemoteRecordBytes)
		slot := OfflineRemoteRecord{ID: id}
		if readErr != nil {
			slot.Issue = readErr.Error()
			result.Slots = append(result.Slots, slot)
			continue
		}
		slot.Present = true
		presentSlots++
		slot.Code = binary.LittleEndian.Uint32(record[0:4])
		slot.Bits = record[4]
		slot.Protocol = record[5]
		slot.PulseMicros = binary.LittleEndian.Uint16(record[6:8])
		slot.ActionKind = record[8]
		slot.ActionValue = record[9]
		slot.Behavior = record[10]
		slot.StoredChecksum = record[11]
		slot.ComputedChecksum = avrCRC8(record[:11])
		slot.Occupied = slot.Code != 0 || slot.Bits != 0 || slot.Protocol != 0 ||
			slot.PulseMicros != 0 || slot.ActionKind != 0 || slot.ActionValue != 0 ||
			slot.Behavior != 0
		if slot.Occupied {
			var issues []string
			if slot.Code == 0 {
				issues = append(issues, "RF code is zero")
			}
			if slot.Bits == 0 || slot.Bits > 32 {
				issues = append(issues, "bit count is outside 1..32")
			}
			if slot.Protocol == 0 {
				issues = append(issues, "protocol is zero")
			}
			if slot.ActionKind > 5 {
				issues = append(issues, "action kind exceeds 5")
			}
			valueLimits := [...]byte{0, 4, 4, 8, 2, 11}
			if slot.ActionKind > 0 && slot.ActionKind <= 5 &&
				slot.ActionValue >= valueLimits[slot.ActionKind] {
				issues = append(issues, "action value is outside the action kind's range")
			}
			if slot.Behavior > 5 {
				issues = append(issues, "behavior exceeds 5")
			}
			if slot.StoredChecksum != slot.ComputedChecksum {
				issues = append(issues, "CRC-8 mismatch")
			}
			slot.Valid = len(issues) == 0
			slot.Issue = strings.Join(issues, "; ")
			if slot.Valid {
				result.ValidCount++
			} else {
				result.InvalidCount++
			}
		}
		result.Slots = append(result.Slots, slot)
	}
	if presentSlots != EEPROMRemoteCapacity {
		storeIssues = append(storeIssues, fmt.Sprintf(
			"only %d of %d RF slots are present", presentSlots, EEPROMRemoteCapacity,
		))
	}
	result.Valid = result.Present && len(storeIssues) == 0 && result.InvalidCount == 0
	if !result.Present && result.Issue == "" {
		result.Issue = "RF store header is absent"
	} else if len(storeIssues) != 0 {
		result.Issue = strings.Join(storeIssues, "; ")
	}
	return result
}

func decodeOfflineResetJournal(image *IntelHexImage) OfflineResetJournalDecode {
	result := OfflineResetJournalDecode{
		Slots: make([]OfflineResetJournalRecord, 0, EEPROMResetJournalSlots),
	}
	var newest uint32
	presentSlots := byte(0)
	for slotID := byte(0); slotID < EEPROMResetJournalSlots; slotID++ {
		address := EEPROMResetJournalAddress + uint32(slotID)*EEPROMResetJournalRecordSize
		record, err := image.BytesAt(address, EEPROMResetJournalRecordSize)
		slot := OfflineResetJournalRecord{Slot: slotID}
		if err != nil {
			result.Slots = append(result.Slots, slot)
			continue
		}
		result.Present = true
		presentSlots++
		slot.Present = true
		slot.Count = binary.LittleEndian.Uint32(record[0:4])
		slot.StoredChecksum = record[4]
		slot.Marker = record[5]
		checksumInput := []byte{0x1F, record[0], record[1], record[2], record[3]}
		slot.ComputedChecksum = avrCRC8(checksumInput)
		slot.Occupied = slot.Marker != 0xFF || slot.Count != ^uint32(0) ||
			slot.StoredChecksum != 0xFF
		slot.Valid = slot.Marker == 0xA7 && slot.Count != 0 &&
			slot.Count != ^uint32(0) && slot.StoredChecksum == slot.ComputedChecksum
		if slot.Valid {
			result.ValidRecords++
			if result.NewestCount == nil || int32(slot.Count-newest) > 0 {
				newest = slot.Count
				idCopy := slotID
				countCopy := slot.Count
				result.NewestSlot = &idCopy
				result.NewestCount = &countCopy
			}
		}
		if slot.Occupied && !slot.Valid {
			result.InvalidRecords++
		}
		result.Slots = append(result.Slots, slot)
	}
	result.Complete = presentSlots == EEPROMResetJournalSlots
	result.Valid = result.Complete && result.InvalidRecords == 0
	return result
}

func decodeDecimalBits(value byte) byte {
	if value == 0 {
		return 2
	}
	return value - 1
}

func avrCRC8(data []byte) byte {
	var crc byte
	for _, value := range data {
		crc ^= value
		for bit := 0; bit < 8; bit++ {
			if crc&0x80 != 0 {
				crc = crc<<1 ^ 0x07
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
