package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/http/httpguts"

	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/integrationproxy"
	"pccontroller.local/controller/internal/netpolicy"
	"pccontroller.local/controller/internal/secretstore"
)

// Integrations contains PC-host integrations only. None of these values is
// mirrored into MCU EEPROM.
type Integrations struct {
	Hotkeys                []Hotkey          `json:"hotkeys,omitempty"`
	Keyboard               KeyboardControl   `json:"keyboard_control"`
	Lifecycle              LifecycleSafety   `json:"lifecycle_safety"`
	Notifications          Notifications     `json:"notifications"`
	StatusLED              StatusLEDPolicy   `json:"status_led"`
	BuzzerMirror           BuzzerMirror      `json:"buzzer_mirror"`
	Discovery              Discovery         `json:"discovery"`
	InboundWebhooksEnabled bool              `json:"inbound_webhooks_enabled"`
	OutboundWebhooks       []Webhook         `json:"outbound_webhooks,omitempty"`
	WebSocketClients       []WebSocketClient `json:"websocket_clients,omitempty"`
	TextMappings           []TextMapping     `json:"text_mappings,omitempty"`
	LocalDevice            LocalDevice       `json:"local_device"`
	DataHub                DataHub           `json:"data_hub"`
}

// BuzzerMirror controls optional host playback of board-generated tones. The
// board event is always forwarded; these switches affect presentation only.
type BuzzerMirror struct {
	Enabled         bool   `json:"enabled"`
	NativeEnabled   bool   `json:"native_enabled"`
	WebAudioEnabled bool   `json:"web_audio_enabled"`
	DriverDirectory string `json:"driver_directory"`
}

func DefaultBuzzerMirror() BuzzerMirror {
	return BuzzerMirror{
		WebAudioEnabled: true,
	}
}

const (
	LifecycleActionLeave      = "leave"
	LifecycleActionStopMotion = "stop-motion"
	LifecycleActionAllOff     = "all-off"
)

// LifecycleSafety defines the bounded hardware action applied when Windows
// locks the interactive session or suspends. Stop-motion is deliberately the
// default: it fails safe for momentary movement without unexpectedly clearing
// unrelated latched outputs.
type LifecycleSafety struct {
	SessionLock     string `json:"session_lock"`
	Suspend         string `json:"suspend"`
	RefreshOnResume bool   `json:"refresh_on_resume"`
}

func DefaultLifecycleSafety() LifecycleSafety {
	return LifecycleSafety{
		SessionLock:     LifecycleActionStopMotion,
		Suspend:         LifecycleActionStopMotion,
		RefreshOnResume: true,
	}
}

// LocalDevice configures an optional LAN companion that implements the
// PCController Local Device living capability contract. Browser code never talks
// to the target directly.
type LocalDevice struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url,omitempty"`
}

// DataHub configures an optional loopback-only data service. Browser requests
// cross PCController's authenticated same-origin bridge, and PCController
// credentials are stripped before forwarding.
type DataHub struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url,omitempty"`
}

// KeyboardControl owns opt-in, low-level PC keyboard bindings. It is separate
// from one-shot RegisterHotKey chords because motion needs real down/up edges.
type KeyboardControl struct {
	Enabled  bool                     `json:"enabled"`
	Bindings []KeyboardControlBinding `json:"bindings,omitempty"`
}

// KeyboardControlBinding selects a primary action and an optional Ctrl action
// for one ordinary keyboard key. The hook never suppresses the original key.
type KeyboardControlBinding struct {
	Name    string                 `json:"name"`
	Enabled bool                   `json:"enabled"`
	Key     string                 `json:"key"`
	Primary KeyboardControlAction  `json:"primary"`
	Control *KeyboardControlAction `json:"control,omitempty"`
}

// KeyboardControlAction maps a key edge to motion, relay, or PWM control.
// Behavior is momentary, toggle, or latch; motion is always momentary.
type KeyboardControlAction struct {
	Type         string `json:"type"`
	Behavior     string `json:"behavior"`
	Side         string `json:"side,omitempty"`
	Direction    string `json:"direction,omitempty"`
	Relay        byte   `json:"relay,omitempty"`
	Channel      byte   `json:"channel,omitempty"`
	Value        uint16 `json:"value,omitempty"`
	ReleaseValue uint16 `json:"release_value,omitempty"`
	Active       bool   `json:"active,omitempty"`
}

