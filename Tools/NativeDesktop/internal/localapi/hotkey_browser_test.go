package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/ipc"
)

type hotkeyBrowserEnvelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result"`
	Error  *ipc.Error      `json:"error"`
}

func requestBrowserHotkey(
	t *testing.T,
	running runningBrowserAPI,
	cookie *http.Cookie,
	csrf string,
	method string,
	path string,
	body string,
) (*http.Response, hotkeyBrowserEnvelope) {
	t.Helper()
	request, err := http.NewRequest(
		method,
		running.origin+path,
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		request.AddCookie(cookie)
		request.Header.Set("Origin", running.origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
	}
	if csrf != "" {
		request.Header.Set(csrfHeader, csrf)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var envelope hotkeyBrowserEnvelope
	if len(data) != 0 {
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode %s %s response %q: %v", method, path, data, err)
		}
	}
	return response, envelope
}

func decodeHotkeyResult[T any](
	t *testing.T,
	envelope hotkeyBrowserEnvelope,
) T {
	t.Helper()
	var result T
	if !envelope.OK {
		t.Fatalf("request failed: %#v", envelope.Error)
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		t.Fatalf("decode hotkey result: %v", err)
	}
	return result
}

func TestBrowserHotkeyCaptureLeaseLifecycleAndProbe(t *testing.T) {
	var (
		callbackMu sync.Mutex
		captures   []bool
		probes     []string
	)
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	running := startBrowserAPI(
		t,
		source,
		handler.Handle,
		func(options *Options) {
			options.HotkeyCapture = func(
				_ context.Context,
				enabled bool,
			) error {
				callbackMu.Lock()
				captures = append(captures, enabled)
				callbackMu.Unlock()
				return nil
			}
			options.HotkeyProbe = func(
				_ context.Context,
				chord string,
			) error {
				callbackMu.Lock()
				probes = append(probes, chord)
				callbackMu.Unlock()
				if chord == "Ctrl+Alt+X" {
					return errors.New("shortcut is already registered")
				}
				return nil
			}
		},
	)
	cookie, csrf := mintBrowserSession(t, running)

	response, envelope := requestBrowserHotkey(
		t,
		running,
		cookie,
		"",
		http.MethodGet,
		"/ui/bootstrap",
		"",
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d", response.StatusCode)
	}
	bootstrap := decodeHotkeyResult[bootstrapResult](t, envelope)
	if !bootstrap.Capabilities["hotkey_capture"] ||
		!bootstrap.Capabilities["hotkey_probe"] {
		t.Fatalf("hotkey capabilities = %#v", bootstrap.Capabilities)
	}

	response, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/capture",
		`{}`,
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("first capture status = %d, error = %#v", response.StatusCode, envelope.Error)
	}
	first := decodeHotkeyResult[hotkeyCaptureResult](t, envelope)
	if !validRandomToken(first.LeaseID) ||
		first.LeaseTTL != hotkeyCaptureLeaseTTL.Milliseconds() ||
		!first.ExpiresAt.After(time.Now()) {
		t.Fatalf("first capture result = %#v", first)
	}

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/capture",
		`{}`,
	)
	second := decodeHotkeyResult[hotkeyCaptureResult](t, envelope)
	if second.LeaseID == first.LeaseID {
		t.Fatal("two browser recorders received the same lease ID")
	}
	callbackMu.Lock()
	if len(captures) != 1 || !captures[0] {
		t.Fatalf("capture transitions after two leases = %#v", captures)
	}
	callbackMu.Unlock()

	unknownLease, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	response, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/probe",
		`{"lease_id":"`+unknownLease+`","chord":"Ctrl+K"}`,
	)
	if response.StatusCode != http.StatusNotFound ||
		envelope.Error == nil || envelope.Error.Code != "lease_not_found" {
		t.Fatalf("unknown lease probe = %d %#v", response.StatusCode, envelope)
	}

	response, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/probe",
		`{"lease_id":"`+first.LeaseID+`","chord":"Ctrl+F12"}`,
	)
	if response.StatusCode != http.StatusBadRequest ||
		envelope.Error == nil || envelope.Error.Code != "reserved_hotkey" {
		t.Fatalf("reserved F12 probe = %d %#v", response.StatusCode, envelope)
	}

	response, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/probe",
		`{"lease_id":"`+first.LeaseID+`","chord":" control + alt + k "}`,
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("available probe status = %d, error = %#v", response.StatusCode, envelope.Error)
	}
	available := decodeHotkeyResult[hotkeyProbeResult](t, envelope)
	if available.LeaseID != first.LeaseID ||
		available.Chord != "Ctrl+Alt+K" ||
		!available.Available ||
		available.Message != "" ||
		available.ExpiresAt.Before(first.ExpiresAt) {
		t.Fatalf("available probe result = %#v, first = %#v", available, first)
	}

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/probe",
		`{"lease_id":"`+first.LeaseID+`","chord":"Ctrl+Alt+X"}`,
	)
	unavailable := decodeHotkeyResult[hotkeyProbeResult](t, envelope)
	if unavailable.Available || unavailable.Message == "" ||
		unavailable.Chord != "Ctrl+Alt+X" {
		t.Fatalf("unavailable probe result = %#v", unavailable)
	}

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/capture",
		`{"lease_id":"`+first.LeaseID+`"}`,
	)
	renewed := decodeHotkeyResult[hotkeyCaptureResult](t, envelope)
	if renewed.LeaseID != first.LeaseID ||
		renewed.ExpiresAt.Before(available.ExpiresAt) {
		t.Fatalf("renewed capture result = %#v", renewed)
	}

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodDelete,
		"/ui/hotkeys/capture",
		`{"lease_id":"`+first.LeaseID+`"}`,
	)
	released := decodeHotkeyResult[hotkeyReleaseResult](t, envelope)
	if !released.Released {
		t.Fatal("first lease was not reported released")
	}
	callbackMu.Lock()
	if len(captures) != 1 {
		t.Fatalf("non-final release transitions = %#v", captures)
	}
	callbackMu.Unlock()

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodDelete,
		"/ui/hotkeys/capture",
		`{"lease_id":"`+second.LeaseID+`"}`,
	)
	_ = decodeHotkeyResult[hotkeyReleaseResult](t, envelope)

	_, envelope = requestBrowserHotkey(
		t,
		running,
		cookie,
		csrf,
		http.MethodPost,
		"/ui/hotkeys/capture",
		`{}`,
	)
	shutdownLease := decodeHotkeyResult[hotkeyCaptureResult](t, envelope)
	if shutdownLease.LeaseID == "" {
		t.Fatal("shutdown cleanup lease is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := running.server.Close(ctx); err != nil {
		t.Fatal(err)
	}
	callbackMu.Lock()
	defer callbackMu.Unlock()
	if len(captures) != 4 ||
		!captures[0] || captures[1] || !captures[2] || captures[3] {
		t.Fatalf("capture lifecycle transitions = %#v, want [true false true false]", captures)
	}
	if len(probes) != 2 ||
		probes[0] != "Ctrl+Alt+K" ||
		probes[1] != "Ctrl+Alt+X" {
		t.Fatalf("native probe calls = %#v", probes)
	}
}

