package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/ipc"
)

const testToken = "local-api-test-token"

type recordedHandler struct {
	mu       sync.Mutex
	requests []ipc.Request
	result   any
	err      error
}

func (h *recordedHandler) Handle(_ context.Context, request ipc.Request) (any, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, request)
	return h.result, h.err
}

func (h *recordedHandler) last(t *testing.T) ipc.Request {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.requests) == 0 {
		t.Fatal("IPC handler was not called")
	}
	return h.requests[len(h.requests)-1]
}

func (h *recordedHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.requests)
}

func TestRouteMapping(t *testing.T) {
	tests := []struct {
		name       string
		httpMethod string
		path       string
		body       string
		command    string
		params     string
	}{
		{name: "ping", httpMethod: http.MethodGet, path: "/ping", command: "ping"},
		{name: "status", httpMethod: http.MethodGet, path: "/status", command: "status"},
		{name: "device on", httpMethod: http.MethodPost, path: "/device/on", command: "device.on"},
		{name: "device off", httpMethod: http.MethodPost, path: "/device/off", command: "device.off"},
		{name: "device toggle", httpMethod: http.MethodPost, path: "/device/toggle", command: "device.toggle"},
		{name: "device refresh", httpMethod: http.MethodPost, path: "/device/refresh", command: "device.refresh"},
		{name: "show window", httpMethod: http.MethodPost, path: "/window/show", command: "window.show"},
		{name: "hide window", httpMethod: http.MethodPost, path: "/window/hide", command: "window.hide"},
		{name: "show settings", httpMethod: http.MethodPost, path: "/settings/show", command: "settings.show"},
		{name: "show diagnostics", httpMethod: http.MethodPost, path: "/diagnostics/show", command: "diagnostics.show"},
		{name: "show web UI", httpMethod: http.MethodPost, path: "/web/show", command: "web.show"},
		{name: "reconnect", httpMethod: http.MethodPost, path: "/reconnect", command: "reconnect"},
		{
			name:       "diagnostics",
			httpMethod: http.MethodGet,
			path:       "/diagnostics?endpoint=%2Finfo",
			command:    "diagnostics.fetch",
			params:     `{"endpoint":"/info"}`,
		},
		{name: "get config", httpMethod: http.MethodGet, path: "/config", command: "config.get"},
		{
			name:       "registration status",
			httpMethod: http.MethodGet,
			path:       "/integration/registration",
			command:    "integration.registration.status",
		},
		{
			name:       "register shell integration",
			httpMethod: http.MethodPost,
			path:       "/integration/registration",
			body:       `{"method":"integration.shell.register"}`,
			command:    "integration.shell.register",
		},
		{name: "get device config", httpMethod: http.MethodGet, path: "/device/config", command: "device.config.get"},
		{name: "refresh device config", httpMethod: http.MethodPost, path: "/device/config/refresh", command: "device.config.refresh"},
		{
			name:       "patch config",
			httpMethod: http.MethodPatch,
			path:       "/config",
			body:       `{"ui":{"always_on_top":true},"device":{"timeout":"3s"}}`,
			command:    "config.patch",
			params:     `{"ui":{"always_on_top":true},"device":{"timeout":"3s"}}`,
		},
		{
			name:       "notification",
			httpMethod: http.MethodPost,
			path:       "/notifications",
			body:       `{"title":"Device","message":"Turned on"}`,
			command:    "notifications.show",
			params:     `{"title":"Device","message":"Turned on"}`,
		},
		{
			name:       "audio cue",
			httpMethod: http.MethodPost,
			path:       "/audio/play",
			command:    "audio.play",
			params:     `{"count":1,"cue":"message"}`,
		},
		{
			name:       "selected audio cue",
			httpMethod: http.MethodPost,
			path:       "/audio/play",
			body:       `{"cue":"error","count":2}`,
			command:    "audio.play",
			params:     `{"cue":"error","count":2}`,
		},
		{
			name:       "import local audio file",
			httpMethod: http.MethodPost,
			path:       "/audio/import",
			body:       `{"path":"C:\\Sounds\\custom.wav"}`,
			command:    "audio.import",
			params:     `{"path":"C:\\Sounds\\custom.wav"}`,
		},
		{name: "version", httpMethod: http.MethodGet, path: "/version", command: "version"},
		{name: "quit", httpMethod: http.MethodPost, path: "/quit", command: "quit"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &recordedHandler{result: map[string]any{"accepted": true}}
			request := httptest.NewRequest(test.httpMethod, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+testToken)
			if test.body != "" {
				request.Header.Set("Content-Type", "application/json; charset=utf-8")
			}
			recorder := httptest.NewRecorder()

			newAPI(testToken, handler.Handle).ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var responseBody response
			if err := json.Unmarshal(recorder.Body.Bytes(), &responseBody); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if !responseBody.OK {
				t.Fatalf("unexpected response: %s", recorder.Body.String())
			}

			got := handler.last(t)
			if got.ProtocolVersion != ipc.ProtocolVersion {
				t.Errorf("protocol version = %d", got.ProtocolVersion)
			}
			if got.ID == "" {
				t.Error("request ID is empty")
			}
			if got.Method != test.command {
				t.Errorf("command = %q, want %q", got.Method, test.command)
			}
			assertJSONEqual(t, got.Params, test.params)
		})
	}
}

