package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/diagnostics"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/model"
)

const csrfHeader = "X-RayanLamp-CSRF"

// PublicConfig is the browser-safe configuration representation. In
// particular, it has no field capable of serializing LocalAPIToken.
type PublicConfig struct {
	Lamp          config.LampConfig          `json:"lamp"`
	UI            config.UIConfig            `json:"ui"`
	Notifications config.NotificationsConfig `json:"notifications"`
	Hotkeys       config.HotkeysConfig       `json:"hotkeys"`
	Automation    config.AutomationConfig    `json:"automation"`
	Integration   PublicIntegrationConfig    `json:"integration"`
	Diagnostics   config.DiagnosticsConfig   `json:"diagnostics"`
}

type PublicIntegrationConfig struct {
	StartWithWindows        bool   `json:"start_with_windows"`
	LocalAPIEnabled         bool   `json:"local_api_enabled"`
	LocalAPIListen          string `json:"local_api_listen"`
	LocalAPITokenConfigured bool   `json:"local_api_token_configured"`
}

type sessionResult struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type bootstrapResult struct {
	Sequence     uint64                `json:"sequence"`
	Revision     uint64                `json:"revision"`
	Snapshot     model.Snapshot        `json:"snapshot"`
	Config       PublicConfig          `json:"config"`
	Version      map[string]string     `json:"version"`
	Capabilities map[string]bool       `json:"capabilities"`
	EventURL     string                `json:"event_url"`
	Diagnostics  *diagnostics.Snapshot `json:"diagnostics,omitempty"`
}

type uiConfigResult struct {
	Config   PublicConfig `json:"config"`
	Revision uint64       `json:"revision"`
}

type diagnosticsRefreshResult struct {
	Diagnostics diagnostics.Snapshot `json:"diagnostics"`
	Warning     string               `json:"warning,omitempty"`
}

func redactConfig(cfg config.Config) PublicConfig {
	cfg.Diagnostics.SafeEndpoints = append([]string(nil), cfg.Diagnostics.SafeEndpoints...)
	cfg.Diagnostics.ExpertEndpoints = append([]string(nil), cfg.Diagnostics.ExpertEndpoints...)
	return PublicConfig{
		Lamp:          cfg.Lamp,
		UI:            cfg.UI,
		Notifications: cfg.Notifications,
		Hotkeys:       cfg.Hotkeys,
		Automation:    cfg.Automation,
		Integration: PublicIntegrationConfig{
			StartWithWindows:        cfg.Integration.StartWithWindows,
			LocalAPIEnabled:         cfg.Integration.LocalAPIEnabled,
			LocalAPIListen:          cfg.Integration.LocalAPIListen,
			LocalAPITokenConfigured: cfg.Integration.LocalAPIToken != "",
		},
		Diagnostics: cfg.Diagnostics,
	}
}

func (a *api) session(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPost)
		return
	}
	if !a.validBrowserHost(r) || !a.validSessionMintRequest(r) {
		writeError(w, http.StatusForbidden, "forbidden", "browser session origin is not allowed")
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if existing, ok := a.sessions.authenticate(r); ok {
		writeJSON(w, http.StatusOK, response{
			OK: true,
			Result: sessionResult{
				CSRFToken: existing.csrfToken,
				ExpiresAt: existing.expires,
			},
		})
		return
	}
	cookieValue, csrfToken, expires, err := a.sessions.issue()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "unable to create browser session")
		return
	}
	http.SetCookie(w, browserCookie(cookieValue))
	writeJSON(w, http.StatusOK, response{
		OK: true,
		Result: sessionResult{
			CSRFToken: csrfToken,
			ExpiresAt: expires,
		},
	})
}

func (a *api) validSessionMintRequest(r *http.Request) bool {
	origin, validOriginHeader := requestOrigin(r)
	if !validOriginHeader {
		return false
	}
	fetchSite := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")))
	fetchMode := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Mode")))
	switch r.Method {
	case http.MethodPost:
		if !a.sameOrigin(origin) {
			return false
		}
		return fetchSite == "" || fetchSite == "same-origin"
	case http.MethodGet:
		if origin != "" && !a.sameOrigin(origin) {
			return false
		}
		switch fetchSite {
		case "", "same-origin":
			return true
		case "none":
			return fetchMode == "" || fetchMode == "navigate"
		default:
			return false
		}
	default:
		return false
	}
}