// DefaultKeyboardControl preconfigures useful mappings but leaves the global
// low-level hook disabled until the user deliberately opts in.
func DefaultKeyboardControl() KeyboardControl {
	motion := func(side, direction string) KeyboardControlAction {
		return KeyboardControlAction{
			Type: "motion", Behavior: "momentary",
			Side: side, Direction: direction,
		}
	}
	relay := func(number byte, behavior string) KeyboardControlAction {
		return KeyboardControlAction{
			Type: "relay", Behavior: behavior, Relay: number,
		}
	}
	result := KeyboardControl{
		// Global ordinary-key capture is intentionally opt-in.
		Enabled: false,
	}
	for _, item := range []struct {
		name, key, side, direction string
	}{
		{"side-b-up", "A", "B", "up"},
		{"side-b-down", "S", "B", "down"},
		{"side-a-up", "K", "A", "up"},
		{"side-a-down", "L", "A", "down"},
	} {
		alternate := motion(item.side, item.direction)
		alternate.Behavior = "latch"
		result.Bindings = append(result.Bindings, KeyboardControlBinding{
			Name: item.name, Enabled: true, Key: item.key,
			Primary: motion(item.side, item.direction), Control: &alternate,
		})
	}
	for number := byte(1); number <= 8; number++ {
		alternate := relay(number, "momentary")
		result.Bindings = append(result.Bindings, KeyboardControlBinding{
			Name: fmt.Sprintf("relay-%d", number), Enabled: true,
			Key: fmt.Sprintf("%d", number), Primary: relay(number, "toggle"),
			Control: &alternate,
		})
	}
	alternate := KeyboardControlAction{
		Type: "pwm", Behavior: "momentary", Channel: 0,
		Value: 4095, ReleaseValue: 0,
	}
	result.Bindings = append(result.Bindings, KeyboardControlBinding{
		Name: "pwm-1", Enabled: true, Key: "9",
		Primary: KeyboardControlAction{
			Type: "pwm", Behavior: "toggle", Channel: 0,
			Value: 4095, ReleaseValue: 0,
		},
		Control: &alternate,
	})
	return result
}

type Hotkey struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Chord   string `json:"chord"`
	Command string `json:"command"`
}

type Notifications struct {
	Enabled          bool                 `json:"enabled"`
	DoorRunningBeep  bool                 `json:"door_running_beep"`
	DoorRunningToast bool                 `json:"door_running_toast"`
	ImportantKinds   []string             `json:"important_kinds,omitempty"`
	Actions          []NotificationAction `json:"actions,omitempty"`
}

type NotificationAction struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Command string `json:"command"`
}

type Discovery struct {
	MDNSEnabled        bool   `json:"mdns_enabled"`
	DNSSDenabled       bool   `json:"dns_sd_enabled"`
	SSDPEnabled        bool   `json:"ssdp_enabled"`
	UPnPEnabled        bool   `json:"upnp_enabled"`
	WSDiscoveryEnabled bool   `json:"ws_discovery_enabled"`
	BroadcastEnabled   bool   `json:"broadcast_enabled"`
	NetBIOSEnabled     bool   `json:"netbios_enabled"`
	BroadcastPort      int    `json:"broadcast_port,omitempty"`
	InstanceName       string `json:"instance_name,omitempty"`
}

// DefaultDiscovery keeps bounded public advertisement and active discovery
// available on first run. It does not enable remote control or weaken IPC auth.
func DefaultDiscovery() Discovery {
	return Discovery{
		MDNSEnabled: true, DNSSDenabled: true, SSDPEnabled: true, UPnPEnabled: true,
		WSDiscoveryEnabled: true, BroadcastEnabled: true, NetBIOSEnabled: true,
		BroadcastPort: 37889,
	}
}

// RemoteConnectable reports whether the configured IPC listener is actually
// reachable beyond loopback. Advertisement remains independent from remote
// control, but consumers need this exact distinction before offering Connect.
func (value IPC) RemoteConnectable() bool {
	if !value.AllowRemote {
		return false
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(value.Listen))
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if parsed := net.ParseIP(host); parsed != nil {
		return !parsed.IsLoopback()
	}
	return host != ""
}

