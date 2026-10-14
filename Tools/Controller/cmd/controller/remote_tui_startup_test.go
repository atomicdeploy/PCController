package main

import (
	"context"
	"encoding/json"
	"errors"
	"pccontroller.local/controller/internal/appconfig"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestRemoteTUIBootstrapIndependentReadsOverlap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var mu sync.Mutex
	arrived := 0
	ready := make(chan struct{})
	barrier := func(ctx context.Context) error {
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(ready)
		}
		mu.Unlock()
		select {
		case <-ready:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	engine := shell.New(10)
	initial, at, commands, err := remoteTUIBootstrap(ctx, func(ctx context.Context) (control.Snapshot, error) {
		return control.Snapshot{Connected: true}, barrier(ctx)
	}, func(ctx context.Context) (*shell.Engine, error) { return engine, barrier(ctx) })
	if err != nil || !initial.Connected || at.IsZero() || commands != engine {
		t.Fatalf("bootstrap did not run both reads concurrently: %v", err)
	}
}

func TestRemoteTUIBootstrapCancellationAndFailuresRemainVisible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, _, err := remoteTUIBootstrap(ctx, func(ctx context.Context) (control.Snapshot, error) {
		<-ctx.Done()
		return control.Snapshot{}, ctx.Err()
	}, func(ctx context.Context) (*shell.Engine, error) { <-ctx.Done(); return nil, ctx.Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was hidden: %v", err)
	}
	want := errors.New("catalog unavailable")
	_, _, _, err = remoteTUIBootstrap(context.Background(), func(context.Context) (control.Snapshot, error) { return control.Snapshot{}, nil }, func(context.Context) (*shell.Engine, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("bootstrap failure was hidden: %v", err)
	}
}

func TestRemoteTUIStartupPreferencesReadTogetherWithoutWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var mu sync.Mutex
	arrived := 0
	ready := make(chan struct{})
	client := &remoteTUIIPC{}
	client.callFn = func(ctx context.Context, method string, _ any, target any) error {
		var value any
		switch method {
		case "controller.ui.config.get":
			value = remoteUISettingsWire{AppTitle: "Cinema"}
		case "controller.rf.presentation":
			config := appconfig.DefaultRFConfig()
			config.Learning.TimeoutSeconds = 45
			value = remoteRFPresentationWire{Config: config}
		default:
			return errors.New("unexpected startup mutation or request")
		}
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(ready)
		}
		mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return ctx.Err()
		}
		encoded, _ := json.Marshal(value)
		return json.Unmarshal(encoded, target)
	}
	ui, known, rf := remoteTUIStartupPreferences(ctx, client)
	if !known || ui.AppTitle != "Cinema" || rf.Learning.TimeoutSeconds != 45 {
		t.Fatalf("parallel preferences missing: known=%v ui=%s timeout=%d", known, ui.AppTitle, rf.Learning.TimeoutSeconds)
	}
}
