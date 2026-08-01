// Package localdevice implements the PCController Local Device v1 contract.
// It contains bounded local-network transport and typed protocol values only;
// application configuration and UI wiring live outside this package.
package localdevice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ContractVersion  = "pccontroller.local-device/v1"
	CapabilitiesPath = "/pccontroller/device/v1/capabilities"
	SnapshotPath     = "/pccontroller/device/v1/snapshot"
	ActionsPath      = "/pccontroller/device/v1/actions"
	EventsPath       = "/pccontroller/device/v1/events"

	defaultRequestTimeout = 5 * time.Second
	defaultBodyLimit      = int64(1 << 20)
	maxMessageBytes       = 256
	maxIdentityBytes      = 160
)

var (
	ErrInvalidBaseURL       = errors.New("invalid local device base URL")
	ErrUnsafeResolvedHost   = errors.New("local device host resolved outside the local network")
	ErrInvalidAction        = errors.New("invalid local device action")
	ErrInvalidResponse      = errors.New("invalid local device response")
	ErrResponseTooLarge     = errors.New("local device response exceeds the configured limit")
	ErrActionRejected       = errors.New("local device rejected the action")
	ErrUnsupportedTransport = errors.New("local device client requires an HTTP transport")
)

// ActionType is one operation in the fixed Local Device v1 action vocabulary.
type ActionType string

const (
	ActionPowerOn        ActionType = "power.on"
	ActionPowerOff       ActionType = "power.off"
	ActionPowerToggle    ActionType = "power.toggle"
	ActionDisplayMessage ActionType = "display.message"
	ActionAlertPulse     ActionType = "alert.pulse"
)

// PowerState is the device's reported power state.
type PowerState string

const (
	PowerUnknown PowerState = "unknown"
	PowerOn      PowerState = "on"
	PowerOff     PowerState = "off"
)

// Capabilities is the complete, fixed-shape capability document returned by
// GET CapabilitiesPath. Unknown JSON members are rejected.
type Capabilities struct {
	Contract string       `json:"contract"`
	DeviceID string       `json:"device_id"`
	Name     string       `json:"name,omitempty"`
	Model    string       `json:"model,omitempty"`
	Firmware string       `json:"firmware,omitempty"`
	Actions  []ActionType `json:"actions"`
	Events   []EventType  `json:"events,omitempty"`
}

