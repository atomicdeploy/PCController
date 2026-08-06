//go:build windows

package nativeshell

import (
	"testing"
	"unicode/utf16"
	"unsafe"
)

func TestWin32StructureSizesMatchSDK(t *testing.T) {
	wantNotifyIconData := uintptr(956)
	wantWindowClass := uintptr(48)
	wantHighContrast := uintptr(12)
	wantIconInfo := uintptr(20)
	if unsafe.Sizeof(uintptr(0)) == 8 {
		wantNotifyIconData = 976
		wantWindowClass = 80
		wantHighContrast = 16
		wantIconInfo = 32
	}
	if got := unsafe.Sizeof(notifyIconData{}); got != wantNotifyIconData {
		t.Fatalf("NOTIFYICONDATAW size=%d; want %d", got, wantNotifyIconData)
	}
	if got := unsafe.Sizeof(windowClassEx{}); got != wantWindowClass {
		t.Fatalf("WNDCLASSEXW size=%d; want %d", got, wantWindowClass)
	}
	if got := unsafe.Sizeof(highContrast{}); got != wantHighContrast {
		t.Fatalf("HIGHCONTRASTW size=%d; want %d", got, wantHighContrast)
	}
	if got := unsafe.Sizeof(iconInfo{}); got != wantIconInfo {
		t.Fatalf("ICONINFO size=%d; want %d", got, wantIconInfo)
	}
}

func TestMenuItemsEqualDetectsStateChanges(t *testing.T) {
	offline := BuildMenu(State{ConnectionState: "offline"})
	copyOfOffline := append([]MenuItem(nil), offline...)
	if !menuItemsEqual(offline, copyOfOffline) {
		t.Fatal("equivalent cached menu models differ")
	}
	connected := BuildMenu(State{Connected: true, Port: "COM18"})
	if menuItemsEqual(offline, connected) {
		t.Fatal("offline and connected menu models compare equal")
	}
	copyOfOffline[0].Label = "changed"
	if menuItemsEqual(offline, copyOfOffline) {
		t.Fatal("changed status label was not detected")
	}
}

func TestIconResourceNamesAreDistinctAndStable(t *testing.T) {
	want := map[IconState]string{
		IconConnected:    "TRAY_CONNECTED",
		IconReconnecting: "TRAY_RECONNECTING",
		IconPaused:       "TRAY_PAUSED",
		IconOffline:      "TRAY_OFFLINE",
	}
	seen := make(map[string]bool, len(want))
	for state, resource := range want {
		if got := iconResourceName(state); got != resource {
			t.Fatalf("iconResourceName(%q)=%q; want %q", state, got, resource)
		}
		if seen[resource] {
			t.Fatalf("duplicate native icon resource %q", resource)
		}
		seen[resource] = true
	}
}

func TestNativeThemeStateRejectsReentrantAndRedundantApplication(t *testing.T) {
	var state nativeThemeState
	if !state.begin(true) {
		t.Fatal("initial dark theme application was rejected")
	}
	if state.begin(true) {
		t.Fatal("re-entrant theme application was accepted")
	}
	if state.requestPost() {
		t.Fatal("self-generated theme notification was posted while applying")
	}
	state.complete(true)
	if state.begin(true) {
		t.Fatal("unchanged theme mode was applied twice")
	}
	if !state.begin(false) {
		t.Fatal("light theme transition was rejected")
	}
	state.complete(false)
}

func TestNativeThemeStateCoalescesDeferredNotifications(t *testing.T) {
	var state nativeThemeState
	if !state.requestPost() {
		t.Fatal("first deferred notification was rejected")
	}
	if state.requestPost() {
		t.Fatal("duplicate deferred notification was not coalesced")
	}
	state.consumePost()
	if !state.requestPost() {
		t.Fatal("notification could not be posted after consuming the prior one")
	}
}

func TestPointFromPackedPreservesSignedCoordinates(t *testing.T) {
	x, y := int16(-12), int16(340)
	packed := uintptr(uint16(x)) | uintptr(uint16(y))<<16
	if got := pointFromPacked(packed); got != (winPoint{X: -12, Y: 340}) {
		t.Fatalf("pointFromPacked=%+v", got)
	}
}

