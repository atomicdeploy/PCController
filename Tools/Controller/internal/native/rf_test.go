package native

import (
	"encoding/binary"
	"testing"
)

func TestParseRFEntries(t *testing.T) {
	payload := make([]byte, 4+RFEntryPayloadSize)
	payload[0] = RFEntriesSchema
	payload[1] = 7
	payload[2] = 3
	payload[3] = 1
	payload[4] = 9
	binary.LittleEndian.PutUint32(payload[5:9], 0x12AB34CD)
	payload[9] = 24
	payload[10] = 2
	binary.LittleEndian.PutUint16(payload[11:13], 350)
	payload[13] = RFActionRelay
	payload[14] = 5
	payload[15] = RFBehaviorToggle

	page, err := ParseRFEntries(payload)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 7 || page.NextCursor != 3 || len(page.Entries) != 1 {
		t.Fatalf("unexpected page: %+v", page)
	}
	entry := page.Entries[0]
	if entry.ID != 9 || entry.Code != 0x12AB34CD || entry.Bits != 24 ||
		entry.Protocol != 2 || entry.PulseUS != 350 ||
		entry.ActionKind != RFActionRelay || entry.ActionValue != 5 ||
		entry.Behavior != RFBehaviorToggle {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestParseRFEntriesRejectsProtocolOverflow(t *testing.T) {
	payload := make([]byte, MaxPayload)
	payload[0] = RFEntriesSchema
	payload[3] = byte(rfEntriesMaximumEntries + 1)
	if _, err := ParseRFEntries(payload); err == nil {
		t.Fatal("RF entry count beyond the protocol capacity was accepted")
	}
}

func TestRFMapPayloadValidation(t *testing.T) {
	payload, err := RFMapPayload(3, RFActionKey, 2, RFBehaviorPress)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) != string([]byte{3, RFActionKey, 2, RFBehaviorPress}) {
		t.Fatalf("payload = % X", payload)
	}
	if _, err := RFMapPayload(3, RFActionRelay, 8, RFBehaviorToggle); err == nil {
		t.Fatal("expected invalid relay value")
	}
}

func TestHostPanelPayloadAndCapabilityGatedBuzzerBusy(t *testing.T) {
	payload, err := HostPanelPayload("HOST", "PC Host", "Device: online", 2, 4095)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != 40 || payload[0] != DisplayHostPanel ||
		binary.LittleEndian.Uint16(payload[1:3]) != 0x2FFF || payload[3] != 36 {
		t.Fatalf("host panel payload = % X", payload)
	}
	if release := HostPanelReleasePayload(); string(release) != string([]byte{DisplayReleaseHostPanel, 0, 0, 0}) {
		t.Fatalf("host panel release = % X", release)
	}

	status := Status{Flags: StatusBuzzerBusy}
	if busy, known := BuzzerBusy(Hello{}, status); busy || known {
		t.Fatalf("legacy status bit 12 was misread as buzzer: busy=%t known=%t", busy, known)
	}
	if busy, known := BuzzerBusy(Hello{Capabilities: CapabilityBuzzerBusy}, status); !busy || !known {
		t.Fatalf("new buzzer flag not read: busy=%t known=%t", busy, known)
	}
}

func TestParseFrontPanelSchema2CaptureMetadata(t *testing.T) {
	payload := make([]byte, 47)
	payload[0] = 2
	copy(payload[1:5], []byte{0x3F, 0x06, 0x5B, 0x4F})
	payload[5], payload[6], payload[7], payload[8] = 6, 3, 0x27, 3
	copy(payload[9:25], []byte("PC Host         "))
	copy(payload[25:41], []byte("Macro 3 01:24   "))
	payload[41], payload[42], payload[43] = 5, 14, 9
	payload[44], payload[45], payload[46] = 0x82, 0xBC, 0x0A
	panel, err := ParseFrontPanel(payload)
	if err != nil {
		t.Fatal(err)
	}
	if !panel.HostCaptured || panel.HostState != 2 || panel.HostEditableValue != 0xABC ||
		panel.LCDLine1 != "PC Host" || panel.LCDLine2 != "Macro 3 01:24" || !panel.Blink {
		t.Fatalf("front panel=%+v", panel)
	}
}
