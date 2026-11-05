// Package localapi exposes the application's IPC commands over a small,
// loopback-only HTTP API.
package localapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/ipc"
)

const (
	maxBodyBytes         = int64(ipc.MaxMessageBytes)
	maxHeaderBytes       = 16 << 10
	maxNotificationRunes = 4096
	maxTitleRunes        = 128
	commandTimeout       = 12 * time.Second
	maxAudioBytes        = int64(4 << 20)
)

// Server is a running local API server. Addr is the listener's actual address,
// including its selected port when New is called with port zero.
type Server struct {
	Addr string

	httpServer *http.Server
	listener   net.Listener
	api        *api
	done       chan struct{}
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
}

type api struct {
	handler           ipc.Handler
	tokenHash         [sha256.Size]byte
	nextID            atomic.Uint64
	uiConfigMu        sync.Mutex
	expectedHost      string
	capabilities      map[string]bool
	sessions          *sessionStore
	hub               *eventHub
	diagnosticsSource DiagnosticsSource
	appHandler        http.Handler
	audioAsset        AudioAssetFunc
	audioImport       AudioImportFunc
	hotkeyCapture     *hotkeyCaptureCoordinator
	eventSockets      *webSocketRegistry
	terminalSockets   *terminalSocketRegistry
	// eventBeforeUpgrade is an unexported synchronization seam used by
	// lifecycle tests to hold an admitted event handshake immediately before
	// the HTTP connection is hijacked.
	eventBeforeUpgrade func()
	// terminalBeforeUpgrade is an unexported synchronization seam used by
	// lifecycle tests to hold an admitted handshake immediately before the
	// HTTP connection is hijacked.
	terminalBeforeUpgrade func()
}

// Options configures the HTTP API and its optional embedded browser surface.
// EventSource is required for browser bootstrap and real-time events, but is
// optional for legacy REST-only callers.
type Options struct {
	Listen      string
	BearerToken string
	Handler     ipc.Handler
	EventSource EventSource
	AppHandler  http.Handler
	// AudioAsset resolves the exact WAV bytes used by the native player.
	// AudioImport stores one validated custom WAV and returns its stable
	// content-addressed identity. Both are optional for REST-only hosts.
	AudioAsset  AudioAssetFunc
	AudioImport AudioImportFunc
	// Capabilities overrides the browser bootstrap defaults for a host that
	// deliberately omits native services. The GUI host normally leaves this
	// nil; the headless dashboard host reports its reduced native surface.
	Capabilities map[string]bool

	// HotkeyCapture suspends or restores the process-wide native hotkeys. It is
	// called with true for the first active browser capture lease, and false
	// when the final lease is released, expires, or the server closes. A
	// request-scoped false error must mean the transition was not applied, so
	// the lease can be retained and retried. Restore warnings after capture was
	// successfully disabled should be handled by the callback and return nil.
	HotkeyCapture func(context.Context, bool) error
	// HotkeyProbe checks whether a canonical chord is currently available. It
	// is called only while the requester holds a valid capture lease.
	HotkeyProbe func(context.Context, string) error
}

type response struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result,omitempty"`
	Error  *ipc.Error `json:"error,omitempty"`
}

type notificationParams struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}

// New starts a loopback HTTP server. listen must contain an explicit loopback
// host (for example, "127.0.0.1:0", "[::1]:0", or "localhost:0").
func New(listen, bearerToken string, handler ipc.Handler) (*Server, error) {
	return NewWithOptions(Options{
		Listen:      listen,
		BearerToken: bearerToken,
		Handler:     handler,
	})
}

