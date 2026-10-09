package main

import (
	"testing"

	"pccontroller.local/controller/internal/nativeshell"
)

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
