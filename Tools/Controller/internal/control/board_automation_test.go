package control

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"pccontroller.local/controller/internal/native"
)

type fakeBoardAutomationTransport struct {
	generation uint16
	records    map[byte]native.AutomationRecord
}

func (fake *fakeBoardAutomationTransport) Request(_ context.Context, opcode byte, payload []byte, _ ...byte) (native.Frame, error) {
	switch opcode {
	case native.OpAutomationList:
		cursor := payload[0]
		page := []byte{native.AutomationSchema, byte(fake.generation), byte(fake.generation >> 8), byte(len(fake.records)), 0xFF, 0}
		for id := cursor; id < native.AutomationCapacity; id++ {
			record, ok := fake.records[id]
			if !ok {
				continue
			}
			if page[5] == 3 {
				page[4] = id
				break
			}
			encoded, _ := native.AutomationPutPayload(record)
			page = append(page, encoded...)
			page[5]++
		}
		return native.Frame{Opcode: native.OpAutomationListResp, Payload: page}, nil
	case native.OpAutomationPut:
		record := decodeAutomationFixture(payload)
		if record.ID == native.AutomationAllocateID {
			for id := byte(0); id < native.AutomationCapacity; id++ {
				if _, used := fake.records[id]; !used {
					record.ID = id
					break
				}
			}
		}
		fake.generation++
		fake.records[record.ID] = record
		encoded, _ := native.AutomationPutPayload(record)
		response := []byte{native.AutomationSchema, byte(fake.generation), byte(fake.generation >> 8)}
		response = append(response, encoded...)
		return native.Frame{Opcode: native.OpAutomationRecord, Payload: response}, nil
	case native.OpAutomationRemove:
		delete(fake.records, payload[0])
		fake.generation++
		return native.Frame{Opcode: native.OpACK}, nil
	case native.OpAutomationClear:
		fake.records = make(map[byte]native.AutomationRecord)
		fake.generation++
		return native.Frame{Opcode: native.OpACK}, nil
	default:
		return native.Frame{}, fmt.Errorf("unexpected opcode 0x%02X", opcode)
	}
}

func decodeAutomationFixture(payload []byte) native.AutomationRecord {
	return native.AutomationRecord{
		ID: payload[0], Flags: payload[1], EventKind: payload[2],
		EventValue: payload[3], EventMask: payload[4], ActionKind: payload[5],
		ActionTarget: payload[6], Value: binary.LittleEndian.Uint16(payload[7:9]),
		Extra: binary.LittleEndian.Uint16(payload[9:11]), Options: payload[11],
	}
}

func boardAutomationFixture(id byte) native.AutomationRecord {
	return native.AutomationRecord{
		ID: id, Flags: native.AutomationEnabled,
		EventKind: native.AutomationEventDoor, EventValue: 1, EventMask: 0xFF,
		ActionKind: native.AutomationActionRelay, ActionTarget: 4,
		Value: uint16(native.AutomationRelayOn),
	}
}

func TestBoardAutomationServiceCRUDAndReadback(t *testing.T) {
	fake := &fakeBoardAutomationTransport{generation: 7, records: map[byte]native.AutomationRecord{
		1: boardAutomationFixture(1), 4: boardAutomationFixture(4),
		7: boardAutomationFixture(7), 9: boardAutomationFixture(9),
	}}
	service := &BoardAutomationService{transport: fake}
	snapshot, err := service.List(context.Background())
	if err != nil || snapshot.Generation != 7 || len(snapshot.Records) != 4 || snapshot.Records[3].ID != 9 {
		t.Fatalf("List() = %#v, %v", snapshot, err)
	}

	added := boardAutomationFixture(native.AutomationAllocateID)
	readback, err := service.Put(context.Background(), added)
	if err != nil || readback.Record.ID != 0 || readback.Generation != 8 {
		t.Fatalf("Put() = %#v, %v", readback, err)
	}
	if err := service.Remove(context.Background(), 4); err != nil {
		t.Fatalf("Remove(): %v", err)
	}
	if _, found := fake.records[4]; found {
		t.Fatal("removed record remains")
	}
	if err := service.Clear(context.Background()); err != nil {
		t.Fatalf("Clear(): %v", err)
	}
	if len(fake.records) != 0 {
		t.Fatalf("clear left %d records", len(fake.records))
	}
}

func TestBoardAutomationServiceRequiresAdvertisedCapability(t *testing.T) {
	service := &BoardAutomationService{
		transport: &fakeBoardAutomationTransport{records: make(map[byte]native.AutomationRecord)},
		support:   func() bool { return false },
	}
	if _, err := service.List(context.Background()); err != ErrBoardAutomationsUnsupported {
		t.Fatalf("List error = %v", err)
	}
}

func TestBoardAutomationCLIParsingAndFormatting(t *testing.T) {
	event, err := parseAutomationEvent("bluetooth")
	if err != nil || event != native.AutomationEventBluetooth {
		t.Fatalf("event = %d, %v", event, err)
	}
	action, err := parseAutomationAction("motion-stop")
	if err != nil || action != native.AutomationActionMotionStop {
		t.Fatalf("action = %d, %v", action, err)
	}
	formatted := formatBoardAutomations(BoardAutomationSnapshot{Generation: 2, Records: []native.AutomationRecord{boardAutomationFixture(3)}})
	if formatted == "" || formatted == "Board automations generation 2: empty" {
		t.Fatalf("unexpected format %q", formatted)
	}
}