type Webhook struct {
	Name             string            `json:"name"`
	Enabled          bool              `json:"enabled"`
	EventKind        string            `json:"event_kind,omitempty"`
	URL              string            `json:"url"`
	Method           string            `json:"method"`
	Headers          map[string]string `json:"headers,omitempty"`
	SecretHeaders    map[string]string `json:"secret_headers,omitempty"`
	BodyTemplate     string            `json:"body_template,omitempty"`
	TimeoutMS        int               `json:"timeout_ms,omitempty"`
	MaxAttempts      int               `json:"max_attempts,omitempty"`
	RetryInitialMS   int               `json:"retry_initial_ms,omitempty"`
	RetryMaximumMS   int               `json:"retry_maximum_ms,omitempty"`
	SigningSecret    string            `json:"signing_secret,omitempty"`
	SigningSecretRef string            `json:"signing_secret_ref,omitempty"`
}

type WebSocketClient struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	URL           string   `json:"url"`
	Protocol      string   `json:"protocol,omitempty"`
	AuthToken     string   `json:"auth_token,omitempty"`
	AuthTokenRef  string   `json:"auth_token_ref,omitempty"`
	Topics        []string `json:"topics,omitempty"`
	ForwardEvents bool     `json:"forward_events"`
	AllowCommands bool     `json:"allow_commands"`
}

// TextMapping maps a typed, source-tagged message to an explicit host command.
// It is disabled unless both the mapping and its containing configuration are
// deliberately enabled by the user.
type TextMapping struct {
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Type     string `json:"type,omitempty"`
	Contains string `json:"contains,omitempty"`
	Command  string `json:"command"`
}

