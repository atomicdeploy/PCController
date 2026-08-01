package appconfig

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/integrationproxy"
)

// Integrations contains PC-host integrations only. None of these values is
// mirrored into MCU EEPROM.
type Integrations struct {
	Hotkeys                []Hotkey          `json:"hotkeys,omitempty"`
	Keyboard               KeyboardControl   `json:"keyboard_control"`
	Notifications          Notifications     `json:"notifications"`
	StatusLED              StatusLEDPolicy   `json:"status_led"`
	Discovery              Discovery         `json:"discovery"`
	InboundWebhooksEnabled bool              `json:"inbound_webhooks_enabled"`
	OutboundWebhooks       []Webhook         `json:"outbound_webhooks,omitempty"`
	WebSocketClients       []WebSocketClient `json:"websocket_clients,omitempty"`
	TextMappings           []TextMapping     `json:"text_mappings,omitempty"`
	LocalDevice            LocalDevice       `json:"local_device"`
	DataHub                DataHub           `json:"data_hub"`
}

// LocalDevice configures an optional LAN companion that implements the
// PCController Local Device v1 capability contract. Browser code never talks
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
	MDNSEnabled  bool   `json:"mdns_enabled"`
	SSDPEnabled  bool   `json:"ssdp_enabled"`
	InstanceName string `json:"instance_name,omitempty"`
}

type Webhook struct {
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	EventKind    string            `json:"event_kind,omitempty"`
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyTemplate string            `json:"body_template,omitempty"`
	TimeoutMS    int               `json:"timeout_ms,omitempty"`
}

type WebSocketClient struct {
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	URL           string   `json:"url"`
	Protocol      string   `json:"protocol,omitempty"`
	AuthToken     string   `json:"auth_token,omitempty"`
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
	names := make(map[string]bool)
	for index, hotkey := range value.Integrations.Hotkeys {
		name := strings.ToLower(strings.TrimSpace(hotkey.Name))
		if name == "" || names[name] {
			return fmt.Errorf("integrations.hotkeys[%d].name is required and must be unique", index)
		}
		names[name] = true
		if strings.TrimSpace(hotkey.Chord) == "" || strings.TrimSpace(hotkey.Command) == "" {
			return fmt.Errorf("integrations.hotkeys[%d] requires chord and command", index)
		}
	}
	if err := validateKeyboardControl(value.Integrations.Keyboard); err != nil {
		return err
	}
	if err := validateStatusLEDPolicy(value.Integrations.StatusLED); err != nil {
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
	if (value.Integrations.Discovery.MDNSEnabled ||
		value.Integrations.Discovery.SSDPEnabled) && !value.IPC.AllowRemote {
		return fmt.Errorf("network discovery requires ipc.allow_remote and authenticated remote access")
	}
	names = make(map[string]bool)
	for index, webhook := range value.Integrations.OutboundWebhooks {
		name := strings.ToLower(strings.TrimSpace(webhook.Name))
		if name == "" || names[name] {
			return fmt.Errorf("integrations.outbound_webhooks[%d].name is required and must be unique", index)
		}
		names[name] = true
		parsed, err := url.ParseRequestURI(webhook.URL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("integrations.outbound_webhooks[%d].url must be HTTP(S)", index)
		}
		switch strings.ToUpper(strings.TrimSpace(webhook.Method)) {
		case "GET", "POST", "PUT", "PATCH", "DELETE":
		default:
			return fmt.Errorf("integrations.outbound_webhooks[%d].method is unsupported", index)
		}
		if webhook.TimeoutMS != 0 && (webhook.TimeoutMS < 100 || webhook.TimeoutMS > 60_000) {
			return fmt.Errorf("integrations.outbound_webhooks[%d].timeout_ms must be 100..60000", index)
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
		if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") || parsed.Host == "" {
			return fmt.Errorf("integrations.websocket_clients[%d].url must be WS(S)", index)
		}
		switch strings.ToLower(strings.TrimSpace(peer.Protocol)) {
		case "", "jsonrpc", "socketio":
		default:
			return fmt.Errorf("integrations.websocket_clients[%d].protocol must be jsonrpc or socketio", index)
		}
		if peer.AllowCommands && strings.TrimSpace(peer.AuthToken) == "" {
			return fmt.Errorf("integrations.websocket_clients[%d] allowing commands requires auth_token", index)
		}
		host := strings.Trim(parsed.Hostname(), "[]")
		loopback := strings.EqualFold(host, "localhost")
		if address := net.ParseIP(host); address != nil {
			loopback = address.IsLoopback()
		}
		if peer.ForwardEvents && !loopback && strings.TrimSpace(peer.AuthToken) == "" {
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
	if value.AllowRemote && len(strings.TrimSpace(value.AuthToken)) < 24 {
		return fmt.Errorf("ipc.auth_token must contain at least 24 characters when remote access is enabled")
	}
	if len(value.AuthToken) > 512 || !printableText(value.AuthToken) {
		return fmt.Errorf("ipc.auth_token must be at most 512 printable characters")
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
	}
	result.WebSocketClients = make([]WebSocketClient, len(source.WebSocketClients))
	for index, value := range source.WebSocketClients {
		result.WebSocketClients[index] = value
		result.WebSocketClients[index].Topics = append([]string(nil), value.Topics...)
	}
	result.TextMappings = append([]TextMapping(nil), source.TextMappings...)
	return result
}
