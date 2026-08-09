package ipcjson

import (
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/native"
)

func intPointer(value int) *int { return &value }

func TestOpcodeExchangeParamsAreOpaqueBoundedAndVersionless(t *testing.T) {
	params := opcodeExchangeParams{
		Opcode: intPointer(0xE1), ExpectOpcode: intPointer(0xE2),
		PayloadHex: "AA:bb-01",
	}
	opcode, payload, expected, err := params.values()
	if err != nil {
		t.Fatal(err)
	}
	if opcode != 0xE1 || expected != 0xE2 ||
		string(payload) != string([]byte{0xAA, 0xBB, 0x01}) {
		t.Fatalf("exchange=%02X % X -> %02X", opcode, payload, expected)
	}

	params = opcodeExchangeParams{Opcode: intPointer(0xE1)}
	_, _, expected, err = params.values()
	if err != nil || expected != native.OpACK {
		t.Fatalf("default ACK expectation=%02X err=%v", expected, err)
	}
	for _, invalid := range []opcodeExchangeParams{
		{},
		{Opcode: intPointer(0)},
		{Opcode: intPointer(256)},
		{Opcode: intPointer(1), ExpectOpcode: intPointer(0)},
		{Opcode: intPointer(1), Payload: []byte{1}, PayloadHex: "02"},
		{Opcode: intPointer(1), Payload: make([]byte, native.MaxPayload+1)},
	} {
		if _, _, _, err := invalid.values(); err == nil {
			t.Fatalf("invalid opcode exchange accepted: %#v", invalid)
		}
	}
}

func TestOpcodeSubscriptionTopicAndAuthorization(t *testing.T) {
	streamTopics, err := normalizeSubscription(wsSubscription{
		Topics: []string{" events ", "state", "debug", "state"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := streamTopics.Topics; len(got) != 3 || got[0] != "events" ||
		got[1] != "state" || got[2] != "debug" {
		t.Fatalf("normalized stream topics=%#v", got)
	}

	normalized, err := normalizeSubscription(wsSubscription{
		Opcodes: []int{0x9C, 0xE1, 0x9C},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Topics) != 1 || normalized.Topics[0] != "opcodes" ||
		len(normalized.Opcodes) != 2 || normalized.Opcodes[0] != 0x9C ||
		normalized.Opcodes[1] != 0xE1 {
		t.Fatalf("normalized subscription=%#v", normalized)
	}
	if _, err := normalizeSubscription(wsSubscription{
		Topics: []string{"events"}, Opcodes: []int{0xE1},
	}); err == nil {
		t.Fatal("opcode filter without opcodes topic was accepted")
	}
	if _, err := normalizeSubscription(wsSubscription{
		Topics: []string{"opcodes"}, Opcodes: []int{0},
	}); err == nil {
		t.Fatal("reserved opcode zero was accepted")
	}
	if got := requestCapability(
		"controller.opcode.exchange", json.RawMessage(`{"opcode":225}`),
	); got != capabilityBoard {
		t.Fatalf("opcode exchange capability=%q", got)
	}
}
