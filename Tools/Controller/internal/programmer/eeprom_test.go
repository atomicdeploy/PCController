package programmer

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeOfflineEEPROMCurrentSemanticLayout(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		settings := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
		values := settings[:EEPROMSettingsValueBytes]
		values[0] = 0x20 // Door audio disabled.
		values[1] = 1
		values[2] = 180
		values[3] = 7
		values[4] = 5
		values[5] = 123
		values[6] = 0x06
		binary.LittleEndian.PutUint16(values[7:9], 250)
		copy(values[9:17], []byte{1, 2, 3, 4, 5, 6, 7, 8})
		values[17] = 13
		values[18] = 0x01 | 0x06 | 0x10 | 0xC0
		binary.LittleEndian.PutUint16(values[19:21], 0x3FFF)
		copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
		values[28] = (9 << 3) | 2
		values[29] = 0xF0
		values[30] = 100
		settings[EEPROMSettingsValueBytes] = avrCRC8(values)

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
		}
		record := data[EEPROMRemoteEntriesAddress : EEPROMRemoteEntriesAddress+EEPROMRemoteRecordBytes]
		binary.LittleEndian.PutUint32(record[0:4], 0xDEADBEEF)
		record[4] = 24
		record[5] = 1
		binary.LittleEndian.PutUint16(record[6:8], 350)
		record[8] = 4
		record[9] = 1
		record[10] = 3
		record[11] = avrCRC8(record[:11])

		writeResetRecord(data, 3, 40)
		writeResetRecord(data, 4, 41)
	})
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.SourceKind != "offline-eeprom-hex" ||
		decoded.Layout != "settings-unversioned-31/rf-record12-cap20/reset-journal-320/automation-v1-dual-bank-704" ||
		!decoded.Settings.Supported || !decoded.Settings.Valid ||
		decoded.Settings.Format != "current/unversioned-31+crc8" ||
		decoded.Settings.ValueBytes != 31 {
		t.Fatalf("settings decode invalid: %#v", decoded.Settings)
	}
	settings := decoded.Settings.Values
	if settings.Silent || settings.ProgrammingMode || settings.DoorAudioEnabled ||
		!settings.RelayAudioEnabled || settings.StreamPeriodMS != 250 ||
		settings.UserPWM[7] != 8 || settings.DefaultMenuPage != 13 ||
		settings.VoltageDecimals != 0 || settings.CurrentDecimals != 2 ||
		settings.StatusColor != 3 || !settings.SaveLastMenuPage ||
		settings.VisibleMenuMask != 0x3FFF || settings.MenuOrder[6] != 0xDC ||
		settings.DisplayClosedBrightness != 2 || settings.MotionExitHoldSeconds != 9 ||
		settings.OutputPersistence != 0x06 || settings.RelayRestoreMask != 0xF0 {
		t.Fatalf("unexpected decoded settings: %#v", settings)
	}
	if settings.MotionBreakMS != 100 {
		t.Fatalf("exact motion break was not decoded: %#v", settings)
	}
	if !decoded.Remotes.Valid || decoded.Remotes.ValidCount != 1 ||
		decoded.Remotes.InvalidCount != 0 || len(decoded.Remotes.Slots) != 20 {
		t.Fatalf("unexpected RF decode: %#v", decoded.Remotes)
	}
	remote := decoded.Remotes.Slots[0]
	if !remote.Valid || remote.Code != 0xDEADBEEF || remote.Bits != 24 ||
		remote.PulseMicros != 350 || remote.ActionKind != 4 || remote.Behavior != 3 {
		t.Fatalf("unexpected remote: %#v", remote)
	}
	if decoded.ResetJournal.ValidRecords != 2 || decoded.ResetJournal.NewestCount == nil ||
		*decoded.ResetJournal.NewestCount != 41 || decoded.ResetJournal.NewestSlot == nil ||
		*decoded.ResetJournal.NewestSlot != 4 {
		t.Fatalf("unexpected reset journal: %#v", decoded.ResetJournal)
	}
}

