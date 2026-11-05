package device

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	pathpkg "path"
	"pccontroller.local/desktop/compatibility"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/model"
)

const (
	defaultHTTPTimeout = 6 * time.Second
	defaultBodyLimit   = int64(1 << 20)
	defaultUserAgent   = "PCControllerDesktop/1"
	maxEndpointLength  = 2048
)

var (
	ErrInvalidBaseURL   = errors.New("invalid device base URL")
	ErrUnsafeEndpoint   = errors.New("unsafe diagnostic endpoint")
	ErrUnexpectedState  = errors.New("device returned an unrecognized state")
	ErrResponseTooLarge = errors.New("device response exceeds the configured limit")
	ErrNilContext       = errors.New("nil context")
)

// HTTPStatusError reports a non-2xx response from the device.
type HTTPStatusError struct {
	StatusCode int
	Status     string
	Path       string
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("device request %s returned %s", e.Path, e.Status)
	}
	return fmt.Sprintf("device request %s returned %s: %s", e.Path, e.Status, e.Body)
}

// Option configures a Client.
type Option func(*Client) error

// Client is safe for concurrent HTTP calls and for one or more independent
// WebSocket listeners.
type Client struct {
	baseURL     *url.URL
	httpClient  *http.Client
	wsDialer    *websocket.Dialer
	userAgent   string
	maxBodySize int64
	ws          websocketConfig
	resolution  *resolutionCache
}

// New validates baseURL and creates a device client. A missing scheme defaults
// to http. Only a root http/https URL is accepted.
func New(baseURL string, options ...Option) (*Client, error) {
	normalized, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBaseURL, err)
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = defaultHTTPTimeout

	c := &Client{
		baseURL:     u,
		httpClient:  &http.Client{Timeout: defaultHTTPTimeout},
		wsDialer:    &dialer,
		userAgent:   defaultUserAgent,
		maxBodySize: defaultBodyLimit,
		ws:          newWebSocketConfig(),
		resolution:  newResolutionCache(nil),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		if err := option(c); err != nil {
			return nil, err
		}
	}
	if err := c.validateConfiguration(); err != nil {
		return nil, err
	}
	if err := c.configureNetworkClients(); err != nil {
		return nil, err
	}
	return c, nil
}

