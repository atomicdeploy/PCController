package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/buildinfo"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/diagnostics"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/webui"
)

type testEventSource struct {
	mu          sync.Mutex
	snapshot    model.Snapshot
	cfg         config.Config
	nextID      int
	subscribers map[int]chan model.Event
}

func newTestEventSource() *testEventSource {
	cfg := config.Default()
	cfg.Integration.LocalAPIToken = "never-expose-this-local-api-token"
	return &testEventSource{
		snapshot: model.Snapshot{
			State:           model.StateOff,
			DeviceURL:       cfg.Device.BaseURL,
			HTTPReachable:   true,
			LastSource:      "test",
			LastConnected:   time.Now().UTC(),
			LastChanged:     time.Now().UTC(),
			PendingCommand:  "",
			WebSocketOnline: true,
		},
		cfg:         cfg,
		subscribers: make(map[int]chan model.Event),
	}
}

func (s *testEventSource) Snapshot() model.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *testEventSource) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *testEventSource) Subscribe(buffer int) (<-chan model.Event, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	channel := make(chan model.Event, buffer)
	s.subscribers[id] = channel
	return channel, func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

func (s *testEventSource) emit(event model.Event) {
	s.mu.Lock()
	s.snapshot = event.Snapshot
	channels := make([]chan model.Event, 0, len(s.subscribers))
	for _, channel := range s.subscribers {
		channels = append(channels, channel)
	}
	s.mu.Unlock()
	for _, channel := range channels {
		channel <- event
	}
}

type runningBrowserAPI struct {
	server *Server
	origin string
	client *http.Client
}

func startBrowserAPI(
	t *testing.T,
	source EventSource,
	handler ipc.Handler,
	configure ...func(*Options),
) runningBrowserAPI {
	t.Helper()
	options := Options{
		Listen:      "127.0.0.1:0",
		BearerToken: testToken,
		Handler:     handler,
		EventSource: source,
		AppHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><title>PCController</title>")
		}),
	}
	for _, apply := range configure {
		apply(&options)
	}
	server, err := NewWithOptions(options)
	if err != nil {
		t.Fatalf("start browser API: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Close(ctx)
	})
	return runningBrowserAPI{
		server: server,
		origin: strings.TrimSuffix(server.BrowserURL(), "/app/"),
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

type testDiagnosticsSource struct {
	*testEventSource

	diagnosticsMu   sync.Mutex
	diagnostics     diagnostics.Snapshot
	diagnosticsNext int
	diagnosticsSubs map[int]chan diagnostics.Snapshot
	refreshed       []string
}

func newTestDiagnosticsSource() *testDiagnosticsSource {
	source := newTestEventSource()
	snapshot := diagnostics.NewSnapshot(source.Config().Device.BaseURL)
	snapshot.Device.DeviceName = "PCController"
	snapshot.Endpoints["/info"] = diagnostics.EndpointResult{
		Document: diagnostics.Document{
			Endpoint:  "/info",
			FetchedAt: time.Now().UTC(),
			Raw:       "secret-raw-diagnostics",
			SafeRaw:   "[redacted]",
		},
		LastSuccess: time.Now().UTC(),
	}
	return &testDiagnosticsSource{
		testEventSource: source,
		diagnostics:     snapshot,
		diagnosticsSubs: make(map[int]chan diagnostics.Snapshot),
	}
}

func (s *testDiagnosticsSource) DiagnosticsSnapshot() diagnostics.Snapshot {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	return s.diagnostics.Clone()
}

func (s *testDiagnosticsSource) SubscribeDiagnostics(
	buffer int,
) (<-chan diagnostics.Snapshot, func()) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	s.diagnosticsNext++
	id := s.diagnosticsNext
	channel := make(chan diagnostics.Snapshot, buffer)
	s.diagnosticsSubs[id] = channel
	return channel, func() {
		s.diagnosticsMu.Lock()
		delete(s.diagnosticsSubs, id)
		s.diagnosticsMu.Unlock()
	}
}

func (s *testDiagnosticsSource) RefreshDiagnostics(
	_ context.Context,
	endpoints ...string,
) (diagnostics.Snapshot, error) {
	s.diagnosticsMu.Lock()
	s.refreshed = append([]string(nil), endpoints...)
	s.diagnostics.UpdatedAt = time.Now().UTC()
	snapshot := s.diagnostics.Clone()
	channels := make([]chan diagnostics.Snapshot, 0, len(s.diagnosticsSubs))
	for _, channel := range s.diagnosticsSubs {
		channels = append(channels, channel)
	}
	s.diagnosticsMu.Unlock()
	for _, channel := range channels {
		channel <- snapshot
	}
	return snapshot, nil
}

