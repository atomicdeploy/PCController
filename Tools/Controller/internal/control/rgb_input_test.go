package control

import (
	"pccontroller.local/controller/internal/native"
	"testing"
)

func TestRGBInputBoardAdmission(t *testing.T) {
	state := Snapshot{Connected: true, ConnectionGeneration: 7, Hello: native.Hello{Capabilities: native.CapabilityAddressableLED}}
	if err := checkRGBInputBoard(state, 7); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Snapshot){
		func(s *Snapshot) { s.Connected = false },
		func(s *Snapshot) { s.ConnectionGeneration++ },
		func(s *Snapshot) { s.Paused = true },
		func(s *Snapshot) { s.Programming.Active = true },
		func(s *Snapshot) { s.EmergencyStop.Active = true },
		func(s *Snapshot) { s.Hello.Capabilities = 0 },
	} {
		copy := state
		change(&copy)
		if err := checkRGBInputBoard(copy, 7); err == nil {
			t.Fatal("unsafe input admitted")
		}
	}
}