func TestAuthenticationAndHealth(t *testing.T) {
	handler := &recordedHandler{result: map[string]string{"state": "on"}}
	apiHandler := newAPI(testToken, handler.Handle)

	t.Run("health is unauthenticated", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		apiHandler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Errorf("Content-Type = %q", got)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q", got)
		}
		if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("X-Content-Type-Options = %q", got)
		}
		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("unexpected CORS header: %q", got)
		}
	})

	for _, test := range []struct {
		name    string
		headers []string
	}{
		{name: "missing"},
		{name: "wrong token", headers: []string{"Bearer wrong"}},
		{name: "wrong scheme", headers: []string{"Basic " + testToken}},
		{name: "token whitespace", headers: []string{"Bearer " + testToken + " "}},
		{name: "duplicate", headers: []string{"Bearer " + testToken, "Bearer " + testToken}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/status", nil)
			for _, value := range test.headers {
				request.Header.Add("Authorization", value)
			}
			recorder := httptest.NewRecorder()
			apiHandler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header")
			}
		})
	}
	for _, path := range []string{
		"/settings/show",
		"/diagnostics/show",
		"/web/show",
		"/quit",
	} {
		t.Run("missing token "+path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, path, nil)
			recorder := httptest.NewRecorder()
			apiHandler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
		})
	}
	if handler.count() != 0 {
		t.Fatalf("handler called %d times for unauthorized requests", handler.count())
	}

	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	request.Header.Set("Authorization", "bEaReR "+testToken)
	recorder := httptest.NewRecorder()
	apiHandler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("valid authentication status = %d", recorder.Code)
	}
	if handler.count() != 1 {
		t.Fatalf("handler call count = %d", handler.count())
	}
}

func TestVersionedHTTPAndWebSocketAliasesAreAbsent(t *testing.T) {
	handler := &recordedHandler{}
	apiHandler := newAPI(testToken, handler.Handle)
	for _, path := range []string{
		"/v1/status",
		"/v1/events",
		"/v1/ui/bootstrap",
		"/v1/ui/terminal",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		recorder := httptest.NewRecorder()
		apiHandler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf(
				"%s status = %d, want 404; body = %s",
				path,
				recorder.Code,
				recorder.Body.String(),
			)
		}
	}
	if handler.count() != 0 {
		t.Fatalf("legacy aliases reached dispatcher %d times", handler.count())
	}
}