func mintBrowserSession(
	t *testing.T,
	running runningBrowserAPI,
) (*http.Cookie, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, running.origin+"/app/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", running.origin)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatalf("mint browser session: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("mint status = %d, body = %s", response.StatusCode, data)
	}
	var body struct {
		OK     bool `json:"ok"`
		Result struct {
			CSRFToken string    `json:"csrf_token"`
			ExpiresAt time.Time `json:"expires_at"`
		} `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || !validRandomToken(body.Result.CSRFToken) || body.Result.ExpiresAt.IsZero() {
		t.Fatalf("invalid session response: %#v", body)
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name != browserSessionCookie {
			continue
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("insecure browser cookie: %#v", cookie)
		}
		if cookie.Domain != "" || cookie.Path != "/" || cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
			t.Fatalf("browser cookie is not ephemeral and host-only: %#v", cookie)
		}
		return cookie, body.Result.CSRFToken
	}
	t.Fatal("browser session cookie was not set")
	return nil, ""
}

func TestBrowserSessionRequiresExactHostAndOrigin(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	running := startBrowserAPI(t, source, handler.Handle)

	for _, test := range []struct {
		name   string
		host   string
		origin string
	}{
		{name: "missing origin"},
		{name: "foreign origin", origin: "https://example.test"},
		{name: "wrong host", host: "localhost.invalid", origin: running.origin},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, running.origin+"/app/session", nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.host != "" {
				request.Host = test.host
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			response, err := running.client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.StatusCode)
			}
		})
	}

	cookie, csrf := mintBrowserSession(t, running)
	request, err := http.NewRequest(http.MethodPost, running.origin+"/app/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var body struct {
		Result sessionResult `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Result.CSRFToken != csrf {
		t.Fatal("a second tab rotated the shared browser session CSRF token")
	}
	if len(response.Cookies()) != 0 {
		t.Fatal("an existing valid browser session was needlessly replaced")
	}
}

func TestBrowserMutationRequiresCSRFAndBearerRemainsCompatible(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: model.StateOn}
	running := startBrowserAPI(t, source, handler.Handle)
	cookie, csrf := mintBrowserSession(t, running)

	call := func(token string, bearer bool) int {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, running.origin+"/device/on", nil)
		if err != nil {
			t.Fatal(err)
		}
		if bearer {
			request.Header.Set("Authorization", "Bearer "+testToken)
		} else {
			request.AddCookie(cookie)
			request.Header.Set("Origin", running.origin)
			request.Header.Set("Sec-Fetch-Site", "same-origin")
			if token != "" {
				request.Header.Set(csrfHeader, token)
			}
		}
		response, err := running.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		return response.StatusCode
	}

	if got := call("", false); got != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d", got)
	}
	if got := call("wrong", false); got != http.StatusForbidden {
		t.Fatalf("wrong CSRF status = %d", got)
	}
	if got := call(csrf, false); got != http.StatusOK {
		t.Fatalf("valid browser status = %d", got)
	}
	if got := call("", true); got != http.StatusOK {
		t.Fatalf("bearer status = %d", got)
	}
	if handler.count() != 2 {
		t.Fatalf("handler calls = %d, want 2", handler.count())
	}
}

