package control

import (
	"context"
	"errors"
	"testing"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

func TestRetainedFailedOpenCannotAdvertiseOrUseAuthentication(t *testing.T) {
	port := &observedWritePort{reconnectTestPort: newReconnectTestPort(), wrote: make(chan struct{})}
	session := link.NewForPort("TEST", port)
	runtime := New(Options{})
	t.Cleanup(func() { _ = runtime.Close() })
	if err := runtime.retainFailedOpen(link.OpenResult{Session: session, Port: ports.Info{Name: "TEST"}}, link.ErrSerialOpenPending); !errors.Is(err, link.ErrSerialOpenPending) {
		t.Fatal(err)
	}
	snapshot := runtime.Snapshot()
	if snapshot.Connected || snapshot.ConnectionPhase != "blocked" || snapshot.ConnectionState != "close_failed" || !snapshot.Paused {
		t.Fatalf("cleanup owner advertised as authenticated: %+v", snapshot)
	}
	if runtime.currentSession() != session {
		t.Fatal("cleanup ownership discarded")
	}
	if _, err := runtime.RefreshStatus(context.Background()); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("status reached quarantined session: %v", err)
	}
	if _, err := runtime.Request(context.Background(), native.OpPWMSet, []byte{0, 1, 0}, native.OpACK); !errors.Is(err, link.ErrClosed) {
		t.Fatalf("mutation reached quarantined session: %v", err)
	}
	select {
	case <-port.wrote:
		t.Fatal("quarantined transport received bytes")
	default:
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.currentSession() != nil {
		t.Fatal("cleanup owner retained after successful close")
	}
}