func TestDecodeOfflineEEPROMSelectsNewestCommittedAutomationBank(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, 0, 7, map[byte][]byte{
			2: {1, 1, 1, 1, 3, 2, 2, 0, 0, 0, 0},
		})
		writeAutomationBank(data, 1, 8, map[byte][]byte{
			5: {1, 3, 0, 0xFF, 1, 0, 0, 0, 0, 0, 0},
		})
	})
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	automations := decoded.Automations
	if !automations.Valid || automations.ActiveBank == nil || *automations.ActiveBank != 1 ||
		automations.ActiveGeneration == nil || *automations.ActiveGeneration != 8 ||
		automations.ValidCount != 1 || len(automations.Records) != 1 {
		t.Fatalf("automation store = %#v", automations)
	}
	record := automations.Records[0]
	if record.ID != 5 || !record.Enabled || record.EventKind != 3 ||
		record.EventValue != 0 || record.ActionKind != 1 || !record.Valid {
		t.Fatalf("automation record = %#v", record)
	}
	if !automations.Banks[0].Valid || !automations.Banks[1].Valid ||
		automations.Banks[1].ValidRecords != EEPROMAutomationCapacity {
		t.Fatalf("automation banks = %#v", automations.Banks)
	}
}

func TestDecodeOfflineEEPROMFallsBackFromTornAutomationBank(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, 0, 10, map[byte][]byte{
			1: {1, 8, 0, 0, 2, 1, 0, 0, 0, 0, 0},
		})
		writeAutomationBank(data, 1, 11, map[byte][]byte{
			3: {1, 2, 2, 0xFF, 6, 0, 0x70, 0x03, 120, 0, 0},
		})
		data[EEPROMAutomationBank1Address+9] = 0 // Commit marker is written last.
	})
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	automations := decoded.Automations
	if !automations.Valid || automations.ActiveBank == nil || *automations.ActiveBank != 0 ||
		automations.ActiveGeneration == nil || *automations.ActiveGeneration != 10 ||
		automations.Banks[1].Valid || !strings.Contains(automations.Banks[1].Issue, "commit") {
		t.Fatalf("torn-bank fallback = %#v", automations)
	}

	path = writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, 0, 0xFFFF, nil)
		writeAutomationBank(data, 1, 0, nil)
	})
	decoded, err = DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Automations.ActiveBank == nil || *decoded.Automations.ActiveBank != 1 {
		t.Fatalf("generation wrap did not select bank 1: %#v", decoded.Automations)
	}
}

func TestDecodeOfflineEEPROMReportsCRCAndHeaderDamage(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		settings := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
		values := settings[:EEPROMSettingsValueBytes]
		values[1] = 1
		values[4] = 5
		binary.LittleEndian.PutUint16(values[7:9], 500)
		binary.LittleEndian.PutUint16(values[19:21], 0x3FFF)
		copy(values[21:28], []byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC})
		values[28] = 0
		values[30] = 1
		settings[EEPROMSettingsValueBytes] = 0x99
		header := data[EEPROMRemoteHeaderAddress : EEPROMRemoteHeaderAddress+4]
		binary.LittleEndian.PutUint16(header[0:2], 0x4C52)
		header[2] = 9
		header[3] = EEPROMRemoteCapacity
		for id := byte(0); id < EEPROMRemoteCapacity; id++ {
			start := EEPROMRemoteEntriesAddress + uint32(id)*EEPROMRemoteRecordBytes
			for index := range data[start : start+EEPROMRemoteRecordBytes] {
				data[start+uint32(index)] = 0
			}
		}
		record := data[EEPROMRemoteEntriesAddress : EEPROMRemoteEntriesAddress+EEPROMRemoteRecordBytes]
		for index := range record {
			record[index] = 0
		}
		binary.LittleEndian.PutUint32(record[0:4], 42)
		record[4] = 33
		record[5] = 0
		record[11] = 0x55
	})
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Settings.Valid || !strings.Contains(decoded.Settings.Issue, "CRC-8") {
		t.Fatalf("settings corruption not reported: %#v", decoded.Settings)
	}
	if decoded.Remotes.Valid || !strings.Contains(decoded.Remotes.Issue, "record_bytes") ||
		decoded.Remotes.InvalidCount != 1 || decoded.Remotes.Slots[0].Valid {
		t.Fatalf("RF corruption not reported: %#v", decoded.Remotes)
	}
}

func TestDecodeOfflineEEPROMRejectsUnsupportedShortSettingsLayout(t *testing.T) {
	const unsupportedValueBytes = 19
	values := make([]byte, unsupportedValueBytes)
	values[0] = 0x01 | 0x04
	values[1] = 1
	values[2] = 128
	values[4] = 5
	values[5] = 128
	values[6] = 2
	binary.LittleEndian.PutUint16(values[7:9], 500)
	values[17] = 3
	values[18] = 0x01
	record := append(append([]byte(nil), values...), avrCRC8(values))
	image := &IntelHexImage{data: make(map[uint32]byte, len(record))}
	for offset, value := range record {
		image.data[EEPROMSettingsAddress+uint32(offset)] = value
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "unsupported.eep")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := decoded.Settings
	if settings.Supported || settings.Valid ||
		!strings.Contains(settings.Issue, "unsupported settings layout") {
		t.Fatalf("unsupported settings layout was accepted: %#v", settings)
	}
}