// NewWithOptions starts a loopback HTTP server with the optional embedded app
// and shared application event source enabled.
func NewWithOptions(options Options) (*Server, error) {
	if options.Handler == nil {
		return nil, errors.New("localapi: nil IPC handler")
	}
	if err := validateBearerToken(options.BearerToken); err != nil {
		return nil, err
	}

	networkAddress, err := validateListenAddress(options.Listen)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", networkAddress)
	if err != nil {
		return nil, fmt.Errorf("localapi: listen on %s: %w", networkAddress, err)
	}
	if !listenerIsLoopback(listener) {
		_ = listener.Close()
		return nil, errors.New("localapi: listener did not resolve to a loopback address")
	}
	browserHost, err := browserAuthority(options.Listen, listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		return nil, err
	}

	apiHandler := newConfiguredAPI(
		options.BearerToken,
		options.Handler,
		browserHost,
		options.EventSource,
		options.AppHandler,
		options.AudioAsset,
		options.AudioImport,
		options.HotkeyCapture,
		options.HotkeyProbe,
	)
	apiHandler.capabilities = cloneCapabilities(options.Capabilities)
	httpServer := &http.Server{
		Handler:           apiHandler,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       15 * time.Second,
		// A complete parsed diagnostics refresh may make several bounded
		// firmware requests sequentially. Keep the transport deadline beyond
		// the handler's 45-second context so it can return its partial/last-good
		// snapshot instead of being cut off while writing the response.
		WriteTimeout:   60 * time.Second,
		IdleTimeout:    30 * time.Second,
		MaxHeaderBytes: maxHeaderBytes,
	}
	server := &Server{
		Addr:       listener.Addr().String(),
		httpServer: httpServer,
		listener:   listener,
		api:        apiHandler,
		done:       make(chan struct{}),
		closeDone:  make(chan struct{}),
	}
	go func() {
		defer close(server.done)
		_ = httpServer.Serve(listener)
	}()
	return server, nil
}

func cloneCapabilities(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]bool, len(source))
	for name, enabled := range source {
		name = strings.TrimSpace(name)
		if name != "" {
			result[name] = enabled
		}
	}
	return result
}

// Close gracefully shuts down the API server. If ctx expires, active
// connections are forcibly closed and the context error is returned.
func (s *Server) Close(ctx context.Context) error {
	if s == nil || s.httpServer == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	s.closeOnce.Do(func() {
		// Close both WebSocket admission gates and start their GoingAway
		// notifications before HTTP or host cleanup gets a chance to block.
		if s.api != nil {
			s.api.signalShutdown()
		}
		go s.runClose(ctx)
	})

	// Prefer an already completed shutdown even if the caller supplied an
	// already-cancelled context to an idempotent Close call.
	select {
	case <-s.closeDone:
		return s.closeErr
	default:
	}
	select {
	case <-s.closeDone:
		return s.closeErr
	case <-ctx.Done():
		// A later concurrent caller may have the shorter deadline. Force the
		// HTTP side closed for that caller without racing a second teardown.
		_ = s.httpServer.Close()
		return ctx.Err()
	}
}

func (s *Server) runClose(ctx context.Context) {
	var apiDone <-chan error
	if s.api != nil {
		result := make(chan error, 1)
		apiDone = result
		go func() {
			result <- s.api.finishShutdown(ctx)
		}()
	}

	httpErr := s.httpServer.Shutdown(ctx)
	if httpErr != nil {
		_ = s.httpServer.Close()
	}

	var apiErr error
	if apiDone != nil {
		select {
		case apiErr = <-apiDone:
		case <-ctx.Done():
			apiErr = ctx.Err()
		}
	}
	if apiErr != nil {
		_ = s.httpServer.Close()
	}

	if httpErr == nil && apiErr == nil {
		<-s.done
	} else {
		select {
		case <-s.done:
		case <-ctx.Done():
		}
	}
	s.closeErr = errors.Join(httpErr, apiErr)
	close(s.closeDone)
}

