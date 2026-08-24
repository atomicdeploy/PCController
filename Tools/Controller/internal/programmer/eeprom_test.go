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
		values[19] = (9 << 3) | 2
		values[20] = 0xF0
		values[21] = 100
		values[22] = 7
		copy(values[23:31], []byte("EDGE-01"))
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
		decoded.Layout != "settings-schema1-core22-record32/rf-record12-cap20/reset-journal-336" ||
		!decoded.Settings.Supported || !decoded.Settings.Valid ||
		decoded.Settings.Format != "schema1/core22+name9+crc8" ||
		decoded.Settings.Schema != EEPROMSettingsRecordSchema || decoded.Settings.ValueBytes != 31 {
		t.Fatalf("settings decode invalid: %#v", decoded.Settings)
	}
	settings := decoded.Settings.Values
	if settings.Silent || settings.ProgrammingMode || settings.DoorAudioEnabled ||
		!settings.RelayAudioEnabled || settings.StreamPeriodMS != 250 ||
		settings.UserPWM[7] != 8 || settings.DefaultMenuPage != 13 ||
		settings.VoltageDecimals != 0 || settings.CurrentDecimals != 2 ||
		settings.StatusColor != 3 || !settings.SaveLastMenuPage ||
		settings.DisplayClosedBrightness != 2 || settings.MotionExitHoldSeconds != 9 ||
		settings.OutputPersistence != 0x06 || settings.RelayRestoreMask != 0xF0 ||
		settings.BoardName != "EDGE-01" {
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

func TestDecodeOfflineEEPROMReportsCRCAndHeaderDamage(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		settings := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMSettingsRecordBytes]
		values := settings[:EEPROMSettingsValueBytes]
		values[1] = 1
		values[4] = 5
		binary.LittleEndian.PutUint16(values[7:9], 500)
		values[19] = 0
		values[21] = 1
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
				record := data[EEPROMSettingsAddress : EEPROMSettingsAddress+EEPROMMenuLayoutRecordBytes]
				values := record[:EEPROMMenuLayoutValueBytes]
				values[1] = 1
				values[4] = 5
				values[5] = 128
				values[6] = 2
				binary.LittleEndian.PutUint16(values[7:9], 500)
				binary.LittleEndian.PutUint16(values[19:21], test.mask)
				copy(values[21:28], test.order[:])
				values[28] = 0
				values[30] = 1
				for index := 31; index < int(EEPROMSettingsValueBytes); index++ {
					values[index] = 0
				}
				record[EEPROMMenuLayoutValueBytes] = avrCRC8(values)
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
	record[4] = avrCRC8(record[0:4])
	record[5] = 0xA7
}