func (value Config) validateIntegrations() error {
	if err := validateIPC(value.IPC); err != nil {
		return err
	}
	if err := ValidateHotkeys(value.Integrations.Hotkeys); err != nil {
		return err
	}
	if err := validateKeyboardControl(value.Integrations.Keyboard); err != nil {
		return err
	}
	if err := validateLifecycleSafety(value.Integrations.Lifecycle); err != nil {
		return err
	}
	if err := validateStatusLEDPolicy(value.Integrations.StatusLED); err != nil {
		return err
	}
	if err := validateBuzzerMirror(value.Integrations.BuzzerMirror); err != nil {
		return err
	}
	if baseURL := strings.TrimSpace(value.Integrations.LocalDevice.BaseURL); baseURL != "" {
		if baseURL != value.Integrations.LocalDevice.BaseURL {
			return fmt.Errorf("integrations.local_device.base_url must not contain surrounding whitespace")
		}
		if _, err := integrationproxy.NormalizeDeviceTarget(baseURL); err != nil {
			return fmt.Errorf("integrations.local_device.base_url: %w", err)
		}
	} else if value.Integrations.LocalDevice.Enabled {
		return fmt.Errorf("integrations.local_device.base_url is required when enabled")
	}
	if baseURL := strings.TrimSpace(value.Integrations.DataHub.BaseURL); baseURL != "" {
		if baseURL != value.Integrations.DataHub.BaseURL {
			return fmt.Errorf("integrations.data_hub.base_url must not contain surrounding whitespace")
		}
		if _, err := integrationproxy.NormalizeDataHubTarget(baseURL); err != nil {
			return fmt.Errorf("integrations.data_hub.base_url: %w", err)
		}
	} else if value.Integrations.DataHub.Enabled {
		return fmt.Errorf("integrations.data_hub.base_url is required when enabled")
	}
	if len(value.Integrations.Notifications.Actions) > 5 {
		return fmt.Errorf("integrations.notifications.actions supports at most five actions")
	}
	for index, action := range value.Integrations.Notifications.Actions {
		if strings.TrimSpace(action.ID) == "" || strings.TrimSpace(action.Label) == "" ||
			strings.TrimSpace(action.Command) == "" {
			return fmt.Errorf("integrations.notifications.actions[%d] requires id, label, and command", index)
		}
	}
	if len(value.Integrations.Discovery.InstanceName) > 63 {
		return fmt.Errorf("integrations.discovery.instance_name must be at most 63 characters")
	}
	discovery := value.Integrations.Discovery
	if discovery.BroadcastPort != 0 && (discovery.BroadcastPort < 1024 || discovery.BroadcastPort > 65535) {
		return fmt.Errorf("integrations.discovery.broadcast_port must be 1024..65535")
	}
	names := make(map[string]bool)
	if len(value.Integrations.OutboundWebhooks) > 64 {
		return fmt.Errorf("integrations.outbound_webhooks supports at most 64 targets")
	}
	for index, webhook := range value.Integrations.OutboundWebhooks {
		name := strings.ToLower(strings.TrimSpace(webhook.Name))
		if name == "" || names[name] {
			return fmt.Errorf("integrations.outbound_webhooks[%d].name is required and must be unique", index)
		}
		if webhook.Name != strings.TrimSpace(webhook.Name) || len(webhook.Name) > 96 {
			return fmt.Errorf("integrations.outbound_webhooks[%d].name must be trimmed and at most 96 characters", index)
		}
		names[name] = true
		if webhook.URL != strings.TrimSpace(webhook.URL) || len(webhook.URL) > 4096 {
			return fmt.Errorf("integrations.outbound_webhooks[%d].url must be trimmed and at most 4096 characters", index)
		}
		parsedWebhookURL, err := netpolicy.ParseHTTPURL(webhook.URL, "outbound webhook URL")
		if err != nil {
			return fmt.Errorf("integrations.outbound_webhooks[%d].url: %w", index, err)
		}
		if key := credentialQueryKey(parsedWebhookURL); key != "" {
			return fmt.Errorf("integrations.outbound_webhooks[%d].url query %q may contain a credential; use a secret-backed header", index, key)
		}
		if webhook.Method != strings.TrimSpace(webhook.Method) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].method must not contain surrounding whitespace", index)
		}
		switch strings.ToUpper(strings.TrimSpace(webhook.Method)) {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return fmt.Errorf("integrations.outbound_webhooks[%d].method is unsupported", index)
		}
		if webhook.TimeoutMS != 0 && (webhook.TimeoutMS < 100 || webhook.TimeoutMS > 60_000) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].timeout_ms must be 100..60000", index)
		}
		if webhook.MaxAttempts != 0 && (webhook.MaxAttempts < 1 || webhook.MaxAttempts > 20) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].max_attempts must be 1..20", index)
		}
		if webhook.RetryInitialMS != 0 && (webhook.RetryInitialMS < 50 || webhook.RetryInitialMS > 60_000) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].retry_initial_ms must be 50..60000", index)
		}
		if webhook.RetryMaximumMS != 0 && (webhook.RetryMaximumMS < 100 || webhook.RetryMaximumMS > 86_400_000) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].retry_maximum_ms must be 100..86400000", index)
		}
		initial := webhook.RetryInitialMS
		if initial == 0 {
			initial = 500
		}
		if webhook.RetryMaximumMS != 0 && webhook.RetryMaximumMS < initial {
			return fmt.Errorf("integrations.outbound_webhooks[%d].retry_maximum_ms must be at least retry_initial_ms", index)
		}
		if err := validateSecretChoice(
			fmt.Sprintf("integrations.outbound_webhooks[%d].signing_secret", index),
			webhook.SigningSecret, webhook.SigningSecretRef,
		); err != nil {
			return err
		}
		if webhook.SigningSecret != "" && (len(webhook.SigningSecret) < 16 || len(webhook.SigningSecret) > 4096) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].signing_secret must be 16..4096 bytes when configured", index)
		}
		if len(webhook.EventKind) > 128 || strings.ContainsAny(webhook.EventKind, "\r\n") {
			return fmt.Errorf("integrations.outbound_webhooks[%d].event_kind is invalid", index)
		}
		if len(webhook.BodyTemplate) > 256<<10 {
			return fmt.Errorf("integrations.outbound_webhooks[%d].body_template exceeds 262144 bytes", index)
		}
		if err := validateWebhookTemplatePlaceholders(webhook.BodyTemplate); err != nil {
			return fmt.Errorf("integrations.outbound_webhooks[%d].body_template: %w", index, err)
		}
		if err := validateWebhookJSONTemplate(webhook.BodyTemplate, webhook.Headers); err != nil {
			return fmt.Errorf("integrations.outbound_webhooks[%d].body_template: %w", index, err)
		}
		if len(webhook.Headers)+len(webhook.SecretHeaders) > 32 {
			return fmt.Errorf("integrations.outbound_webhooks[%d].headers supports at most 32 fields", index)
		}
		headerNames := make(map[string]bool, len(webhook.Headers)+len(webhook.SecretHeaders))
		for key, header := range webhook.Headers {
			if len(key) > 128 || !httpguts.ValidHeaderFieldName(key) ||
				len(header) > 8192 || !httpguts.ValidHeaderFieldValue(header) {
				return fmt.Errorf("integrations.outbound_webhooks[%d].headers[%q] is invalid", index, key)
			}
			if reservedWebhookHeader(key) {
				return fmt.Errorf("integrations.outbound_webhooks[%d].headers[%q] is managed by the delivery service", index, key)
			}
			headerNames[strings.ToLower(key)] = true
		}
		for key, reference := range webhook.SecretHeaders {
			if len(key) > 128 || !httpguts.ValidHeaderFieldName(key) || reservedWebhookHeader(key) {
				return fmt.Errorf("integrations.outbound_webhooks[%d].secret_headers[%q] is invalid or managed by the delivery service", index, key)
			}
			if headerNames[strings.ToLower(key)] {
				return fmt.Errorf("integrations.outbound_webhooks[%d] header %q cannot be both plaintext and secret-backed", index, key)
			}
			if err := secretstore.ValidateReference(reference); err != nil {
				return fmt.Errorf("integrations.outbound_webhooks[%d].secret_headers[%q]: %w", index, key, err)
			}
			headerNames[strings.ToLower(key)] = true
		}
	}
	names = make(map[string]bool)
	for index, peer := range value.Integrations.WebSocketClients {
		name := strings.ToLower(strings.TrimSpace(peer.Name))
		if name == "" || names[name] {
			return fmt.Errorf("integrations.websocket_clients[%d].name is required and must be unique", index)
		}
		names[name] = true
		parsed, err := url.Parse(peer.URL)
		if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" ||
			parsed.User != nil || parsed.Fragment != "" {
			return fmt.Errorf("integrations.websocket_clients[%d].url must be WS(S) without credentials or a fragment", index)
		}
		if key := credentialQueryKey(parsed); key != "" {
			return fmt.Errorf("integrations.websocket_clients[%d].url query %q may contain a credential; use auth_token_ref", index, key)
		}
		switch strings.ToLower(strings.TrimSpace(peer.Protocol)) {
		case "", "jsonrpc", "socketio":
		default:
			return fmt.Errorf("integrations.websocket_clients[%d].protocol must be jsonrpc or socketio", index)
		}
		if err := validateSecretChoice(
			fmt.Sprintf("integrations.websocket_clients[%d].auth_token", index),
			peer.AuthToken, peer.AuthTokenRef,
		); err != nil {
			return err
		}
		if peer.AllowCommands && !secretConfigured(peer.AuthToken, peer.AuthTokenRef) {
			return fmt.Errorf("integrations.websocket_clients[%d] allowing commands requires auth_token", index)
		}
		host := strings.Trim(parsed.Hostname(), "[]")
		loopback := strings.EqualFold(host, "localhost")
		if address := net.ParseIP(host); address != nil {
			loopback = address.IsLoopback()
		}
		if peer.ForwardEvents && !loopback && !secretConfigured(peer.AuthToken, peer.AuthTokenRef) {
			return fmt.Errorf("integrations.websocket_clients[%d] forwarding events remotely requires auth_token", index)
		}
		topics := make(map[string]bool)
		for topicIndex, topic := range peer.Topics {
			topic = strings.ToLower(strings.TrimSpace(topic))
			if topic == "telemetry" {
				topic = "status"
			}
			if topic != "events" && topic != "status" {
				return fmt.Errorf(
					"integrations.websocket_clients[%d].topics[%d] must be events or status",
					index, topicIndex,
				)
			}
			if topics[topic] {
				return fmt.Errorf(
					"integrations.websocket_clients[%d].topics[%d] is duplicated",
					index, topicIndex,
				)
			}
			topics[topic] = true
		}
	}
	for index, mapping := range value.Integrations.TextMappings {
		if strings.TrimSpace(mapping.Name) == "" || strings.TrimSpace(mapping.Command) == "" {
			return fmt.Errorf("integrations.text_mappings[%d] requires name and command", index)
		}
		if mapping.Source == "" && mapping.Target == "" && mapping.Type == "" && mapping.Contains == "" {
			return fmt.Errorf("integrations.text_mappings[%d] must specify at least one match", index)
		}
	}
	return nil
}