func newAPI(bearerToken string, handler ipc.Handler) *api {
	return newConfiguredAPI(
		bearerToken,
		handler,
		"",
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
}

func newConfiguredAPI(
	bearerToken string,
	handler ipc.Handler,
	expectedHost string,
	eventSource EventSource,
	appHandler http.Handler,
	audioAsset AudioAssetFunc,
	audioImport AudioImportFunc,
	hotkeyCapture func(context.Context, bool) error,
	hotkeyProbe func(context.Context, string) error,
) *api {
	diagnosticsSource, _ := eventSource.(DiagnosticsSource)
	return &api{
		handler:           handler,
		tokenHash:         sha256.Sum256([]byte(bearerToken)),
		expectedHost:      expectedHost,
		sessions:          newSessionStore(),
		hub:               newEventHub(eventSource),
		diagnosticsSource: diagnosticsSource,
		appHandler:        appHandler,
		audioAsset:        audioAsset,
		audioImport:       audioImport,
		eventSockets:      newWebSocketRegistry(maxStreamClients),
		terminalSockets:   newTerminalSocketRegistry(),
		hotkeyCapture: newHotkeyCaptureCoordinator(
			hotkeyCapture,
			hotkeyProbe,
			hotkeyCaptureLeaseTTL,
			maxHotkeyCaptureLeases,
		),
	}
}

// BrowserURL is the exact loopback origin used by the embedded application.
func (s *Server) BrowserURL() string {
	if s == nil || s.api == nil || s.api.expectedHost == "" {
		return ""
	}
	return "http://" + s.api.expectedHost + "/app/"
}

// BroadcastConfig immediately synchronizes a persisted configuration change
// to every connected browser tab without restarting the listener.
func (s *Server) BroadcastConfig(cfg config.Config) {
	if s != nil && s.api != nil && s.api.hub != nil {
		s.api.hub.broadcastConfig(cfg)
	}
}

// BroadcastConfigPreview synchronizes an in-progress native settings preview
// without advancing the persisted configuration revision.
func (s *Server) BroadcastConfigPreview(cfg config.Config) {
	if s != nil && s.api != nil && s.api.hub != nil {
		s.api.hub.broadcastConfigPreview(cfg)
	}
}

// BroadcastDiagnostics publishes an already parsed, redacted diagnostics
// snapshot to every connected browser.
func (s *Server) BroadcastDiagnostics(snapshot diagnostics.Snapshot) error {
	if s == nil || s.api == nil || s.api.hub == nil {
		return errors.New("localapi: event stream is unavailable")
	}
	s.api.hub.broadcastDiagnostics(snapshot)
	return nil
}

func (a *api) signalShutdown() {
	if a == nil {
		return
	}
	if a.eventSockets != nil {
		a.eventSockets.shutdown()
	}
	if a.terminalSockets != nil {
		a.terminalSockets.shutdown()
	}
	if a.sessions != nil {
		a.sessions.close()
	}
}

func (a *api) finishShutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.signalShutdown()

	type componentResult struct {
		err error
	}
	results := make(chan componentResult, 4)
	components := 0
	start := func(work func() error) {
		components++
		go func() {
			results <- componentResult{err: work()}
		}()
	}
	if a.eventSockets != nil {
		start(func() error { return a.eventSockets.wait(ctx) })
	}
	if a.terminalSockets != nil {
		start(func() error { return a.terminalSockets.wait(ctx) })
	}
	if a.hub != nil {
		start(func() error {
			a.hub.close()
			return nil
		})
	}
	if a.hotkeyCapture != nil {
		start(func() error { return a.hotkeyCapture.close(ctx) })
	}

	var joined error
	for range components {
		select {
		case result := <-results:
			joined = errors.Join(joined, result.err)
		case <-ctx.Done():
			return errors.Join(joined, ctx.Err())
		}
	}
	return joined
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)

	if r.URL.Path == "/" {
		if !a.validBrowserHost(r) {
			writeError(w, http.StatusForbidden, "forbidden", "invalid browser host")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
			return
		}
		http.Redirect(w, r, "/app/", http.StatusTemporaryRedirect)
		return
	}
	if r.URL.Path == "/app/session" {
		a.session(w, r)
		return
	}
	if r.URL.Path == "/app" || strings.HasPrefix(r.URL.Path, "/app/") {
		a.serveApp(w, r)
		return
	}
	if r.URL.Path == "/healthz" {
		if !allowMethod(w, r, http.MethodGet) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	if r.URL.Path == "/events" {
		a.eventsWebSocket(w, r)
		return
	}
	if r.URL.Path == "/ui/terminal" {
		a.terminalWebSocket(w, r)
		return
	}
	if !isCoordinatorAPIPath(r.URL.Path) {
		writeError(w, http.StatusNotFound, "not_found", "route not found")
		return
	}
	mode, session := a.authenticate(r)
	if mode == authenticationNone {
		w.Header().Set("WWW-Authenticate", `Bearer realm="RayanLamp local API"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
		return
	}
	if mode == authenticationSession && !a.validSessionRequest(r, session) {
		writeError(w, http.StatusForbidden, "forbidden", "browser origin or CSRF token is invalid")
		return
	}

	if strings.HasPrefix(r.URL.Path, "/ui/") {
		if mode != authenticationSession {
			writeError(w, http.StatusForbidden, "forbidden", "browser session required")
			return
		}
		switch r.URL.Path {
		case "/ui/bootstrap":
			a.uiBootstrap(w, r)
		case "/ui/config":
			a.uiConfig(w, r)
		case "/ui/diagnostics":
			a.uiDiagnostics(w, r)
		case "/ui/diagnostics/refresh":
			a.refreshUIDiagnostics(w, r)
		case "/ui/hotkeys/capture":
			a.uiHotkeyCapture(w, r)
		case "/ui/hotkeys/probe":
			a.uiHotkeyProbe(w, r)
		case "/ui/integration/registration":
			a.integrationRegistration(w, r)
		case "/ui/audio/import":
			a.uiAudioImport(w, r)
		default:
			if strings.HasPrefix(r.URL.Path, "/ui/audio/") {
				a.uiAudioAsset(w, r)
			} else {
				writeError(w, http.StatusNotFound, "not_found", "route not found")
			}
		}
		return
	}
	if mode == authenticationSession && r.URL.Path == "/config" {
		writeError(w, http.StatusForbidden, "forbidden", "browser sessions must use /ui/config")
		return
	}
	if mode == authenticationSession && r.URL.Path == "/audio/import" {
		writeError(w, http.StatusForbidden, "forbidden", "browser sessions must use /ui/audio/import")
		return
	}

	switch r.URL.Path {
	case "/ping":
		a.noBodyCommand(w, r, http.MethodGet, "ping")
	case "/status":
		a.noBodyCommand(w, r, http.MethodGet, "status")
	case "/lamp/on":
		a.noBodyCommand(w, r, http.MethodPost, "lamp.on")
	case "/lamp/off":
		a.noBodyCommand(w, r, http.MethodPost, "lamp.off")
	case "/lamp/toggle":
		a.noBodyCommand(w, r, http.MethodPost, "lamp.toggle")
	case "/lamp/refresh":
		a.noBodyCommand(w, r, http.MethodPost, "lamp.refresh")
	case "/window/show":
		a.noBodyCommand(w, r, http.MethodPost, "window.show")
	case "/window/hide":
		a.noBodyCommand(w, r, http.MethodPost, "window.hide")
	case "/settings/show":
		a.noBodyCommand(w, r, http.MethodPost, "settings.show")
	case "/diagnostics/show":
		a.noBodyCommand(w, r, http.MethodPost, "diagnostics.show")
	case "/web/show":
		a.noBodyCommand(w, r, http.MethodPost, "web.show")
	case "/reconnect":
		a.noBodyCommand(w, r, http.MethodPost, "reconnect")
	case "/diagnostics":
		a.diagnostics(w, r)
	case "/terminal/status":
		a.noBodyCommand(w, r, http.MethodGet, "terminal.status")
	case "/terminal/connect":
		a.noBodyCommand(w, r, http.MethodPost, "terminal.connect")
	case "/terminal/disconnect":
		a.noBodyCommand(w, r, http.MethodPost, "terminal.disconnect")
	case "/terminal/output":
		a.terminalOutput(w, r)
	case "/terminal/input":
		a.terminalInput(w, r)
	case "/device/config":
		a.noBodyCommand(w, r, http.MethodGet, "device.config.get")
	case "/device/config/refresh":
		a.noBodyCommand(w, r, http.MethodPost, "device.config.refresh")
	case "/config":
		a.config(w, r)
	case "/integration/registration":
		a.integrationRegistration(w, r)
	case "/notifications":
		a.notifications(w, r)
	case "/audio/play":
		a.audioPlayback(w, r)
	case "/audio/import":
		a.audioFileImport(w, r)
	case "/version":
		a.noBodyCommand(w, r, http.MethodGet, "version")
	case "/quit":
		a.noBodyCommand(w, r, http.MethodPost, "quit")
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	}
}

func isCoordinatorAPIPath(value string) bool {
	switch value {
	case "/ping",
		"/status",
		"/reconnect",
		"/diagnostics",
		"/diagnostics/show",
		"/device/config",
		"/device/config/refresh",
		"/config",
		"/integration/registration",
		"/notifications",
		"/audio/play",
		"/audio/import",
		"/version",
		"/settings/show",
		"/web/show",
		"/quit":
		return true
	}
	for _, prefix := range []string{
		"/lamp/",
		"/window/",
		"/terminal/",
		"/ui/",
	} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func (a *api) terminalOutput(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	query := r.URL.Query()
	for name, values := range query {
		if name != "after" && name != "limit" && name != "wait_ms" {
			writeError(w, http.StatusBadRequest, "invalid_query", "unknown terminal output parameter")
			return
		}
		if len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_query", "terminal output parameters must appear once")
			return
		}
	}
	parseUint := func(name string, bits int) (uint64, bool) {
		value := strings.TrimSpace(query.Get(name))
		if value == "" {
			return 0, true
		}
		parsed, err := strconv.ParseUint(value, 10, bits)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_query", name+" must be an unsigned integer")
			return 0, false
		}
		return parsed, true
	}
	after, ok := parseUint("after", 64)
	if !ok {
		return
	}
	limit, ok := parseUint("limit", 31)
	if !ok {
		return
	}
	waitMS, ok := parseUint("wait_ms", 31)
	if !ok {
		return
	}
	a.execute(w, r, "terminal.read", map[string]any{
		"after":   after,
		"limit":   int(limit),
		"wait_ms": int(waitMS),
	})
}

func (a *api) terminalInput(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	raw, err := readJSONBody(w, r)
	if err != nil {
		writeBodyError(w, err)
		return
	}
	var params struct {
		DataBase64 string `json:"data_base64"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "body must contain only data_base64")
		return
	}
	if strings.TrimSpace(params.DataBase64) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "data_base64 is required")
		return
	}
	a.execute(w, r, "terminal.write", params)
}

