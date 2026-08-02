package native

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestAutomationPayloadAndReadbackRoundTrip(t *testing.T) {
	create := AutomationRecord{
		ID: AutomationAllocateID, Flags: AutomationEnabled,
		EventKind: AutomationEventHost, EventValue: AutomationHostDisconnected,
		EventMask: 0xFF, ActionKind: AutomationActionSafeStop,
	}
	payload, err := AutomationPutPayload(create)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != AutomationRecordPayloadSize || payload[0] != AutomationAllocateID || payload[11] != 0 {
		t.Fatalf("automation put payload = % X", payload)
	}

	created := create
	created.ID = 3
	response := make([]byte, 3+AutomationRecordPayloadSize)
	response[0] = AutomationSchema
	binary.LittleEndian.PutUint16(response[1:3], 42)
	copy(response[3:], encodeAutomationRecord(created))
	readback, err := ParseAutomationRecord(response)
	if err != nil {
		t.Fatal(err)
	}
	if readback.Generation != 42 || readback.Record != created || !readback.Record.Enabled() {
		t.Fatalf("automation readback = %+v", readback)
	}
}

func TestParseAutomationListPage(t *testing.T) {
	records := []AutomationRecord{
		{ID: 0, Flags: AutomationEnabled, EventKind: AutomationEventDoor, EventValue: 1, EventMask: 1, ActionKind: AutomationActionRelay, ActionTarget: 2, Value: uint16(AutomationRelayToggle)},
		{ID: 4, Flags: AutomationEnabled, EventKind: AutomationEventBluetooth, EventValue: 2, EventMask: 0xFF, ActionKind: AutomationActionBuzzer, Value: 880, Extra: 120},
		{ID: 9, EventKind: AutomationEventLearnedRF, EventValue: 19, EventMask: 0xFF, ActionKind: AutomationActionHostMacroRequest, ActionTarget: 7},
	}
	payload := make([]byte, 6+len(records)*AutomationRecordPayloadSize)
	payload[0] = AutomationSchema
	binary.LittleEndian.PutUint16(payload[1:3], 0x1234)
	payload[3], payload[4], payload[5] = 7, 10, byte(len(records))
	for index, record := range records {
		copy(payload[6+index*AutomationRecordPayloadSize:], encodeAutomationRecord(record))
	}
	page, err := ParseAutomationList(payload)
	if err != nil {
		t.Fatal(err)
	}
	if page.Schema != AutomationSchema || page.Generation != 0x1234 || page.Total != 7 ||
		page.NextCursor != 10 || len(page.Records) != 3 || page.Records[1] != records[1] {
		t.Fatalf("automation page = %+v", page)
	}
}

func TestAutomationProtocolBounds(t *testing.T) {
	if CapabilityBoardAutomation != 1<<25 {
		t.Fatalf("automation capability = 0x%08X", CapabilityBoardAutomation)
	}
	if OpcodeName(OpAutomationList) != "AUTOMATION_LIST" ||
		OpcodeName(OpAutomationRecord) != "AUTOMATION_RECORD" {
		t.Fatalf("automation opcode names missing")
	}
	if _, err := AutomationListPayload(AutomationCapacity); err == nil {
		t.Fatal("out-of-range automation cursor was accepted")
	}
	if _, err := AutomationRemovePayload(AutomationAllocateID); err == nil {
		t.Fatal("allocation sentinel was accepted for remove")
	}
	if payload := AutomationClearPayload(); payload != nil {
		t.Fatalf("automation clear payload = % X, want nil", payload)
	}

	tooMany := make([]byte, MaxPayload)
	tooMany[0], tooMany[5] = AutomationSchema, byte(automationMaximumPageRecords+1)
	if _, err := ParseAutomationList(tooMany); err == nil {
		t.Fatal("oversized automation page was accepted")
	}
	badReadback := make([]byte, 3+AutomationRecordPayloadSize)
	badReadback[0], badReadback[3] = AutomationSchema, AutomationAllocateID
	badReadback[4], badReadback[5], badReadback[8] = AutomationEnabled, AutomationEventBoot, AutomationActionSafeStop
	if _, err := ParseAutomationRecord(badReadback); err == nil || !strings.Contains(err.Error(), "allocation id") {
		t.Fatalf("allocation sentinel response error = %v", err)
	}
}

func TestValidateAutomationRecordSafetyBounds(t *testing.T) {
	base := AutomationRecord{
		ID: 1, Flags: AutomationEnabled, EventKind: AutomationEventDoor,
		EventValue: 1, EventMask: 1, ActionKind: AutomationActionPWM,
		ActionTarget: 10, Value: 4095,
	}
	if err := ValidateAutomationRecord(base); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*AutomationRecord)
	}{
		{"unknown flags", func(record *AutomationRecord) { record.Flags = 0x80 }},
		{"door state", func(record *AutomationRecord) { record.EventValue = 2 }},
		{"unknown event", func(record *AutomationRecord) { record.EventKind = 9 }},
		{"PWM channel", func(record *AutomationRecord) { record.ActionTarget = 11 }},
		{"PWM level", func(record *AutomationRecord) { record.Value = 4096 }},
		{"reserved options", func(record *AutomationRecord) { record.Options = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := base
			test.mutate(&record)
			if err := ValidateAutomationRecord(record); err == nil {
				t.Fatalf("invalid record was accepted: %+v", record)
			}
		})
	}
	hostActuator := base
	hostActuator.EventKind = AutomationEventHost
	hostActuator.EventValue = AutomationHostConnected
	if err := ValidateAutomationRecord(hostActuator); err == nil {
		t.Fatal("host-event PWM action was accepted")
	}
	motionAll := base
	motionAll.ActionKind = AutomationActionMotionStop
	motionAll.ActionTarget = 0xFF
	motionAll.Value = 0
	if err := ValidateAutomationRecord(motionAll); err != nil {
		t.Fatalf("all-motion stop was rejected: %v", err)
	}
}