func TestDeveloperAndMockRoutesAreAbsent(t *testing.T) {
	handler := &recordedHandler{}
	apiHandler := newAPI(testToken, handler.Handle)
	for _, path := range []string{
		"/debug/pprof/",
		"/metrics",
		"/trace",
		"/mock/event",
		"/mock/disconnect",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		recorder := httptest.NewRecorder()
		apiHandler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf(
				"%s status = %d, want 404; body = %s",
				path,
				recorder.Code,
				recorder.Body.String(),
			)
		}
	}
	if handler.count() != 0 {
		t.Fatalf("developer route reached dispatcher %d times", handler.count())
	}
}

func TestFirmwareCredentialProjectionPathsAreNotCoordinatorRoutes(t *testing.T) {
	handler := &recordedHandler{}
	apiHandler := newAPI(testToken, handler.Handle)
	for _, path := range []string{"/config/ap", "/config/st"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		recorder := httptest.NewRecorder()
		apiHandler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Errorf(
				"%s status = %d, want 404; body = %s",
				path,
				recorder.Code,
				recorder.Body.String(),
			)
		}
	}
	if handler.count() != 0 {
		t.Fatalf("firmware credential paths reached dispatcher %d times", handler.count())
	}
}

func TestStrictMethodsContentTypesAndInputs(t *testing.T) {
	handler := &recordedHandler{}
	apiHandler := newAPI(testToken, handler.Handle)

	tests := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		wantStatus  int
		wantAllow   string
	}{
		{
			name:       "wrong status method",
			method:     http.MethodPost,
			path:       "/status",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
		},
		{
			name:       "wrong config method",
			method:     http.MethodPost,
			path:       "/config",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet + ", " + http.MethodPatch,
		},
		{
			name:       "wrong device config method",
			method:     http.MethodPatch,
			path:       "/device/config",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
		},
		{
			name:       "body on no-body route",
			method:     http.MethodPost,
			path:       "/device/on",
			body:       `{}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing JSON content type",
			method:     http.MethodPatch,
			path:       "/config",
			body:       `{}`,
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name:        "non-object config patch",
			method:      http.MethodPatch,
			path:        "/config",
			body:        `[]`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "invalid config JSON",
			method:      http.MethodPatch,
			path:        "/config",
			body:        `{`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "unknown notification field",
			method:      http.MethodPost,
			path:        "/notifications",
			body:        `{"message":"hi","extra":true}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:        "empty notification message",
			method:      http.MethodPost,
			path:        "/notifications",
			body:        `{"title":"Device","message":" "}`,
			contentType: "application/json",
			wantStatus:  http.StatusBadRequest,
		},
		{
			name:       "missing diagnostics endpoint",
			method:     http.MethodGet,
			path:       "/diagnostics",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "extra diagnostics query",
			method:     http.MethodGet,
			path:       "/diagnostics?endpoint=%2Finfo&other=x",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown versionless route",
			method:     http.MethodGet,
			path:       "/nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "legacy version prefix is not an alias",
			method:     http.MethodGet,
			path:       "/v1/status",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "preflight is not enabled",
			method:     http.MethodOptions,
			path:       "/status",
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Authorization", "Bearer "+testToken)
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			recorder := httptest.NewRecorder()
			apiHandler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("Allow"); got != test.wantAllow {
				t.Errorf("Allow = %q, want %q", got, test.wantAllow)
			}
			if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Errorf("unexpected CORS header: %q", got)
			}
		})
	}
	if handler.count() != 0 {
		t.Fatalf("handler called %d times for invalid requests", handler.count())
	}
}

