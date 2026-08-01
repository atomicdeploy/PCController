package control

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

type fakeRFReplaceTransport struct {
	records         map[byte]native.RFEntry
	probeCode       byte
	replaceCommands int
	corruptReadback bool
}

func (fake *fakeRFReplaceTransport) Command(_ context.Context, opcode byte, payload []byte) error {
	switch opcode {
	case native.OpRFLearnReplace:
		if len(payload) == 0 {
			return &link.RemoteError{RequestOpcode: opcode, Code: fake.probeCode}
		}
		if len(payload) != native.RFEntryPayloadSize {
			return errors.New("bad fake replace payload")
		}
		entry := native.RFEntry{
			ID: payload[0], Code: binary.LittleEndian.Uint32(payload[1:5]),
			Bits: payload[5], Protocol: payload[6],
			PulseUS:    binary.LittleEndian.Uint16(payload[7:9]),
			ActionKind: payload[9], ActionValue: payload[10], Behavior: payload[11],
		}
		fake.records[entry.ID] = entry
		fake.replaceCommands++
		return nil
	case native.OpRFLearnRemove:
		delete(fake.records, payload[0])
		return nil
	default:
		return errors.New("unexpected fake command")
	}
}

func (fake *fakeRFReplaceTransport) Request(_ context.Context, opcode byte, payload []byte, _ ...byte) (native.Frame, error) {
	if opcode != native.OpRFLearnList || len(payload) != 1 {
		return native.Frame{}, errors.New("unexpected fake request")
	}
	entries := make([]native.RFEntry, 0, len(fake.records))
	for _, entry := range fake.records {
		if entry.ID >= payload[0] {
			entries = append(entries, entry)
		}
	}
	sortRFRecords(entries)
	if len(entries) > 3 {
		entries = entries[:3]
	}
	next := byte(0xFF)
	if len(entries) > 0 {
		last := entries[len(entries)-1].ID
		for id := range fake.records {
			if id > last && (next == 0xFF || id < next) {
				next = id
			}
		}
	}
	result := []byte{native.RFEntriesSchema, byte(len(fake.records)), next, byte(len(entries))}
	for _, entry := range entries {
		encoded, _ := native.RFReplacePayload(entry)
		result = append(result, encoded...)
	}
	if fake.corruptReadback && fake.replaceCommands != 0 && len(entries) != 0 {
		result[5] ^= 0x01
		fake.corruptReadback = false
	}
	return native.Frame{Opcode: native.OpRFEntries, Payload: result}, nil
}

func testRFRecords() []native.RFEntry {
	return []native.RFEntry{
		{ID: 2, Code: 0x123456, Bits: 24, Protocol: 1, PulseUS: 350, ActionKind: native.RFActionKey, ActionValue: 0, Behavior: native.RFBehaviorPress},
		{ID: 7, Code: 0xABCDEF, Bits: 24, Protocol: 1, PulseUS: 350, ActionKind: native.RFActionRelay, ActionValue: 4, Behavior: native.RFBehaviorToggle},
	}
}

func newTestRFService(fake *fakeRFReplaceTransport, capabilities uint32) *RFReplaceService {
	return &RFReplaceService{
		transport: fake,
		capabilities: func() (string, uint32, bool) {
			return "test-device", capabilities, true
		},
	}
}

func TestRFReplaceProbeIsNonMutatingAndCapabilityGated(t *testing.T) {
	fake := &fakeRFReplaceTransport{records: map[byte]native.RFEntry{}, probeCode: native.ErrorBadPayload}
	service := newTestRFService(fake, 0)
	if support := service.Support(); support.Known || support.Supported {
		t.Fatalf("support before probe = %+v", support)
	}
	support, err := service.Probe(context.Background())
	if err != nil || !support.Supported || len(fake.records) != 0 || fake.replaceCommands != 0 {
		t.Fatalf("safe probe support=%+v err=%v fake=%+v", support, err, fake)
	}
	if !service.Support().Supported {
		t.Fatal("successful probe was not cached for connected firmware identity")
	}

	unsupported := newTestRFService(&fakeRFReplaceTransport{
		records: map[byte]native.RFEntry{}, probeCode: native.ErrorUnsupported,
	}, 0)
	support, err = unsupported.Probe(context.Background())
	if err != nil || !support.Known || support.Supported {
		t.Fatalf("unsupported probe support=%+v err=%v", support, err)
	}
}

func TestRFReplaceWritesFullSnapshotAndVerifiesReadback(t *testing.T) {
	original := testRFRecords()
	fake := &fakeRFReplaceTransport{
		records: map[byte]native.RFEntry{2: original[0], 7: original[1]},
	}
	service := newTestRFService(fake, native.CapabilityRFLearnReplace)
	desired := []native.RFEntry{original[1], original[0]}
	desired[0].ID = 0
	desired[1].ID = 1
	if err := service.Replace(context.Background(), desired); err != nil {
		t.Fatal(err)
	}
	readback, err := service.Fetch(context.Background())
	if err != nil || !equalRFRecords(readback, desired) {
		t.Fatalf("readback=%+v err=%v desired=%+v", readback, err, desired)
	}
	if _, stale := fake.records[7]; stale {
		t.Fatal("stale sparse source ID survived full snapshot replacement")
	}
}

func TestRFReplaceMismatchAutomaticallyRollsBack(t *testing.T) {
	original := testRFRecords()
	fake := &fakeRFReplaceTransport{
		records:         map[byte]native.RFEntry{2: original[0], 7: original[1]},
		corruptReadback: true,
	}
	service := newTestRFService(fake, native.CapabilityRFLearnReplace)
	desired := []native.RFEntry{original[1], original[0]}
	desired[0].ID = 0
	desired[1].ID = 1
	err := service.Replace(context.Background(), desired)
	if err == nil || !strings.Contains(err.Error(), "original learned-remote snapshot restored") {
		t.Fatalf("rollback error = %v", err)
	}
	readback, fetchErr := service.Fetch(context.Background())
	if fetchErr != nil || !equalRFRecords(readback, original) {
		t.Fatalf("rollback readback=%+v err=%v original=%+v", readback, fetchErr, original)
	}
}