func requestOrigin(r *http.Request) (string, bool) {
	if r == nil {
		return "", false
	}
	values := r.Header.Values("Origin")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 {
		return "", false
	}
	value := strings.TrimSpace(values[0])
	if value != values[0] || strings.ContainsAny(value, "\r\n") {
		return "", false
	}
	return value, true
}

func (a *api) serveApp(w http.ResponseWriter, r *http.Request) {
	if !a.validBrowserHost(r) {
		writeError(w, http.StatusForbidden, "forbidden", "invalid browser host")
		return
	}
	if r.URL.Path == "/app" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
			return
		}
		http.Redirect(w, r, "/app/", http.StatusTemporaryRedirect)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet+", "+http.MethodHead)
		return
	}
	if a.appHandler == nil {
		writeError(w, http.StatusNotFound, "not_found", "web application is unavailable")
		return
	}
	// Buffer the small embedded response so the outer security boundary can
	// retain an exact-host CSP while preserving the asset handler's cache,
	// content type, ETag, range, and service-worker headers.
	buffered := newBufferedAppResponse()
	a.appHandler.ServeHTTP(buffered, r)
	for name, values := range buffered.header {
		w.Header()[name] = append([]string(nil), values...)
	}
	setAppSecurityHeaders(w, a.appConnectSources())
	w.WriteHeader(buffered.statusCode())
	if r.Method != http.MethodHead {
		_, _ = w.Write(buffered.body.Bytes())
	}
}

type bufferedAppResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newBufferedAppResponse() *bufferedAppResponse {
	return &bufferedAppResponse{header: make(http.Header)}
}

func (w *bufferedAppResponse) Header() http.Header {
	return w.header
}

func (w *bufferedAppResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *bufferedAppResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *bufferedAppResponse) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (a *api) uiBootstrap(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if a.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "browser state is unavailable")
		return
	}
	sequence, revision, snapshot, cfg, diagnosticsSnapshot := a.hub.current()
	capabilities := map[string]bool{
		"lamp_control":     true,
		"realtime_events":  true,
		"websocket_events": true,
		"native_shell":     true,
		"window_control":   true,
		"configuration":    true,
		"diagnostics":      a.diagnosticsSource != nil,
		"device_terminal":  true,
		"notifications":    true,
		"audio_sfx":        a.audioAsset != nil && a.audioImport != nil,
		"developer_tools":  buildinfo.DeveloperTools,
		"global_hotkeys": a.hotkeyCapture != nil &&
			a.hotkeyCapture.captureAvailable(),
		"hotkey_capture": a.hotkeyCapture != nil &&
			a.hotkeyCapture.captureAvailable(),
		"hotkey_probe": a.hotkeyCapture != nil &&
			a.hotkeyCapture.probeAvailable(),
	}
	for name, enabled := range a.capabilities {
		capabilities[name] = enabled
	}
	writeJSON(w, http.StatusOK, response{
		OK: true,
		Result: bootstrapResult{
			Sequence:     sequence,
			Revision:     revision,
			Snapshot:     snapshot,
			Config:       cfg,
			Version:      browserVersion(),
			Capabilities: capabilities,
			EventURL:     "/events",
			Diagnostics:  diagnosticsSnapshot,
		},
	})
}

func (a *api) uiConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if err := requireEmptyBody(r); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		if a.hub == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "browser configuration is unavailable")
			return
		}
		_, revision, _, cfg, _ := a.hub.current()
		w.Header().Set("ETag", quoteRevision(revision))
		writeJSON(w, http.StatusOK, response{
			OK: true,
			Result: uiConfigResult{
				Config:   cfg,
				Revision: revision,
			},
		})
	case http.MethodPatch:
		if !requireJSONContentType(w, r) {
			return
		}
		raw, err := readJSONBody(w, r)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		if err := validateBrowserConfigPatch(raw); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", err.Error())
			return
		}
		// Keep the revision precondition and the shared dispatcher mutation in
		// one browser transaction. Without this lock, two tabs could both
		// observe revision N before either callback broadcast revision N+1.
		a.uiConfigMu.Lock()
		defer a.uiConfigMu.Unlock()
		if a.hub == nil {
			writeError(w, http.StatusServiceUnavailable, "unavailable", "browser configuration is unavailable")
			return
		}
		if !a.requireUIConfigRevision(w, r) {
			return
		}
		a.writeBrowserConfigPatch(w, r, raw)
	default:
		methodNotAllowed(w, http.MethodGet+", "+http.MethodPatch)
	}
}

