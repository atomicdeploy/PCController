package main

import (
	"context"
	"io"
	"pccontroller.local/controller/internal/hostui"
	"testing"
	"time"
)

func TestOSDCLIRejectsInvalidArgumentsBeforeAnyPresentation(t *testing.T) {
	if err := runOSD([]string{"show", "--text", "x", "--sound", "https://remote/sound.mp3", "--flash-taskbar"}, io.Discard, io.Discard); err == nil {
		t.Fatal("arbitrary audio source accepted")
	}
	for _, args := range [][]string{{}, {"wrong"}, {"show", "--duration", "0s", "--text", "x"}, {"show", "--duration", "3m", "--text", "x"}, {"show", "--text", "x", "--icon", "https://remote/image.png"}, {"show", "--text", "x", "--action", "Run=file:///unsafe"}, {"status", "--standalone"}, {"show", "--action", "not-an-action"}, {"show", "--text", ""}} {
		if err := runOSD(args, io.Discard, io.Discard); err == nil {
			t.Fatal(args)
		}
	}
}

func TestOSDWaitReportsChoiceReplacementAndCancellation(t *testing.T) {
	initial := hostui.OSDStatus{ID: "one", Accepted: 3, Active: true}
	for _, tc := range []struct {
		result hostui.OSDStatus
		reason string
	}{
		{hostui.OSDStatus{ID: "one", Accepted: 3, Dismissal: "action", LastActionID: "yes"}, "action"},
		{hostui.OSDStatus{ID: "one", Accepted: 3, Dismissal: "expired"}, "expired"},
		{hostui.OSDStatus{ID: "one", Accepted: 4, Active: true}, "replaced"},
		{hostui.OSDStatus{ID: "two", Accepted: 4, Active: true}, "replaced"},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		result, err := waitForOSD(ctx, initial, func(context.Context) (hostui.OSDStatus, error) { return tc.result, nil })
		cancel()
		if err != nil || result.Active || result.Dismissal != tc.reason {
			t.Fatal(result, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := waitForOSD(ctx, initial, func(context.Context) (hostui.OSDStatus, error) {
		t.Fatal("read after cancellation")
		return initial, nil
	}); err == nil {
		t.Fatal("cancellation was ignored")
	}
	var actions osdActions
	if err := (osdButtons{actions: &actions}).Set("yes=Continue"); err != nil || actions[0].ID != "yes" || actions[0].URI != "" {
		t.Fatal(actions, err)
	}
	if err := runOSD([]string{"status", "--wait"}, io.Discard, io.Discard); err == nil {
		t.Fatal("status accepted --wait")
	}
}
