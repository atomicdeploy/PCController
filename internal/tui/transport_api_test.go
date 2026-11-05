package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
)

const transportTestToken = "tui-local-api-test-token-that-must-stay-secret"

func TestAPITransportCommandAndConfigCapabilities(t *testing.T) {
	t.Parallel()

	type observedRequest struct {
		method string
		path   string
		body   string
	}
	var (
		mu       sync.Mutex
		observed []observedRequest
		cfg      = config.Default()
	)
	cfg.Lamp.BaseURL = "http://example.local"
	server := httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+transportTestToken {
			t.Errorf("authorization = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		mu.Lock()
		observed = append(observed, observedRequest{
			method: r.Method,
			path:   r.URL.RequestURI(),
			body:   string(body),
		})
		if r.Method == http.MethodPatch && r.URL.Path == "/config" {
			cfg.UI.AlwaysOnTop = true
		}
		current := cfg
		mu.Unlock()

		result := any(map[string]bool{"accepted": true})
		if r.URL.Path == "/config" {
			result = current
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": result,
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()

	tests := []struct {
		method     string
		params     any
		wantMethod string
		wantPath   string
		wantBody   string
	}{
		{"ping", nil, http.MethodGet, "/ping", ""},
		{"show", nil, http.MethodPost, "/window/show", ""},
		{"settings.show", nil, http.MethodPost, "/settings/show", ""},
		{"diagnostics.show", nil, http.MethodPost, "/diagnostics/show", ""},
		{"dashboard.web", nil, http.MethodPost, "/web/show", ""},
		{
			"diagnostics.fetch",
			map[string]string{"endpoint": "/info"},
			http.MethodGet,
			"/diagnostics?endpoint=%2Finfo",
			"",
		},
		{
			"terminal.read",
			map[string]any{"after": uint64(3), "limit": 128, "wait_ms": 250},
			http.MethodGet,
			"/terminal/output?after=3&limit=128&wait_ms=250",
			"",
		},
		{"device.config.get", nil, http.MethodGet, "/device/config", ""},
		{
			"device.config.refresh",
			nil,
			http.MethodPost,
			"/device/config/refresh",
			"",
		},
		{
			"integration.registration.status",
			nil,
			http.MethodGet,
			"/integration/registration",
			"",
		},
		{
			"integration.shell.register",
			nil,
			http.MethodPost,
			"/integration/registration",
			`{"method":"integration.shell.register"}`,
		},
		{
			"integration.com.unregister",
			nil,
			http.MethodPost,
			"/integration/registration",
			`{"method":"integration.com.unregister"}`,
		},
		{
			"notify",
			map[string]string{"title": "Lamp", "message": "Ready"},
			http.MethodPost,
			"/notifications",
			`{"message":"Ready","title":"Lamp"}`,
		},
		{"audio.play", nil, http.MethodPost, "/audio/play", ""},
		{
			"audio.play",
			map[string]any{"cue": "off", "count": 2},
			http.MethodPost,
			"/audio/play",
			`{"count":2,"cue":"off"}`,
		},
		{
			"audio.import",
			map[string]string{"path": `C:\Sounds\custom.wav`},
			http.MethodPost,
			"/audio/import",
			`{"path":"C:\\Sounds\\custom.wav"}`,
		},
		{"exit", nil, http.MethodPost, "/quit", ""},
	}
	for _, test := range tests {
		if _, err := transport.ExecuteCommand(
			context.Background(),
			test.method,
			test.params,
		); err != nil {
			t.Fatalf("ExecuteCommand(%q): %v", test.method, err)
		}
		mu.Lock()
		got := observed[len(observed)-1]
		mu.Unlock()
		if got.method != test.wantMethod ||
			got.path != test.wantPath ||
			got.body != test.wantBody {
			t.Errorf(
				"ExecuteCommand(%q) request = %#v, want method=%q path=%q body=%q",
				test.method,
				got,
				test.wantMethod,
				test.wantPath,
				test.wantBody,
			)
		}
	}

	loaded, err := transport.GetConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lamp.BaseURL != cfg.Lamp.BaseURL {
		t.Errorf("GetConfig base URL = %q", loaded.Lamp.BaseURL)
	}
	updated, err := transport.PatchConfig(
		context.Background(),
		map[string]any{"ui": map[string]bool{"always_on_top": true}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UI.AlwaysOnTop {
		t.Fatal("PatchConfig did not decode the normalized configuration")
	}
	mu.Lock()
	lastTwo := append([]observedRequest(nil), observed[len(observed)-2:]...)
	mu.Unlock()
	if lastTwo[0].method != http.MethodGet || lastTwo[0].path != "/config" {
		t.Errorf("GetConfig request = %#v", lastTwo[0])
	}
	if lastTwo[1].method != http.MethodPatch ||
		lastTwo[1].path != "/config" ||
		lastTwo[1].body != `{"ui":{"always_on_top":true}}` {
		t.Errorf("PatchConfig request = %#v", lastTwo[1])
	}

	mu.Lock()
	before := len(observed)
	mu.Unlock()
	if _, err := transport.ExecuteCommand(
		context.Background(),
		"ping",
		map[string]bool{"unexpected": true},
	); err == nil {
		t.Fatal("no-parameter command accepted parameters")
	}
	if _, err := transport.ExecuteCommand(
		context.Background(),
		"not-a-command",
		nil,
	); err == nil {
		t.Fatal("unsupported command was accepted")
	}
	mu.Lock()
	after := len(observed)
	mu.Unlock()
	if after != before {
		t.Fatalf("invalid commands reached HTTP server: before=%d after=%d", before, after)
	}
}

func TestValidateLoopbackAPIURLAcceptsLocalAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "localhost with implicit scheme",
			input: "localhost:47891",
			want:  "http://localhost:47891",
		},
		{
			name:  "localhost HTTPS",
			input: "https://localhost/",
			want:  "https://localhost",
		},
		{
			name:  "IPv4 loopback",
			input: " http://127.0.0.1:47891/ ",
			want:  "http://127.0.0.1:47891",
		},
		{
			name:  "IPv4 loopback subnet",
			input: "http://127.25.50.75:8080",
			want:  "http://127.25.50.75:8080",
		},
		{
			name:  "IPv6 loopback",
			input: "http://[::1]:47891/",
			want:  "http://[::1]:47891",
		},
		{
			name:  "IPv4-mapped IPv6 loopback",
			input: "http://[::ffff:127.0.0.1]:47891",
			want:  "http://[::ffff:127.0.0.1]:47891",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateLoopbackAPIURL(test.input)
			if err != nil {
				t.Fatalf("validateLoopbackAPIURL(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestValidateLoopbackAPIURLRejectsUnsafeAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "empty", input: " ", wantErr: "empty"},
		{
			name:    "unsupported scheme",
			input:   "ftp://localhost:47891",
			wantErr: "must use http or https",
		},
		{
			name:    "credentials",
			input:   "http://operator:password@localhost:47891",
			wantErr: "must not contain credentials",
		},
		{
			name:    "path",
			input:   "http://localhost:47891/status",
			wantErr: "must not contain a path",
		},
		{
			name:    "encoded path",
			input:   "http://localhost:47891/%73tatus",
			wantErr: "must not contain a path",
		},
		{
			name:    "query",
			input:   "http://localhost:47891?token=secret",
			wantErr: "must not contain credentials, a query, or a fragment",
		},
		{
			name:    "fragment",
			input:   "http://localhost:47891/#status",
			wantErr: "must not contain credentials, a query, or a fragment",
		},
		{
			name:    "public IPv4",
			input:   "http://192.0.2.10:47891",
			wantErr: "must use localhost or a loopback IP address",
		},
		{
			name:    "public IPv6",
			input:   "http://[2001:db8::1]:47891",
			wantErr: "must use localhost or a loopback IP address",
		},
		{
			name:    "lookalike hostname",
			input:   "http://localhost.example.com:47891",
			wantErr: "must use localhost or a loopback IP address",
		},
		{
			name:    "local network hostname",
			input:   "http://rayanlamp.local:47891",
			wantErr: "must use localhost or a loopback IP address",
		},
		{
			name:    "missing host",
			input:   "http:///",
			wantErr: "has no host",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := validateLoopbackAPIURL(test.input)
			if err == nil {
				t.Fatalf("unsafe URL accepted as %q", got)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestNewAPITransportValidatesTokenAndBoundsClient(t *testing.T) {
	t.Parallel()

	for _, token := range []string{"", " ", "two words", "line\nbreak", "tab\tvalue"} {
		token := token
		t.Run("reject_token_"+fmt.Sprintf("%q", token), func(t *testing.T) {
			t.Parallel()

			_, err := newAPITransport(
				"http://localhost:47891",
				token,
				time.Second,
				nil,
			)
			if err == nil {
				t.Fatalf("token %q unexpectedly accepted", token)
			}
			if strings.TrimSpace(token) != "" &&
				strings.Contains(err.Error(), token) {
				t.Errorf("validation error leaked token %q: %v", token, err)
			}
		})
	}

	originalClient := &http.Client{Timeout: 30 * time.Second}
	transport, err := newAPITransport(
		"http://localhost:47891/",
		"  valid-token  ",
		2*time.Second,
		originalClient,
	)
	if err != nil {
		t.Fatalf("newAPITransport: %v", err)
	}
	if transport.client == originalClient {
		t.Fatal("newAPITransport mutated and reused the caller's client")
	}
	if originalClient.Timeout != 30*time.Second {
		t.Errorf("caller's timeout mutated to %s", originalClient.Timeout)
	}
	if transport.client.Timeout != 2*time.Second {
		t.Errorf("bounded client timeout = %s, want 2s", transport.client.Timeout)
	}
	if transport.token != "valid-token" {
		t.Error("token was not normalized")
	}
	if transport.baseURL != "http://localhost:47891" {
		t.Errorf("base URL = %q", transport.baseURL)
	}
	if transport.Mode() != ModeAPI {
		t.Errorf("mode = %q, want %q", transport.Mode(), ModeAPI)
	}
	if transport.Label() == "" {
		t.Error("transport label is empty")
	}
	if transport.Events() != nil {
		t.Error("polling API transport unexpectedly exposes an event channel")
	}
	if err := transport.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestAPITransportStatusAndActionRoutes(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if got := request.Header.Get("Authorization"); got != "Bearer "+transportTestToken {
			t.Errorf("Authorization = %q", got)
		}
		if got := request.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.Path)
		mu.Unlock()

		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/status":
			_, _ = io.WriteString(writer, `{
				"ok": true,
				"result": {
					"state": "on",
					"http_reachable": true,
					"websocket_online": true,
					"device_url": "http://rayanlamp.local"
				}
			}`)
		case request.Method == http.MethodPost &&
			request.URL.Path == "/lamp/refresh":
			_, _ = io.WriteString(writer, `{
				"ok": true,
				"result": {
					"state": "on",
					"http_reachable": true,
					"websocket_online": true,
					"device_url": "http://rayanlamp.local"
				}
			}`)
		case request.Method == http.MethodPost:
			_, _ = io.WriteString(writer, `{"ok":true,"result":{"accepted":true}}`)
		default:
			http.Error(writer, `{"ok":false}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		2*time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatalf("newAPITransport: %v", err)
	}

	snapshot, err := transport.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	assertTransportTestSnapshot(t, snapshot)

	tests := []struct {
		action   Action
		wantPath string
	}{
		{action: ActionToggle, wantPath: "/lamp/toggle"},
		{action: ActionOn, wantPath: "/lamp/on"},
		{action: ActionOff, wantPath: "/lamp/off"},
		{action: ActionRefresh, wantPath: "/lamp/refresh"},
		{action: ActionReconnect, wantPath: "/reconnect"},
	}
	for _, test := range tests {
		mu.Lock()
		before := len(requests)
		mu.Unlock()
		snapshot, err = transport.Execute(context.Background(), test.action)
		if err != nil {
			t.Fatalf("Execute(%q): %v", test.action, err)
		}
		assertTransportTestSnapshot(t, snapshot)

		mu.Lock()
		got := append([]string(nil), requests[before:]...)
		mu.Unlock()
		want := []string{http.MethodPost + " " + test.wantPath}
		if test.action != ActionRefresh {
			want = append(want, http.MethodGet+" /status")
		}
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("Execute(%q) requests = %q, want %q", test.action, got, want)
		}
	}

	if _, err := transport.Execute(context.Background(), Action("delete")); err == nil {
		t.Error("unsupported action unexpectedly succeeded")
	}
}

func TestAPITransportDoesNotReportAcceptedMutationAsFailed(t *testing.T) {
	t.Parallel()

	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Method == http.MethodPost {
			_, _ = io.WriteString(writer, `{"ok":true,"result":{"accepted":true}}`)
			return
		}
		statusCalls++
		if statusCalls == 1 {
			_, _ = io.WriteString(writer, `{"ok":true,"result":{"state":"off"}}`)
			return
		}
		http.Error(writer, `{"ok":false,"error":{"code":"offline","message":"gone"}}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		2*time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.Status(context.Background()); err != nil {
		t.Fatal(err)
	}

	_, err = transport.Execute(context.Background(), ActionToggle)
	var confirmation *ConfirmationError
	if !errors.As(err, &confirmation) {
		t.Fatalf("Execute error = %T %v, want *ConfirmationError", err, err)
	}
	if confirmation.Action != ActionToggle {
		t.Fatalf("confirmation action = %q", confirmation.Action)
	}
}

func TestAPITransportDiagnosticsUsesCanonicalAuthenticatedRoute(t *testing.T) {
	t.Parallel()

	var gotRequest string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if got := request.Header.Get("Authorization"); got != "Bearer "+transportTestToken {
			t.Errorf("Authorization = %q", got)
		}
		gotRequest = request.Method + " " + request.URL.RequestURI()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(
			writer,
			`{"ok":true,"result":{"endpoint":"/info","content":"{\"state\":\"on\"}","bytes":14}}`,
		)
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		2*time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := transport.FetchDiagnostics(context.Background(), "/info")
	if err != nil {
		t.Fatal(err)
	}
	if gotRequest != "GET /diagnostics?endpoint=%2Finfo" {
		t.Errorf("diagnostics request = %q", gotRequest)
	}
	if result.Endpoint != "/info" ||
		result.Content != `{"state":"on"}` ||
		result.Bytes != 14 {
		t.Errorf("diagnostics result = %+v", result)
	}

	if _, err := transport.FetchDiagnostics(
		context.Background(),
		"/info?token=never",
	); err == nil {
		t.Error("unsafe diagnostic endpoint unexpectedly reached the API")
	}
}

func TestAPITransportDoesNotFollowRedirectsOrForwardAuthorization(t *testing.T) {
	t.Parallel()

	var (
		targetRequests int
		targetAuth     string
	)
	target := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		targetRequests++
		targetAuth = request.Header.Get("Authorization")
		_, _ = io.WriteString(writer, `{"ok":true,"result":{"state":"on"}}`)
	}))
	defer target.Close()

	redirect := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Location", target.URL+"/status")
		writer.WriteHeader(http.StatusFound)
		_, _ = io.WriteString(writer, `{"ok":false}`)
	}))
	defer redirect.Close()

	transport, err := newAPITransport(
		redirect.URL,
		transportTestToken,
		time.Second,
		redirect.Client(),
	)
	if err != nil {
		t.Fatalf("newAPITransport: %v", err)
	}
	_, err = transport.Status(context.Background())
	if err == nil {
		t.Fatal("redirect response unexpectedly accepted")
	}
	if !strings.Contains(err.Error(), "HTTP 302") {
		t.Errorf("redirect error = %q", err)
	}
	if targetRequests != 0 {
		t.Errorf("redirect target received %d request(s)", targetRequests)
	}
	if targetAuth != "" {
		t.Errorf("redirect target received authorization %q", targetAuth)
	}
	if strings.Contains(err.Error(), transportTestToken) {
		t.Error("redirect error leaked the bearer token")
	}
}

func TestAPITransportResponseErrorsAndTokenRedaction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    string
		wantAbsent string
	}{
		{
			name:    "coded HTTP error",
			status:  http.StatusServiceUnavailable,
			body:    `{"ok":false,"error":{"code":"offline","message":"lamp unavailable"}}`,
			wantErr: "offline: lamp unavailable",
		},
		{
			name:    "coded logical error",
			status:  http.StatusOK,
			body:    `{"ok":false,"error":{"code":"busy","message":"try later"}}`,
			wantErr: "busy: try later",
		},
		{
			name:    "uncoded HTTP error",
			status:  http.StatusBadGateway,
			body:    `{"ok":true}`,
			wantErr: "local API returned HTTP 502",
		},
		{
			name:    "uncoded logical error",
			status:  http.StatusOK,
			body:    `{"ok":false}`,
			wantErr: "local API returned HTTP 200",
		},
		{
			name:    "invalid JSON",
			status:  http.StatusInternalServerError,
			body:    `not json`,
			wantErr: "local API returned HTTP 500 with invalid JSON",
		},
		{
			name:       "invalid snapshot",
			status:     http.StatusOK,
			body:       `{"ok":true,"result":{"state":"definitely-not-a-state"}}`,
			wantErr:    "decode local API status",
			wantAbsent: transportTestToken,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter,
				_ *http.Request,
			) {
				writer.WriteHeader(test.status)
				_, _ = io.WriteString(writer, test.body)
			}))
			defer server.Close()

			transport, err := newAPITransport(
				server.URL,
				transportTestToken,
				time.Second,
				server.Client(),
			)
			if err != nil {
				t.Fatalf("newAPITransport: %v", err)
			}
			_, err = transport.Status(context.Background())
			if err == nil {
				t.Fatal("Status unexpectedly succeeded")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, test.wantErr)
			}
			if strings.Contains(err.Error(), transportTestToken) {
				t.Errorf("error leaked bearer token: %v", err)
			}
			if test.wantAbsent != "" && strings.Contains(err.Error(), test.wantAbsent) {
				t.Errorf("error %q contains forbidden text", err)
			}
		})
	}
}

func TestAPITransportRejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = io.WriteString(
			writer,
			strings.Repeat("x", int(maxAPIResponseBytes)+1),
		)
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		5*time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatalf("newAPITransport: %v", err)
	}
	_, err = transport.Status(context.Background())
	if err == nil {
		t.Fatal("oversized response unexpectedly accepted")
	}
	if !strings.Contains(err.Error(), "response exceeds the size limit") {
		t.Fatalf("error = %q", err)
	}
	if strings.Contains(err.Error(), transportTestToken) {
		t.Error("oversized-response error leaked bearer token")
	}
}

func TestAPITransportNetworkErrorDoesNotLeakToken(t *testing.T) {
	t.Parallel()

	client := &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("simulated failure for " + transportTestToken)
		}),
	}
	transport, err := newAPITransport(
		"http://localhost:47891",
		transportTestToken,
		time.Second,
		client,
	)
	if err != nil {
		t.Fatalf("newAPITransport: %v", err)
	}
	_, err = transport.Status(context.Background())
	if err == nil {
		t.Fatal("network failure unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "local API is unavailable") {
		t.Errorf("error = %q", err)
	}
	if strings.Contains(err.Error(), transportTestToken) {
		t.Errorf("network error leaked bearer token: %v", err)
	}
}

func TestAPITransportRedactsReflectedServerErrorAndClosesIdleConnections(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(
			writer,
			`{"ok":false,"error":{"code":"reflected","message":"credential %s"}}`,
			transportTestToken,
		)
	}))
	defer server.Close()

	transport, err := newAPITransport(
		server.URL,
		transportTestToken,
		time.Second,
		server.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Status(context.Background())
	if err == nil {
		t.Fatal("reflected server error unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), transportTestToken) ||
		!strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("reflected credential was not redacted: %v", err)
	}

	tracker := &closeTrackingRoundTripper{}
	owned, err := newAPITransport(
		"http://127.0.0.1:47891",
		transportTestToken,
		time.Second,
		&http.Client{Transport: tracker},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	if !tracker.closed {
		t.Fatal("Close did not close idle API connections")
	}
}

func assertTransportTestSnapshot(t *testing.T, snapshot model.Snapshot) {
	t.Helper()
	if snapshot.State != model.StateOn {
		t.Errorf("state = %q, want on", snapshot.State)
	}
	if !snapshot.HTTPReachable {
		t.Error("HTTP reachability was not decoded")
	}
	if !snapshot.WebSocketOnline {
		t.Error("WebSocket state was not decoded")
	}
	if snapshot.DeviceURL != "http://rayanlamp.local" {
		t.Errorf("device URL = %q", snapshot.DeviceURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return function(request)
}

type closeTrackingRoundTripper struct {
	closed bool
}

func (*closeTrackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func (transport *closeTrackingRoundTripper) CloseIdleConnections() {
	transport.closed = true
}