func validateBuzzerMirror(value BuzzerMirror) error {
	if value.Enabled && !value.NativeEnabled && !value.WebAudioEnabled {
		return fmt.Errorf("integrations.buzzer_mirror enables the host buzzer path but selects no native or WebAudio output")
	}
	if value.DriverDirectory != strings.TrimSpace(value.DriverDirectory) ||
		len(value.DriverDirectory) > 1024 ||
		strings.ContainsAny(value.DriverDirectory, "\r\n\"") {
		return fmt.Errorf("integrations.buzzer_mirror.driver_directory is invalid")
	}
	return nil
}

func validateLifecycleSafety(value LifecycleSafety) error {
	for name, action := range map[string]string{
		"session_lock": value.SessionLock,
		"suspend":      value.Suspend,
	} {
		switch action {
		case LifecycleActionLeave, LifecycleActionStopMotion, LifecycleActionAllOff:
		default:
			return fmt.Errorf(
				"integrations.lifecycle_safety.%s must be leave, stop-motion, or all-off",
				name,
			)
		}
	}
	return nil
}

// ValidateHotkeys applies the same server-authoritative rules to file, CLI,
// and IPC updates. Accelerator aliases are compared canonically so two
// spellings cannot register the same Windows chord.
func ValidateHotkeys(values []Hotkey) error {
	if len(values) > 64 {
		return errors.New("integrations.hotkeys supports at most 64 bindings")
	}
	names := make(map[string]bool, len(values))
	accelerators := make(map[string]bool, len(values))
	for index, hotkey := range values {
		name := strings.TrimSpace(hotkey.Name)
		nameKey := strings.ToLower(name)
		if name == "" || name != hotkey.Name || len(name) > 64 ||
			containsControl(name) || names[nameKey] {
			return fmt.Errorf(
				"integrations.hotkeys[%d].name must be a unique, trimmed 1..64 byte value without control characters",
				index,
			)
		}
		names[nameKey] = true
		command := strings.TrimSpace(hotkey.Command)
		if command == "" || command != hotkey.Command || len(command) > 512 || containsControl(command) {
			return fmt.Errorf(
				"integrations.hotkeys[%d].command must be a trimmed 1..512 byte single line",
				index,
			)
		}
		accelerator, err := hostui.ParseAccelerator(hotkey.Chord)
		if err != nil {
			return fmt.Errorf("integrations.hotkeys[%d].chord: %w", index, err)
		}
		key := strings.ToLower(accelerator.Canonical)
		if accelerators[key] {
			return fmt.Errorf(
				"integrations.hotkeys[%d].chord duplicates accelerator %s",
				index,
				accelerator.Canonical,
			)
		}
		accelerators[key] = true
	}
	return nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func validateKeyboardControl(value KeyboardControl) error {
	if len(value.Bindings) > 32 {
		return fmt.Errorf("integrations.keyboard_control.bindings supports at most 32 entries")
	}
	names := make(map[string]bool)
	keys := make(map[uint32]bool)
	enabled := 0
	for index, binding := range value.Bindings {
		if binding.Enabled {
			enabled++
		}
		name := strings.ToLower(strings.TrimSpace(binding.Name))
		if name == "" || names[name] {
			return fmt.Errorf(
				"integrations.keyboard_control.bindings[%d].name is required and must be unique",
				index,
			)
		}
		names[name] = true
		key, _, err := hostui.ParseKeyboardKey(binding.Key)
		if err != nil {
			return fmt.Errorf(
				"integrations.keyboard_control.bindings[%d].key: %w", index, err,
			)
		}
		if keys[key] {
			return fmt.Errorf(
				"integrations.keyboard_control.bindings[%d].key duplicates %q",
				index, binding.Key,
			)
		}
		keys[key] = true
		if err := validateKeyboardAction(binding.Primary); err != nil {
			return fmt.Errorf(
				"integrations.keyboard_control.bindings[%d].primary: %w", index, err,
			)
		}
		if binding.Control != nil {
			if err := validateKeyboardAction(*binding.Control); err != nil {
				return fmt.Errorf(
					"integrations.keyboard_control.bindings[%d].control: %w", index, err,
				)
			}
		}
	}
	if value.Enabled && enabled == 0 {
		return fmt.Errorf("integrations.keyboard_control requires at least one enabled binding")
	}
	return nil
}

func validateKeyboardAction(action KeyboardControlAction) error {
	actionType := strings.ToLower(strings.TrimSpace(action.Type))
	behavior := strings.ToLower(strings.TrimSpace(action.Behavior))
	switch behavior {
	case "momentary", "toggle", "latch":
	default:
		return fmt.Errorf("behavior must be momentary, toggle, or latch")
	}
	switch actionType {
	case "motion":
		if behavior != "momentary" && behavior != "latch" {
			return fmt.Errorf("motion behavior must be momentary or latch")
		}
		if !strings.EqualFold(action.Side, "A") && !strings.EqualFold(action.Side, "B") {
			return fmt.Errorf("motion side must be A or B")
		}
		if !strings.EqualFold(action.Direction, "up") &&
			!strings.EqualFold(action.Direction, "down") {
			return fmt.Errorf("motion direction must be up or down")
		}
	case "relay":
		if action.Relay < 1 || action.Relay > 8 {
			return fmt.Errorf("relay must be 1..8")
		}
	case "pwm":
		if action.Channel > 15 {
			return fmt.Errorf("PWM channel must be 0..15")
		}
		if action.Value > 4095 || action.ReleaseValue > 4095 {
			return fmt.Errorf("PWM values must be 0..4095")
		}
	default:
		return fmt.Errorf("type must be motion, relay, or pwm")
	}
	return nil
}

func validateIPC(value IPC) error {
	address := strings.TrimSpace(value.Listen)
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("ipc.listen must be host:port: %w", err)
	}
	host = strings.Trim(host, "[]")
	loopback := strings.EqualFold(host, "localhost")
	if parsed := net.ParseIP(host); parsed != nil {
		loopback = parsed.IsLoopback()
	}
	if !loopback && !value.AllowRemote {
		return fmt.Errorf("ipc.listen is non-loopback; set ipc.allow_remote deliberately")
	}
	if err := validateSecretChoice("ipc.auth_token", value.AuthToken, value.AuthTokenRef); err != nil {
		return err
	}
	if value.AllowRemote && !secretConfigured(value.AuthToken, value.AuthTokenRef) {
		return fmt.Errorf("ipc.auth_token or ipc.auth_token_ref is required when remote access is enabled")
	}
	if value.AllowRemote && value.AuthTokenRef == "" && len(strings.TrimSpace(value.AuthToken)) < 24 {
		return fmt.Errorf("ipc.auth_token must contain at least 24 characters when remote access is enabled")
	}
	if len(value.AuthToken) > 512 || !printableText(value.AuthToken) {
		return fmt.Errorf("ipc.auth_token must be at most 512 printable characters")
	}
	principal := strings.TrimSpace(value.RemotePrincipal)
	if principal == "" || principal != value.RemotePrincipal || len(principal) > 64 ||
		!printableASCII(principal) || strings.ContainsAny(principal, " =\t") {
		return fmt.Errorf("ipc.remote_principal must be a trimmed 1..64 byte printable identifier without spaces or equals signs")
	}
	if value.AllowRemote && len(value.AllowedOrigins) == 0 {
		return fmt.Errorf("ipc.allowed_origins is required when remote access is enabled")
	}
	for index, origin := range value.AllowedOrigins {
		if strings.TrimSpace(origin) == "" || strings.ContainsAny(origin, "\r\n") {
			return fmt.Errorf("ipc.allowed_origins[%d] is invalid", index)
		}
		if value.AllowRemote && strings.TrimSpace(origin) == "*" {
			return fmt.Errorf("ipc.allowed_origins[%d] cannot allow every origin", index)
		}
	}
	for name, path := range map[string]string{
		"websocket_path": value.WebSocketPath,
		"socket_io_path": value.SocketIOPath,
	} {
		if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
			return fmt.Errorf("ipc.%s must be an absolute path without query or fragment", name)
		}
	}
	if value.WebSocketPath == value.SocketIOPath {
		return fmt.Errorf("ipc.websocket_path and ipc.socket_io_path must differ")
	}
	if value.RemotePolicy.Programming && !value.RemotePolicy.ConnectionControl {
		return fmt.Errorf("ipc.remote_policy.programming requires connection_control")
	}
	return nil
}

