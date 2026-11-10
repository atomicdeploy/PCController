//go:build windows && !no_native_gui

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/tui"
)

func TestDesktopTransportDoesNotInventBoardConnection(t *testing.T) {
	ack := tui.RemoteLiveUpdate{ConnectionChange: true, Connected: true}
	offline := control.Snapshot{Paused: true, ConnectionState: "disconnected"}
	got := desktopTransportState(offline, ack)
	if got.Connected || !got.Paused || got.ConnectionState != "disconnected" {
		t.Fatalf("IPC acknowledgement invented a board connection: %+v", got)
	}
	online := control.Snapshot{Connected: true, ConnectionGeneration: 9}
	if !desktopTransportState(online, ack).Connected {
		t.Fatal("healthy IPC must retain an authoritative connected snapshot")
	}
	lost := desktopTransportState(online, tui.RemoteLiveUpdate{ConnectionChange: true})
	if lost.Connected || lost.ConnectionGeneration != 9 {
		t.Fatal("transport loss must revoke controls without changing board identity")
	}
	if desktopTransportState(lost, ack).Connected {
		t.Fatal("reconnected IPC must wait for the authoritative board snapshot")
	}
}

func TestDesktopOwnedCoreCompletionCancelsGUI(t *testing.T) {
	for _, coreErr := range []error{nil, context.Canceled, errors.New("listener failed")} {
		ctx, cancel := context.WithCancel(context.Background())
		completed := make(chan error, 1)
		completed <- coreErr
		var log bytes.Buffer
		monitorNativeCore(completed, cancel, &log)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("GUI outlived its owned core")
		}
		wantFailure := coreErr != nil && !errors.Is(coreErr, context.Canceled)
		if strings.Contains(log.String(), "listener failed") != wantFailure {
			t.Fatalf("unexpected core failure report: %q", log.String())
		}
	}
}