// requireUIConfigRevision enforces the same optimistic-concurrency contract
// for every browser convenience mutation. The caller must hold uiConfigMu.
func (a *api) requireUIConfigRevision(
	w http.ResponseWriter,
	r *http.Request,
) bool {
	ifMatch, present, err := parseIfMatchRevision(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_revision", err.Error())
		return false
	}
	if !present {
		writeError(
			w,
			http.StatusPreconditionRequired,
			"revision_required",
			"If-Match with the current configuration revision is required",
		)
		return false
	}
	if a.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "browser configuration is unavailable")
		return false
	}
	_, revision, _, _, _ := a.hub.current()
	if revision != ifMatch {
		writeError(
			w,
			http.StatusPreconditionFailed,
			"revision_conflict",
			"configuration revision is stale; reload and retry",
		)
		return false
	}
	return true
}

// writeBrowserConfigPatch applies a patch while uiConfigMu is held and returns
// only the browser-safe projection. Keeping this path shared prevents
// convenience mutations such as audio import from accidentally returning the
// local API bearer token or another native-only field.
func (a *api) writeBrowserConfigPatch(w http.ResponseWriter, r *http.Request, raw json.RawMessage) {
	result, err := a.invokeRaw(r, "config.patch", raw)
	if err != nil {
		status, code, message := mapCommandError(err)
		writeError(w, status, code, message)
		return
	}
	updated, err := decodeConfigResult(result)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "unable to decode updated configuration")
		return
	}
	public := redactConfig(updated)
	// Application wiring normally broadcasts during ConfigApplied before this
	// handler returns. Keep the local API independently correct when a custom
	// handler applies a config without that callback.
	if a.hub != nil {
		_, _, _, current, _ := a.hub.current()
		if !reflect.DeepEqual(current, public) {
			a.hub.broadcastConfig(updated)
		}
	}
	_, revision, _, current, _ := a.hub.current()
	w.Header().Set("ETag", quoteRevision(revision))
	writeJSON(w, http.StatusOK, response{
		OK: true,
		Result: uiConfigResult{
			Config:   current,
			Revision: revision,
		},
	})
}

func (a *api) uiDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if err := requireEmptyBody(r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if a.diagnosticsSource == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "diagnostics are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response{
		OK:     true,
		Result: a.diagnosticsSource.DiagnosticsSnapshot(),
	})
}

func (a *api) refreshUIDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodPost) {
		return
	}
	if a.diagnosticsSource == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "diagnostics are unavailable")
		return
	}
	var endpoints []string
	if r.Body != nil && r.Body != http.NoBody && r.ContentLength != 0 {
		if !requireJSONContentType(w, r) {
			return
		}
		raw, err := readJSONBody(w, r)
		if err != nil {
			writeBodyError(w, err)
			return
		}
		var params struct {
			Endpoints []string `json:"endpoints"`
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_json", "body may contain only an endpoints string array")
			return
		}
		if len(params.Endpoints) > 16 {
			writeError(w, http.StatusBadRequest, "invalid_endpoints", "too many diagnostics endpoints")
			return
		}
		endpoints = params.Endpoints
	}
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) != endpoint {
			writeError(w, http.StatusBadRequest, "invalid_endpoint", "diagnostics endpoints must be exact paths")
			return
		}
		if err := (config.DiagnosticsConfig{}).ValidateRequest(http.MethodGet, endpoint); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_endpoint", err.Error())
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	snapshot, refreshErr := a.diagnosticsSource.RefreshDiagnostics(ctx, endpoints...)
	result := diagnosticsRefreshResult{Diagnostics: snapshot}
	if refreshErr != nil {
		result.Warning = refreshErr.Error()
	}
	writeJSON(w, http.StatusOK, response{OK: true, Result: result})
}