type authenticationMode uint8

const (
	authenticationNone authenticationMode = iota
	authenticationBearer
	authenticationSession
)

func (a *api) authenticate(r *http.Request) (authenticationMode, browserSession) {
	if a.authorized(r) {
		return authenticationBearer, browserSession{}
	}
	if session, ok := a.sessions.authenticate(r); ok {
		return authenticationSession, session
	}
	return authenticationNone, browserSession{}
}

func (a *api) validSessionRequest(r *http.Request, session browserSession) bool {
	if !a.validBrowserHost(r) {
		return false
	}
	origin, validOriginHeader := requestOrigin(r)
	if !validOriginHeader {
		return false
	}
	if origin != "" && !a.sameOrigin(origin) {
		return false
	}
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	if fetchSite != "" && fetchSite != "same-origin" && fetchSite != "none" {
		return false
	}
	if !isMutationMethod(r.Method) {
		return true
	}
	if !a.sameOrigin(origin) || fetchSite == "none" {
		return false
	}
	csrfValues := r.Header.Values(csrfHeader)
	return len(csrfValues) == 1 &&
		a.sessions.validateCSRF(session, csrfValues[0])
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func (a *api) authorized(r *http.Request) bool {
	values := r.Header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	const prefix = "Bearer "
	value := values[0]
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return false
	}
	candidate := value[len(prefix):]
	if strings.TrimSpace(candidate) != candidate || strings.ContainsAny(candidate, "\r\n\t ") {
		return false
	}
	candidateHash := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(a.tokenHash[:], candidateHash[:]) == 1
}