func TestBrowserHotkeyRoutesAreSessionOnlyStrictAndBounded(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	running := startBrowserAPI(
		t,
		source,
		handler.Handle,
		func(options *Options) {
			options.HotkeyCapture = func(context.Context, bool) error {
				return nil
			}
			options.HotkeyProbe = func(context.Context, string) error {
				return nil
			}
		},
	)
	cookie, csrf := mintBrowserSession(t, running)

	request, err := http.NewRequest(
		http.MethodPost,
		running.origin+"/ui/hotkeys/capture",
		strings.NewReader(`{}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	bearerResponse, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	bearerResponse.Body.Close()
	if bearerResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("bearer capture status = %d, want 403", bearerResponse.StatusCode)
	}

	response, _ := requestBrowserHotkey(
		t,
		running,
		cookie,
		"",
		http.MethodPost,
		"/ui/hotkeys/capture",
		`{}`,
	)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want 403", response.StatusCode)
	}

	for _, test := range []struct {
		name   string
		method string
		body   string
		status int
		code   string
	}{
		{
			name:   "unknown field",
			method: http.MethodPost,
			body:   `{"unexpected":true}`,
			status: http.StatusBadRequest,
			code:   "invalid_json",
		},
		{
			name:   "non-object",
			method: http.MethodPost,
			body:   `null`,
			status: http.StatusBadRequest,
			code:   "invalid_json",
		},
		{
			name:   "oversized",
			method: http.MethodPost,
			body:   `{"lease_id":"` + strings.Repeat("x", 5<<10) + `"}`,
			status: http.StatusRequestEntityTooLarge,
			code:   "request_too_large",
		},
		{
			name:   "invalid release lease",
			method: http.MethodDelete,
			body:   `{"lease_id":"not-opaque"}`,
			status: http.StatusBadRequest,
			code:   "invalid_lease",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, envelope := requestBrowserHotkey(
				t,
				running,
				cookie,
				csrf,
				test.method,
				"/ui/hotkeys/capture",
				test.body,
			)
			if response.StatusCode != test.status ||
				envelope.Error == nil || envelope.Error.Code != test.code {
				t.Fatalf(
					"response = %d %#v, want %d/%s",
					response.StatusCode,
					envelope,
					test.status,
					test.code,
				)
			}
		})
	}

	response, _ = requestBrowserHotkey(
		t,
		running,
		cookie,
		"",
		http.MethodGet,
		"/ui/hotkeys/capture",
		"",
	)
	if response.StatusCode != http.StatusMethodNotAllowed ||
		response.Header.Get("Allow") != "POST, DELETE" {
		t.Fatalf(
			"GET capture = %d Allow %q",
			response.StatusCode,
			response.Header.Get("Allow"),
		)
	}
}

func TestBrowserHotkeyRoutesAdvertiseAndEnforceAvailability(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}

	t.Run("callbacks absent", func(t *testing.T) {
		running := startBrowserAPI(t, source, handler.Handle)
		cookie, csrf := mintBrowserSession(t, running)
		response, envelope := requestBrowserHotkey(
			t,
			running,
			cookie,
			csrf,
			http.MethodPost,
			"/ui/hotkeys/capture",
			`{}`,
		)
		if response.StatusCode != http.StatusServiceUnavailable ||
			envelope.Error == nil || envelope.Error.Code != "unavailable" {
			t.Fatalf("capture without callback = %d %#v", response.StatusCode, envelope)
		}
	})

	t.Run("probe absent", func(t *testing.T) {
		running := startBrowserAPI(
			t,
			source,
			handler.Handle,
			func(options *Options) {
				options.HotkeyCapture = func(context.Context, bool) error {
					return nil
				}
			},
		)
		cookie, csrf := mintBrowserSession(t, running)
		_, envelope := requestBrowserHotkey(
			t,
			running,
			cookie,
			csrf,
			http.MethodPost,
			"/ui/hotkeys/capture",
			`{}`,
		)
		lease := decodeHotkeyResult[hotkeyCaptureResult](t, envelope)
		response, envelope := requestBrowserHotkey(
			t,
			running,
			cookie,
			csrf,
			http.MethodPost,
			"/ui/hotkeys/probe",
			`{"lease_id":"`+lease.LeaseID+`","chord":"Ctrl+K"}`,
		)
		if response.StatusCode != http.StatusServiceUnavailable ||
			envelope.Error == nil || envelope.Error.Code != "unavailable" {
			t.Fatalf("probe without callback = %d %#v", response.StatusCode, envelope)
		}
	})
}
