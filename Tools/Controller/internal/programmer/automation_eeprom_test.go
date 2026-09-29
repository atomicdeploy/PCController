package programmer

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestDecodeOfflineEEPROMSelectsNewestCommittedAutomationBank(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, EEPROMAutomationBank0Address, 7, [][]byte{
			automationSemanticRecord(1, 8, 0, 0xFF, 8, 5, 0, 0, 0),
		}, EEPROMAutomationCommit)
		writeAutomationBank(data, EEPROMAutomationBank1Address, 8, [][]byte{
			automationSemanticRecord(1, 1, 1, 0xFF, 6, 0, 1000, 1100, 0),
		}, EEPROMAutomationCommit)
	})

	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Automations
	if !got.Present || !got.Valid || got.ActiveBank == nil || *got.ActiveBank != 1 ||
		got.ActiveGeneration == nil || *got.ActiveGeneration != 8 || got.ValidCount != 1 ||
		len(got.Records) != 1 {
		t.Fatalf("unexpected automation decode: %#v", got)
	}
	record := got.Records[0]
	if !record.Valid || !record.Enabled || record.EventKind != 1 || record.EventValue != 1 ||
		record.EventMask != 0xFF || record.ActionKind != 6 || record.ActionTarget != 0 ||
		record.Value != 1000 || record.Extra != 1100 || record.Options != 0 {
		t.Fatalf("unexpected active automation record: %#v", record)
	}
}

func TestDecodeOfflineEEPROMIgnoresTornAutomationBank(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, EEPROMAutomationBank0Address, 20, [][]byte{
			automationSemanticRecord(1, 8, 0, 0xFF, 8, 5, 0, 0, 0),
		}, EEPROMAutomationCommit)
		writeAutomationBank(data, EEPROMAutomationBank1Address, 21, [][]byte{
			automationSemanticRecord(1, 1, 1, 0xFF, 6, 0, 1000, 100, 0),
		}, 0)
	})

	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Automations
	if !got.Valid || got.ActiveBank == nil || *got.ActiveBank != 0 ||
		got.ActiveGeneration == nil || *got.ActiveGeneration != 20 {
		t.Fatalf("torn bank was selected: %#v", got)
	}
	if got.Banks[1].Valid || !strings.Contains(got.Banks[1].Issue, "commit") {
		t.Fatalf("torn bank was not diagnosed: %#v", got.Banks[1])
	}
}

func TestDecodeOfflineEEPROMAutomationGenerationWrap(t *testing.T) {
	path := writeEEPROMFixture(t, func(data []byte) {
		writeAutomationBank(data, EEPROMAutomationBank0Address, 0xFFFF, nil, EEPROMAutomationCommit)
		writeAutomationBank(data, EEPROMAutomationBank1Address, 0, nil, EEPROMAutomationCommit)
	})

	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Automations
	if !got.Valid || got.ActiveBank == nil || *got.ActiveBank != 1 ||
		got.ActiveGeneration == nil || *got.ActiveGeneration != 0 {
		t.Fatalf("generation wrap was not honored: %#v", got)
	}
}

func TestDecodeOfflineEEPROMReportsAbsentAutomationProfile(t *testing.T) {
	path := writeEEPROMFixture(t, func([]byte) {})
	decoded, err := DecodeOfflineEEPROMHex(path)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Automations
	if got.Present || got.Valid || !strings.Contains(got.Issue, "not present") {
		t.Fatalf("absent automation profile was misdiagnosed: %#v", got)
	}
}

func writeAutomationBank(data []byte, address uint32, generation uint16, records [][]byte, commit byte) {
	bank := data[address : address+EEPROMAutomationBankBytes]
	for index := range bank {
		bank[index] = 0
	}
	header := bank[:EEPROMAutomationHeaderBytes]
	binary.LittleEndian.PutUint16(header[0:2], EEPROMAutomationMagic)
	header[2] = EEPROMAutomationSchema
	header[3] = byte(EEPROMAutomationRecordBytes)
	header[4] = EEPROMAutomationCapacity
	header[5] = 0
	binary.LittleEndian.PutUint16(header[6:8], generation)
	header[8] = avrCRC8(header[:8])
	header[9] = commit
	for id, semantic := range records {
		if id >= int(EEPROMAutomationCapacity) {
			break
		}
		recordAddress := EEPROMAutomationHeaderBytes + uint32(id)*EEPROMAutomationRecordBytes
		record := bank[recordAddress : recordAddress+EEPROMAutomationRecordBytes]
		copy(record[:11], semantic)
		record[11] = avrCRC8(record[:11])
	}
}

func automationSemanticRecord(flags, eventKind, eventValue, eventMask, actionKind, actionTarget byte, value, extra uint16, options byte) []byte {
	record := make([]byte, 11)
	record[0] = flags
	record[1] = eventKind
	record[2] = eventValue
	record[3] = eventMask
	record[4] = actionKind
	record[5] = actionTarget
	binary.LittleEndian.PutUint16(record[6:8], value)
	binary.LittleEndian.PutUint16(record[8:10], extra)
	record[10] = options
	return record
}
