package comserver

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"rayanlamp.local/app/internal/ipc"
)

func TestResolveName(t *testing.T) {
	tests := map[string]int32{
		"On":        dispIDOn,
		"off":       dispIDOff,
		" TOGGLE ":  dispIDToggle,
		"Show":      dispIDShow,
		"Hide":      dispIDHide,
		"Reconnect": dispIDReconnect,
		"Notify":    dispIDNotify,
		"GetState":  dispIDGetState,
		"State":     dispIDState,
		"message":   dispIDMessage,
	}
	for name, expected := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := resolveName(name)
			if !ok || got != expected {
				t.Fatalf("resolveName(%q) = %d, %t; want %d, true", name, got, ok, expected)
			}
		})
	}
	if id, ok := resolveName("missing"); ok || id != -1 {
		t.Fatalf("unknown name resolved to %d, %t", id, ok)
	}
}

func TestBridgeDispatchMapping(t *testing.T) {
	tests := []struct {
		name       string
		dispID     int32
		flags      uint16
		args       []any
		wantMethod string
		wantParams string
	}{
		{name: "on", dispID: dispIDOn, flags: dispatchMethod, wantMethod: "lamp.on"},
		{name: "off", dispID: dispIDOff, flags: dispatchMethod, wantMethod: "lamp.off"},
		{name: "toggle", dispID: dispIDToggle, flags: dispatchMethod, wantMethod: "lamp.toggle"},
		{name: "show", dispID: dispIDShow, flags: dispatchMethod, wantMethod: "window.show"},
		{name: "hide", dispID: dispIDHide, flags: dispatchMethod, wantMethod: "window.hide"},
		{name: "reconnect", dispID: dispIDReconnect, flags: dispatchMethod, wantMethod: "reconnect"},
		{
			name:       "notify",
			dispID:     dispIDNotify,
			flags:      dispatchMethod,
			args:       []any{"Maintenance complete"},
			wantMethod: "notifications.show",
			wantParams: `{"message":"Maintenance complete"}`,
		},
		{name: "get state", dispID: dispIDGetState, flags: dispatchMethod, wantMethod: "status"},
		{name: "state property", dispID: dispIDState, flags: dispatchPropertyGet, wantMethod: "status"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var captured ipc.Request
			handler := func(_ context.Context, request ipc.Request) (any, error) {
				captured = request
				if request.Method == "status" {
					return map[string]any{"state": "ON", "websocket_online": true}, nil
				}
				return "ok", nil
			}
			automationBridge, err := newBridge(handler)
			if err != nil {
				t.Fatal(err)
			}
			result, err := automationBridge.invoke(context.Background(), test.dispID, test.flags, test.args)
			if err != nil {
				t.Fatalf("invoke: %v", err)
			}
			if captured.ProtocolVersion != ipc.ProtocolVersion || captured.ID == "" {
				t.Fatalf("invalid IPC envelope: %#v", captured)
			}
			if captured.Method != test.wantMethod {
				t.Errorf("method = %q, want %q", captured.Method, test.wantMethod)
			}
			assertDispatchJSON(t, captured.Params, test.wantParams)
			if test.wantMethod == "status" && result != "on" {
				t.Errorf("state result = %#v, want on", result)
			}
		})
	}
}

func TestAutomationWindowMemberDispIDsRemainStable(t *testing.T) {
	if dispIDShow != 4 {
		t.Fatalf("Show DISPID = %d, want stable value 4", dispIDShow)
	}
	if dispIDHide != 9 {
		t.Fatalf("Hide DISPID = %d, want appended value 9", dispIDHide)
	}
}

func TestBridgeRejectsInvalidInvocations(t *testing.T) {
	automationBridge, err := newBridge(func(context.Context, ipc.Request) (any, error) {
		t.Fatal("handler called for invalid invocation")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		dispID int32
		flags  uint16
		args   []any
		want   error
	}{
		{name: "unknown member", dispID: 999, flags: dispatchMethod, want: errUnknownMember},
		{name: "method as property", dispID: dispIDOn, flags: dispatchPropertyGet, want: errMemberAccess},
		{name: "write read-only property", dispID: dispIDState, flags: dispatchPropertyPut, want: errMemberAccess},
		{name: "extra parameter", dispID: dispIDToggle, flags: dispatchMethod, args: []any{"extra"}, want: errBadParameterCount},
		{name: "missing notification", dispID: dispIDNotify, flags: dispatchMethod, want: errBadParameterCount},
		{name: "wrong notification type", dispID: dispIDNotify, flags: dispatchMethod, args: []any{42}, want: errTypeMismatch},
		{name: "blank notification", dispID: dispIDNotify, flags: dispatchMethod, args: []any{" "}, want: errTypeMismatch},
		{name: "oversized notification", dispID: dispIDNotify, flags: dispatchMethod, args: []any{strings.Repeat("x", maxNotifyRunes+1)}, want: errTypeMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := automationBridge.invoke(context.Background(), test.dispID, test.flags, test.args)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestAutomationState(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "direct on", value: "HIGH", want: "on"},
		{name: "direct off", value: json.RawMessage(`"false"`), want: "off"},
		{name: "object", value: map[string]any{"state": "on"}, want: "on"},
		{name: "numeric object off", value: json.RawMessage(`{"state":1}`), want: "off"},
		{name: "numeric object on", value: json.RawMessage(`{"state":2}`), want: "on"},
		{name: "missing", value: map[string]any{"connected": true}, want: "unknown"},
		{name: "nil", value: nil, want: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := automationState(test.value); got != test.want {
				t.Fatalf("automationState(%#v) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestRegistrationCommand(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "Rayan Lamp.exe")
	absolute, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	command, err := RegistrationCommand(executable)
	if err != nil {
		t.Fatal(err)
	}
	want := `"` + filepath.Clean(absolute) + `" /com-server`
	if command != want {
		t.Fatalf("command = %q, want %q", command, want)
	}

	for _, invalid := range []string{"", "  ", "bad\"path.exe", "bad\npath.exe", "bad\x00path.exe"} {
		if command, err := RegistrationCommand(invalid); err == nil {
			t.Fatalf("RegistrationCommand(%q) = %q, want error", invalid, command)
		}
	}
}

func TestNewBridgeRejectsNilHandler(t *testing.T) {
	if _, err := newBridge(nil); err == nil {
		t.Fatal("nil handler accepted")
	}
}

func assertDispatchJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("params = %s, want none", got)
		}
		return
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode params: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatal(err)
	}
	gotData, _ := json.Marshal(gotValue)
	wantData, _ := json.Marshal(wantValue)
	if string(gotData) != string(wantData) {
		t.Fatalf("params = %s, want %s", gotData, wantData)
	}
}