func (a *api) noBodyCommand(w http.ResponseWriter, r *http.Request, method, command string) {
	if !allowMethod(w, r, method) {
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	a.execute(w, r, command, nil)
}

func (a *api) diagnostics(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	query := r.URL.Query()
	if len(query) != 1 {
		writeError(w, http.StatusBadRequest, "invalid_query", "exactly one endpoint parameter is required")
		return
	}
	endpoints, ok := query["endpoint"]
	if !ok || len(endpoints) != 1 || strings.TrimSpace(endpoints[0]) == "" {
		writeError(w, http.StatusBadRequest, "invalid_query", "exactly one endpoint parameter is required")
		return
	}
	a.execute(w, r, "diagnostics.fetch", map[string]string{"endpoint": endpoints[0]})
}

func (a *api) config(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if err := requireEmptyBody(r); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		a.execute(w, r, "config.get", nil)
	case http.MethodPatch:
		if !requireJSONContentType(w, r) {
			return
		}
		raw, err := readJSONBody(w, r)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		trimmed := strings.TrimSpace(string(raw))
		if len(trimmed) < 2 || trimmed[0] != '{' {
			writeError(w, http.StatusBadRequest, "invalid_body", "configuration patch must be a JSON object")
			return
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "request body must contain one valid JSON object")
			return
		}
		a.executeRaw(w, r, "config.patch", raw)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPatch)
	}
}