func TestBodyLimit(t *testing.T) {
	handler := &recordedHandler{}
	body := `{"value":"` + strings.Repeat("x", ipc.MaxMessageBytes) + `"}`
	request := httptest.NewRequest(http.MethodPatch, "/config", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	newAPI(testToken, handler.Handle).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if handler.count() != 0 {
		t.Fatal("handler called for oversized body")
	}
}

type codedTestError struct {
	code    string
	message string
}

func (e codedTestError) Error() string { return e.message }
func (e codedTestError) Code() string  { return e.code }

func TestCommandErrorMapping(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantLeak   bool
	}{
		{name: "invalid argument", err: codedTestError{code: "invalid_argument", message: "bad endpoint"}, wantStatus: http.StatusBadRequest, wantCode: "invalid_argument", wantLeak: true},
		{name: "offline", err: codedTestError{code: "offline", message: "device unavailable"}, wantStatus: http.StatusServiceUnavailable, wantCode: "offline", wantLeak: true},
		{name: "timeout", err: context.DeadlineExceeded, wantStatus: http.StatusGatewayTimeout, wantCode: "timeout"},
		{name: "uncoded hidden", err: errors.New("secret internal detail"), wantStatus: http.StatusInternalServerError, wantCode: "internal_error"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &recordedHandler{err: test.err}
			request := httptest.NewRequest(http.MethodGet, "/status", nil)
			request.Header.Set("Authorization", "Bearer "+testToken)
			recorder := httptest.NewRecorder()

			newAPI(testToken, handler.Handle).ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
			var got response
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Error == nil || got.Error.Code != test.wantCode {
				t.Fatalf("error = %#v, want code %q", got.Error, test.wantCode)
			}
			if !test.wantLeak && strings.Contains(recorder.Body.String(), "secret internal detail") {
				t.Fatal("uncoded internal error was exposed")
			}
		})
	}
}

func TestNewValidatesLoopbackAndServes(t *testing.T) {
	handler := &recordedHandler{result: map[string]string{"version": "test"}}
	for _, address := range []string{
		"",
		":0",
		"0.0.0.0:0",
		"[::]:0",
		"192.0.2.10:0",
		"example.com:1234",
		"127.0.0.1:http",
	} {
		t.Run("reject "+address, func(t *testing.T) {
			server, err := New(address, testToken, handler.Handle)
			if err == nil {
				_ = server.Close(context.Background())
				t.Fatalf("New(%q) succeeded", address)
			}
		})
	}
	if _, err := New("127.0.0.1:0", "", handler.Handle); err == nil {
		t.Fatal("empty token accepted")
	}
	if _, err := New("127.0.0.1:0", "bad token", handler.Handle); err == nil {
		t.Fatal("token containing whitespace accepted")
	}
	if _, err := New("127.0.0.1:0", testToken, nil); err == nil {
		t.Fatal("nil handler accepted")
	}

	server, err := New("localhost:0", testToken, handler.Handle)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Close(ctx); err != nil {
			t.Errorf("close server: %v", err)
		}
	})
	host, _, err := net.SplitHostPort(server.Addr)
	if err != nil {
		t.Fatalf("parse Addr: %v", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("Addr is not loopback: %q", server.Addr)
	}

	request, err := http.NewRequest(http.MethodGet, "http://"+server.Addr+"/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	client := &http.Client{Timeout: 2 * time.Second}
	httpResponse, err := client.Do(request)
	if err != nil {
		t.Fatalf("GET running server: %v", err)
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", httpResponse.StatusCode)
	}
}

func TestWriteJSONHandlesUnencodableResult(t *testing.T) {
	handler := &recordedHandler{result: make(chan int)}
	request := httptest.NewRequest(http.MethodGet, "/status", nil)
	request.Header.Set("Authorization", "Bearer "+testToken)
	recorder := httptest.NewRecorder()

	newAPI(testToken, handler.Handle).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if !bytes.Contains(recorder.Body.Bytes(), []byte(`"internal_error"`)) {
		t.Fatalf("body = %s", recorder.Body.String())
	}
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	if want == "" {
		if len(got) != 0 {
			t.Fatalf("params = %s, want none", got)
		}
		return
	}
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode params %q: %v", got, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected params: %v", err)
	}
	gotJSON, _ := json.Marshal(gotValue)
	wantJSON, _ := json.Marshal(wantValue)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("params = %s, want %s", gotJSON, wantJSON)
	}
}