func secretConfigured(value, reference string) bool {
	return strings.TrimSpace(value) != "" || strings.TrimSpace(reference) != ""
}

func credentialQueryKey(value *url.URL) string {
	if value == nil {
		return ""
	}
	for key := range value.Query() {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", "-"))
		for _, marker := range []string{"token", "secret", "password", "credential", "api-key", "apikey", "authorization"} {
			if strings.Contains(normalized, marker) {
				return key
			}
		}
	}
	return ""
}

func validateSecretChoice(path, value, reference string) error {
	if value != "" && reference != "" {
		return fmt.Errorf("%s and %s_ref are mutually exclusive", path, path)
	}
	if reference != "" {
		if err := secretstore.ValidateReference(reference); err != nil {
			return fmt.Errorf("%s_ref: %w", path, err)
		}
	}
	return nil
}

func validateWebhookTemplatePlaceholders(value string) error {
	for cursor := 0; cursor < len(value); {
		start := strings.Index(value[cursor:], "{{")
		if start < 0 {
			return nil
		}
		start += cursor
		end := strings.Index(value[start+2:], "}}")
		if end < 0 {
			return errors.New("contains an unterminated placeholder")
		}
		end += start + 2
		switch strings.TrimSpace(value[start+2 : end]) {
		case "id", "kind", "text", "source", "time", "event", "metadata":
		default:
			return fmt.Errorf("contains unsupported placeholder %q", value[start+2:end])
		}
		cursor = end + 2
	}
	return nil
}

