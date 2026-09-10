package hostbridge

import (
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/ipcjson"
)

func TestPeerResponseEnvelopeSafetyAndEvolution(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","id":12,"result":null,"future":{"feature":true}}`,
		`{"jsonrpc":"2.0","id":"op-12","result":{"next_offset":4}}`,
		`{"jsonrpc":"2.0","id":12,"error":{"code":-32004,"message":"uncertain"}}`,
	} {
		if _, err := decodePeerResponse([]byte(raw)); err != nil {
			t.Fatalf("valid %s: %v", raw, err)
		}
	}
	for _, raw := range []string{
		`{"id":12,"result":true}`,
		`{"jsonrpc":"2.0","id":12,"result":true,"error":{"code":-1,"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":12}`,
		`{"jsonrpc":"2.0","id":null,"result":true}`,
		`{"jsonrpc":"2.0","id":{},"result":true}`,
		`{"jsonrpc":"2.0","id":12,"error":null}`,
		`{"jsonrpc":"2.0","id":12,"method":"controller.event","result":true}`,
	} {
		if _, err := decodePeerResponse([]byte(raw)); err == nil {
			t.Fatalf("unsafe envelope accepted: %s", raw)
		}
	}
}

func TestPeerMalformedMatchingAckResolvesAsUncertain(t *testing.T) {
	session := newPeerRPCSession(func(any) error { return nil })
	response := make(chan ipcjson.Response, 1)
	session.pending["12"] = response
	if !session.resolveRaw([]byte(`{"jsonrpc":"wrong","id":12,"result":{"state":"completed"}}`)) {
		t.Fatal("pending call was not resolved")
	}
	got := <-response
	if got.Error == nil || got.Error.Code != -32004 || got.Result != nil {
		t.Fatalf("malformed ACK became authoritative: %#v", got)
	}
	if session.resolveRaw([]byte(`{"jsonrpc":"2.0","id":12,"result":true}`)) {
		t.Fatal("settled call resolved twice")
	}
	if string(got.ID) != string(json.RawMessage("12")) {
		t.Fatalf("lost identity: %s", got.ID)
	}
}
