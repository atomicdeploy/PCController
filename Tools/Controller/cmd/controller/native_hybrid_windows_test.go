//go:build windows && !no_native_gui

package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
)

func TestNativeHybridRejectsIncoherentModesBeforeStartup(t *testing.T) {
	store, err := appconfig.Open(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"--hidden"}, {"--tui", "--preview"}} {
		var output bytes.Buffer
		if err := runNativeGUI(args, &output, &output, store); err == nil {
			t.Fatalf("incoherent flags accepted: %v", args)
		}
	}
	if _, err := prepareNativeHybridConsole(&bytes.Buffer{}); err == nil {
		t.Fatal("redirected output must not be silently rebound")
	}
}

func TestNativeHybridWindowRequestAcknowledgementAndNoReplay(t *testing.T) {
	requests := make(chan desktop.WindowRequest, 2)
	want := errors.New("window is closing")
	go func() { request := <-requests; request.Result <- want }()
	if err := requestNativeWindow(context.Background(), requests, "toggle"); !errors.Is(err, want) {
		t.Fatalf("window acknowledgement = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := requestNativeWindow(ctx, requests, "toggle"); err == nil || !strings.Contains(err.Error(), "outcome unknown") {
		t.Fatalf("unacknowledged toggle = %v", err)
	}
	if len(requests) != 1 {
		t.Fatal("a toggle was retried or lost")
	}
}