func validateWebhookJSONTemplate(value string, headers map[string]string) error {
	if strings.TrimSpace(value) == "" || !webhookBodyDefaultsToJSON(headers) {
		return nil
	}
	rawSamples := map[string][]byte{
		"id": []byte("1"), "kind": []byte(`"event"`),
		"text": []byte(`"text"`), "source": []byte(`"source"`),
		"time":  []byte(`"2026-01-01T00:00:00Z"`),
		"event": []byte(`{"id":1}`), "metadata": []byte(`{"key":"value"}`),
	}
	stringSamples := map[string]string{
		"id": "1", "kind": "event", "text": "text", "source": "source",
		"time": "2026-01-01T00:00:00Z", "event": `{"id":1}`,
		"metadata": `{"key":"value"}`,
	}
	var rendered bytes.Buffer
	inString, escaped := false, false
	for cursor := 0; cursor < len(value); {
		if value[cursor] == '{' && cursor+1 < len(value) && value[cursor+1] == '{' {
			end := strings.Index(value[cursor+2:], "}}")
			if end < 0 {
				return errors.New("contains an unterminated placeholder")
			}
			end += cursor + 2
			name := strings.TrimSpace(value[cursor+2 : end])
			if inString {
				quoted, _ := json.Marshal(stringSamples[name])
				rendered.Write(quoted[1 : len(quoted)-1])
			} else {
				rendered.Write(rawSamples[name])
			}
			cursor = end + 2
			continue
		}
		current := value[cursor]
		rendered.WriteByte(current)
		if inString {
			if escaped {
				escaped = false
			} else if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
		} else if current == '"' {
			inString = true
		}
		cursor++
	}
	if !json.Valid(rendered.Bytes()) {
		return errors.New("must render as valid JSON for the configured content type")
	}
	return nil
}