func (a *api) notifications(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	raw, err := readJSONBody(w, r)
	if err != nil {
		writeBodyError(w, err)
		return
	}
	var params notificationParams
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "body must contain only title and message strings")
		return
	}
	if !utf8.ValidString(params.Title) || !utf8.ValidString(params.Message) {
		writeError(w, http.StatusBadRequest, "invalid_body", "title and message must be valid UTF-8")
		return
	}
	if utf8.RuneCountInString(params.Title) > maxTitleRunes {
		writeError(w, http.StatusBadRequest, "invalid_body", "title exceeds 128 characters")
		return
	}
	if strings.TrimSpace(params.Message) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "message is required")
		return
	}
	if utf8.RuneCountInString(params.Message) > maxNotificationRunes {
		writeError(w, http.StatusBadRequest, "invalid_body", "message exceeds 4096 characters")
		return
	}
	a.execute(w, r, "notifications.show", params)
}

func (a *api) audioPlayback(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	hasBody := r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0
	if !hasBody {
		a.execute(w, r, "audio.play", map[string]any{
			"cue":   "message",
			"count": 1,
		})
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	raw, err := readJSONBody(w, r)
	if err != nil {
		writeBodyError(w, err)
		return
	}
	a.executeRaw(w, r, "audio.play", raw)
}

func (a *api) audioFileImport(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if !requireJSONContentType(w, r) {
		return
	}
	raw, err := readJSONBody(w, r)
	if err != nil {
		writeBodyError(w, err)
		return
	}
	a.executeRaw(w, r, "audio.import", raw)
}

func (a *api) execute(w http.ResponseWriter, r *http.Request, method string, params any) {
	request, err := ipc.NewRequest(a.requestID(), method, params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "unable to encode command")
		return
	}
	a.invoke(w, r, request)
}

func (a *api) executeRaw(w http.ResponseWriter, r *http.Request, method string, params json.RawMessage) {
	request := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID:              a.requestID(),
		Method:          method,
		Params:          params,
	}
	a.invoke(w, r, request)
}

func (a *api) invoke(w http.ResponseWriter, r *http.Request, request ipc.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), commandTimeout)
	defer cancel()

	result, err := a.handler(ctx, request)
	if err != nil {
		status, code, message := mapCommandError(err)
		writeError(w, status, code, message)
		return
	}
	writeJSON(w, http.StatusOK, response{OK: true, Result: result})
}

func (a *api) requestID() string {
	return "http-" + strconv.FormatUint(a.nextID.Add(1), 10)
}