// NormalizeBaseURL validates and canonicalizes a device URL. The returned URL
// has no trailing slash, credentials, query, fragment, or path prefix.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("%w: URL is empty", ErrInvalidBaseURL)
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.Contains(raw, `\`) {
		return "", fmt.Errorf("%w: URL contains invalid characters", ErrInvalidBaseURL)
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidBaseURL, err)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%w: unsupported scheme %q", ErrInvalidBaseURL, u.Scheme)
	}
	if u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("%w: hostname is required", ErrInvalidBaseURL)
	}
	if u.User != nil {
		return "", fmt.Errorf("%w: credentials are not allowed", ErrInvalidBaseURL)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return "", fmt.Errorf("%w: query strings and fragments are not allowed", ErrInvalidBaseURL)
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("%w: a path prefix is not allowed", ErrInvalidBaseURL)
	}
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", fmt.Errorf("%w: invalid port", ErrInvalidBaseURL)
		}
	}

	u.Path = ""
	u.RawPath = ""
	return strings.TrimSuffix(u.String(), "/"), nil
}

// WithHTTPClient supplies an HTTP client. New clones it before installing the
// direct .local dialer and redirect policy, so the caller's value is untouched.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) error {
		if client == nil {
			return errors.New("device: nil HTTP client")
		}
		c.httpClient = client
		return nil
	}
}

// WithWebSocketDialer supplies a WebSocket dialer. New clones it before
// installing the shared .local resolver.
func WithWebSocketDialer(dialer *websocket.Dialer) Option {
	return func(c *Client) error {
		if dialer == nil {
			return errors.New("device: nil WebSocket dialer")
		}
		copy := *dialer
		c.wsDialer = &copy
		return nil
	}
}

// WithBodyLimit changes the maximum accepted HTTP response size.
func WithBodyLimit(bytes int64) Option {
	return func(c *Client) error {
		if bytes < 1 {
			return errors.New("device: body limit must be positive")
		}
		c.maxBodySize = bytes
		return nil
	}
}

// WithUserAgent changes the HTTP and WebSocket User-Agent header.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) error {
		userAgent = strings.TrimSpace(userAgent)
		if userAgent == "" || strings.IndexFunc(userAgent, unicode.IsControl) >= 0 {
			return errors.New("device: invalid User-Agent")
		}
		c.userAgent = userAgent
		return nil
	}
}

// BaseURL returns the normalized http/https base URL.
func (c *Client) BaseURL() string {
	if c == nil || c.baseURL == nil {
		return ""
	}
	return c.baseURL.String()
}

// WebSocketURL returns the corresponding /ws URL.
func (c *Client) WebSocketURL() string {
	if c == nil || c.baseURL == nil {
		return ""
	}
	u := *c.baseURL
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = "/ws"
	return u.String()
}

// CloseIdleConnections releases keep-alive HTTP connections which are not
// currently serving a request. It never interrupts an active request and is
// safe to call repeatedly during reconnect, configuration replacement, and
// shutdown.
func (c *Client) CloseIdleConnections() {
	if c == nil || c.httpClient == nil {
		return
	}
	c.httpClient.CloseIdleConnections()
}

// Get reads the compatibility device's state and requires a recognized response.
func (c *Client) Get(ctx context.Context) (model.OutputState, error) {
	state, body, err := c.command(ctx, compatibility.StateEndpoint("get"))
	if err != nil {
		return model.StateUnknown, err
	}
	if state == model.StateUnknown {
		return state, fmt.Errorf("%w: %q", ErrUnexpectedState, responseSnippet(body))
	}
	return state, nil
}

// On uses the compatibility state endpoint. A successful response without a state is interpreted
// as on, because the command itself is authoritative.
func (c *Client) On(ctx context.Context) (model.OutputState, error) {
	state, _, err := c.command(ctx, compatibility.StateEndpoint("on"))
	if err == nil && state == model.StateUnknown {
		state = model.StateOn
	}
	return state, err
}

// Off uses the compatibility state endpoint. A successful response without a state is interpreted
// as off, because the command itself is authoritative.
func (c *Client) Off(ctx context.Context) (model.OutputState, error) {
	state, _, err := c.command(ctx, compatibility.StateEndpoint("off"))
	if err == nil && state == model.StateUnknown {
		state = model.StateOff
	}
	return state, err
}

// Toggle uses the compatibility state endpoint and returns a state when the firmware includes
// one. StateUnknown with a nil error means the command succeeded but returned
// no state.
func (c *Client) Toggle(ctx context.Context) (model.OutputState, error) {
	state, _, err := c.command(ctx, compatibility.StateEndpoint("toggle"))
	return state, err
}

// Fetch reads a safe same-device diagnostic endpoint such as /info. Absolute
// URLs, traversal, fragments, and known state-changing device paths are rejected.
func (c *Client) Fetch(ctx context.Context, endpoint string) ([]byte, error) {
	ref, err := safeDiagnosticEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	u := c.resolve(ref.Path, ref.RawQuery)
	return c.doGET(ctx, u)
}

func (c *Client) command(ctx context.Context, endpoint string) (model.OutputState, []byte, error) {
	body, err := c.doGET(ctx, c.resolve(endpoint, ""))
	if err != nil {
		return model.StateUnknown, nil, err
	}
	return parseStatePayload(body), body, nil
}

func (c *Client) resolve(endpoint, rawQuery string) *url.URL {
	u := *c.baseURL
	u.Path = endpoint
	u.RawPath = ""
	u.RawQuery = rawQuery
	return &u
}

func (c *Client) doGET(ctx context.Context, u *url.URL) ([]byte, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/plain, application/json;q=0.9, */*;q=0.1")
	request.Header.Set("User-Agent", c.userAgent)
	request.Header.Set("Cache-Control", "no-cache, no-store")
	request.Header.Set("Pragma", "no-cache")

	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			c.resolution.invalidate()
		}
		return nil, fmt.Errorf("device GET %s: %w", u.EscapedPath(), err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, c.maxBodySize+1))
	if err != nil {
		if ctx.Err() == nil {
			c.resolution.invalidate()
		}
		return nil, fmt.Errorf("device GET %s: %w", u.EscapedPath(), err)
	}
	if int64(len(body)) > c.maxBodySize {
		return nil, ErrResponseTooLarge
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, &HTTPStatusError{
			StatusCode: response.StatusCode,
			Status:     response.Status,
			Path:       u.EscapedPath(),
			Body:       responseSnippet(body),
		}
	}
	return body, nil
}

func safeDiagnosticEndpoint(endpoint string) (*url.URL, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" || len(endpoint) > maxEndpointLength {
		return nil, fmt.Errorf("%w: endpoint is empty or too long", ErrUnsafeEndpoint)
	}
	if strings.IndexFunc(endpoint, unicode.IsControl) >= 0 || strings.Contains(endpoint, `\`) ||
		strings.HasPrefix(endpoint, "//") {
		return nil, fmt.Errorf("%w: invalid endpoint characters", ErrUnsafeEndpoint)
	}
	ref, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsafeEndpoint, err)
	}
	if ref.IsAbs() || ref.Opaque != "" || ref.Host != "" || ref.User != nil || ref.Fragment != "" {
		return nil, fmt.Errorf("%w: endpoint must be a same-device path", ErrUnsafeEndpoint)
	}
	if !strings.HasPrefix(ref.Path, "/") {
		ref.Path = "/" + ref.Path
		ref.RawPath = ""
	}
	if strings.Contains(ref.Path, `\`) {
		return nil, fmt.Errorf("%w: backslashes are not allowed", ErrUnsafeEndpoint)
	}
	for _, segment := range strings.Split(ref.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, fmt.Errorf("%w: path traversal is not allowed", ErrUnsafeEndpoint)
		}
	}
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(ref.Path, "/"))
	switch strings.ToLower(strings.TrimSuffix(cleaned, "/")) {
	case compatibility.StateEndpoint("on"), compatibility.StateEndpoint("off"), compatibility.StateEndpoint("toggle"):
		return nil, fmt.Errorf("%w: state-changing endpoint", ErrUnsafeEndpoint)
	}

	query, err := url.ParseQuery(ref.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid query string", ErrUnsafeEndpoint)
	}
	return &url.URL{Path: cleaned, RawQuery: query.Encode()}, nil
}

func parseStatePayload(body []byte) model.OutputState {
	text := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if state := model.ParseOutputState(text); state != model.StateUnknown {
		return state
	}

	var stringValue string
	if json.Unmarshal(body, &stringValue) == nil {
		if state := model.ParseOutputState(stringValue); state != model.StateUnknown {
			return state
		}
	}

	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return model.StateUnknown
	}
	for key, value := range object {
		if compatibility.IsStateField(key) {
			if json.Unmarshal(value, &stringValue) == nil {
				if state := model.ParseOutputState(stringValue); state != model.StateUnknown {
					return state
				}
			}
			var boolValue bool
			if json.Unmarshal(value, &boolValue) == nil {
				if boolValue {
					return model.StateOn
				}
				return model.StateOff
			}
		}
	}
	return model.StateUnknown
}

func responseSnippet(body []byte) string {
	const limit = 256
	text := strings.TrimSpace(string(body))
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, text)
	if len(text) > limit {
		text = text[:limit] + "…"
	}
	return text
}
