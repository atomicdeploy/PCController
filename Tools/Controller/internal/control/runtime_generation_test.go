package control

import (
	"context"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func TestRequestAtGenerationRejectsStaleBeforeTransport(t *testing.T) {
	runtime := New(Options{})
	runtime.session = &link.Session{}
	runtime.generation = 7
	_, err := runtime.requestAtGeneration(
		context.Background(), 6, native.OpStatus, nil, native.OpStatus,
	)
	if err == nil || !strings.Contains(err.Error(), "generation 6 is no longer active") {
		t.Fatalf("stale generation reached transport: %v", err)
	}
}

func TestObserveAtGenerationRejectsReplacementState(t *testing.T) {
	runtime := New(Options{})
	runtime.session = &link.Session{}
	runtime.generation = 8
	settings := native.DefaultSettings()
	payload, err := settings.Payload()
	if err != nil {
		t.Fatal(err)
	}
	if _, observed := runtime.observeAtGeneration(
		native.Frame{Opcode: native.OpSettings, Payload: payload}, 7,
	); observed {
		t.Fatal("stale response mutated replacement generation")
	}
	if runtime.Snapshot().HaveSettings {
		t.Fatal("stale settings became live replacement-board state")
	}
}

func TestSettingsObservationPublishesPersistedBoardName(t *testing.T) {
	runtime := New(Options{})
	settings := native.DefaultSettings()
	setPayload, err := native.SettingsWithBoardNamePayload(settings, "CAFE-01")
	if err != nil {
		t.Fatal(err)
	}
	response := append([]byte{}, setPayload[:15]...)
	response = append(response, 1, 1, byte(len("CAFE-01")))
	response = append(response, "CAFE-01"...)
	runtime.observe(native.Frame{Opcode: native.OpSettings, Payload: response})

	snapshot := runtime.Snapshot()
	if !snapshot.HaveSettings || !snapshot.HaveBoardName ||
		snapshot.BoardName.Name != "CAFE-01" || !snapshot.BoardName.Persisted {
		t.Fatalf("settings observation did not publish board name: %#v", snapshot)
	}
}