func validateListenAddress(listen string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listen))
	if err != nil {
		return "", fmt.Errorf("localapi: invalid listen address: %w", err)
	}
	if host == "" {
		return "", errors.New("localapi: listen address must include an explicit loopback host")
	}
	if port == "" {
		return "", errors.New("localapi: listen address must include a port")
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return "", errors.New("localapi: listen port must be numeric")
	}

	switch strings.ToLower(host) {
	case "localhost":
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("localapi: listen host must be a loopback IP address or localhost")
	}
	return net.JoinHostPort(ip.String(), port), nil
}

func browserAuthority(configured, actual string) (string, error) {
	configuredHost, _, err := net.SplitHostPort(strings.TrimSpace(configured))
	if err != nil {
		return "", fmt.Errorf("localapi: invalid configured browser address: %w", err)
	}
	_, actualPort, err := net.SplitHostPort(actual)
	if err != nil {
		return "", fmt.Errorf("localapi: invalid listener browser address: %w", err)
	}
	if strings.EqualFold(configuredHost, "localhost") {
		return net.JoinHostPort("localhost", actualPort), nil
	}
	ip := net.ParseIP(configuredHost)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("localapi: browser host must be loopback")
	}
	return net.JoinHostPort(ip.String(), actualPort), nil
}

func listenerIsLoopback(listener net.Listener) bool {
	address, ok := listener.Addr().(*net.TCPAddr)
	return ok && address.IP != nil && address.IP.IsLoopback()
}

func validateBearerToken(token string) error {
	if token == "" {
		return errors.New("localapi: bearer token must not be empty")
	}
	if strings.TrimSpace(token) != token || strings.ContainsAny(token, "\r\n\t ") {
		return errors.New("localapi: bearer token must not contain whitespace")
	}
	return nil
}

func requireEmptyBody(r *http.Request) error {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	if r.ContentLength == 0 {
		return nil
	}
	var one [1]byte
	n, err := r.Body.Read(one[:])
	if n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return errors.New("request body must be empty")
	}
	return nil
}

func requireJSONContentType(w http.ResponseWriter, r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	return true
}

func readJSONBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, errors.New("request body is required")
	}
	if !json.Valid(data) {
		return nil, errors.New("request body must contain valid JSON")
	}
	return data, nil
}

func writeBodyError(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body exceeds 1 MiB")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
}

func allowMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	methodNotAllowed(w, method)
	return false
}

func methodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func mapCommandError(err error) (status int, code, message string) {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, "timeout", "command timed out"
	}
	if errors.Is(err, context.Canceled) {
		return http.StatusRequestTimeout, "canceled", "request canceled"
	}

	code = "internal_error"
	message = "internal server error"
	var coded ipc.CodedError
	if !errors.As(err, &coded) {
		return http.StatusInternalServerError, code, message
	}
	code = strings.ToLower(strings.TrimSpace(coded.Code()))
	if code == "" {
		return http.StatusInternalServerError, "internal_error", message
	}
	message = err.Error()
	switch code {
	case "bad_request", "invalid_argument", "validation_error", "invalid_config",
		"invalid_endpoint", "unsafe_endpoint", "invalid_request":
		status = http.StatusBadRequest
	case "unauthorized", "unauthenticated":
		status = http.StatusUnauthorized
	case "forbidden", "access_denied":
		status = http.StatusForbidden
	case "not_found":
		status = http.StatusNotFound
	case "conflict", "already_running", "partial":
		status = http.StatusConflict
	case "request_too_large", "too_large":
		status = http.StatusRequestEntityTooLarge
	case "rate_limited":
		status = http.StatusTooManyRequests
	case "offline", "unavailable", "device_unavailable", "not_connected", "busy":
		status = http.StatusServiceUnavailable
	case "timeout", "deadline_exceeded":
		status = http.StatusGatewayTimeout
	case "unsupported", "not_supported":
		status = http.StatusNotImplemented
	default:
		status = http.StatusInternalServerError
	}
	return status, code, message
}

func setSecurityHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, response{
		OK:    false,
		Error: &ipc.Error{Code: code, Message: message},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		data = []byte(`{"ok":false,"error":{"code":"internal_error","message":"unable to encode response"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}