func validateBrowserConfigPatch(raw []byte) error {
	var object map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return errors.New("configuration patch must be a JSON object")
	}
	integrationRaw, ok := object["integration"]
	if !ok {
		return nil
	}
	var integration map[string]json.RawMessage
	if err := json.Unmarshal(integrationRaw, &integration); err != nil ||
		integration == nil {
		return errors.New("integration patch must be a JSON object")
	}
	for key := range integration {
		switch key {
		case "start_with_windows", "local_api_enabled", "local_api_listen":
			// These are the complete browser-writable integration surface.
		case "local_api_token":
			return fmt.Errorf("browser sessions may not modify integration.local_api_token")
		default:
			return fmt.Errorf("browser sessions may not modify integration.%s", key)
		}
	}
	return nil
}

func (a *api) invokeRaw(r *http.Request, method string, params json.RawMessage) (any, error) {
	ctx, cancel := context.WithTimeout(r.Context(), commandTimeout)
	defer cancel()
	return a.handler(ctx, ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		ID:              a.requestID(),
		Method:          method,
		Params:          params,
	})
}

func decodeConfigResult(value any) (config.Config, error) {
	if cfg, ok := value.(config.Config); ok {
		return cfg, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return config.Config{}, err
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func browserVersion() map[string]string {
	return map[string]string{
		"name":         buildinfo.Name,
		"display_name": buildinfo.DisplayName,
		"version":      buildinfo.Version,
		"company":      buildinfo.Company,
		"description":  buildinfo.Description,
		"commit":       buildinfo.Commit,
		"date":         buildinfo.Date,
		"edition":      buildinfo.Edition,
	}
}

func parseIfMatchRevision(value string) (uint64, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	if strings.HasPrefix(value, "W/") {
		return 0, true, errors.New("If-Match must contain a strong numeric revision")
	}
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	if value == "" || strings.ContainsAny(value, ",* \t\r\n") {
		return 0, true, errors.New("If-Match must contain one numeric revision")
	}
	revision, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, true, errors.New("If-Match must contain one numeric revision")
	}
	return revision, true, nil
}

func quoteRevision(revision uint64) string {
	return `"` + strconv.FormatUint(revision, 10) + `"`
}

func (a *api) validBrowserHost(r *http.Request) bool {
	if a == nil || r == nil || a.expectedHost == "" {
		return false
	}
	host := strings.TrimSpace(r.Host)
	return host == r.Host && strings.EqualFold(host, a.expectedHost)
}

func (a *api) sameOrigin(raw string) bool {
	if a == nil || strings.TrimSpace(raw) == "" {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil ||
		!strings.EqualFold(parsed.Scheme, "http") ||
		parsed.User != nil ||
		parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return false
	}
	return strings.EqualFold(parsed.Host, a.expectedHost)
}

func (a *api) appConnectSources() []string {
	// Direct mode is browser-configurable and its device origin may differ
	// from the relay's persisted lamp URL. Keep executable content self-only,
	// while allowing the trusted bundled client to connect to an explicitly
	// selected HTTP(S)/WebSocket lamp endpoint.
	sources := []string{"'self'"}
	if a != nil && a.expectedHost != "" {
		sources = append(sources, "ws://"+a.expectedHost)
	}
	sources = append(sources, "http:", "https:", "ws:", "wss:")
	return uniqueConnectSources(sources)
}

func uniqueConnectSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	result := make([]string, 0, len(sources))
	for _, source := range sources {
		if source == "" {
			continue
		}
		if _, exists := seen[source]; exists {
			continue
		}
		seen[source] = struct{}{}
		result = append(result, source)
	}
	return result
}

func setAppSecurityHeaders(w http.ResponseWriter, connectSources []string) {
	header := w.Header()
	if header.Get("Cache-Control") == "" {
		header.Set("Cache-Control", "no-store")
	}
	if len(connectSources) == 0 {
		connectSources = []string{"'self'"}
	}
	header.Set(
		"Content-Security-Policy",
		"default-src 'self'; "+
			"base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; "+
			"script-src 'self'; style-src 'self'; font-src 'self'; "+
			"img-src 'self' data: blob:; "+
			"media-src 'self' blob:; "+
			"connect-src "+strings.Join(connectSources, " ")+"; "+
			"worker-src 'self'; manifest-src 'self'",
	)
	header.Set("Cross-Origin-Opener-Policy", "same-origin")
	header.Set("Cross-Origin-Resource-Policy", "same-origin")
	header.Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
}