func TestUIConfigIsRedactedRevisionedAndRejectsTokenPatch(t *testing.T) {
	source := newTestEventSource()
	updated := source.Config()
	updated.UI.AlwaysOnTop = true
	updated.Integration.LocalAPIToken = "still-secret-after-patch"
	handler := &recordedHandler{result: updated}
	running := startBrowserAPI(t, source, handler.Handle)
	cookie, csrf := mintBrowserSession(t, running)

	get := func(path string) (*http.Response, []byte) {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, running.origin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Origin", running.origin)
		response, err := running.client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}

	bootstrapResponse, bootstrap := get("/ui/bootstrap")
	if bootstrapResponse.StatusCode != http.StatusOK {
		t.Fatalf("bootstrap status = %d, body = %s", bootstrapResponse.StatusCode, bootstrap)
	}
	if strings.Contains(string(bootstrap), source.Config().Integration.LocalAPIToken) ||
		strings.Contains(string(bootstrap), `"local_api_token":`) {
		t.Fatalf("bootstrap leaked API token: %s", bootstrap)
	}
	if !strings.Contains(string(bootstrap), `"local_api_token_configured":true`) ||
		!strings.Contains(string(bootstrap), `"revision":1`) {
		t.Fatalf("bootstrap missing redacted security/revision metadata: %s", bootstrap)
	}
	if bytes.Contains(bootstrap, []byte(`"protocol_version"`)) {
		t.Fatalf("bootstrap exposed obsolete protocol version: %s", bootstrap)
	}

	configResponse, configBody := get("/ui/config")
	if configResponse.StatusCode != http.StatusOK || configResponse.Header.Get("ETag") != `"1"` {
		t.Fatalf("config status/etag = %d/%q, body = %s", configResponse.StatusCode, configResponse.Header.Get("ETag"), configBody)
	}

	patch := func(body, ifMatch string) (*http.Response, []byte) {
		t.Helper()
		request, err := http.NewRequest(
			http.MethodPatch,
			running.origin+"/ui/config",
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		request.AddCookie(cookie)
		request.Header.Set("Origin", running.origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set(csrfHeader, csrf)
		request.Header.Set("Content-Type", "application/json")
		if ifMatch != "" {
			request.Header.Set("If-Match", ifMatch)
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
		return response, data
	}

	forbidden, forbiddenBody := patch(
		`{"integration":{"local_api_token":"attacker-controlled"}}`,
		`"1"`,
	)
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("token patch status = %d, body = %s", forbidden.StatusCode, forbiddenBody)
	}

	nullIntegration, nullIntegrationBody := patch(
		`{"integration":null}`,
		`"1"`,
	)
	if nullIntegration.StatusCode != http.StatusForbidden {
		t.Fatalf(
			"null integration patch status = %d, body = %s",
			nullIntegration.StatusCode,
			nullIntegrationBody,
		)
	}

	missingRevision, missingRevisionBody := patch(
		`{"ui":{"always_on_top":true}}`,
		"",
	)
	if missingRevision.StatusCode != http.StatusPreconditionRequired {
		t.Fatalf(
			"missing revision status = %d, body = %s",
			missingRevision.StatusCode,
			missingRevisionBody,
		)
	}

	applied, appliedBody := patch(`{"ui":{"always_on_top":true}}`, `"1"`)
	if applied.StatusCode != http.StatusOK || applied.Header.Get("ETag") != `"2"` {
		t.Fatalf("safe patch status/etag = %d/%q, body = %s", applied.StatusCode, applied.Header.Get("ETag"), appliedBody)
	}
	if strings.Contains(string(appliedBody), updated.Integration.LocalAPIToken) ||
		strings.Contains(string(appliedBody), `"local_api_token":`) {
		t.Fatalf("config patch leaked API token: %s", appliedBody)
	}
	if !strings.Contains(string(appliedBody), `"revision":2`) {
		t.Fatalf("config patch omitted revision: %s", appliedBody)
	}

	stale, staleBody := patch(`{"ui":{"always_on_top":false}}`, `"1"`)
	if stale.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("stale patch status = %d, body = %s", stale.StatusCode, staleBody)
	}
}

func TestBootstrapCapabilityOverridesAreHostSpecific(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	overrides := map[string]bool{
		"native_shell":   false,
		"window_control": false,
		"notifications":  false,
		"global_hotkeys": false,
	}
	running := startBrowserAPI(
		t,
		source,
		handler.Handle,
		func(options *Options) {
			options.Capabilities = overrides
		},
	)
	// NewWithOptions must retain an immutable copy owned by the server.
	overrides["native_shell"] = true

	cookie, _ := mintBrowserSession(t, running)
	request, err := http.NewRequest(
		http.MethodGet,
		running.origin+"/ui/bootstrap",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var envelope struct {
		OK     bool            `json:"ok"`
		Result bootstrapResult `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !envelope.OK {
		t.Fatalf("bootstrap status = %d, envelope = %#v", response.StatusCode, envelope)
	}
	for _, name := range []string{
		"native_shell",
		"window_control",
		"notifications",
		"global_hotkeys",
	} {
		if envelope.Result.Capabilities[name] {
			t.Errorf("headless override %q = true", name)
		}
	}
	if !envelope.Result.Capabilities["device_control"] ||
		!envelope.Result.Capabilities["configuration"] {
		t.Fatalf(
			"host-neutral capabilities were lost: %#v",
			envelope.Result.Capabilities,
		)
	}
}

func TestBootstrapDefaultsRemainNativeHostCompatible(t *testing.T) {
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
	cookie, _ := mintBrowserSession(t, running)
	request, err := http.NewRequest(
		http.MethodGet,
		running.origin+"/ui/bootstrap",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Origin", running.origin)
	response, err := running.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var envelope struct {
		OK     bool            `json:"ok"`
		Result bootstrapResult `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"native_shell",
		"window_control",
		"notifications",
		"global_hotkeys",
		"hotkey_capture",
		"hotkey_probe",
	} {
		if !envelope.Result.Capabilities[name] {
			t.Errorf("native default capability %q = false", name)
		}
	}
	if got := envelope.Result.Capabilities["developer_tools"]; got != buildinfo.DeveloperTools {
		t.Errorf(
			"developer_tools capability = %v, want %v",
			got,
			buildinfo.DeveloperTools,
		)
	}
	if got := envelope.Result.Version["edition"]; got != buildinfo.Edition {
		t.Errorf("browser edition = %q, want %q", got, buildinfo.Edition)
	}
}

func TestWebSocketHelloFanoutAndShutdown(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	running := startBrowserAPI(t, source, handler.Handle)
	cookie, _ := mintBrowserSession(t, running)
	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/events"

	dial := func(origin string) (*websocket.Conn, *http.Response, error) {
		headers := http.Header{}
		headers.Set("Origin", origin)
		headers.Set("Cookie", cookie.Name+"="+cookie.Value)
		dialer := *websocket.DefaultDialer
		dialer.HandshakeTimeout = 2 * time.Second
		return dialer.Dial(socketURL, headers)
	}

	if connection, response, err := dial("https://example.test"); err == nil {
		connection.Close()
		t.Fatal("foreign-origin WebSocket connected")
	} else if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign-origin handshake response = %#v, err = %v", response, err)
	}

	first, _, err := dial(running.origin)
	if err != nil {
		t.Fatalf("dial first WebSocket: %v", err)
	}
	defer first.Close()
	second, _, err := dial(running.origin)
	if err != nil {
		t.Fatalf("dial second WebSocket: %v", err)
	}
	defer second.Close()

	readEnvelope := func(connection *websocket.Conn) StreamEnvelope {
		t.Helper()
		_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, payload, err := connection.ReadMessage()
		if err != nil {
			t.Fatalf("read WebSocket envelope: %v", err)
		}
		if bytes.Contains(payload, []byte(`"protocol_version"`)) {
			t.Fatalf("WebSocket exposed obsolete protocol version: %s", payload)
		}
		var envelope StreamEnvelope
		if err := json.Unmarshal(payload, &envelope); err != nil {
			t.Fatalf("decode WebSocket envelope: %v", err)
		}
		return envelope
	}
	for index, connection := range []*websocket.Conn{first, second} {
		hello := readEnvelope(connection)
		if hello.Type != "hello" || hello.Sequence != 0 || hello.Revision != 1 ||
			hello.Snapshot == nil || hello.Config == nil {
			t.Fatalf("hello %d = %#v", index, hello)
		}
	}

	snapshot := source.Snapshot()
	snapshot.State = model.StateOn
	source.emit(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: model.StateOff,
		FromRemote:    true,
	})
	for index, connection := range []*websocket.Conn{first, second} {
		event := readEnvelope(connection)
		if event.Type != "snapshot" || event.Sequence != 1 ||
			event.Snapshot == nil || event.Snapshot.State != model.StateOn {
			t.Fatalf("snapshot %d = %#v", index, event)
		}
	}

	cfg := source.Config()
	cfg.UI.AlwaysOnTop = true
	running.server.BroadcastConfigPreview(cfg)
	for index, connection := range []*websocket.Conn{first, second} {
		event := readEnvelope(connection)
		if event.Type != "config" || event.Sequence != 2 ||
			event.Revision != 1 || !event.Transient ||
			event.Config == nil || !event.Config.UI.AlwaysOnTop {
			t.Fatalf("config preview %d = %#v", index, event)
		}
	}

	cfg.UI.Animations = !cfg.UI.Animations
	running.server.BroadcastConfig(cfg)
	for index, connection := range []*websocket.Conn{first, second} {
		event := readEnvelope(connection)
		if event.Type != "config" || event.Sequence != 3 ||
			event.Revision != 2 || event.Transient ||
			event.Config == nil || !event.Config.UI.AlwaysOnTop {
			t.Fatalf("config %d = %#v", index, event)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := running.server.Close(ctx); err != nil {
		t.Fatalf("close server: %v", err)
	}
	_ = first.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := first.ReadMessage(); err == nil {
		t.Fatal("WebSocket remained open after server shutdown")
	}
}

func TestConfiguredLocalhostIsPreservedForBrowserOrigin(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	server, err := NewWithOptions(Options{
		Listen:      "localhost:0",
		BearerToken: testToken,
		Handler:     handler.Handle,
		EventSource: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	if !strings.HasPrefix(server.BrowserURL(), "http://localhost:") {
		t.Fatalf("BrowserURL = %q", server.BrowserURL())
	}
}

func TestEmbeddedAppIsServedUnderAppPrefix(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	server, err := NewWithOptions(Options{
		Listen:      "127.0.0.1:0",
		BearerToken: testToken,
		Handler:     handler.Handle,
		EventSource: source,
		AppHandler:  webui.Handler(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close(context.Background())
	client := &http.Client{Timeout: 3 * time.Second}

	request, err := http.NewRequest(http.MethodGet, server.BrowserURL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Accept", "text/html")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	index, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK ||
		!strings.Contains(string(index), `<div id="app">`) {
		t.Fatalf("embedded index = %d %s", response.StatusCode, index)
	}
	if response.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("index Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	csp := response.Header.Get("Content-Security-Policy")
	expectedSocketOrigin := "ws" + strings.TrimPrefix(
		strings.TrimSuffix(server.BrowserURL(), "/app/"),
		"http",
	)
	if !strings.Contains(csp, "worker-src 'self'") ||
		!strings.Contains(csp, "media-src 'self' blob:") ||
		!strings.Contains(csp, "connect-src 'self' "+expectedSocketOrigin) ||
		!strings.Contains(csp, "http:") ||
		!strings.Contains(csp, "https:") ||
		!strings.Contains(csp, "ws:") ||
		!strings.Contains(csp, "wss:") ||
		strings.Contains(csp, "ws://127.0.0.1:*") {
		t.Fatalf("app CSP = %q", csp)
	}

	serviceWorker, err := client.Get(server.BrowserURL() + "sw.js")
	if err != nil {
		t.Fatal(err)
	}
	serviceWorker.Body.Close()
	if serviceWorker.StatusCode != http.StatusOK ||
		serviceWorker.Header.Get("Service-Worker-Allowed") != "/app/" ||
		serviceWorker.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf(
			"service worker headers = status %d, scope %q, cache %q",
			serviceWorker.StatusCode,
			serviceWorker.Header.Get("Service-Worker-Allowed"),
			serviceWorker.Header.Get("Cache-Control"),
		)
	}
}

func TestDiagnosticsBootstrapRoutesAndStreamUseParsedSnapshot(t *testing.T) {
	source := newTestDiagnosticsSource()
	handler := &recordedHandler{result: map[string]bool{"ok": true}}
	running := startBrowserAPI(t, source, handler.Handle)
	cookie, csrf := mintBrowserSession(t, running)

	request := func(method, path, body string) (*http.Response, []byte) {
		t.Helper()
		httpRequest, err := http.NewRequest(
			method,
			running.origin+path,
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		httpRequest.AddCookie(cookie)
		httpRequest.Header.Set("Origin", running.origin)
		httpRequest.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if isMutationMethod(method) {
			httpRequest.Header.Set(csrfHeader, csrf)
		}
		response, err := running.client.Do(httpRequest)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}

	bootstrapResponse, bootstrap := request(http.MethodGet, "/ui/bootstrap", "")
	if bootstrapResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(bootstrap), `"diagnostics":true`) ||
		!strings.Contains(string(bootstrap), `"device_name":"PCController"`) {
		t.Fatalf("diagnostics bootstrap = %d %s", bootstrapResponse.StatusCode, bootstrap)
	}
	if strings.Contains(string(bootstrap), "secret-raw-diagnostics") {
		t.Fatalf("diagnostics bootstrap leaked raw data: %s", bootstrap)
	}

	getResponse, getBody := request(http.MethodGet, "/ui/diagnostics", "")
	if getResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(getBody), `"device_name":"PCController"`) {
		t.Fatalf("GET diagnostics = %d %s", getResponse.StatusCode, getBody)
	}

	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/events"
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	connection, _, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	var hello StreamEnvelope
	if err := connection.ReadJSON(&hello); err != nil {
		t.Fatal(err)
	}
	if hello.Diagnostics == nil || hello.Diagnostics.Device.DeviceName != "PCController" {
		t.Fatalf("diagnostics hello = %#v", hello)
	}

	unsafeResponse, unsafeBody := request(
		http.MethodPost,
		"/ui/diagnostics/refresh",
		`{"endpoints":["/pu"]}`,
	)
	if unsafeResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsafe diagnostics = %d %s", unsafeResponse.StatusCode, unsafeBody)
	}

	refreshResponse, refreshBody := request(
		http.MethodPost,
		"/ui/diagnostics/refresh",
		`{"endpoints":["/info"]}`,
	)
	if refreshResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(refreshBody), `"diagnostics"`) {
		t.Fatalf("refresh diagnostics = %d %s", refreshResponse.StatusCode, refreshBody)
	}
	source.diagnosticsMu.Lock()
	refreshed := append([]string(nil), source.refreshed...)
	source.diagnosticsMu.Unlock()
	if len(refreshed) != 1 || refreshed[0] != "/info" {
		t.Fatalf("refresh endpoints = %#v", refreshed)
	}

	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var event StreamEnvelope
	if err := connection.ReadJSON(&event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "diagnostics" || event.Diagnostics == nil ||
		event.Diagnostics.Device.DeviceName != "PCController" {
		t.Fatalf("diagnostics event = %#v", event)
	}
}

func TestBrowserRegistrationRouteIsReadOnlyByDefaultAndCSRFProtected(t *testing.T) {
	source := newTestEventSource()
	handler := &recordedHandler{result: map[string]any{
		"executable":       `C:\Apps\PCControllerDesktop.exe`,
		"shell_registered": false,
		"com_registered":   false,
	}}
	running := startBrowserAPI(t, source, handler.Handle)
	cookie, csrf := mintBrowserSession(t, running)

	request := func(method, body, csrfValue string) (*http.Response, []byte) {
		t.Helper()
		httpRequest, err := http.NewRequest(
			method,
			running.origin+"/ui/integration/registration",
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}
		httpRequest.AddCookie(cookie)
		httpRequest.Header.Set("Origin", running.origin)
		httpRequest.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if csrfValue != "" {
			httpRequest.Header.Set(csrfHeader, csrfValue)
		}
		response, err := running.client.Do(httpRequest)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return response, data
	}

	getResponse, getBody := request(http.MethodGet, "", "")
	if getResponse.StatusCode != http.StatusOK ||
		!strings.Contains(string(getBody), `"shell_registered":false`) {
		t.Fatalf("GET registration = %d %s", getResponse.StatusCode, getBody)
	}
	if got := handler.last(t).Method; got != "integration.registration.status" {
		t.Fatalf("GET command = %q", got)
	}

	before := handler.count()
	noCSRFResponse, noCSRFBody := request(
		http.MethodPost,
		`{"method":"integration.shell.register"}`,
		"",
	)
	if noCSRFResponse.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without CSRF = %d %s", noCSRFResponse.StatusCode, noCSRFBody)
	}
	if handler.count() != before {
		t.Fatal("registration mutation reached handler without CSRF")
	}

	invalidResponse, invalidBody := request(
		http.MethodPost,
		`{"method":"quit"}`,
		csrf,
	)
	if invalidResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST invalid method = %d %s", invalidResponse.StatusCode, invalidBody)
	}
	if handler.count() != before {
		t.Fatal("unsupported registration method reached handler")
	}

	postResponse, postBody := request(
		http.MethodPost,
		`{"method":"integration.shell.register"}`,
		csrf,
	)
	if postResponse.StatusCode != http.StatusOK {
		t.Fatalf("POST registration = %d %s", postResponse.StatusCode, postBody)
	}
	if got := handler.last(t); got.Method != "integration.shell.register" ||
		len(got.Params) != 0 {
		t.Fatalf("POST command = %#v", got)
	}
}