func TestCopyUTF16TerminatesAndDoesNotSplitSurrogatePair(t *testing.T) {
	target := make([]uint16, 3)
	copyUTF16(target, "A😀B")
	if target[2] != 0 {
		t.Fatalf("copyUTF16 result=%#v is not terminated", target)
	}
	if got := string(utf16.Decode(target[:1])); got != "A" {
		t.Fatalf("copyUTF16 decoded=%q; want %q", got, "A")
	}
}

func TestDecodeNativeSystemEvent(t *testing.T) {
	tests := []struct {
		name    string
		message uint32
		wParam  uintptr
		want    SystemEventKind
		ok      bool
	}{
		{name: "session lock", message: wmWTSSessionChange, wParam: wtsSessionLock, want: SystemEventSessionLocked, ok: true},
		{name: "session unlock", message: wmWTSSessionChange, wParam: wtsSessionUnlock, want: SystemEventSessionUnlocked, ok: true},
		{name: "suspend", message: wmPowerBroadcast, wParam: pbtAPMSuspend, want: SystemEventPowerSuspending, ok: true},
		{name: "critical resume", message: wmPowerBroadcast, wParam: pbtAPMResumeCritical, want: SystemEventPowerResumed, ok: true},
		{name: "interactive resume", message: wmPowerBroadcast, wParam: pbtAPMResumeSuspend, want: SystemEventPowerResumed, ok: true},
		{name: "automatic resume", message: wmPowerBroadcast, wParam: pbtAPMResumeAutomatic, want: SystemEventPowerResumed, ok: true},
		{name: "unrelated session", message: wmWTSSessionChange, wParam: 1},
		{name: "unrelated message", message: wmTimer, wParam: trayTimerID},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event, ok := decodeNativeSystemEvent(test.message, test.wParam)
			if ok != test.ok || event.Kind != test.want {
				t.Fatalf("decodeNativeSystemEvent=(%q,%t); want (%q,%t)", event.Kind, ok, test.want, test.ok)
			}
		})
	}
}

func TestSystemEventEmitterDeduplicatesStatesAndKeepsTransitions(t *testing.T) {
	var received []SystemEvent
	emitter := systemEventEmitter{callback: func(event SystemEvent) {
		received = append(received, event)
	}}
	sequence := []struct {
		event SystemEvent
		want  bool
	}{
		{event: SystemEvent{Kind: SystemEventSessionLocked}, want: true},
		{event: SystemEvent{Kind: SystemEventSessionLocked}, want: false},
		{event: SystemEvent{Kind: SystemEventSessionUnlocked}, want: true},
		{event: SystemEvent{Kind: SystemEventSessionLocked}, want: true},
		{event: SystemEvent{Kind: SystemEventPowerResumed}, want: true},
		{event: SystemEvent{Kind: SystemEventPowerResumed}, want: false},
		{event: SystemEvent{Kind: "unsupported"}, want: false},
	}
	for index, step := range sequence {
		if got := emitter.emit(step.event); got != step.want {
			t.Fatalf("emit step %d=%t; want %t", index, got, step.want)
		}
	}
	if len(received) != 4 {
		t.Fatalf("received %d events; want 4: %#v", len(received), received)
	}
}

func TestNetworkChangeStateSeedsAndDetectsAddressOnlyChanges(t *testing.T) {
	var state networkChangeState
	initial := networkSnapshot{signature: "one", interfaceCount: 2, addressCount: 3}
	if event, changed := state.observe(initial); changed || event.Kind != "" {
		t.Fatalf("initial observation emitted %+v", event)
	}
	if event, changed := state.observe(initial); changed || event.Kind != "" {
		t.Fatalf("duplicate observation emitted %+v", event)
	}
	changedSnapshot := networkSnapshot{signature: "two", interfaceCount: 2, addressCount: 3}
	event, changed := state.observe(changedSnapshot)
	if !changed || event.Kind != SystemEventNetworkChanged {
		t.Fatalf("changed observation=(%+v,%t)", event, changed)
	}
	if event.NetworkSignature != "two" || event.InterfaceCount != 2 || event.AddressCount != 3 {
		t.Fatalf("network change payload=%+v", event)
	}
	if _, changed := state.observe(changedSnapshot); changed {
		t.Fatal("duplicate changed snapshot emitted twice")
	}
}
