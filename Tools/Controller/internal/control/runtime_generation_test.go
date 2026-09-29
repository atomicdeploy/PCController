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
