package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/shell"
)

func testHostInstancePaths(t *testing.T) hostInstancePaths {
	t.Helper()
	directory := t.TempDir()
	return hostInstancePaths{
		LockName: productidentity.StableAppID + ".Test." + strings.ReplaceAll(t.Name(), "/", ".") + "." + time.Now().Format("150405.000000000"),
		LockPath: filepath.Join(directory, "host-instance.lock"),
		RecordPath: filepath.Join(
			directory,
			"host-instance.json",
		),
	}
}

func TestHostInstanceClaimSerializesDifferentIPCConfigurations(t *testing.T) {
	paths := testHostInstancePaths(t)
	first, err := claimHostInstance(paths, "web")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := claimHostInstance(paths, "tui")
	if second != nil {
		_ = second.Close()
		t.Fatal("second host unexpectedly acquired the per-user claim")
	}
	if !errors.Is(err, errHostInstanceOwned) {
		t.Fatalf("second claim error=%v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := claimHostInstance(paths, "shell")
	if err != nil {
		t.Fatalf("claim was not released: %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestHostInstanceRecordResolvesAuthenticatedPrimaryAtDifferentEndpoint(t *testing.T) {
	paths := testHostInstancePaths(t)
	claim, err := claimHostInstance(paths, "web")
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()

	previous := currentPrimaryEndpoint()
	defer primaryEndpoint.Store(previous)
	configured := previous
	configured.Listen = "127.0.0.1:65530"
	configured.WebSocketPath = "/instance-test"
	configured.SocketIOPath = "/instance-socket/"
	configured.AuthToken = "0123456789abcdefghijklmnopqrstuv"
	primaryEndpoint.Store(configured)

	runtime := control.New(control.Options{})
	defer runtime.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAtWithIdentity(
		ctx,
		"127.0.0.1:0",
		runtime,
		shell.New(4),
		claim.identity,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := os.WriteFile(paths.RecordPath, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := claim.publish(server.listener, configured); err != nil {
		t.Fatal(err)
	}

	resolveContext, stopResolve := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopResolve()
	record, err := resolveHostInstance(resolveContext, paths)
	if err != nil {
		t.Fatal(err)
	}
	if record.Listen == configured.Listen || record.Listen != server.listener.Addr().String() {
		t.Fatalf("resolved listen=%q configured=%q live=%q", record.Listen, configured.Listen, server.listener.Addr())
	}
	if record.InstanceID != claim.identity.ID || record.DelegationToken != claim.identity.Token ||
		record.DelegationToken == configured.AuthToken || record.Surface != "web" {
		t.Fatalf("resolved record=%#v", record)
	}
}

func TestHostInstanceRecordRejectsLiveEndpointWithDifferentIdentity(t *testing.T) {
	paths := testHostInstancePaths(t)
	claim, err := claimHostInstance(paths, "web")
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()

	previous := currentPrimaryEndpoint()
	defer primaryEndpoint.Store(previous)
	configured := previous
	configured.AuthToken = "0123456789abcdefghijklmnopqrstuv"
	primaryEndpoint.Store(configured)

	runtime := control.New(control.Options{})
	defer runtime.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, err := startPrimaryIPCAtWithIdentity(
		ctx, "127.0.0.1:0", runtime, shell.New(4), claim.identity,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := claim.publish(server.listener, configured); err != nil {
		t.Fatal(err)
	}
	record, err := readHostInstanceRecord(paths.RecordPath)
	if err != nil {
		t.Fatal(err)
	}
	record.InstanceID = "ffffffffffffffffffffffffffffffff"
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.RecordPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	record, err = readHostInstanceRecord(paths.RecordPath)
	if err != nil {
		t.Fatal(err)
	}

	verifyContext, stopVerify := context.WithTimeout(context.Background(), time.Second)
	defer stopVerify()
	if err := verifyHostInstanceRecord(verifyContext, record); err == nil ||
		!strings.Contains(err.Error(), "does not match") {
		t.Fatalf("tampered record verification error=%v", err)
	}
}

func TestWebURLForAppActionRoutesOnlyKnownPages(t *testing.T) {
	value, err := webURLForAppAction(
		"http://127.0.0.1:8787/",
		hostui.AppAction{Kind: "app.page", Value: "events"},
	)
	if err != nil || value != "http://127.0.0.1:8787/#/events" {
		t.Fatalf("events URL=%q err=%v", value, err)
	}
	if _, err := webURLForAppAction(
		"http://127.0.0.1:8787/",
		hostui.AppAction{Kind: "app.page", Value: "unknown"},
	); err == nil {
		t.Fatal("unknown cold Web page was accepted")
	}
	value, err = webURLForAppAction(
		"http://127.0.0.1:8787/",
		hostui.AppAction{Kind: "command", Value: "status"},
	)
	if err != nil || value != "http://127.0.0.1:8787/" {
		t.Fatalf("command URL=%q err=%v", value, err)
	}
}
