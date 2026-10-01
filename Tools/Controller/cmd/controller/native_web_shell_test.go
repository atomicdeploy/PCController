package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/nativeshell"
)

type testNativeShell struct{}

func (*testNativeShell) Close() error { return nil }

func TestPrimaryNativeShellOwnsDefaultTrayAndHonorsExplicitOptOut(t *testing.T) {
	original := startNativeShell
	t.Cleanup(func() { startNativeShell = original })

	called := 0
	want := &testNativeShell{}
	startNativeShell = func(
		context.Context, func(), string,
		*control.Runtime, *appconfig.Store, *primaryIPC,
	) (nativeshell.Shell, error) {
		called++
		return want, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got, err := startPrimaryNativeShell(ctx, cancel, "http://127.0.0.1:8787/", nil, nil, nil, false)
	if err != nil || got != want || called != 1 {
		t.Fatalf("enabled primary shell=(%v,%v), calls=%d", got, err, called)
	}
	got, err = startPrimaryNativeShell(ctx, cancel, "", nil, nil, nil, true)
	if err != nil || got != nil || called != 1 {
		t.Fatalf("disabled primary shell=(%v,%v), calls=%d", got, err, called)
	}
}

func TestTUIPrimaryCompositionOwnsTrayWithoutCancelingIntegrationContext(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve native shell contract test path")
	}
	parsed, err := parser.ParseFile(
		token.NewFileSet(), filepath.Join(filepath.Dir(testFile), "main.go"), nil, 0,
	)
	if err != nil {
		t.Fatal(err)
	}
	var function *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		candidate, candidateOK := declaration.(*ast.FuncDecl)
		if candidateOK && candidate.Name.Name == "runTUIWithInitialAction" {
			function = candidate
			break
		}
	}
	if function == nil {
		t.Fatal("runTUIWithInitialAction composition root was not found")
	}

	primaryStart := token.NoPos
	shellStarts := 0
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, callOK := node.(*ast.CallExpr)
		if !callOK {
			return true
		}
		name, nameOK := call.Fun.(*ast.Ident)
		if !nameOK {
			return true
		}
		if name.Name == "startPrimaryIPCClaimed" {
			primaryStart = call.Pos()
			return true
		}
		if name.Name != "startPrimaryNativeShell" {
			return true
		}
		shellStarts++
		if primaryStart == token.NoPos || call.Pos() <= primaryStart {
			t.Error("native shell must start only after this process becomes the local primary")
		}
		if len(call.Args) != 7 {
			t.Fatalf("startPrimaryNativeShell has %d arguments; want 7", len(call.Args))
		}
		exit, exitOK := call.Args[1].(*ast.SelectorExpr)
		if !exitOK {
			t.Error("TUI tray Exit must be a primary method")
			return true
		}
		owner, ownerOK := exit.X.(*ast.Ident)
		if !ownerOK || owner.Name != "primary" || exit.Sel.Name != "RequestQuit" {
			t.Error("TUI tray Exit must signal primary.RequestQuit without canceling integration context")
		}
		disabled, disabledOK := call.Args[6].(*ast.StarExpr)
		if !disabledOK {
			t.Error("TUI tray startup must receive the explicit opt-out value")
			return true
		}
		disabledName, disabledNameOK := disabled.X.(*ast.Ident)
		if !disabledNameOK || disabledName.Name != "noTray" {
			t.Error("TUI tray startup must forward the explicit --no-tray flag")
		}
		return true
	})
	if shellStarts != 1 {
		t.Fatalf("runTUIWithInitialAction starts %d native shells; want exactly one local-primary startup", shellStarts)
	}
}

func TestNativeWebPageURL(t *testing.T) {
	got, err := nativeWebPageURL("http://127.0.0.1:8787/", " Settings ")
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://127.0.0.1:8787/#/settings"; got != want {
		t.Fatalf("nativeWebPageURL=%q; want %q", got, want)
	}
	for _, page := range []string{"", "terminal", "../settings"} {
		if value, err := nativeWebPageURL("http://127.0.0.1:8787/", page); err == nil {
			t.Errorf("nativeWebPageURL page %q unexpectedly returned %q", page, value)
		}
	}
}

func TestNativeWebPageURLRejectsNonHTTPURL(t *testing.T) {
	for _, value := range []string{"", "file:///tmp/controller", "javascript:alert(1)"} {
		if got, err := nativeWebPageURL(value, "dashboard"); err == nil {
			t.Errorf("nativeWebPageURL(%q) unexpectedly returned %q", value, got)
		}
	}
}

func TestNativeSystemRuntimeEventUsesTypedKindsAndStates(t *testing.T) {
	tests := []struct {
		input     nativeshell.SystemEvent
		wantKind  string
		wantState string
	}{
		{input: nativeshell.SystemEvent{Kind: nativeshell.SystemEventSessionLocked}, wantKind: "host.session.locked", wantState: "locked"},
		{input: nativeshell.SystemEvent{Kind: nativeshell.SystemEventSessionUnlocked}, wantKind: "host.session.unlocked", wantState: "unlocked"},
		{input: nativeshell.SystemEvent{Kind: nativeshell.SystemEventPowerSuspending}, wantKind: "host.power.suspending", wantState: "suspending"},
		{input: nativeshell.SystemEvent{Kind: nativeshell.SystemEventPowerResumed}, wantKind: "host.power.resumed", wantState: "resumed"},
		{
			input: nativeshell.SystemEvent{
				Kind: nativeshell.SystemEventNetworkChanged, NetworkSignature: "abc123",
				InterfaceCount: 3, AddressCount: 5,
			},
			wantKind: "host.network.changed", wantState: "changed",
		},
	}
	for _, test := range tests {
		event, ok := nativeSystemRuntimeEvent(test.input)
		if !ok {
			t.Fatalf("nativeSystemRuntimeEvent(%q) was rejected", test.input.Kind)
		}
		if event.Kind != test.wantKind || event.State != test.wantState ||
			event.Lifecycle != "changed" || event.Source != "host.native" {
			t.Errorf("nativeSystemRuntimeEvent(%q)=%+v", test.input.Kind, event)
		}
		if test.input.Kind == nativeshell.SystemEventNetworkChanged {
			if event.Metadata["signature"] != "abc123" ||
				event.Metadata["interface_count"] != "3" ||
				event.Metadata["address_count"] != "5" {
				t.Errorf("network metadata=%v", event.Metadata)
			}
		}
	}
	if event, ok := nativeSystemRuntimeEvent(nativeshell.SystemEvent{Kind: "unsupported"}); ok || event.Kind != "" {
		t.Fatalf("unsupported native event=(%+v,%t)", event, ok)
	}
}

func TestNativeLifecycleModeSeparatesSafetyFromReconciliation(t *testing.T) {
	for _, test := range []struct {
		kind              nativeshell.SystemEventKind
		safety, reconcile bool
	}{
		{nativeshell.SystemEventSessionLocked, true, false},
		{nativeshell.SystemEventPowerSuspending, true, false},
		{nativeshell.SystemEventSessionUnlocked, false, true},
		{nativeshell.SystemEventPowerResumed, false, true},
		{nativeshell.SystemEventNetworkChanged, false, true},
		{"unknown", false, false},
	} {
		safety, reconcile := nativeLifecycleMode(test.kind)
		if safety != test.safety || reconcile != test.reconcile {
			t.Errorf("nativeLifecycleMode(%q)=(%t,%t), want (%t,%t)", test.kind, safety, reconcile, test.safety, test.reconcile)
		}
	}
}
