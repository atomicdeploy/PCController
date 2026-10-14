package control

import (
	"context"
	"testing"
)

func TestRFObservedAddRejectsUnobservedAndDisconnectedCodes(t *testing.T) {
	runtime := New(Options{})
	if runtime.AddObservedRF(context.Background(), 42, 24, 1) == nil {
		t.Fatal("unobserved waveform accepted")
	}
	runtime.PublishStructuredEvent(rfReceptionFixture(42, 24, 1))
	if runtime.AddObservedRF(context.Background(), 42, 24, 1) == nil {
		t.Fatal("disconnected board accepted")
	}
	if runtime.AddObservedRF(context.Background(), 42, 16, 1) == nil {
		t.Fatal("partial waveform accepted")
	}
}