// Snapshot is the typed state document returned by GET SnapshotPath and
// optionally carried by action results and events.
type Snapshot struct {
	Contract       string     `json:"contract"`
	DeviceID       string     `json:"device_id"`
	Sequence       uint64     `json:"sequence"`
	Power          PowerState `json:"power"`
	DisplayMessage string     `json:"display_message,omitempty"`
	AlertPulses    int        `json:"alert_pulses,omitempty"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Action is the only request body accepted by POST ActionsPath. Fields not
// used by the selected Type must remain at their zero values.
type Action struct {
	Type    ActionType `json:"type"`
	Message string     `json:"message,omitempty"`
	Pulses  int        `json:"pulses,omitempty"`
}

// ActionResult is the fixed-shape success document returned by ActionsPath.
type ActionResult struct {
	Contract    string     `json:"contract"`
	Accepted    bool       `json:"accepted"`
	Action      ActionType `json:"action"`
	CompletedAt time.Time  `json:"completed_at"`
	Snapshot    *Snapshot  `json:"snapshot,omitempty"`
}

// HTTPStatusError reports a non-success response without retaining its body.
type HTTPStatusError struct {
	Method     string
	Path       string
	StatusCode int
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("local device %s %s returned HTTP %d", e.Method, e.Path, e.StatusCode)
}

// ClientOptions sets transport and resource bounds. A supplied HTTP client is
// copied before its timeout, proxy, redirect, and dial behavior are hardened.
type ClientOptions struct {
	HTTPClient *http.Client
	Timeout    time.Duration
	BodyLimit  int64
	UserAgent  string
}

// Client talks to exactly one normalized local device root.
type Client struct {
	base      *url.URL
	http      *http.Client
	bodyLimit int64
	userAgent string
}

// NormalizeBaseURL validates and canonicalizes a root HTTP(S) URL. Public IPs,
// public-looking DNS names, credentials, paths, queries, fragments, and control
// characters are rejected. A missing scheme defaults to http.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\t\\") {
		return "", ErrInvalidBaseURL
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil {
		return "", ErrInvalidBaseURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", ErrInvalidBaseURL
	}
	if parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", ErrInvalidBaseURL
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", ErrInvalidBaseURL
	}
	host := parsed.Hostname()
	if host == "" || !isLocalHostname(host) {
		return "", ErrInvalidBaseURL
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return "", ErrInvalidBaseURL
		}
	}
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.ForceQuery = false
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

func isLocalHostname(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if zone := strings.LastIndexByte(host, '%'); zone >= 0 {
		ip := net.ParseIP(host[:zone])
		return ip != nil && ip.IsLinkLocalUnicast()
	}
	if ip := net.ParseIP(host); ip != nil {
		return isLocalIP(ip)
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".lan") ||
		strings.HasSuffix(host, ".home.arpa") {
		return validHostname(host)
	}
	return !strings.Contains(host, ".") && validHostname(host)
}

func validHostname(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if !((character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') || character == '-') {
				return false
			}
		}
	}
	return true
}

func isLocalIP(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// NewClient creates a client whose every dial is re-resolved and constrained
// to loopback, private, or link-local addresses.
func NewClient(baseURL string, options ClientOptions) (*Client, error) {
	normalized, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return nil, ErrInvalidBaseURL
	}
	httpClient := &http.Client{}
	if options.HTTPClient != nil {
		copyClient := *options.HTTPClient
		httpClient = &copyClient
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	httpClient.Timeout = timeout
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	transport, err := localOnlyTransport(httpClient.Transport)
	if err != nil {
		return nil, err
	}
	httpClient.Transport = transport
	bodyLimit := options.BodyLimit
	if bodyLimit <= 0 {
		bodyLimit = defaultBodyLimit
	}
	return &Client{
		base:      parsed,
		http:      httpClient,
		bodyLimit: bodyLimit,
		userAgent: strings.TrimSpace(options.UserAgent),
	}, nil
}

func localOnlyTransport(roundTripper http.RoundTripper) (*http.Transport, error) {
	if roundTripper == nil {
		roundTripper = http.DefaultTransport
	}
	source, ok := roundTripper.(*http.Transport)
	if !ok {
		return nil, ErrUnsupportedTransport
	}
	transport := source.Clone()
	transport.Proxy = nil
	transport.DialContext = dialLocalAddress
	transport.DialTLS = nil
	transport.DialTLSContext = nil
	return transport, nil
}

func dialLocalAddress(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	parseHost := host
	if zone := strings.LastIndexByte(parseHost, '%'); zone >= 0 {
		parseHost = parseHost[:zone]
	}
	dialer := &net.Dialer{}
	if ip := net.ParseIP(parseHost); ip != nil {
		if !isLocalIP(ip) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeResolvedHost, host)
		}
		return dialer.DialContext(ctx, network, address)
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("local device host %q resolved to no addresses", host)
	}
	for _, candidate := range addresses {
		if !isLocalIP(candidate.IP) {
			return nil, fmt.Errorf("%w: %s resolved to %s", ErrUnsafeResolvedHost, host, candidate.IP)
		}
	}
	var dialErrors []error
	for _, candidate := range addresses {
		candidateHost := candidate.IP.String()
		if candidate.Zone != "" {
			candidateHost += "%" + candidate.Zone
		}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(candidateHost, port))
		if err == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, err)
	}
	return nil, errors.Join(dialErrors...)
}

// BaseURL returns the normalized HTTP(S) root.
func (client *Client) BaseURL() string { return client.base.String() }

// CloseIdleConnections releases pooled HTTP connections.
func (client *Client) CloseIdleConnections() { client.http.CloseIdleConnections() }

// EventsURL returns the corresponding ws or wss Local Device v1 event URL.
func (client *Client) EventsURL() string {
	result := *client.base
	if result.Scheme == "https" {
		result.Scheme = "wss"
	} else {
		result.Scheme = "ws"
	}
	result.Path = EventsPath
	return result.String()
}

// Capabilities performs the fixed capability GET and validates its schema.
func (client *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var result Capabilities
	if err := client.doJSON(ctx, http.MethodGet, CapabilitiesPath, nil, &result); err != nil {
		return Capabilities{}, err
	}
	if err := validateCapabilities(result); err != nil {
		return Capabilities{}, err
	}
	return cloneCapabilities(result), nil
}

// Snapshot performs a passive state GET. It never sends a WebSocket command.
func (client *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var result Snapshot
	if err := client.doJSON(ctx, http.MethodGet, SnapshotPath, nil, &result); err != nil {
		return Snapshot{}, err
	}
	if err := validateSnapshot(result); err != nil {
		return Snapshot{}, err
	}
	return result, nil
}

// Action validates and posts one typed Local Device v1 action.
func (client *Client) Action(ctx context.Context, action Action) (ActionResult, error) {
	if err := action.Validate(); err != nil {
		return ActionResult{}, err
	}
	var result ActionResult
	if err := client.doJSON(ctx, http.MethodPost, ActionsPath, action, &result); err != nil {
		return ActionResult{}, err
	}
	if result.Contract != ContractVersion || result.Action != action.Type {
		return ActionResult{}, fmt.Errorf("%w: action result identity", ErrInvalidResponse)
	}
	if !result.Accepted {
		return ActionResult{}, ErrActionRejected
	}
	if result.CompletedAt.IsZero() {
		return ActionResult{}, fmt.Errorf("%w: missing action completion time", ErrInvalidResponse)
	}
	if result.Snapshot != nil {
		if err := validateSnapshot(*result.Snapshot); err != nil {
			return ActionResult{}, err
		}
		copySnapshot := *result.Snapshot
		result.Snapshot = &copySnapshot
	}
	return result, nil
}

// Validate enforces the exact parameter shape for the selected action type.
func (action Action) Validate() error {
	switch action.Type {
	case ActionPowerOn, ActionPowerOff, ActionPowerToggle:
		if action.Message != "" || action.Pulses != 0 {
			return fmt.Errorf("%w: power actions do not accept parameters", ErrInvalidAction)
		}
	case ActionDisplayMessage:
		if action.Pulses != 0 || !validDisplayMessage(action.Message) {
			return fmt.Errorf("%w: display message must be valid UTF-8, contain no NUL, and be at most %d bytes", ErrInvalidAction, maxMessageBytes)
		}
	case ActionAlertPulse:
		if action.Message != "" || action.Pulses < 1 || action.Pulses > 10 {
			return fmt.Errorf("%w: alert pulses must be between 1 and 10", ErrInvalidAction)
		}
	default:
		return fmt.Errorf("%w: unsupported type %q", ErrInvalidAction, action.Type)
	}
	return nil
}

func validDisplayMessage(message string) bool {
	return utf8.ValidString(message) && len([]byte(message)) <= maxMessageBytes &&
		!strings.ContainsRune(message, 0)
}

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	requestBody any,
	responseBody any,
) error {
	if ctx == nil {
		return errors.New("local device request requires a non-nil context")
	}
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	target := *client.base
	target.Path = path
	request, err := http.NewRequestWithContext(ctx, method, target.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.userAgent != "" {
		request.Header.Set("User-Agent", client.userAgent)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return &HTTPStatusError{Method: method, Path: path, StatusCode: response.StatusCode}
	}
	limited := io.LimitReader(response.Body, client.bodyLimit+1)
	encoded, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if int64(len(encoded)) > client.bodyLimit {
		return fmt.Errorf("%w: %d bytes", ErrResponseTooLarge, client.bodyLimit)
	}
	if err := decodeStrictJSON(encoded, responseBody); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}

func decodeStrictJSON(encoded []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateCapabilities(value Capabilities) error {
	if value.Contract != ContractVersion || !validIdentity(value.DeviceID, true) ||
		!validIdentity(value.Name, false) || !validIdentity(value.Model, false) ||
		!validIdentity(value.Firmware, false) {
		return fmt.Errorf("%w: capability identity", ErrInvalidResponse)
	}
	seenActions := make(map[ActionType]struct{}, len(value.Actions))
	for _, action := range value.Actions {
		if !isKnownAction(action) {
			return fmt.Errorf("%w: capability action %q", ErrInvalidResponse, action)
		}
		if _, exists := seenActions[action]; exists {
			return fmt.Errorf("%w: duplicate capability action %q", ErrInvalidResponse, action)
		}
		seenActions[action] = struct{}{}
	}
	seenEvents := make(map[EventType]struct{}, len(value.Events))
	for _, event := range value.Events {
		if !isKnownEvent(event) {
			return fmt.Errorf("%w: capability event %q", ErrInvalidResponse, event)
		}
		if _, exists := seenEvents[event]; exists {
			return fmt.Errorf("%w: duplicate capability event %q", ErrInvalidResponse, event)
		}
		seenEvents[event] = struct{}{}
	}
	return nil
}

func validateSnapshot(value Snapshot) error {
	if value.Contract != ContractVersion || !validIdentity(value.DeviceID, true) || value.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: snapshot identity", ErrInvalidResponse)
	}
	if value.Power != PowerUnknown && value.Power != PowerOn && value.Power != PowerOff {
		return fmt.Errorf("%w: snapshot power %q", ErrInvalidResponse, value.Power)
	}
	if !validDisplayMessage(value.DisplayMessage) || value.AlertPulses < 0 || value.AlertPulses > 10 {
		return fmt.Errorf("%w: snapshot payload", ErrInvalidResponse)
	}
	return nil
}

func validIdentity(value string, required bool) bool {
	if value == "" {
		return !required
	}
	if !utf8.ValidString(value) || len([]byte(value)) > maxIdentityBytes {
		return false
	}
	for _, character := range value {
		if character == 0 || (character < 0x20 && character != '\t') || character == 0x7f {
			return false
		}
	}
	return true
}

func isKnownAction(value ActionType) bool {
	switch value {
	case ActionPowerOn, ActionPowerOff, ActionPowerToggle, ActionDisplayMessage, ActionAlertPulse:
		return true
	default:
		return false
	}
}

func cloneCapabilities(value Capabilities) Capabilities {
	value.Actions = append([]ActionType(nil), value.Actions...)
	value.Events = append([]EventType(nil), value.Events...)
	return value
}