func TestDecodeOfflineEEPROMRejectsInvalidCurrentMenuLayout(t *testing.T) {
	for _, test := range []struct {
		name  string
		mask  uint16
		order [7]byte
		issue string
	}{
		{
			name:  "empty-visible-mask",
			order: [7]byte{0x10, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC},
			issue: "visible menu mask",
		},
		{
			name:  "duplicate-order",
			mask:  0x3FFF,
			order: [7]byte{0x00, 0x32, 0x54, 0x76, 0x98, 0xBA, 0xDC},
			issue: "duplicated",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := writeEEPROMFixture(t, func(data []byte) {
				record := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
				values := record[:EEPROMSettingsValueBytes]
				values[1] = 1
				values[4] = 5
				values[5] = 128
				values[6] = 2
				binary.LittleEndian.PutUint16(values[7:9], 500)
				binary.LittleEndian.PutUint16(values[19:21], test.mask)
				copy(values[21:28], test.order[:])
				values[28] = 0
				values[30] = 1
				record[EEPROMSettingsValueBytes] = avrCRC8(values)
			})
			decoded, err := DecodeOfflineEEPROMHex(path)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Settings.Valid || !decoded.Settings.Supported ||
				!strings.Contains(decoded.Settings.Issue, test.issue) {
				t.Fatalf("invalid layout was not rejected: %#v", decoded.Settings)
			}
		})
	}
}

func TestDecodeOfflineEEPROMRejectsOutOfRangeAddress(t *testing.T) {
	image := &IntelHexImage{data: map[uint32]byte{PCControllerEEPROMBytes: 1}}
	content, _ := image.Canonical()
	path := filepath.Join(t.TempDir(), "bad.eep")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := DecodeOfflineEEPROMHex(path)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected EEPROM address guard, got %v", err)
	}
}

func writeEEPROMFixture(t *testing.T, mutate func([]byte)) string {
	t.Helper()
	data := make([]byte, PCControllerEEPROMBytes)
	for index := range data {
		data[index] = 0xFF
	}
	mutate(data)
	image := &IntelHexImage{data: make(map[uint32]byte, len(data))}
	for address, value := range data {
		image.data[uint32(address)] = value
	}
	content, err := image.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "eeprom.hex")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeResetRecord(data []byte, slot byte, count uint32) {
	start := EEPROMResetJournalAddress + uint32(slot)*EEPROMResetJournalRecordSize
	record := data[start : start+EEPROMResetJournalRecordSize]
	binary.LittleEndian.PutUint32(record[0:4], count)
	record[4] = avrCRC8([]byte{0x1F, record[0], record[1], record[2], record[3]})
	record[5] = 0xA7
}

func writeAutomationBank(data []byte, bank byte, generation uint16, records map[byte][]byte) {
	address := EEPROMAutomationBank0Address
	if bank == 1 {
		address = EEPROMAutomationBank1Address
	}
	raw := data[address : address+EEPROMAutomationBankBytes]
	for index := range raw {
		raw[index] = 0
	}
	binary.LittleEndian.PutUint16(raw[0:2], EEPROMAutomationMagic)
	raw[2] = EEPROMAutomationSchema
	raw[3] = byte(EEPROMAutomationRecordBytes)
	raw[4] = EEPROMAutomationCapacity
	raw[5] = 0
	binary.LittleEndian.PutUint16(raw[6:8], generation)
	raw[8] = avrCRC8(raw[:8])
	raw[9] = EEPROMAutomationCommit
	for id, semantic := range records {
		if id >= EEPROMAutomationCapacity || len(semantic) != int(EEPROMAutomationRecordBytes-1) {
			panic("invalid automation fixture")
		}
		start := EEPROMAutomationHeaderBytes + uint32(id)*EEPROMAutomationRecordBytes
		record := raw[start : start+EEPROMAutomationRecordBytes]
		copy(record[:EEPROMAutomationRecordBytes-1], semantic)
		record[EEPROMAutomationRecordBytes-1] = avrCRC8(record[:EEPROMAutomationRecordBytes-1])
	}
}