func webhookBodyDefaultsToJSON(headers map[string]string) bool {
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			return strings.Contains(strings.ToLower(value), "json")
		}
	}
	return true
}

func reservedWebhookHeader(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "host", "content-length", "transfer-encoding", "connection", "trailer",
		"upgrade", "proxy-connection", "idempotency-key",
		"x-pccontroller-delivery-id", "x-pccontroller-correlation-id",
		"x-pccontroller-attempt-id", "x-pccontroller-attempt",
		"x-pccontroller-timestamp", "x-pccontroller-nonce",
		"x-pccontroller-signature":
		return true
	default:
		return false
	}
}

func cloneIntegrations(source Integrations) Integrations {
	result := source
	result.Hotkeys = append([]Hotkey(nil), source.Hotkeys...)
	result.Keyboard.Bindings = make(
		[]KeyboardControlBinding,
		len(source.Keyboard.Bindings),
	)
	for index, binding := range source.Keyboard.Bindings {
		result.Keyboard.Bindings[index] = binding
		if binding.Control != nil {
			alternate := *binding.Control
			result.Keyboard.Bindings[index].Control = &alternate
		}
	}
	result.Notifications.ImportantKinds = append(
		[]string(nil), source.Notifications.ImportantKinds...,
	)
	result.Notifications.Actions = append(
		[]NotificationAction(nil), source.Notifications.Actions...,
	)
	result.OutboundWebhooks = make([]Webhook, len(source.OutboundWebhooks))
	for index, value := range source.OutboundWebhooks {
		result.OutboundWebhooks[index] = value
		result.OutboundWebhooks[index].Headers = make(map[string]string, len(value.Headers))
		for key, header := range value.Headers {
			result.OutboundWebhooks[index].Headers[key] = header
		}
		result.OutboundWebhooks[index].SecretHeaders = make(map[string]string, len(value.SecretHeaders))
		for key, reference := range value.SecretHeaders {
			result.OutboundWebhooks[index].SecretHeaders[key] = reference
		}
	}
	result.WebSocketClients = make([]WebSocketClient, len(source.WebSocketClients))
	for index, value := range source.WebSocketClients {
		result.WebSocketClients[index] = value
		result.WebSocketClients[index].Topics = append([]string(nil), value.Topics...)
	}
	result.TextMappings = append([]TextMapping(nil), source.TextMappings...)
	return result
}
