// Package ipcjson implements newline-delimited JSON-RPC 2.0 over loopback TCP
// or separate input/output streams. It deliberately avoids platform-specific transports,
// so the same automation works on Windows, Linux, and macOS.
package ipcjson

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/hostfacts"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	Version       = "2.0"
	DefaultListen = "127.0.0.1:8787"
	maxMessage    = 1024 * 1024
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Auth    string          `json:"auth,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type opcodeExchangeParams struct {
	Opcode       *int   `json:"opcode"`
	ExpectOpcode *int   `json:"expect_opcode,omitempty"`
	Payload      []byte `json:"payload,omitempty"`
	PayloadHex   string `json:"payload_hex,omitempty"`
}

func (params opcodeExchangeParams) values() (byte, []byte, byte, error) {
	if params.Opcode == nil || *params.Opcode < 1 || *params.Opcode > 255 {
		return 0, nil, 0, errors.New("opcode is required and must be 1..255; 0 is reserved")
	}
	expected := int(native.OpACK)
	if params.ExpectOpcode != nil {
		expected = *params.ExpectOpcode
	}
	if expected < 1 || expected > 255 {
		return 0, nil, 0, errors.New("expect_opcode must be 1..255; 0 is reserved")
	}
	if len(params.Payload) != 0 && strings.TrimSpace(params.PayloadHex) != "" {
		return 0, nil, 0, errors.New("supply payload or payload_hex, not both")
	}
	payload := append([]byte(nil), params.Payload...)
	if value := strings.TrimSpace(params.PayloadHex); value != "" {
		value = strings.NewReplacer(" ", "", ":", "", "-", "", "_", "").Replace(value)
		value = strings.TrimPrefix(strings.ToLower(value), "0x")
		if len(value)%2 != 0 {
			return 0, nil, 0, errors.New("payload_hex must contain complete bytes")
		}
		var err error
		payload, err = hex.DecodeString(value)
		if err != nil {
			return 0, nil, 0, fmt.Errorf("decode payload_hex: %w", err)
		}
	}
	if len(payload) > native.MaxPayload {
		return 0, nil, 0, native.ErrPayloadTooLong
	}
	return byte(*params.Opcode), payload, byte(expected), nil
}

// rfMapParams is deliberately semantic: network callers name the target they
// intend instead of depending on the firmware's compact enum layout.
type rfMapParams struct {
	ID       *int   `json:"id"`
	Action   string `json:"action"`
	Target   string `json:"target,omitempty"`
	Behavior string `json:"behavior,omitempty"`
}

func (params rfMapParams) mapping() (controller.RFMapping, error) {
	if params.ID == nil {
		return controller.RFMapping{}, errors.New("RF learned entry id is required")
	}
	if *params.ID < 0 || *params.ID > 19 {
		return controller.RFMapping{}, errors.New("RF learned entry id must be 0..19")
	}
	action := strings.ToLower(strings.TrimSpace(params.Action))
	target := strings.ToLower(strings.TrimSpace(params.Target))
	behavior := strings.ToLower(strings.TrimSpace(params.Behavior))
	mapping := controller.RFMapping{
		Action: controller.RFActionNone, Behavior: controller.RFBehaviorPress,
	}
	parseNumber := func(minimum, maximum byte, label string) (byte, error) {
		value, err := strconv.ParseUint(target, 10, 8)
		if err != nil || value < uint64(minimum) || value > uint64(maximum) {
			return 0, fmt.Errorf("RF %s target must be %d..%d", label, minimum, maximum)
		}
		return byte(value), nil
	}
	parseBehavior := func() (controller.RFBehavior, error) {
		switch behavior {
		case "", "press":
			return controller.RFBehaviorPress, nil
		case "toggle":
			return controller.RFBehaviorToggle, nil
		case "momentary":
			return controller.RFBehaviorMomentary, nil
		default:
			return 0, errors.New("RF behavior must be press, toggle, or momentary")
		}
	}

	switch action {
	case "none", "unmapped":
		if target != "" || behavior != "" {
			return controller.RFMapping{}, errors.New("unmapped RF entries do not accept target or behavior")
		}
		return mapping, nil
	case "key":
		value, err := parseNumber(1, 4, "key")
		if err != nil {
			return controller.RFMapping{}, err
		}
		mapping.Action, mapping.Value = controller.RFActionKey, value-1
		mapping.Behavior, err = parseBehavior()
		return mapping, err
	case "menu":
		values := map[string]byte{
			"prev": native.MenuPrevious, "next": native.MenuNext,
			"dec": native.MenuDecrease, "inc": native.MenuIncrease,
		}
		value, ok := values[target]
		if !ok {
			return controller.RFMapping{}, errors.New("RF menu target must be prev, next, dec, or inc")
		}
		if behavior != "" {
			return controller.RFMapping{}, errors.New("RF menu mappings do not accept behavior")
		}
		mapping.Action, mapping.Value = controller.RFActionMenu, value
		return mapping, nil
	case "relay":
		value, err := parseNumber(5, 8, "relay")
		if err != nil {
			return controller.RFMapping{}, err
		}
		mapping.Action, mapping.Value = controller.RFActionRelay, value-1
		mapping.Behavior, err = parseBehavior()
		return mapping, err
	case "side":
		sides := map[string]byte{"left": 0, "a": 0, "right": 1, "b": 1}
		value, ok := sides[target]
		if !ok {
			return controller.RFMapping{}, errors.New("RF side target must be left/A or right/B")
		}
		motions := map[string]controller.RFBehavior{
			"up": controller.RFBehaviorUp, "down": controller.RFBehaviorDown,
			"stop": controller.RFBehaviorStop,
		}
		motion, ok := motions[behavior]
		if !ok {
			return controller.RFMapping{}, errors.New("RF side behavior must be up, down, or stop")
		}
		mapping.Action, mapping.Value, mapping.Behavior = controller.RFActionSide, value, motion
		return mapping, nil
	case "pwm":
		value, err := parseNumber(0, 10, "PWM")
		if err != nil {
			return controller.RFMapping{}, err
		}
		mapping.Action, mapping.Value = controller.RFActionPWM, value
		mapping.Behavior, err = parseBehavior()
		return mapping, err
	default:
		return controller.RFMapping{}, errors.New("RF action must be none, key, menu, relay, side, or pwm")
	}
}

func (rpcError *RPCError) Error() string {
	return rpcError.Message
}

// Access records transport provenance for authorization and message tagging.
// Remote means a non-loopback network peer, not merely a WebSocket client.
type Access struct {
	Remote         bool
	Transport      string
	Principal      string
	Authentication string
	authenticated  bool
}

const (
	capabilityRead         = "read"
	capabilityEvents       = "events"
	capabilityMessages     = "messages"
	capabilityBoard        = "board_commands"
	capabilityHostConfig   = "host_configuration"
	capabilityConnection   = "connection_control"
	capabilityReset        = "reset"
	capabilityProgramming  = "programming"
	capabilityShutdown     = "shutdown"
	capabilityVirtualKeys  = "virtual_keys"
	capabilityPowerActions = "power_actions"
	capabilityAutomations  = "host_automations"
	capabilityBridgeCalls  = "bridge_calls"
	capabilityIntegrations = "integrations"
)

type Service struct {
	Client           *controller.Client
	WebSocketPath    string
	SocketIOPath     string
	WebUI            http.Handler
	IntegrationProxy http.Handler
	LocalDevice      LocalDeviceService
	HostFacts        hostfacts.Provider
	Artifacts        *artifacts.Service
	ReleaseDiscovery ReleaseDiscoveryService
	AuthToken        string
	// AuthorizationDisabled is the explicit alpha-only escape hatch used by the
	// host while the durable remote-login design is deferred. It deliberately
	// bypasses credential and remote-capability policy; the future authentication
	// feature replaces this flag with its own reviewed policy.
	AuthorizationDisabled bool
	RemotePrincipal       string
	AllowedOrigins        []string
	InboundWebhooks       bool
	HostVersion           string
	HostSourceHash        string
	HostBuildTime         string
	HostInstanceID        string
	HostInstanceToken     string
	HostProcessID         int
	HostSurface           string
	CoordinatorInstanceID string
	AppAction             func(hostui.AppAction) error
	AppInstances          *hostui.InstanceRegistry
	Shutdown              func()
	HostConfig            func() appconfig.Config
	UpdateHostConfig      func(func(*appconfig.Config) error) error
	BridgeList            func() any
	BridgeCall            func(context.Context, string, Request) (Response, error)
	WebhookAdmin          func() WebhookAdminService
	HotkeyStatus          func() any
	LastSessionSnapshot   func() (any, error)
	commandMu             sync.Mutex
	sessionMu             sync.Mutex
	sessionTickets        map[[sha256.Size]byte]sessionTicket
	sessionClock          func() time.Time
}

// browserUISettings is the narrow persistent host-owned subset exposed to the
// browser. Board EEPROM settings remain on the independent board command path.
type browserUISettings struct {
	AppTitle        string                           `json:"app_title"`
	Tagline         string                           `json:"tagline"`
	SetupComplete   bool                             `json:"setup_complete"`
	WelcomeMelody   string                           `json:"welcome_melody"`
	Appearance      browserAppearance                `json:"appearance"`
	AppearanceETag  string                           `json:"appearance_etag"`
	SegmentScroll   appconfig.SegmentScroll          `json:"segment_scroll"`
	PeripheralNames map[string]string                `json:"peripheral_names"`
	Peripherals     []appconfig.PeripheralDescriptor `json:"peripherals"`
	Controls        []appconfig.ControlDescriptor    `json:"controls"`
	Changed         *bool                            `json:"changed,omitempty"`
	ChangedFields   []string                         `json:"changed_fields,omitempty"`
	Before          map[string]any                   `json:"before,omitempty"`
	After           map[string]any                   `json:"after,omitempty"`
}

type peripheralSettings struct {
	Names       map[string]string                `json:"peripheral_names"`
	Peripherals []appconfig.PeripheralDescriptor `json:"peripherals"`
	Controls    []appconfig.ControlDescriptor    `json:"controls"`
}

type hostFactsParams struct {
	Profile   string `json:"profile,omitempty"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

type hotkeyMutation struct {
	Operation string  `json:"operation"`
	Name      string  `json:"name"`
	Enabled   *bool   `json:"enabled,omitempty"`
	Chord     *string `json:"chord,omitempty"`
	Command   *string `json:"command,omitempty"`
}

// LocalDeviceService is the narrow host-owned surface exposed to IPC. Browser
// clients cannot choose an upstream URL or bypass the manager's network and
// payload bounds.
type LocalDeviceService interface {
	Status() any
	Action(context.Context, string, string, int) (any, error)
	Inspect(context.Context, string) (any, error)
}

// WebhookQueueStatus is the non-secret operational state of the durable
// outbound-webhook queue. Endpoint URLs, headers, request bodies, and signing
// secrets are deliberately absent from this transport contract.
type WebhookQueueStatus struct {
	Pending       int        `json:"pending"`
	Dead          int        `json:"dead"`
	Completed     int        `json:"completed_dedupe_records"`
	Enqueued      uint64     `json:"enqueued"`
	Delivered     uint64     `json:"delivered"`
	Retried       uint64     `json:"retried"`
	DeadLettered  uint64     `json:"dead_lettered"`
	Dropped       uint64     `json:"dropped"`
	Duplicates    uint64     `json:"duplicates"`
	DeadDiscarded uint64     `json:"dead_discarded"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	InFlight      int        `json:"in_flight"`
	Closing       bool       `json:"closing"`
}

// WebhookDeliveryView is a bounded delivery record safe for administrative
// clients. It identifies the configured target by name without revealing its
// URL, headers, request body, or signing secret.
type WebhookDeliveryView struct {
	ID             string     `json:"id"`
	CorrelationID  string     `json:"correlation_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	Target         string     `json:"target"`
	EventID        uint64     `json:"event_id"`
	EventKind      string     `json:"event_kind"`
	CreatedAt      time.Time  `json:"created_at"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	LastAttemptID  string     `json:"last_attempt_id,omitempty"`
	Attempts       int        `json:"attempts"`
	MaxAttempts    int        `json:"max_attempts"`
	LastStatus     int        `json:"last_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
}

type WebhookDeliveryList struct {
	Deliveries []WebhookDeliveryView `json:"deliveries"`
	Status     WebhookQueueStatus    `json:"status"`
}

type WebhookReplayResult struct {
	Replayed int                `json:"replayed"`
	Status   WebhookQueueStatus `json:"status"`
}

type WebhookClearResult struct {
	Cleared int                `json:"cleared"`
	Status  WebhookQueueStatus `json:"status"`
}

// WebhookAdminService keeps durable-queue ownership in hostbridge while
// exposing one structured contract consistently to IPC, HTTP, WebSocket, and
// remote bridge callers.
type WebhookAdminService interface {
	WebhookStatus(context.Context) (WebhookQueueStatus, error)
	WebhookPending(context.Context, int) (WebhookDeliveryList, error)
	WebhookDead(context.Context, int) (WebhookDeliveryList, error)
	WebhookReplay(context.Context, string) (WebhookReplayResult, error)
	WebhookClearDead(context.Context, string) (WebhookClearResult, error)
}

type webhookSelectorParams struct {
	DeliveryID string `json:"delivery_id,omitempty"`
	All        bool   `json:"all,omitempty"`
	ConfirmAll bool   `json:"confirm_all,omitempty"`
}

func (service *Service) Dispatch(ctx context.Context, request Request) Response {
	return service.dispatch(ctx, request, Access{Transport: "ipc"})
}

// DispatchRemote applies the current file-watched remote capability policy in
// addition to request authentication. Outbound host bridges use this entry
// point for requests arriving from their authenticated peer.
func (service *Service) DispatchRemote(
	ctx context.Context,
	request Request,
	transport string,
) Response {
	access := service.normalizeAccess(Access{
		Remote: true, Transport: transport, Principal: "bridge-peer",
		Authentication: "bridge-session", authenticated: true,
	})
	return service.dispatch(ctx, request, access)
}

func (service *Service) dispatch(
	ctx context.Context,
	request Request,
	access Access,
) Response {
	response := Response{JSONRPC: Version, ID: request.ID}
	if request.JSONRPC != "" && request.JSONRPC != Version {
		response.Error = &RPCError{Code: -32600, Message: "jsonrpc must be \"2.0\""}
		return response
	}
	if service.Client == nil {
		response.Error = &RPCError{Code: -32603, Message: "controller client is unavailable"}
		return response
	}
	if !access.authenticated {
		var authenticated bool
		access, authenticated = service.authenticateAccess(access, request.Auth, "json-rpc-auth")
		if !authenticated {
			service.auditAccess(Access{
				Remote: access.Remote, Transport: access.Transport,
				Principal: "unauthenticated", Authentication: "json-rpc-auth",
			}, request.Method, "authentication", false)
			response.Error = &RPCError{Code: -32001, Message: "authentication required"}
			return response
		}
	}
	access = service.normalizeAccess(access)
	if err := service.authorizeAccess(access, request.Method, request.Params); err != nil {
		response.Error = &RPCError{Code: -32003, Message: err.Error()}
		return response
	}
	// Primary discovery must not queue behind a long board, firmware, or shell
	// operation holding service.mu. Secondary instances use this bounded ping
	// before deciding whether they may approach the local serial device.
	if request.Method == "controller.ping" {
		response.Result = service.primaryPingResult()
		return response
	}
	if request.Method == "controller.event.next" {
		var params struct {
			AfterID   uint64 `json:"after_id"`
			Kind      string `json:"kind,omitempty"`
			Stream    string `json:"stream,omitempty"`
			Opcode    *int   `json:"opcode,omitempty"`
			TimeoutMS int    `json:"timeout_ms,omitempty"`
		}
		if err := decodeParams(request.Params, &params); err != nil {
			response.Error = &RPCError{Code: -32602, Message: err.Error()}
			return response
		}
		timeout := time.Duration(params.TimeoutMS) * time.Millisecond
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		if timeout > 24*time.Hour {
			response.Error = &RPCError{Code: -32602, Message: "timeout_ms exceeds 24 hours"}
			return response
		}
		waitContext, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		var opcode *byte
		if params.Opcode != nil {
			if *params.Opcode < 1 || *params.Opcode > 255 {
				response.Error = &RPCError{Code: -32602, Message: "opcode must be 1..255; 0 is reserved"}
				return response
			}
			value := byte(*params.Opcode)
			opcode = &value
		}
		var event controller.Event
		var waitErr error
		if opcode != nil {
			event, waitErr = service.Client.NextOpcodeEvent(waitContext, params.AfterID, params.Kind, opcode)
		} else {
			event, waitErr = service.Client.NextEventStream(waitContext, params.AfterID, params.Kind, params.Stream)
		}
		if waitErr != nil {
			response.Error = &RPCError{Code: -32000, Message: waitErr.Error()}
		} else {
			response.Result = event
		}
		return response
	}
	if request.Method == "controller.event.latest" {
		response.Result = map[string]uint64{
			"id": service.Client.LatestEventID(),
		}
		return response
	}
	if request.Method == "controller.os.facts.catalog" ||
		request.Method == "controller.host.facts.catalog" {
		response.Result = hostfacts.Catalog()
		return response
	}
	if request.Method == "controller.os.facts" || request.Method == "controller.host.facts" {
		result, err := service.queryHostFacts(ctx, request.Params)
		if err != nil {
			var rpcError *RPCError
			if errors.As(err, &rpcError) {
				response.Error = rpcError
			} else {
				response.Error = &RPCError{Code: -32000, Message: err.Error()}
			}
		} else {
			response.Result = result
		}
		return response
	}
	if strings.HasPrefix(request.Method, "controller.webhooks.") {
		result, err := service.dispatchWebhookAdmin(ctx, request.Method, request.Params)
		if err != nil {
			var rpcError *RPCError
			if errors.As(err, &rpcError) {
				response.Error = rpcError
			} else {
				response.Error = &RPCError{Code: -32000, Message: err.Error()}
			}
		} else {
			response.Result = result
		}
		return response
	}

	var result any
	var err error
	switch request.Method {
	case "controller.device.status":
		if service.LocalDevice == nil {
			err = errors.New("local-device integration is unavailable")
		} else {
			result = service.LocalDevice.Status()
		}
	case "controller.device.action":
		var params struct {
			Action string `json:"action"`
			Text   string `json:"text,omitempty"`
			Count  int    `json:"count,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.LocalDevice == nil {
				err = errors.New("local-device integration is unavailable")
			} else {
				result, err = service.LocalDevice.Action(
					ctx,
					params.Action,
					params.Text,
					params.Count,
				)
			}
		}
	case "controller.device.inspect":
		var params struct {
			Resource string `json:"resource"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.LocalDevice == nil {
				err = errors.New("local-device integration is unavailable")
			} else {
				result, err = service.LocalDevice.Inspect(ctx, params.Resource)
			}
		}
	case "controller.integrations.local.get":
		config := service.hostConfig().Integrations
		result = map[string]any{
			"local_device":     config.LocalDevice,
			"data_hub":         config.DataHub,
			"lifecycle_safety": config.Lifecycle,
		}
	case "controller.integrations.local.set":
		var params struct {
			LocalDevice     appconfig.LocalDevice      `json:"local_device"`
			DataHub         appconfig.DataHub          `json:"data_hub"`
			LifecycleSafety *appconfig.LifecycleSafety `json:"lifecycle_safety,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.UpdateHostConfig == nil {
				err = errors.New("persistent host configuration is unavailable")
			} else {
				err = service.UpdateHostConfig(func(value *appconfig.Config) error {
					value.Integrations.LocalDevice = params.LocalDevice
					value.Integrations.DataHub = params.DataHub
					if params.LifecycleSafety != nil {
						value.Integrations.Lifecycle = *params.LifecycleSafety
					}
					return nil
				})
				if err == nil {
					config := service.hostConfig().Integrations
					result = map[string]any{
						"local_device":     config.LocalDevice,
						"data_hub":         config.DataHub,
						"lifecycle_safety": config.Lifecycle,
					}
				}
			}
		}
	case "controller.ui.config", "controller.ui.config.get":
		result = service.browserUISettings()
	case "controller.peripherals", "controller.peripherals.get":
		result = service.peripheralSettings()
	case "controller.peripherals.set":
		var params struct {
			PeripheralNames map[string]string `json:"peripheral_names"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if params.PeripheralNames == nil {
				err = errors.New("peripheral_names is required")
			} else if err = service.setPeripheralNames(params.PeripheralNames); err == nil {
				result = service.peripheralSettings()
			}
		}
	case "controller.ui.config.set":
		result, err = service.updateBrowserUISettings(request.Params)
	case "controller.hotkeys.get":
		result = service.hotkeySettings(false)
	case "controller.hotkeys.set":
		var params hotkeyMutation
		if err = decodeHotkeyMutation(request.Params, &params); err != nil {
			err = &RPCError{Code: -32602, Message: err.Error()}
		} else {
			result, err = service.applyHotkeyMutation(params)
		}
	case "controller.connect", "controller.open", "controller.port.open":
		var params struct {
			Port string `json:"port"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if params.Port == "" {
				err = service.Client.Connect(ctx)
			} else {
				err = service.Client.Open(ctx, params.Port)
			}
			if err == nil {
				result = service.Client.Snapshot()
			}
		}
	case "controller.close", "controller.port.close":
		err = service.Client.Close()
		result = map[string]bool{"closed": err == nil}
	case "controller.reset.lines", "controller.reset", "controller.port.reset":
		var params struct {
			PulseMS int `json:"pulse_ms"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if params.PulseMS <= 0 {
				params.PulseMS = 120
			}
			err = service.Client.PulseResetFor(
				ctx,
				time.Duration(params.PulseMS)*time.Millisecond,
			)
			result = map[string]bool{"reset": err == nil}
		}
	case "controller.snapshot":
		result = service.Client.Snapshot()
	case "controller.session.snapshot", "controller.session.snapshot.last":
		if service.LastSessionSnapshot == nil {
			err = errors.New("graceful-exit diagnostic snapshot is unavailable")
		} else {
			result, err = service.LastSessionSnapshot()
		}
	case "controller.front_panel", "controller.front-panel":
		result, err = service.Client.RefreshFrontPanel(ctx)
	case "controller.command.catalog":
		result = service.Client.CommandCatalog()
	case "controller.program_state.get", "controller.program-state.get":
		result = service.Client.ProgramState()
	case "controller.program_state.set", "controller.program-state.set":
		var params struct {
			Owner  string `json:"owner,omitempty"`
			Mode   string `json:"mode"`
			Reason string `json:"reason,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			params.Owner = strings.TrimSpace(params.Owner)
			if params.Owner == "" {
				params.Owner = "rpc"
			}
			var mode controller.ProgramMode
			mode, err = parseProgramMode(params.Mode)
			if err == nil {
				result, err = service.Client.SetProgramState(params.Owner, mode, params.Reason)
			}
		}
	case "controller.status":
		var status controller.Status
		status, err = service.Client.Status(ctx)
		if err == nil {
			result = status
		}
	case "controller.pwm.values":
		result, err = service.Client.PWMValues(ctx)
	case "controller.pwm.set":
		var params struct {
			Channel int `json:"channel"`
			Value   int `json:"value"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if params.Channel < 0 || params.Channel > 15 || params.Value < 0 || params.Value > 4095 {
				err = &RPCError{Code: -32602, Message: "channel must be 0..15 and value must be 0..4095"}
			} else if err = service.Client.SetPWMChannel(ctx, byte(params.Channel), uint16(params.Value)); err == nil {
				result, err = service.Client.PWMValues(ctx)
			}
		}
	case "controller.pwm.off":
		if err = service.Client.AllPWMOff(ctx); err == nil {
			result, err = service.Client.PWMValues(ctx)
		}
	case "controller.temperatures":
		var params struct {
			Rescan bool `json:"rescan,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = service.Client.Temperatures(ctx, params.Rescan)
		}
	case "controller.menu.list", "controller.menu.current":
		result, err = service.Client.MenuCatalog(ctx)
	case "controller.menu.layout.get":
		result, err = service.Client.MenuLayout(ctx)
	case "controller.menu.layout.set":
		var params struct {
			VisibleMask uint16 `json:"visible_mask"`
			Order       []byte `json:"order"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = service.Client.SetMenuLayout(ctx, controller.MenuLayout{
				VisibleMask: params.VisibleMask,
				Order:       params.Order,
			})
		}
	case "controller.host_menu.config", "controller.host_menu.config.get":
		result = service.hostConfig().HostMenus
	case "controller.host_menu.configure", "controller.host_menu.config.set":
		var config appconfig.HostMenuConfig
		if err = decodeParams(request.Params, &config); err == nil {
			if service.UpdateHostConfig == nil {
				err = errors.New("persistent host configuration is unavailable")
			} else {
				err = service.UpdateHostConfig(func(value *appconfig.Config) error {
					value.HostMenus = config
					return nil
				})
				if err == nil {
					result = service.hostConfig().HostMenus
				}
			}
		}
	case "controller.host_menu.directory.replace":
		var directory controller.HostMenuDirectory
		if err = decodeParams(request.Params, &directory); err == nil {
			err = service.Client.ReplaceHostMenuDirectory(ctx, directory)
			if err == nil {
				result = directory
			}
		}
	case "controller.host_menu.content.push":
		var content controller.HostMenuContent
		if err = decodeParams(request.Params, &content); err == nil {
			err = service.Client.PushHostMenuContent(ctx, content)
			if err == nil {
				result = content
			}
		}
	case "controller.host_menu.state":
		result, err = service.Client.HostMenuState(ctx)
	case "controller.macro.snapshot", "controller.macro.list", "controller.macro.status":
		result = service.Client.MacroSnapshot()
	case "controller.macro.create":
		var params struct {
			ID       *int   `json:"id"`
			Name     string `json:"name"`
			Category string `json:"category,omitempty"`
			Color    string `json:"color,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if params.ID == nil || *params.ID < 0 || *params.ID > 255 {
				err = errors.New("macro id is required and must be 0..255")
			} else {
				_, err = service.Client.MacroCreate(byte(*params.ID), params.Name, params.Category, params.Color)
				if err == nil {
					result = service.Client.MacroSnapshot()
				}
			}
		}
	case "controller.macro.update":
		var params struct {
			Reference string  `json:"reference"`
			Name      string  `json:"name"`
			Category  *string `json:"category,omitempty"`
			Color     *string `json:"color,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			_, err = service.Client.MacroUpdate(params.Reference, params.Name, params.Category, params.Color)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.delete":
		var params struct {
			Reference string `json:"reference"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			err = service.Client.MacroDelete(params.Reference)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.record.start":
		var params struct {
			Name     string `json:"name"`
			Category string `json:"category,omitempty"`
			Color    string `json:"color,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			_, err = service.Client.MacroRecordStart(params.Name, params.Category, params.Color)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.record.stop":
		var params struct {
			Save *bool `json:"save,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			save := true
			if params.Save != nil {
				save = *params.Save
			}
			_, err = service.Client.MacroRecordStop(save)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.board_record.start":
		var params struct {
			ID *int `json:"id"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if params.ID == nil || *params.ID < 0 || *params.ID > 255 {
				err = errors.New("macro capture id is required and must be 0..255")
			} else {
				_, err = service.Client.MacroBoardRecordStart(ctx, byte(*params.ID))
				if err == nil {
					result = service.Client.MacroSnapshot()
				}
			}
		}
	case "controller.macro.board_record.stop":
		_, err = service.Client.MacroBoardRecordStop(ctx)
		if err == nil {
			result = service.Client.MacroSnapshot()
		}
	case "controller.macro.board_record.clear":
		var params struct {
			Force bool `json:"force,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			_, err = service.Client.MacroBoardRecordClear(ctx, params.Force)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.play":
		var params struct {
			Reference string `json:"reference"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			_, err = service.Client.MacroPlay(ctx, params.Reference)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.macro.cancel":
		var params struct {
			KeepOutputs bool `json:"keep_outputs,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			err = service.Client.MacroCancel(ctx, params.KeepOutputs)
			if err == nil {
				result = service.Client.MacroSnapshot()
			}
		}
	case "controller.menu.jump", "controller.menu.page":
		var params struct {
			Page string `json:"page"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if strings.TrimSpace(params.Page) == "" {
				err = errors.New("page ID or name is required")
			} else {
				result, err = service.Client.SetMenuPageByName(ctx, params.Page)
			}
		}
	case "controller.command.execute":
		var params struct {
			Command string `json:"command"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			command := strings.TrimSpace(params.Command)
			if command == "" {
				err = errors.New("command is required")
			} else if strings.HasPrefix(strings.ToLower(command), "app ") {
				var action hostui.AppAction
				action, err = hostui.ParseAction(command, "ipc-command")
				if err == nil {
					if service.AppAction == nil {
						err = errors.New("primary app action routing is unavailable")
					} else {
						err = service.AppAction(action)
						if err == nil {
							result = map[string]string{"output": "app action accepted"}
						}
					}
				}
			} else if strings.EqualFold(command, "quit") || strings.EqualFold(command, "exit") {
				if service.Shutdown == nil {
					err = errors.New("primary-process shutdown is unavailable")
				} else {
					result = map[string]string{"output": "primary shutdown accepted"}
					go func() {
						time.Sleep(25 * time.Millisecond)
						service.Shutdown()
					}()
				}
			} else {
				var output string
				service.commandMu.Lock()
				output, err = service.Client.Execute(ctx, command)
				service.commandMu.Unlock()
				if err == nil {
					result = map[string]string{"output": output}
				}
			}
		}
	case "controller.rf.list":
		result, err = service.Client.ListLearnedDetailed(ctx)
	case "controller.rf.presentation":
		result = map[string]any{
			"config":  service.Client.RFPresentation(),
			"palette": appconfig.RFCategoryPalette,
		}
	case "controller.rf.learn.start":
		var params struct {
			Mode      string `json:"mode,omitempty"`
			TimeoutMS int64  `json:"timeout_ms,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			var mode controller.RFLearnMode
			mode, err = controller.ParseRFLearnMode(params.Mode)
			if err != nil {
				break
			}
			maximumMS := int64(native.MaxRFLearnSeconds) * 1000
			if params.TimeoutMS < 0 || params.TimeoutMS > maximumMS {
				err = fmt.Errorf("RF learn timeout_ms must be 0..%d", maximumMS)
				break
			}
			if mode == controller.RFLearnIndefinite && params.TimeoutMS != 0 {
				err = errors.New("indefinite RF learning does not accept timeout_ms")
				break
			}
			result, err = service.Client.StartRFLearning(
				ctx,
				controller.RFLearnOptions{
					Mode: mode, Timeout: time.Duration(params.TimeoutMS) * time.Millisecond,
				},
			)
		}
	case "controller.rf.learn.status":
		result = service.Client.RFLearningState()
	case "controller.rf.learn.cancel":
		err = service.Client.CancelRFLearn(ctx)
		result = map[string]bool{"cancelled": err == nil}
	case "controller.rf.map":
		var params rfMapParams
		if err = decodeStrictParams(request.Params, &params); err == nil {
			var mapping controller.RFMapping
			mapping, err = params.mapping()
			if err == nil {
				err = service.Client.MapLearnedRF(ctx, byte(*params.ID), mapping)
			}
			if err == nil {
				result, err = service.Client.ListLearnedDetailed(ctx)
			}
		}
	case "controller.rf.remove":
		var params struct {
			ID *int `json:"id"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if params.ID == nil {
				err = errors.New("RF learned entry id is required")
			} else if *params.ID < 0 || *params.ID > 19 {
				err = errors.New("RF learned entry id must be 0..19")
			} else {
				err = service.Client.RemoveLearnedRF(ctx, byte(*params.ID))
			}
			if err == nil {
				var entries []controller.RFEntryView
				entries, err = service.Client.ListLearnedDetailed(ctx)
				if err == nil {
					for _, entry := range entries {
						if int(entry.ID) == *params.ID {
							err = fmt.Errorf("RF entry %d remains after remove readback", *params.ID)
							break
						}
					}
				}
				if err == nil {
					result = entries
				}
			}
		}
	case "controller.rf.clear":
		var params struct {
			Confirm string `json:"confirm"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if params.Confirm != "CLEAR RF" {
				err = errors.New("RF clear requires confirm=\"CLEAR RF\"")
			} else {
				err = service.Client.ClearLearnedRF(ctx)
			}
			if err == nil {
				var entries []controller.RFEntryView
				entries, err = service.Client.ListLearnedDetailed(ctx)
				if err == nil && len(entries) != 0 {
					err = fmt.Errorf("RF clear readback still contains %d record(s)", len(entries))
				}
				if err == nil {
					result = entries
				}
			}
		}
	case "controller.rf.transmit":
		var params struct {
			Code     *uint32 `json:"code"`
			Bits     *int    `json:"bits"`
			Protocol *int    `json:"protocol"`
			PulseUS  int     `json:"pulse_us,omitempty"`
			Repeats  int     `json:"repeats,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if params.Code == nil || params.Bits == nil || params.Protocol == nil {
				err = errors.New("RF transmit requires code, bits, and protocol")
			} else if *params.Code == 0 {
				err = errors.New("RF code must be nonzero")
			} else if *params.Bits < 1 || *params.Bits > 32 {
				err = errors.New("RF bits must be 1..32")
			} else if *params.Protocol < 1 || *params.Protocol > 12 {
				err = errors.New("RF protocol must be 1..12")
			} else if params.PulseUS < 0 || params.PulseUS > 65535 {
				err = errors.New("RF pulse_us must be 0..65535")
			} else if params.Repeats < 0 || params.Repeats > 20 {
				err = errors.New("RF repeats must be 1..20 when supplied")
			} else {
				err = service.Client.TransmitRF(
					ctx, *params.Code, byte(*params.Bits), byte(*params.Protocol),
					uint16(params.PulseUS), byte(params.Repeats),
				)
			}
			if err == nil {
				repeats := params.Repeats
				if repeats == 0 {
					repeats = 1
				}
				result = map[string]any{
					"transmitted": true, "code": *params.Code,
					"bits": *params.Bits, "protocol": *params.Protocol,
					"pulse_us": params.PulseUS, "repeats": repeats,
				}
			}
		}
	case "controller.history.status":
		var params struct {
			Since string `json:"since,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			var since time.Time
			since, err = parseOptionalTime(params.Since)
			if err == nil {
				result = service.Client.StatusHistory(since)
			}
		}
	case "controller.history.timeline":
		var params struct {
			Since string `json:"since,omitempty"`
			Limit int    `json:"limit,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			var since time.Time
			since, err = parseOptionalTime(params.Since)
			if err == nil {
				result = service.Client.Timeline(since, params.Limit)
			}
		}
	case "controller.lcd.presentation.status":
		result = service.Client.LCDPresentationState()
	case "controller.lcd.presentation.configure":
		var params struct {
			Enabled        bool  `json:"enabled"`
			DebounceMS     int64 `json:"debounce_ms,omitempty"`
			PriorityHoldMS int64 `json:"priority_hold_ms,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			err = service.Client.ConfigureLCDPresentation(
				controller.LCDPresentationOptions{
					Enabled:      params.Enabled,
					Debounce:     time.Duration(params.DebounceMS) * time.Millisecond,
					PriorityHold: time.Duration(params.PriorityHoldMS) * time.Millisecond,
				},
			)
			result = service.Client.LCDPresentationState()
		}
	case "controller.lcd.prompt":
		var params struct {
			Line1 string `json:"line1"`
			Line2 string `json:"line2"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			service.Client.MirrorLCDPrompt(params.Line1, params.Line2)
			result = map[string]bool{"queued": true}
		}
	case "controller.lcd.priority":
		var params struct {
			Kind   string `json:"kind"`
			Line1  string `json:"line1"`
			Line2  string `json:"line2"`
			HoldMS int64  `json:"hold_ms,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			accepted := service.Client.ShowLCDPriority(
				params.Kind,
				params.Line1,
				params.Line2,
				time.Duration(params.HoldMS)*time.Millisecond,
			)
			result = map[string]bool{"accepted": accepted}
		}
	case "controller.display.send":
		var params controller.DisplayRequest
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = service.Client.PresentDisplay(ctx, params)
		}
	case "controller.opcode.send", "controller.opcode.exchange", "controller.opcode.request":
		var params opcodeExchangeParams
		if err = decodeStrictParams(request.Params, &params); err == nil {
			var opcode, expected byte
			var payload []byte
			opcode, payload, expected, err = params.values()
			if err == nil {
				result, err = service.Client.ExchangeOpcode(ctx, opcode, payload, expected)
			}
		}
	case "controller.message.send":
		var message controller.TextMessage
		if err = decodeParams(request.Params, &message); err == nil {
			message = tagInboundAccess(message, access)
			result, err = service.Client.SendTextMessage(ctx, message)
		}
	case "controller.message.delivery":
		var params struct {
			EventID uint64 `json:"event_id"`
			Surface string `json:"surface"`
			Error   string `json:"error,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			result, err = service.Client.AcknowledgeMessageDelivery(
				params.EventID, params.Surface, params.Error,
			)
		}
	case "controller.message.action":
		var params struct {
			EventID    uint64 `json:"event_id"`
			Surface    string `json:"surface"`
			InstanceID string `json:"instance_id,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			var message controller.Event
			message, err = service.Client.MessageForSurface(params.EventID, params.Surface)
			if err == nil {
				var output string
				var actionErr error
				if strings.TrimSpace(message.Action) == "" {
					actionErr = errors.New("message has no action")
				} else {
					var action hostui.AppAction
					action, actionErr = hostui.ParseAction(
						message.Action,
						"message:"+strings.ToLower(strings.TrimSpace(params.Surface)),
					)
					if actionErr == nil && action.Kind == "command" {
						output, actionErr = service.Client.Execute(ctx, action.Value)
					} else if actionErr == nil {
						if service.AppAction == nil {
							actionErr = errors.New("primary app action routing is unavailable")
						} else {
							action.Target = strings.TrimSpace(params.InstanceID)
							actionErr = service.AppAction(action)
							if actionErr == nil {
								output = "accepted"
							}
						}
					}
				}
				result = service.Client.EmitMessageActionOutcome(
					message, params.Surface, output, actionErr,
				)
			}
		}
	case "controller.bridge.list":
		if service.BridgeList == nil {
			err = errors.New("host bridge manager is unavailable")
		} else {
			result = service.BridgeList()
		}
	case "controller.bridge.call":
		var params struct {
			Peer    string  `json:"peer"`
			Request Request `json:"request"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if strings.TrimSpace(params.Peer) == "" || strings.TrimSpace(params.Request.Method) == "" {
				err = errors.New("bridge peer and request.method are required")
			} else if params.Request.Method == "controller.bridge.call" {
				err = errors.New("recursive bridge calls are not permitted")
			} else if service.BridgeCall == nil {
				err = errors.New("host bridge manager is unavailable")
			} else {
				params.Request.Auth = ""
				var bridgeResponse Response
				bridgeResponse, err = service.BridgeCall(ctx, params.Peer, params.Request)
				if err == nil {
					result = map[string]any{
						"peer": params.Peer, "response": bridgeResponse,
					}
				}
			}
		}
	case "controller.app.action":
		var action hostui.AppAction
		if err = decodeParams(request.Params, &action); err == nil {
			if service.AppAction == nil {
				err = errors.New("primary app action routing is unavailable")
			} else {
				action.Source = firstNonempty(action.Source, "ipc")
				err = service.AppAction(action)
				result = map[string]bool{"accepted": err == nil}
			}
		}
	case "controller.app.page":
		var params struct {
			Page   string `json:"page"`
			Target string `json:"target,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.AppAction == nil {
				err = errors.New("primary app page routing is unavailable")
			} else {
				err = service.AppAction(hostui.AppAction{
					Kind: "app.page", Value: params.Page, Source: "ipc", Target: params.Target,
				})
				result = map[string]bool{"accepted": err == nil}
			}
		}
	case "controller.app.navigate":
		var params struct {
			Page   string `json:"page"`
			Target string `json:"target,omitempty"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if service.AppAction == nil {
				err = errors.New("primary app navigation routing is unavailable")
			} else {
				target := strings.TrimSpace(params.Target)
				if target == "" {
					target = "*"
				}
				err = service.AppAction(hostui.AppAction{
					Kind: "app.page", Value: params.Page, Source: "ipc", Target: target,
				})
				result = map[string]bool{"accepted": err == nil}
			}
		}
	case "controller.app.instances":
		if service.AppInstances == nil {
			err = errors.New("app instance registry is unavailable")
		} else {
			result = service.AppInstances.List()
		}
	case "controller.app.bridge":
		if service.AppInstances == nil {
			err = errors.New("app instance registry is unavailable")
		} else if strings.TrimSpace(service.CoordinatorInstanceID) == "" {
			err = errors.New("coordinator bridge instance is unavailable")
		} else if instance, ok := service.AppInstances.Get(service.CoordinatorInstanceID); !ok {
			err = errors.New("coordinator bridge instance is not registered")
		} else {
			result = instance
		}
	case "controller.app.instance.get":
		var params struct {
			ID string `json:"id"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if service.AppInstances == nil {
				err = errors.New("app instance registry is unavailable")
			} else if instance, ok := service.AppInstances.Get(params.ID); !ok {
				err = fmt.Errorf("app instance %q is not registered", strings.TrimSpace(params.ID))
			} else {
				result = instance
			}
		}
	case "controller.app.instance.report":
		var params hostui.AppInstance
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if service.AppInstances == nil {
				err = errors.New("app instance registry is unavailable")
			} else {
				result, err = service.AppInstances.Upsert(params)
			}
		}
	case "controller.app.instance.remove":
		var params struct {
			ID string `json:"id"`
		}
		if err = decodeStrictParams(request.Params, &params); err == nil {
			if service.AppInstances == nil {
				err = errors.New("app instance registry is unavailable")
			} else {
				result = map[string]bool{"removed": service.AppInstances.Remove(params.ID)}
			}
		}
	case "controller.ports":
		result, err = controller.ListPorts()
	case "controller.os.status", "controller.system.status":
		result, err = hostos.Status(ports.EnumerationSource())
	case "controller.os.policy":
		result = service.hostConfig().OSActions
	case "controller.os.configure":
		var policy hostos.Policy
		if err = decodeParams(request.Params, &policy); err == nil {
			if service.UpdateHostConfig == nil {
				err = errors.New("persistent host configuration is unavailable")
			} else if err = hostos.ValidatePolicy(policy); err == nil {
				err = service.UpdateHostConfig(func(config *appconfig.Config) error {
					config.OSActions = hostos.ClonePolicy(policy)
					return nil
				})
				result = service.hostConfig().OSActions
			}
		}
	case "controller.os.key", "controller.virtual_key":
		var params hostos.VirtualKeyRequest
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = service.pressVirtualKey(ctx, params, "rpc")
		}
	case "controller.os.power":
		var params hostos.PowerRequest
		if err = decodeParams(request.Params, &params); err == nil {
			params.Automation = false
			result, err = service.powerAction(ctx, params, "rpc")
		}
	case "controller.discovery.scan":
		var params struct {
			TimeoutMS int  `json:"timeout_ms,omitempty"`
			MDNS      bool `json:"mdns,omitempty"`
			SSDP      bool `json:"ssdp,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if !params.MDNS && !params.SSDP {
				params.MDNS, params.SSDP = true, true
			}
			if params.TimeoutMS == 0 {
				params.TimeoutMS = 1500
			}
			if params.TimeoutMS < 100 || params.TimeoutMS > 30_000 {
				err = errors.New("discovery timeout_ms must be 100..30000")
				break
			}
			discoveryContext, cancel := context.WithTimeout(
				ctx, time.Duration(params.TimeoutMS)*time.Millisecond,
			)
			result, err = discovery.Discover(discoveryContext, params.MDNS, params.SSDP)
			cancel()
		}
	case "controller.quit", "controller.exit":
		if service.Shutdown == nil {
			err = errors.New("primary-process shutdown is unavailable")
			break
		}
		result = map[string]bool{"accepted": true}
		go func() {
			time.Sleep(25 * time.Millisecond)
			service.Shutdown()
		}()
	default:
		if service.ReleaseDiscovery != nil {
			var handled bool
			result, handled, err = service.ReleaseDiscovery.DispatchRPC(ctx, request.Method, request.Params)
			if handled {
				break
			}
		}
		if service.Artifacts != nil {
			var handled bool
			result, handled, err = service.Artifacts.DispatchRPC(ctx, request.Method, request.Params)
			if handled {
				break
			}
		}
		response.Error = &RPCError{Code: -32601, Message: "method not found"}
		return response
	}
	if err != nil {
		var rpcError *RPCError
		if errors.As(err, &rpcError) {
			response.Error = rpcError
		} else {
			response.Error = &RPCError{Code: -32000, Message: err.Error()}
		}
		return response
	}
	response.Result = result
	return response
}

func (service *Service) primaryPingResult() map[string]any {
	return map[string]any{
		"ok": true, "jsonrpc": Version,
		"instance_id":             strings.TrimSpace(service.HostInstanceID),
		"coordinator_instance_id": strings.TrimSpace(service.CoordinatorInstanceID),
		"process_id":              service.HostProcessID,
		"surface":                 strings.TrimSpace(service.HostSurface),
	}
}

func (service *Service) hostConfig() appconfig.Config {
	if service.HostConfig != nil {
		return service.HostConfig()
	}
	return appconfig.Defaults()
}

func (service *Service) browserUISettings() browserUISettings {
	ui := service.hostConfig().UI
	return browserUISettings{
		AppTitle:        productidentity.Title(ui.AppTitle),
		Tagline:         ui.Tagline,
		SetupComplete:   ui.SetupComplete,
		WelcomeMelody:   ui.WelcomeMelody,
		Appearance:      browserAppearanceFromConfig(ui.Appearance),
		AppearanceETag:  appearanceETag(ui.Appearance),
		SegmentScroll:   ui.SegmentScroll,
		PeripheralNames: clonePeripheralNames(ui.PeripheralNames),
		Peripherals:     appconfig.PeripheralDescriptors(),
		Controls:        appconfig.ControlDescriptors(ui.PeripheralNames),
	}
}

func clonePeripheralNames(names map[string]string) map[string]string {
	result := make(map[string]string, len(names))
	for key, name := range names {
		result[key] = name
	}
	return result
}

func normalizePeripheralNames(names map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(names))
	for rawKey, rawName := range names {
		key := strings.TrimSpace(rawKey)
		name := strings.TrimSpace(rawName)
		if key == "" {
			return nil, errors.New("peripheral_names keys must not be blank")
		}
		if name == "" {
			continue
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("peripheral_names contains duplicate normalized key %q", key)
		}
		result[key] = name
	}
	return result, nil
}

func (service *Service) peripheralSettings() peripheralSettings {
	return peripheralSettings{
		Names:       clonePeripheralNames(service.hostConfig().UI.PeripheralNames),
		Peripherals: appconfig.PeripheralDescriptors(),
		Controls:    appconfig.ControlDescriptors(service.hostConfig().UI.PeripheralNames),
	}
}

func (service *Service) setPeripheralNames(names map[string]string) error {
	if service.UpdateHostConfig == nil {
		return errors.New("persistent host configuration is unavailable")
	}
	normalized, err := normalizePeripheralNames(names)
	if err != nil {
		return err
	}
	candidate := service.hostConfig()
	candidate.UI.PeripheralNames = normalized
	if err := candidate.Validate(); err != nil {
		return err
	}
	return service.UpdateHostConfig(func(value *appconfig.Config) error {
		value.UI.PeripheralNames = clonePeripheralNames(normalized)
		return nil
	})
}

func (service *Service) hostFacts() hostfacts.Provider {
	if service.HostFacts != nil {
		return service.HostFacts
	}
	return hostfacts.Default()
}

func (service *Service) queryHostFacts(
	ctx context.Context,
	raw json.RawMessage,
) (hostfacts.Result, error) {
	var params hostFactsParams
	if err := decodeHostFactsParams(raw, &params); err != nil {
		return hostfacts.Result{}, &RPCError{Code: -32602, Message: err.Error()}
	}
	if params.TimeoutMS == 0 {
		params.TimeoutMS = int(hostfacts.DefaultQueryTimeout / time.Millisecond)
	}
	if params.TimeoutMS < 100 ||
		time.Duration(params.TimeoutMS)*time.Millisecond > hostfacts.MaxQueryTimeout {
		return hostfacts.Result{}, &RPCError{
			Code: -32602,
			Message: fmt.Sprintf(
				"host-facts timeout_ms must be 100..%d",
				hostfacts.MaxQueryTimeout/time.Millisecond,
			),
		}
	}
	queryContext, cancel := context.WithTimeout(
		ctx,
		time.Duration(params.TimeoutMS)*time.Millisecond,
	)
	defer cancel()
	return service.hostFacts().Query(queryContext, params.Profile)
}

func decodeHostFactsParams(raw json.RawMessage, target *hostFactsParams) error {
	return decodeStrictParams(raw, target)
}

func decodeHotkeyMutation(raw json.RawMessage, target *hotkeyMutation) error {
	return decodeStrictParams(raw, target)
}

func decodeStrictParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("invalid params: multiple JSON values")
		}
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (service *Service) applyHotkeyMutation(params hotkeyMutation) (map[string]any, error) {
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	operation := strings.ToLower(strings.TrimSpace(params.Operation))
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, errors.New("hotkey name is required")
	}
	if operation != "upsert" && operation != "remove" {
		return nil, errors.New("hotkey operation must be upsert or remove")
	}
	if operation == "remove" && (params.Enabled != nil || params.Chord != nil || params.Command != nil) {
		return nil, errors.New("remove accepts only operation and name")
	}
	err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		bindings := append([]appconfig.Hotkey(nil), config.Integrations.Hotkeys...)
		index := -1
		for candidateIndex := range bindings {
			if strings.EqualFold(bindings[candidateIndex].Name, name) {
				index = candidateIndex
				break
			}
		}
		if operation == "remove" {
			if index < 0 {
				return fmt.Errorf("hotkey %q does not exist", name)
			}
			bindings = append(bindings[:index], bindings[index+1:]...)
		} else {
			if index >= 0 && params.Enabled == nil && params.Chord == nil && params.Command == nil {
				return errors.New("upsert requires enabled, chord, or command for an existing hotkey")
			}
			binding := appconfig.Hotkey{Name: name, Enabled: true}
			if index >= 0 {
				binding = bindings[index]
				binding.Name = name
			}
			if params.Enabled != nil {
				binding.Enabled = *params.Enabled
			}
			if params.Chord != nil {
				accelerator, parseErr := hostui.ParseAccelerator(strings.TrimSpace(*params.Chord))
				if parseErr != nil {
					return fmt.Errorf("hotkey chord: %w", parseErr)
				}
				binding.Chord = accelerator.Canonical
			}
			if params.Command != nil {
				binding.Command = strings.TrimSpace(*params.Command)
			}
			if index < 0 && (params.Chord == nil || params.Command == nil) {
				return errors.New("a new hotkey requires chord and command")
			}
			if index >= 0 {
				bindings[index] = binding
			} else {
				bindings = append(bindings, binding)
			}
		}
		if validateErr := appconfig.ValidateHotkeys(bindings); validateErr != nil {
			return validateErr
		}
		config.Integrations.Hotkeys = bindings
		return nil
	})
	if err != nil {
		return nil, err
	}
	result := service.hotkeySettings(true)
	result["operation"] = operation
	result["name"] = name
	return result, nil
}

func (service *Service) hotkeySettings(applyPending bool) map[string]any {
	result := map[string]any{
		"bindings":      service.hostConfig().Integrations.Hotkeys,
		"apply_pending": applyPending,
	}
	if service.HotkeyStatus != nil {
		result["status"] = service.HotkeyStatus()
	}
	return result
}

func (service *Service) authorizeAccess(
	access Access,
	method string,
	params json.RawMessage,
) error {
	if !access.Remote {
		return nil
	}
	capability := requestCapability(method, params)
	return service.authorizeCapability(access, method, capability)
}

func (service *Service) authorizeCapability(
	access Access,
	operation, capability string,
) error {
	access = service.normalizeAccess(access)
	if service.authorizationDisabled() {
		return nil
	}
	if !access.Remote {
		if capability != capabilityRead && capability != capabilityEvents {
			service.auditAccess(access, operation, capability, true)
		}
		return nil
	}
	config := service.hostConfig()
	if !config.IPC.AllowRemote {
		service.auditAccess(access, operation, capability, false)
		return errors.New("remote network access is disabled")
	}
	if !remoteCapabilityAllowed(config.IPC.RemotePolicy, capability) {
		service.auditAccess(access, operation, capability, false)
		return fmt.Errorf("remote capability %s is disabled", capability)
	}
	if capability != capabilityRead && capability != capabilityEvents {
		service.auditAccess(access, operation, capability, true)
	}
	return nil
}

func (service *Service) auditAccess(
	access Access,
	method, capability string,
	allowed bool,
) {
	if service.Client == nil {
		return
	}
	decision := "denied"
	if allowed {
		decision = "authorized"
	}
	access = service.normalizeAccess(access)
	scope := "local"
	if access.Remote {
		scope = "remote"
	}
	service.Client.EmitHostEvent(
		"security."+scope+"."+decision,
		fmt.Sprintf(
			"principal=%s transport=%s authentication=%s remote=%t decision=%s capability=%s operation=%s",
			access.Principal, access.Transport, access.Authentication, access.Remote,
			decision, capability, method,
		),
	)
}

func remoteCapabilityAllowed(
	policy appconfig.RemoteAccessPolicy,
	capability string,
) bool {
	switch capability {
	case capabilityRead:
		return policy.Read
	case capabilityEvents:
		return policy.Events
	case capabilityMessages:
		return policy.Messages
	case capabilityBoard:
		return policy.BoardCommands
	case capabilityHostConfig:
		return policy.HostConfiguration
	case capabilityConnection:
		return policy.ConnectionControl
	case capabilityReset:
		return policy.Reset
	case capabilityProgramming:
		return policy.Programming
	case capabilityShutdown:
		return policy.Shutdown
	case capabilityVirtualKeys:
		return policy.VirtualKeys
	case capabilityPowerActions:
		return policy.PowerActions
	case capabilityAutomations:
		return policy.HostAutomations
	case capabilityBridgeCalls:
		return policy.BridgeCalls
	case capabilityIntegrations:
		return policy.Integrations
	default:
		return false
	}
}

func requestCapability(method string, params json.RawMessage) string {
	method = strings.ToLower(strings.TrimSpace(method))
	switch method {
	case "controller.event.next", "controller.event.latest", "controller.subscribe",
		"controller.unsubscribe":
		return capabilityEvents
	case "controller.artifact.manifest", "controller.artifact.list", "controller.update.status":
		return capabilityRead
	case "controller.discovery.github.workflow", "controller.discovery.github.release",
		"controller.discovery.manifest", "controller.discovery.local_manifest",
		"controller.discovery.check", "controller.discovery.status":
		return capabilityRead
	case "controller.artifact.fetch", "controller.artifact.upload", "controller.artifact.capture",
		"controller.update.firmware", "controller.restore.flash",
		"controller.update.eeprom", "controller.update.host", "controller.discovery.stage":
		return capabilityProgramming
	case "controller.connect", "controller.open", "controller.port.open",
		"controller.close", "controller.port.close":
		return capabilityConnection
	case "controller.reset.lines", "controller.reset", "controller.port.reset":
		return capabilityReset
	case "controller.quit", "controller.exit":
		return capabilityShutdown
	case "controller.message.send", "controller.message.delivery", "controller.message.action":
		return capabilityMessages
	case "controller.display.send", "controller.opcode.send",
		"controller.opcode.exchange", "controller.opcode.request":
		return capabilityBoard
	case "controller.macro.snapshot", "controller.macro.list", "controller.macro.status":
		return capabilityRead
	case "controller.macro.create", "controller.macro.update", "controller.macro.delete",
		"controller.macro.record.start", "controller.macro.record.stop":
		return capabilityHostConfig
	case "controller.macro.board_record.start", "controller.macro.board_record.stop",
		"controller.macro.board_record.clear", "controller.macro.play", "controller.macro.cancel":
		return capabilityBoard
	case "controller.host_menu.config", "controller.host_menu.config.get",
		"controller.ui.config", "controller.ui.config.get",
		"controller.peripherals", "controller.peripherals.get",
		"controller.os.policy", "controller.os.facts.catalog",
		"controller.host.facts.catalog", "controller.hotkeys.get",
		"controller.bridge.list", "controller.app.instances", "controller.app.instance.get",
		"controller.app.bridge",
		"controller.webhooks.status",
		"controller.webhooks.pending", "controller.webhooks.dead":
		return capabilityRead
	case "controller.host_menu.configure", "controller.host_menu.config.set",
		"controller.ui.config.set",
		"controller.peripherals.set",
		"controller.hotkeys.set",
		"controller.os.configure", "controller.lcd.presentation.configure",
		"controller.app.page", "controller.app.navigate",
		"controller.app.instance.report", "controller.app.instance.remove":
		return capabilityHostConfig
	case "controller.os.key", "controller.virtual_key":
		return capabilityVirtualKeys
	case "controller.os.power":
		return capabilityPowerActions
	case "controller.bridge.call":
		return capabilityBridgeCalls
	case "controller.webhooks.replay", "controller.webhooks.clear":
		return capabilityIntegrations
	case "controller.device.status", "controller.device.action",
		"controller.device.inspect", "controller.integrations.local.get",
		"controller.integrations.local.set":
		return capabilityIntegrations
	case "controller.command.execute":
		var value struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(params, &value)
		return commandCapability(value.Command)
	case "controller.app.action":
		var action hostui.AppAction
		if json.Unmarshal(params, &action) == nil {
			switch strings.ToLower(strings.TrimSpace(action.Kind)) {
			case "command":
				return commandCapability(action.Value)
			case "app.quit":
				return capabilityShutdown
			case "app.port.open", "app.port.close":
				return capabilityConnection
			}
		}
		return capabilityHostConfig
	case "controller.ping", "controller.snapshot", "controller.session.snapshot",
		"controller.session.snapshot.last", "controller.status",
		"controller.front_panel", "controller.front-panel",
		"controller.command.catalog", "controller.program_state.get", "controller.program-state.get",
		"controller.temperatures", "controller.menu.list", "controller.menu.current",
		"controller.menu.layout.get", "controller.host_menu.state",
		"controller.rf.list", "controller.rf.presentation",
		"controller.rf.learn.status", "controller.history.status",
		"controller.history.timeline", "controller.lcd.presentation.status",
		"controller.ports", "controller.os.status", "controller.system.status",
		"controller.os.facts", "controller.host.facts",
		"controller.discovery.scan", "controller.pwm.values":
		return capabilityRead
	case "controller.program_state.set", "controller.program-state.set":
		return capabilityBoard
	default:
		return capabilityBoard
	}
}

func commandCapability(command string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(command)))
	if len(words) == 0 {
		return capabilityBoard
	}
	switch words[0] {
	case "help", "?", "ports", "hello", "status", "st", "temp", "temperature":
		return capabilityRead
	case "event":
		return capabilityEvents
	case "settings":
		if len(words) == 1 || (len(words) == 2 && words[1] == "status") {
			return capabilityRead
		}
		return capabilityBoard
	case "program-state", "run-state":
		if len(words) == 1 || (len(words) == 2 && words[1] == "status") {
			return capabilityRead
		}
		return capabilityBoard
	case "menu":
		if len(words) >= 2 && (words[1] == "list" || words[1] == "current" ||
			(words[1] == "layout" && len(words) == 2)) {
			return capabilityRead
		}
		return capabilityBoard
	case "pwm":
		if len(words) >= 2 && words[1] == "get" {
			return capabilityRead
		}
		return capabilityBoard
	case "silent":
		if len(words) >= 2 && words[1] == "status" {
			return capabilityRead
		}
		return capabilityBoard
	case "melody":
		if len(words) >= 2 && (words[1] == "list" || words[1] == "status") {
			return capabilityRead
		}
		if len(words) >= 2 && (words[1] == "create" || words[1] == "delete") {
			return capabilityHostConfig
		}
		return capabilityBoard
	case "rgb":
		if len(words) >= 3 && words[1] == "effect" && (words[2] == "list" || words[2] == "status") {
			return capabilityRead
		}
		return capabilityBoard
	case "rf":
		if len(words) >= 2 && (words[1] == "list" || words[1] == "status" || words[1] == "inspect") {
			return capabilityRead
		}
		return capabilityBoard
	case "macro":
		if len(words) >= 2 && (words[1] == "list" || words[1] == "show" || words[1] == "status" || words[1] == "monitor" ||
			(words[1] == "record" && len(words) >= 3 && words[2] == "status")) {
			return capabilityRead
		}
		if len(words) >= 2 && (words[1] == "create" || words[1] == "update" || words[1] == "rename" || words[1] == "category" || words[1] == "categorize" || words[1] == "delete" || words[1] == "remove" || words[1] == "record") {
			return capabilityHostConfig
		}
		return capabilityBoard
	case "automation":
		if len(words) >= 2 && words[1] == "list" {
			return capabilityRead
		}
		return capabilityAutomations
	case "webhook":
		if len(words) >= 2 && (words[1] == "status" || words[1] == "pending" || words[1] == "dead") {
			return capabilityRead
		}
		return capabilityIntegrations
	case "hotkeys":
		if len(words) == 2 && words[1] == "status" {
			return capabilityRead
		}
		return capabilityHostConfig
	case "keyboard":
		if len(words) == 2 && (words[1] == "status" || words[1] == "list") {
			return capabilityRead
		}
		return capabilityVirtualKeys
	case "open", "close", "reconnect":
		return capabilityConnection
	case "reset":
		return capabilityReset
	case "toolchain":
		if len(words) == 2 && words[1] == "profile" {
			return capabilityRead
		}
		return capabilityProgramming
	case "board", "boot", "program", "programmer", "firmware", "flash", "upload",
		"restore", "query", "write":
		return capabilityProgramming
	case "bridge":
		if len(words) == 2 && words[1] == "list" {
			return capabilityRead
		}
		return capabilityBridgeCalls
	case "quit", "exit":
		return capabilityShutdown
	case "os":
		if len(words) < 2 {
			return capabilityRead
		}
		switch words[1] {
		case "status", "policy", "facts":
			return capabilityRead
		case "key", "virtual":
			return capabilityVirtualKeys
		case "power", "power-policy":
			return capabilityPowerActions
		case "brightness":
			if len(words) >= 3 && words[2] == "get" {
				return capabilityRead
			}
			return capabilityPowerActions
		}
		return capabilityPowerActions
	default:
		return capabilityBoard
	}
}

func tagInboundMessage(message controller.TextMessage, transport string) controller.TextMessage {
	transport = strings.ToLower(strings.TrimSpace(transport))
	if transport == "" {
		transport = "ipc"
	}
	if message.Metadata == nil {
		message.Metadata = make(map[string]string)
	}
	if claimed := strings.TrimSpace(message.Source); claimed != "" &&
		!strings.EqualFold(claimed, transport) {
		if _, exists := message.Metadata["claimed_source"]; exists || len(message.Metadata) < 64 {
			message.Metadata["claimed_source"] = truncateText(claimed, 64)
		}
	}
	message.Source = transport
	return message
}

func tagInboundAccess(message controller.TextMessage, access Access) controller.TextMessage {
	message = tagInboundMessage(message, access.Transport)
	if message.Metadata == nil {
		message.Metadata = make(map[string]string)
	}
	if principal := strings.TrimSpace(access.Principal); principal != "" &&
		(len(message.Metadata) < 64 || message.Metadata["principal"] != "") {
		message.Metadata["principal"] = truncateText(principal, 96)
	}
	if authentication := strings.TrimSpace(access.Authentication); authentication != "" &&
		(len(message.Metadata) < 64 || message.Metadata["authentication"] != "") {
		message.Metadata["authentication"] = truncateText(authentication, 48)
	}
	return message
}

func (service *Service) pressVirtualKey(
	ctx context.Context,
	request hostos.VirtualKeyRequest,
	source string,
) (hostos.ActionResult, error) {
	result, err := hostos.DefaultExecutor.PressVirtualKey(
		ctx, service.hostConfig().OSActions.VirtualKeys, request,
	)
	if err != nil {
		service.Client.EmitHostEvent("os.virtual-key.audit", source+" denied: "+err.Error())
		return hostos.ActionResult{}, err
	}
	service.Client.EmitHostEvent("os.virtual-key.audit", source+" "+result.Detail)
	return result, nil
}

func (service *Service) powerAction(
	ctx context.Context,
	request hostos.PowerRequest,
	source string,
) (hostos.ActionResult, error) {
	result, err := hostos.DefaultExecutor.Power(
		ctx, service.hostConfig().OSActions.Power, request,
	)
	if err != nil {
		service.Client.EmitHostEvent("os.power.audit", source+" denied: "+err.Error())
		return hostos.ActionResult{}, err
	}
	service.Client.EmitHostEvent("os.power.audit", source+" "+result.Detail)
	return result, nil
}

func Serve(ctx context.Context, listener net.Listener, service *Service) error {
	var wait sync.WaitGroup
	websocketListener := newDispatchListener(listener.Addr())
	websocketServer := &http.Server{
		Handler:           websocketMux(ctx, service),
		ReadHeaderTimeout: 5 * time.Second,
	}
	websocketDone := make(chan error, 1)
	go func() {
		err := websocketServer.Serve(websocketListener)
		if errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) {
			err = nil
		}
		websocketDone <- err
	}()
	defer func() {
		_ = listener.Close()
		_ = websocketListener.Close()
		shutdownContext, cancel := context.WithTimeout(
			context.Background(),
			time.Second,
		)
		_ = websocketServer.Shutdown(shutdownContext)
		cancel()
		wait.Wait()
	}()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
		_ = websocketListener.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			var handedToHTTP atomic.Bool
			defer func() {
				if !handedToHTTP.Load() {
					_ = connection.Close()
				}
			}()
			done := make(chan struct{})
			go func() {
				select {
				case <-ctx.Done():
					if !handedToHTTP.Load() {
						_ = connection.Close()
					}
				case <-done:
				}
			}()
			reader := bufio.NewReaderSize(connection, 4096)
			_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))
			prefix, peekErr := reader.Peek(4)
			_ = connection.SetReadDeadline(time.Time{})
			if peekErr == nil && isHTTPPrefix(string(prefix)) {
				handedToHTTP.Store(true)
				if websocketListener.Dispatch(
					&bufferedConn{Conn: connection, reader: reader},
				) {
					close(done)
					return
				}
				handedToHTTP.Store(false)
				return
			}
			_ = serveStreams(
				ctx,
				reader,
				connection,
				service,
				accessFromAddress(connection.RemoteAddr(), "ipc"),
			)
			close(done)
		}()
	}
}

func accessFromAddress(address net.Addr, transport string) Access {
	result := Access{Remote: true, Transport: transport}
	if address == nil {
		return result
	}
	host, _, err := net.SplitHostPort(address.String())
	if err != nil {
		return result
	}
	host = strings.Trim(host, "[]")
	if parsed := net.ParseIP(host); parsed != nil && parsed.IsLoopback() {
		result.Remote = false
	} else if strings.EqualFold(host, "localhost") {
		result.Remote = false
	}
	return result
}

func accessFromHTTPRequest(request *http.Request, transport string) Access {
	return authenticatedHTTPRequestAccess(request, transport)
}

type stringAddress string

func (address stringAddress) Network() string { return "tcp" }
func (address stringAddress) String() string  { return string(address) }

func isHTTPPrefix(prefix string) bool {
	switch prefix {
	case "GET ", "POST", "PUT ", "HEAD", "OPTI", "PATC", "DELE":
		return true
	default:
		return false
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (connection *bufferedConn) Read(value []byte) (int, error) {
	return connection.reader.Read(value)
}

type dispatchListener struct {
	address     net.Addr
	connections chan net.Conn
	done        chan struct{}
	closeOnce   sync.Once
}

func newDispatchListener(address net.Addr) *dispatchListener {
	return &dispatchListener{
		address: address, connections: make(chan net.Conn), done: make(chan struct{}),
	}
}

func (listener *dispatchListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.done:
		return nil, net.ErrClosed
	}
}

func (listener *dispatchListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.done) })
	return nil
}

func (listener *dispatchListener) Addr() net.Addr { return listener.address }

func (listener *dispatchListener) Dispatch(connection net.Conn) bool {
	select {
	case listener.connections <- connection:
		return true
	case <-listener.done:
		return false
	}
}

type wsNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type wsSubscription struct {
	Topics     []string `json:"topics"`
	Opcodes    []int    `json:"opcodes,omitempty"`
	IntervalMS int      `json:"interval_ms,omitempty"`
	AfterID    uint64   `json:"after_id,omitempty"`
}

func websocketMux(serverContext context.Context, service *Service) http.Handler {
	webSocketPath := service.currentWebSocketPath()
	socketIOPath := service.currentSocketIOPath()
	mux := http.NewServeMux()
	mux.HandleFunc(webSocketPath, func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodPost {
			serveHTTPRPC(writer, request, service, accessFromHTTPRequest(request, "rest"))
			return
		}
		serveWebSocket(
			serverContext,
			writer,
			request,
			service,
			accessFromHTTPRequest(request, "websocket"),
		)
	})
	mux.HandleFunc("/api/ui-config", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		config := service.hostConfig()
		settings := service.browserUISettings()
		writeHTTPJSON(writer, http.StatusOK, map[string]any{
			"name":                settings.AppTitle,
			"tagline":             settings.Tagline,
			"host_version":        strings.TrimSpace(service.HostVersion),
			"source_hash":         strings.TrimSpace(service.HostSourceHash),
			"build_time":          strings.TrimSpace(service.HostBuildTime),
			"setup_complete":      config.UI.SetupComplete,
			"welcome_melody":      config.UI.WelcomeMelody,
			"appearance":          settings.Appearance,
			"appearance_etag":     settings.AppearanceETag,
			"websocket_path":      webSocketPath,
			"socket_io_path":      socketIOPath,
			"session_ticket_path": SessionTicketPath,
			"auth_required":       !service.authorizationDisabled() && strings.TrimSpace(service.currentAuthToken()) != "",
			"integrations": map[string]bool{
				"local_device":          config.Integrations.LocalDevice.Enabled,
				"data_hub":              config.Integrations.DataHub.Enabled,
				"buzzer_host_enabled":   config.Integrations.BuzzerMirror.Enabled,
				"buzzer_native_enabled": config.Integrations.BuzzerMirror.Enabled && config.Integrations.BuzzerMirror.NativeEnabled,
				"buzzer_web_audio":      config.Integrations.BuzzerMirror.Enabled && config.Integrations.BuzzerMirror.WebAudioEnabled,
			},
		})
	})
	mux.HandleFunc(SessionTicketPath, func(writer http.ResponseWriter, request *http.Request) {
		serveSessionTicket(writer, request, service)
	})
	mux.HandleFunc("/api/rpc", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		serveHTTPRPC(writer, request, service, accessFromHTTPRequest(request, "rest"))
	})
	mux.HandleFunc("/api/snapshot", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.Client.Snapshot())
	})
	mux.HandleFunc("/api/peripherals", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodGet {
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.peripheralSettings())
			return
		}
		if request.Method != http.MethodPut {
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
			return
		}
		var params struct {
			PeripheralNames map[string]string `json:"peripheral_names"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil || params.PeripheralNames == nil {
			if err == nil {
				err = errors.New("peripheral_names is required")
			}
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := service.setPeripheralNames(params.PeripheralNames); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.peripheralSettings())
	})
	mux.HandleFunc("/api/pwm", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodGet {
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			values, err := service.Client.PWMValues(request.Context())
			if err != nil {
				writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, values)
			return
		}
		if request.Method != http.MethodPut && request.Method != http.MethodDelete {
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		if request.Method == http.MethodPut {
			var params struct {
				Channel int `json:"channel"`
				Value   int `json:"value"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if params.Channel < 0 || params.Channel > 15 || params.Value < 0 || params.Value > 4095 {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "channel must be 0..15 and value must be 0..4095"})
				return
			}
			if err := service.Client.SetPWMChannel(request.Context(), byte(params.Channel), uint16(params.Value)); err != nil {
				writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
		} else if err := service.Client.AllPWMOff(request.Context()); err != nil {
			writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		values, err := service.Client.PWMValues(request.Context())
		if err != nil {
			writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, values)
	})
	mux.HandleFunc("/api/commands", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.Client.CommandCatalog())
	})
	mux.HandleFunc("/api/program-state", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodGet {
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.ProgramState())
			return
		}
		if request.Method != http.MethodPut && request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		var params struct {
			Owner  string `json:"owner,omitempty"`
			Mode   string `json:"mode"`
			Reason string `json:"reason,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		params.Owner = strings.TrimSpace(params.Owner)
		if params.Owner == "" {
			params.Owner = "rest"
		}
		mode, err := parseProgramMode(params.Mode)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		state, err := service.Client.SetProgramState(params.Owner, mode, params.Reason)
		if err != nil {
			writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, state)
	})
	mux.HandleFunc("/api/menu/catalog", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		catalog, err := service.Client.MenuCatalog(request.Context())
		if err != nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, catalog)
	})
	mux.HandleFunc("/api/menu/layout", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodGet {
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			layout, err := service.Client.MenuLayout(request.Context())
			if err != nil {
				writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, layout)
			return
		}
		if request.Method != http.MethodPut && request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		var params struct {
			VisibleMask uint16 `json:"visible_mask"`
			Order       []byte `json:"order"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		layout, err := service.Client.SetMenuLayout(request.Context(), controller.MenuLayout{
			VisibleMask: params.VisibleMask, Order: params.Order,
		})
		if err != nil {
			writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, layout)
	})
	mux.HandleFunc("/api/host-menus", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method == http.MethodGet {
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.hostConfig().HostMenus)
			return
		}
		if request.Method != http.MethodPut && request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
			return
		}
		if service.UpdateHostConfig == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "persistent host configuration is unavailable"})
			return
		}
		var config appconfig.HostMenuConfig
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&config); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		err := service.UpdateHostConfig(func(value *appconfig.Config) error {
			value.HostMenus = config
			return nil
		})
		if err != nil {
			writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.hostConfig().HostMenus)
	})
	mux.HandleFunc("/api/os/status", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		status, err := hostos.Status(ports.EnumerationSource())
		if err != nil {
			writeHTTPJSON(writer, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, status)
	})
	mux.HandleFunc("/api/os/facts", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		query := request.URL.Query()
		for key, values := range query {
			if (key != "profile" && key != "access_token") || len(values) != 1 {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
					"error": "only one profile query parameter is accepted",
				})
				return
			}
		}
		profile := strings.TrimSpace(query.Get("profile"))
		if strings.EqualFold(profile, "list") || strings.EqualFold(profile, "catalog") {
			writeHTTPJSON(writer, http.StatusOK, hostfacts.Catalog())
			return
		}
		result, err := service.hostFacts().Query(request.Context(), profile)
		if err != nil {
			status := http.StatusServiceUnavailable
			if strings.Contains(err.Error(), "unknown host-facts profile") {
				status = http.StatusBadRequest
			}
			writeHTTPJSON(writer, status, map[string]string{"error": err.Error()})
			return
		}
		writer.Header().Set("Cache-Control", "private, max-age=5")
		writeHTTPJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("/api/os/key", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityVirtualKeys) {
			return
		}
		var params hostos.VirtualKeyRequest
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := service.pressVirtualKey(request.Context(), params, "rest")
		if err != nil {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, result)
	})
	mux.HandleFunc("/api/os/power", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityPowerActions) {
			return
		}
		var params hostos.PowerRequest
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		params.Automation = false
		result, err := service.powerAction(request.Context(), params, "rest")
		if err != nil {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, result)
	})
	mux.HandleFunc("/api/command", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var params struct {
			Command string `json:"command"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		access := accessFromHTTPRequest(request, "rest")
		if err := service.authorizeCapability(
			access,
			"REST "+request.URL.Path,
			commandCapability(params.Command),
		); err != nil {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		}
		output, err := service.Client.Execute(request.Context(), params.Command)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
				"error": err.Error(), "output": output,
			})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, map[string]string{"output": output})
	})
	mux.HandleFunc("/api/messages", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityMessages) {
			return
		}
		var message controller.TextMessage
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&message); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		message = tagInboundAccess(message, accessFromHTTPRequest(request, "rest"))
		event, err := service.Client.SendTextMessage(request.Context(), message)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, event)
	})
	mux.HandleFunc("/api/macros", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		capability := capabilityRead
		if request.Method != http.MethodGet {
			capability = capabilityHostConfig
		}
		if !authorizeHTTPCapability(writer, request, service, capability) {
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		switch request.Method {
		case http.MethodGet:
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		case http.MethodPost:
			var params struct {
				ID       *int   `json:"id"`
				Name     string `json:"name"`
				Category string `json:"category,omitempty"`
				Color    string `json:"color,omitempty"`
			}
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if params.ID == nil || *params.ID < 0 || *params.ID > 255 {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "macro id is required and must be 0..255"})
				return
			}
			if _, err := service.Client.MacroCreate(byte(*params.ID), params.Name, params.Category, params.Color); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusCreated, service.Client.MacroSnapshot())
		case http.MethodPut, http.MethodPatch:
			var params struct {
				Reference string  `json:"reference"`
				Name      string  `json:"name"`
				Category  *string `json:"category,omitempty"`
				Color     *string `json:"color,omitempty"`
			}
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if _, err := service.Client.MacroUpdate(params.Reference, params.Name, params.Category, params.Color); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		case http.MethodDelete:
			var params struct {
				Reference string `json:"reference"`
			}
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := service.Client.MacroDelete(params.Reference); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		default:
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost+", "+http.MethodPut+", "+http.MethodPatch+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/macros/recording", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) ||
			!authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		switch request.Method {
		case http.MethodPost:
			var params struct {
				Name     string `json:"name"`
				Category string `json:"category,omitempty"`
				Color    string `json:"color,omitempty"`
			}
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if _, err := service.Client.MacroRecordStart(params.Name, params.Category, params.Color); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusAccepted, service.Client.MacroSnapshot())
		case http.MethodDelete:
			var params struct {
				Save *bool `json:"save,omitempty"`
			}
			if err := decoder.Decode(&params); err != nil && !errors.Is(err, io.EOF) {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			save := true
			if params.Save != nil {
				save = *params.Save
			}
			if _, err := service.Client.MacroRecordStop(save); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		default:
			writer.Header().Set("Allow", http.MethodPost+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/macros/board-recording", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) ||
			!authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		switch request.Method {
		case http.MethodPost:
			var params struct {
				ID *int `json:"id"`
			}
			decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if params.ID == nil || *params.ID < 0 || *params.ID > 255 {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "macro capture id is required and must be 0..255"})
				return
			}
			if _, err := service.Client.MacroBoardRecordStart(request.Context(), byte(*params.ID)); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusAccepted, service.Client.MacroSnapshot())
		case http.MethodDelete:
			if _, err := service.Client.MacroBoardRecordStop(request.Context()); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		default:
			writer.Header().Set("Allow", http.MethodPost+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/macros/board-recording/clear", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		var params struct {
			Force bool `json:"force,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil && !errors.Is(err, io.EOF) {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if _, err := service.Client.MacroBoardRecordClear(request.Context(), params.Force); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
	})
	mux.HandleFunc("/api/macros/playback", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) ||
			!authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		switch request.Method {
		case http.MethodPost:
			var params struct {
				Reference string `json:"reference"`
			}
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if _, err := service.Client.MacroPlay(request.Context(), params.Reference); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusAccepted, service.Client.MacroSnapshot())
		case http.MethodDelete:
			var params struct {
				KeepOutputs bool `json:"keep_outputs,omitempty"`
			}
			if err := decoder.Decode(&params); err != nil && !errors.Is(err, io.EOF) {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			if err := service.Client.MacroCancel(request.Context(), params.KeepOutputs); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.Client.MacroSnapshot())
		default:
			writer.Header().Set("Allow", http.MethodPost+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/display", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		var params controller.DisplayRequest
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := service.Client.PresentDisplay(request.Context(), params)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("/api/opcode", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBoard) {
			return
		}
		var params opcodeExchangeParams
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		opcode, payload, expected, err := params.values()
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		result, err := service.Client.ExchangeOpcode(request.Context(), opcode, payload, expected)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, result)
	})
	mux.HandleFunc("/api/app/instances", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if service.AppInstances == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "app instance registry is unavailable"})
			return
		}
		switch request.Method {
		case http.MethodGet:
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			if id := strings.TrimSpace(request.URL.Query().Get("id")); id != "" {
				instance, ok := service.AppInstances.Get(id)
				if !ok {
					writeHTTPJSON(writer, http.StatusNotFound, map[string]string{"error": "app instance is not registered"})
					return
				}
				writeHTTPJSON(writer, http.StatusOK, instance)
				return
			}
			writeHTTPJSON(writer, http.StatusOK, service.AppInstances.List())
		case http.MethodPost:
			if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
				return
			}
			var instance hostui.AppInstance
			decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&instance); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			stored, err := service.AppInstances.Upsert(instance)
			if err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, stored)
		case http.MethodDelete:
			if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
				return
			}
			id := strings.TrimSpace(request.URL.Query().Get("id"))
			if id == "" {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "id query parameter is required"})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, map[string]bool{"removed": service.AppInstances.Remove(id)})
		default:
			writer.Header().Set("Allow", http.MethodGet+", "+http.MethodPost+", "+http.MethodDelete)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/app/bridge", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		if service.AppInstances == nil || strings.TrimSpace(service.CoordinatorInstanceID) == "" {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "coordinator bridge instance is unavailable"})
			return
		}
		instance, ok := service.AppInstances.Get(service.CoordinatorInstanceID)
		if !ok {
			writeHTTPJSON(writer, http.StatusNotFound, map[string]string{"error": "coordinator bridge instance is not registered"})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, instance)
	})
	mux.HandleFunc("/api/app/navigate", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
			return
		}
		if service.AppAction == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "primary app navigation routing is unavailable"})
			return
		}
		var params struct {
			Page   string `json:"page"`
			Target string `json:"target,omitempty"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		target := strings.TrimSpace(params.Target)
		if target == "" {
			target = "*"
		}
		if err := service.AppAction(hostui.AppAction{Kind: "app.page", Value: params.Page, Source: "rest", Target: target}); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("/api/app/action", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			writer.Header().Set("Allow", http.MethodPost)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityHostConfig) {
			return
		}
		if service.AppAction == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "primary app action routing is unavailable"})
			return
		}
		var action hostui.AppAction
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&action); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		action.Source = "rest"
		if err := service.AppAction(action); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, map[string]bool{"accepted": true})
	})
	mux.HandleFunc("/api/bridges", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		if service.BridgeList == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{
				"error": "host bridge manager is unavailable",
			})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, service.BridgeList())
	})
	mux.HandleFunc("/api/bridges/call", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodPost {
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityBridgeCalls) {
			return
		}
		if service.BridgeCall == nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{
				"error": "host bridge manager is unavailable",
			})
			return
		}
		var params struct {
			Peer    string  `json:"peer"`
			Request Request `json:"request"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
		if err := decoder.Decode(&params); err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if strings.TrimSpace(params.Peer) == "" || strings.TrimSpace(params.Request.Method) == "" ||
			params.Request.Method == "controller.bridge.call" {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
				"error": "bridge peer and non-recursive request.method are required",
			})
			return
		}
		params.Request.Auth = ""
		response, err := service.BridgeCall(request.Context(), params.Peer, params.Request)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, map[string]any{
			"peer": params.Peer, "response": response,
		})
	})
	registerWebhookAdminHTTP(mux, service)
	mux.HandleFunc("/api/webhooks/inbound", func(writer http.ResponseWriter, request *http.Request) {
		if !service.inboundWebhooksEnabled() {
			http.NotFound(writer, request)
			return
		}
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityMessages) {
			return
		}
		serveInboundWebhook(writer, request, service)
	})
	mux.HandleFunc(socketIOPath, func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		serveSocketIO(
			serverContext,
			writer,
			request,
			service,
			accessFromHTTPRequest(request, "socket_io"),
		)
	})
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeHTTPJSON(writer, http.StatusOK, map[string]any{
			"ok": true,
			"service": productidentity.ServiceName(
				service.hostConfig().UI.AppTitle,
				"IPC",
			),
		})
	})
	if service.IntegrationProxy != nil {
		mux.Handle("/api/integrations/", http.HandlerFunc(func(
			writer http.ResponseWriter,
			request *http.Request,
		) {
			if !authorizeHTTPRequest(writer, request, service) {
				return
			}
			if !authorizeHTTPCapability(
				writer,
				request,
				service,
				capabilityIntegrations,
			) {
				return
			}
			service.IntegrationProxy.ServeHTTP(writer, request)
		}))
	}
	registerArtifactHTTP(mux, service)
	registerReleaseDiscoveryHTTP(mux, service)
	if service.WebUI != nil && webSocketPath != "/" && socketIOPath != "/" {
		mux.Handle("/", service.WebUI)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/v1" || strings.HasPrefix(request.URL.Path, "/api/v1/") {
			http.NotFound(writer, request)
			return
		}
		if serveBrowserCORS(writer, request, service, webSocketPath) {
			return
		}
		mux.ServeHTTP(writer, request)
	})
}

func registerWebhookAdminHTTP(mux *http.ServeMux, service *Service) {
	mux.HandleFunc("/api/webhooks/outbound/status", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
			return
		}
		admin, err := service.webhookAdminService()
		if err != nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		status, err := admin.WebhookStatus(request.Context())
		if err != nil {
			writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusOK, status)
	})

	list := func(dead bool) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if !authorizeHTTPRequest(writer, request, service) {
				return
			}
			if request.Method != http.MethodGet {
				writer.Header().Set("Allow", http.MethodGet)
				http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !authorizeHTTPCapability(writer, request, service, capabilityRead) {
				return
			}
			limit := 0
			if raw := strings.TrimSpace(request.URL.Query().Get("limit")); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil {
					writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "delivery limit must be 1..100"})
					return
				}
				limit = parsed
			}
			limit, err := normalizeWebhookListLimit(limit)
			if err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			admin, err := service.webhookAdminService()
			if err != nil {
				writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
			var result WebhookDeliveryList
			if dead {
				result, err = admin.WebhookDead(request.Context(), limit)
			} else {
				result, err = admin.WebhookPending(request.Context(), limit)
			}
			if err != nil {
				writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, result)
		}
	}
	mux.HandleFunc("/api/webhooks/outbound/pending", list(false))
	mux.HandleFunc("/api/webhooks/outbound/dead", list(true))

	mutate := func(replay bool) http.HandlerFunc {
		return func(writer http.ResponseWriter, request *http.Request) {
			if !authorizeHTTPRequest(writer, request, service) {
				return
			}
			if request.Method != http.MethodPost {
				writer.Header().Set("Allow", http.MethodPost)
				http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if !authorizeHTTPCapability(writer, request, service, capabilityIntegrations) {
				return
			}
			var params webhookSelectorParams
			decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			selector, err := normalizeWebhookSelector(params)
			if err != nil {
				writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			admin, err := service.webhookAdminService()
			if err != nil {
				writeHTTPJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
				return
			}
			var result any
			if replay {
				result, err = admin.WebhookReplay(request.Context(), selector)
			} else {
				result, err = admin.WebhookClearDead(request.Context(), selector)
			}
			if err != nil {
				writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": err.Error()})
				return
			}
			writeHTTPJSON(writer, http.StatusOK, result)
		}
	}
	mux.HandleFunc("/api/webhooks/outbound/replay", mutate(true))
	mux.HandleFunc("/api/webhooks/outbound/clear", mutate(false))
}

func serveInboundWebhook(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
) {
	allowed := "GET, POST, PUT, PATCH, DELETE, OPTIONS"
	writer.Header().Set("Allow", allowed)
	if request.Method == http.MethodOptions {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	switch request.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete:
	default:
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, maxMessage))
	if err != nil {
		writeHTTPJSON(writer, http.StatusRequestEntityTooLarge, map[string]string{"error": err.Error()})
		return
	}
	message := controller.TextMessage{
		Source: "webhook", Target: "host",
		Type: "http." + strings.ToLower(request.Method),
		Text: truncateText(strings.TrimSpace(string(body)), 4096), Metadata: make(map[string]string),
	}
	if len(body) != 0 && strings.Contains(
		strings.ToLower(request.Header.Get("Content-Type")), "application/json",
	) {
		var typed controller.TextMessage
		if json.Unmarshal(body, &typed) == nil &&
			(typed.Text != "" || typed.Line1 != "" || typed.Line2 != "") {
			message = typed
			message.Metadata = sanitizeInboundWebhookMetadata(message.Metadata, 46)
		}
	}
	if message.Metadata == nil {
		message.Metadata = make(map[string]string)
	}
	query := request.URL.Query()
	for _, field := range []struct {
		name   string
		target *string
	}{
		{"source", &message.Source}, {"target", &message.Target},
		{"type", &message.Type}, {"text", &message.Text},
		{"action", &message.Action}, {"line1", &message.Line1},
		{"line2", &message.Line2},
	} {
		if value := query.Get(field.name); value != "" {
			*field.target = value
		}
	}
	message.Metadata["http.method"] = request.Method
	message.Metadata["http.path"] = request.URL.Path
	appendInboundWebhookQueryMetadata(message.Metadata, query, 48)
	appendInboundWebhookHeaderMetadata(message.Metadata, request.Header, 64)
	if message.Text == "" {
		// RequestURI includes the raw query string and can therefore contain
		// credentials. The routed path is sufficient provenance.
		message.Text = request.Method + " " + request.URL.Path
	}
	if strings.TrimSpace(message.Source) == "" {
		message.Source = "webhook"
	}
	if strings.TrimSpace(message.Target) == "" {
		message.Target = "host"
	}
	if strings.TrimSpace(message.Type) == "" {
		message.Type = "http." + strings.ToLower(request.Method)
	}
	message = tagInboundAccess(message, accessFromHTTPRequest(request, "webhook"))
	event, err := service.Client.SendTextMessage(request.Context(), message)
	if err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeHTTPJSON(writer, http.StatusAccepted, event)
}

func truncateText(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

func serveHTTPRPC(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	access Access,
) {
	if request.Method != http.MethodPost {
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || !strings.EqualFold(mediaType, "application/json") {
		writeHTTPJSON(writer, http.StatusUnsupportedMediaType, map[string]string{
			"error": "JSON-RPC requires Content-Type: application/json",
		})
		return
	}
	var rpcRequest Request
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
	if err := decoder.Decode(&rpcRequest); err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, Response{
			JSONRPC: Version,
			Error:   &RPCError{Code: -32700, Message: "parse error: " + err.Error()},
		})
		return
	}
	// The HTTP layer has already authenticated the request. Never copy its
	// durable credential into the decoded application envelope.
	rpcRequest.Auth = ""
	response := service.dispatch(request.Context(), rpcRequest, access)
	status := http.StatusOK
	if response.Error != nil {
		status = http.StatusBadRequest
	}
	writeHTTPJSON(writer, status, response)
}

func writeHTTPJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func serveWebSocket(
	serverContext context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	access Access,
) {
	origins := service.currentAllowedOrigins()
	if len(origins) == 0 {
		origins = []string{"localhost:*", "127.0.0.1:*", "[::1]:*"}
	}
	connection, err := websocket.Accept(
		writer,
		request,
		&websocket.AcceptOptions{
			OriginPatterns: origins,
			Subprotocols:   []string{browserWebSocketProtocol},
		},
	)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(maxMessage)
	// websocket.Accept hijacks the HTTP connection, so request.Context must not
	// be used afterward. The server-owned context keeps shutdown deterministic.
	ctx, cancel := context.WithCancel(serverContext)
	defer cancel()
	var writeMu sync.Mutex
	writeJSON := func(value any) error {
		encoded, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			return encodeErr
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return connection.Write(writeContext, websocket.MessageText, encoded)
	}
	var stopSubscription context.CancelFunc
	defer func() {
		if stopSubscription != nil {
			stopSubscription()
		}
	}()

	for {
		messageType, data, readErr := connection.Read(ctx)
		if readErr != nil {
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		var rpcRequest Request
		if err := json.Unmarshal(data, &rpcRequest); err != nil {
			_ = writeJSON(Response{
				JSONRPC: Version,
				Error:   &RPCError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}
		rpcRequest.Auth = ""
		if rpcRequest.Method == "controller.subscribe" {
			var subscription wsSubscription
			response := Response{JSONRPC: Version, ID: rpcRequest.ID}
			if err := service.authorizeCapability(
				access, "controller.subscribe", capabilityEvents,
			); err != nil {
				response.Error = &RPCError{Code: -32003, Message: err.Error()}
			} else if err := decodeParams(rpcRequest.Params, &subscription); err != nil {
				response.Error = &RPCError{Code: -32602, Message: err.Error()}
			} else if normalized, err := normalizeSubscription(subscription); err != nil {
				response.Error = &RPCError{Code: -32602, Message: err.Error()}
			} else {
				if stopSubscription != nil {
					stopSubscription()
				}
				subscriptionContext, cancel := context.WithCancel(ctx)
				stopSubscription = cancel
				response.Result = map[string]any{
					"subscribed":  true,
					"topics":      normalized.Topics,
					"opcodes":     normalized.Opcodes,
					"interval_ms": normalized.IntervalMS,
					"latest_id":   service.Client.LatestEventID(),
					"principal":   access.Principal,
				}
				startWebSocketSubscription(
					subscriptionContext,
					service.Client,
					normalized,
					writeJSON,
				)
			}
			if len(rpcRequest.ID) != 0 {
				if err := writeJSON(response); err != nil {
					return
				}
			}
			continue
		}
		if rpcRequest.Method == "controller.unsubscribe" {
			if stopSubscription != nil {
				stopSubscription()
				stopSubscription = nil
			}
			if len(rpcRequest.ID) != 0 {
				if err := writeJSON(Response{
					JSONRPC: Version, ID: rpcRequest.ID,
					Result: map[string]bool{"subscribed": false},
				}); err != nil {
					return
				}
			}
			continue
		}
		response := service.dispatch(ctx, rpcRequest, access)
		if len(rpcRequest.ID) != 0 {
			if err := writeJSON(response); err != nil {
				return
			}
		}
	}
}

// serveSocketIO implements a bounded, genuine Engine.IO v4 / Socket.IO v4
// WebSocket transport. It intentionally does not claim long-polling, rooms,
// namespaces, or binary attachment support. The supported events are
// subscribe, unsubscribe, message, command, and rpc.
func serveSocketIO(
	serverContext context.Context,
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	access Access,
) {
	query := request.URL.Query()
	if query.Get("EIO") != "4" || query.Get("transport") != "websocket" {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{
			"error": "this adapter requires EIO=4&transport=websocket",
		})
		return
	}
	origins := service.currentAllowedOrigins()
	if len(origins) == 0 {
		origins = []string{"localhost:*", "127.0.0.1:*", "[::1]:*"}
	}
	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		OriginPatterns: origins,
		Subprotocols:   []string{browserWebSocketProtocol},
	})
	if err != nil {
		return
	}
	defer connection.CloseNow()
	connection.SetReadLimit(maxMessage)
	// websocket.Accept hijacks the HTTP connection, so request.Context must not
	// be used afterward. The server-owned context keeps shutdown deterministic.
	ctx, cancel := context.WithCancel(serverContext)
	defer cancel()

	var writeMu sync.Mutex
	writePacket := func(packet string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeContext, stop := context.WithTimeout(ctx, 10*time.Second)
		defer stop()
		return connection.Write(writeContext, websocket.MessageText, []byte(packet))
	}
	writeEvent := func(name string, payload any) error {
		encoded, encodeErr := json.Marshal([]any{name, payload})
		if encodeErr != nil {
			return encodeErr
		}
		return writePacket("42" + string(encoded))
	}

	sidBytes := make([]byte, 12)
	if _, err := rand.Read(sidBytes); err != nil {
		return
	}
	sid := hex.EncodeToString(sidBytes)
	opened, _ := json.Marshal(map[string]any{
		"sid": sid, "upgrades": []string{},
		"pingInterval": 25000, "pingTimeout": 20000,
		"maxPayload": maxMessage,
	})
	if err := writePacket("0" + string(opened)); err != nil {
		return
	}

	var stopSubscription context.CancelFunc
	defer func() {
		if stopSubscription != nil {
			stopSubscription()
		}
	}()
	go func() {
		ticker := time.NewTicker(25 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if writePacket("2") != nil {
					cancel()
					return
				}
			}
		}
	}()

	for ctx.Err() == nil {
		messageType, payload, readErr := connection.Read(ctx)
		if readErr != nil {
			return
		}
		if messageType != websocket.MessageText {
			continue
		}
		packet := string(payload)
		switch {
		case packet == "2":
			if writePacket("3") != nil {
				return
			}
		case packet == "3":
			// Pong for the server heartbeat.
		case strings.HasPrefix(packet, "40"):
			if writePacket(`40{"sid":"`+sid+`"}`) != nil {
				return
			}
		case strings.HasPrefix(packet, "41"):
			_ = connection.Close(websocket.StatusNormalClosure, "Socket.IO disconnect")
			return
		case strings.HasPrefix(packet, "42"):
			name, raw, decodeErr := decodeSocketIOEvent(packet[2:])
			if decodeErr != nil {
				_ = writeEvent("error", map[string]string{"error": decodeErr.Error()})
				continue
			}
			switch name {
			case "subscribe":
				if err := service.authorizeCapability(
					access, "controller.subscribe", capabilityEvents,
				); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				var subscription wsSubscription
				if err := json.Unmarshal(raw, &subscription); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				normalized, err := normalizeSubscription(subscription)
				if err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				if stopSubscription != nil {
					stopSubscription()
				}
				subscriptionContext, stop := context.WithCancel(ctx)
				stopSubscription = stop
				startWebSocketSubscription(
					subscriptionContext,
					service.Client,
					normalized,
					func(value any) error {
						notification, ok := value.(wsNotification)
						if !ok {
							return writeEvent("controller.data", value)
						}
						return writeEvent(notification.Method, notification.Params)
					},
				)
				_ = writeEvent("subscribed", map[string]any{
					"topics": normalized.Topics, "opcodes": normalized.Opcodes,
					"interval_ms": normalized.IntervalMS,
					"latest_id":   service.Client.LatestEventID(),
					"principal":   access.Principal,
				})
			case "unsubscribe":
				if stopSubscription != nil {
					stopSubscription()
					stopSubscription = nil
				}
				_ = writeEvent("unsubscribed", map[string]bool{"subscribed": false})
			case "message":
				if err := service.authorizeCapability(
					access, "controller.message.send", capabilityMessages,
				); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				var message controller.TextMessage
				if err := json.Unmarshal(raw, &message); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				message = tagInboundAccess(message, access)
				event, err := service.Client.SendTextMessage(ctx, message)
				if err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
				} else {
					_ = writeEvent("message.accepted", event)
				}
			case "command":
				var params struct {
					Command string `json:"command"`
				}
				if err := json.Unmarshal(raw, &params); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				encoded, _ := json.Marshal(params)
				response := service.dispatch(ctx, Request{
					JSONRPC: Version, Method: "controller.command.execute",
					Params: encoded,
				}, access)
				_ = writeEvent("command.response", response)
			case "rpc":
				var rpcRequest Request
				if err := json.Unmarshal(raw, &rpcRequest); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				rpcRequest.Auth = ""
				_ = writeEvent("rpc.response", service.dispatch(ctx, rpcRequest, access))
			default:
				_ = writeEvent("error", map[string]string{
					"error": "unsupported Socket.IO event " + name,
				})
			}
		}
	}
}

func decodeSocketIOEvent(payload string) (string, json.RawMessage, error) {
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(payload), &values); err != nil {
		return "", nil, fmt.Errorf("decode Socket.IO event: %w", err)
	}
	if len(values) != 2 {
		return "", nil, errors.New("Socket.IO event must contain [name,payload]")
	}
	var name string
	if err := json.Unmarshal(values[0], &name); err != nil || strings.TrimSpace(name) == "" {
		return "", nil, errors.New("Socket.IO event name is required")
	}
	return name, values[1], nil
}

func (service *Service) currentAuthToken() string {
	if service.HostConfig != nil {
		return service.HostConfig().IPC.AuthToken
	}
	return service.AuthToken
}

func (service *Service) authorizationDisabled() bool {
	return service != nil && service.AuthorizationDisabled
}

func (service *Service) currentAllowedOrigins() []string {
	if service.HostConfig != nil {
		return append([]string(nil), service.HostConfig().IPC.AllowedOrigins...)
	}
	return append([]string(nil), service.AllowedOrigins...)
}

func (service *Service) inboundWebhooksEnabled() bool {
	if service.HostConfig != nil {
		return service.HostConfig().Integrations.InboundWebhooksEnabled
	}
	return service.InboundWebhooks
}

func authorizeHTTPRequest(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
) bool {
	return service.authorizeHTTPRequest(writer, request)
}

// httpOriginAllowed prevents a browser on an unrelated site from driving the
// loopback control plane. Missing-Origin policy is explicit in the caller:
// loopback native clients are allowed, while remote native clients must supply
// a durable header credential and browser tickets remain Origin-bound.
func httpOriginAllowed(request *http.Request, allowedPatterns []string) bool {
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	originHost := strings.ToLower(parsed.Host)
	if strings.EqualFold(originHost, strings.TrimSpace(request.Host)) {
		return true
	}
	if len(allowedPatterns) == 0 {
		allowedPatterns = []string{"localhost:*", "127.0.0.1:*", "[::1]:*"}
	}
	for _, pattern := range allowedPatterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "*" || pattern == "*:*" {
			continue
		}
		if strings.HasSuffix(pattern, ":*") {
			hostPattern := strings.TrimSuffix(pattern, ":*")
			originName := strings.ToLower(parsed.Hostname())
			if strings.EqualFold(hostPattern, originName) ||
				strings.EqualFold(hostPattern, "["+originName+"]") {
				return true
			}
			continue
		}
		if match, matchErr := path.Match(pattern, originHost); matchErr == nil && match {
			return true
		}
	}
	return false
}

var browserCORSRequestHeaders = map[string]bool{
	"accept":               true,
	"authorization":        true,
	"content-type":         true,
	"if-match":             true,
	"if-none-match":        true,
	"if-range":             true,
	"range":                true,
	"x-content-sha256":     true,
	"x-pccontroller-token": true,
	"x-requested-with":     true,
}

// serveBrowserCORS adds a browser-readable response only for an origin already
// accepted by the controller's non-wildcard origin policy. It handles bounded
// preflight requests without authenticating them; the subsequent request still
// requires the ordinary bearer token and capability checks.
func serveBrowserCORS(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	webSocketPath string,
) bool {
	if request == nil || request.URL == nil {
		return false
	}
	origin := strings.TrimSpace(request.Header.Get("Origin"))
	if origin == "" || !corsControlPath(request.URL.Path, webSocketPath) {
		return false
	}
	if !httpOriginAllowed(request, service.currentAllowedOrigins()) {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
			"error": "request origin is not allowed",
		})
		return true
	}

	header := writer.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	header.Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Disposition, Content-Length, Content-Range, ETag, X-PCController-Authentication, X-PCController-Principal")
	header.Add("Vary", "Origin")
	if request.Method != http.MethodOptions {
		return false
	}

	method := strings.ToUpper(strings.TrimSpace(request.Header.Get("Access-Control-Request-Method")))
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete:
	default:
		writer.Header().Set("Allow", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
		writeHTTPJSON(writer, http.StatusMethodNotAllowed, map[string]string{
			"error": "CORS request method is not allowed",
		})
		return true
	}
	requestedHeaders := request.Header.Values("Access-Control-Request-Headers")
	for _, line := range requestedHeaders {
		for _, value := range strings.Split(line, ",") {
			name := strings.ToLower(strings.TrimSpace(value))
			if name != "" && !browserCORSRequestHeaders[name] {
				writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
					"error": "CORS request header is not allowed",
				})
				return true
			}
		}
	}
	header.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, If-Match, If-None-Match, If-Range, Range, X-Content-SHA256, X-PCController-Token, X-Requested-With")
	header.Set("Access-Control-Max-Age", "600")
	header.Add("Vary", "Access-Control-Request-Method")
	header.Add("Vary", "Access-Control-Request-Headers")
	writer.WriteHeader(http.StatusNoContent)
	return true
}

func corsControlPath(requestPath, webSocketPath string) bool {
	return requestPath == "/api" || strings.HasPrefix(requestPath, "/api/") ||
		requestPath == webSocketPath
}

func authorizeHTTPCapability(
	writer http.ResponseWriter,
	request *http.Request,
	service *Service,
	capability string,
) bool {
	access := accessFromHTTPRequest(request, "rest")
	if err := service.authorizeCapability(
		access,
		request.Method+" "+request.URL.Path,
		capability,
	); err != nil {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func normalizeSubscription(value wsSubscription) (wsSubscription, error) {
	if len(value.Topics) == 0 {
		if len(value.Opcodes) != 0 {
			value.Topics = []string{"opcodes"}
		} else {
			value.Topics = []string{"events"}
		}
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(value.Topics))
	for _, topic := range value.Topics {
		topic = strings.ToLower(strings.TrimSpace(topic))
		if topic == "telemetry" {
			topic = "status"
		}
		if topic == "opcode" {
			topic = "opcodes"
		}
		if topic != "events" && topic != "state" && topic != "debug" &&
			topic != "status" && topic != "opcodes" {
			return wsSubscription{}, fmt.Errorf("unknown subscription topic %q", topic)
		}
		if !seen[topic] {
			seen[topic] = true
			result = append(result, topic)
		}
	}
	value.Topics = result
	seenOpcodes := make(map[int]bool)
	opcodes := make([]int, 0, len(value.Opcodes))
	for _, opcode := range value.Opcodes {
		if opcode < 1 || opcode > 255 {
			return wsSubscription{}, errors.New("subscription opcodes must be 1..255; 0 is reserved")
		}
		if !seenOpcodes[opcode] {
			seenOpcodes[opcode] = true
			opcodes = append(opcodes, opcode)
		}
	}
	value.Opcodes = opcodes
	if len(value.Opcodes) != 0 && !seen["opcodes"] {
		return wsSubscription{}, errors.New("opcode filters require the opcodes topic")
	}
	if seen["status"] {
		if value.IntervalMS == 0 {
			value.IntervalMS = 200
		}
		if value.IntervalMS < 50 || value.IntervalMS > 60_000 {
			return wsSubscription{}, errors.New("status interval_ms must be 50..60000")
		}
	} else {
		value.IntervalMS = 0
	}
	return value, nil
}

func startWebSocketSubscription(
	ctx context.Context,
	client *controller.Client,
	subscription wsSubscription,
	write func(any) error,
) {
	for _, topic := range subscription.Topics {
		switch topic {
		case "events":
			afterID := subscription.AfterID
			if afterID == 0 {
				// Capture the cursor before acknowledging the subscription. An
				// event published immediately after that acknowledgement must not
				// be skipped while this goroutine is still being scheduled.
				afterID = client.LatestEventID()
			}
			go streamWebSocketEventStream(ctx, client, afterID, "activity", "controller.event", write)
		case "state":
			afterID := subscription.AfterID
			if afterID == 0 {
				afterID = client.LatestEventID()
			}
			go streamWebSocketEventStream(ctx, client, afterID, "state", "controller.state", write)
		case "debug":
			afterID := subscription.AfterID
			if afterID == 0 {
				afterID = client.LatestEventID()
			}
			go streamWebSocketEventStream(ctx, client, afterID, "debug", "controller.debug", write)
		case "opcodes":
			afterID := subscription.AfterID
			if afterID == 0 {
				afterID = client.LatestEventID()
			}
			go streamWebSocketOpcodes(
				ctx, client, afterID, subscription.Opcodes, write,
			)
		case "status":
			go streamWebSocketStatus(
				ctx,
				client,
				time.Duration(subscription.IntervalMS)*time.Millisecond,
				write,
			)
		}
	}
}

func streamWebSocketOpcodes(
	ctx context.Context,
	client *controller.Client,
	afterID uint64,
	opcodes []int,
	write func(any) error,
) {
	allowed := make(map[byte]bool, len(opcodes))
	for _, opcode := range opcodes {
		allowed[byte(opcode)] = true
	}
	for ctx.Err() == nil {
		event, err := client.NextOpcodeEvent(ctx, afterID, "", nil)
		if err != nil {
			return
		}
		afterID = event.ID
		if event.Opcode == 0 {
			continue
		}
		if len(allowed) != 0 && !allowed[event.Opcode] {
			continue
		}
		if err := write(wsNotification{
			JSONRPC: Version,
			Method:  "controller.opcode",
			Params:  event,
		}); err != nil {
			return
		}
	}
}

func streamWebSocketEventStream(
	ctx context.Context,
	client *controller.Client,
	afterID uint64,
	stream string,
	method string,
	write func(any) error,
) {
	for ctx.Err() == nil {
		event, err := client.NextEventStream(ctx, afterID, "", stream)
		if err != nil {
			return
		}
		afterID = event.ID
		if err := write(wsNotification{
			JSONRPC: Version,
			Method:  method,
			Params:  event,
		}); err != nil {
			return
		}
	}
}

// streamableEventKind remains the compatibility classifier for callers that
// have not yet received an explicit stream field.
func streamableEventKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "telemetry", "rx", "tx", "front_panel.segment", "status_led.changed", "buzzer.note":
		return false
	default:
		return true
	}
}

func streamWebSocketStatus(
	ctx context.Context,
	client *controller.Client,
	interval time.Duration,
	write func(any) error,
) {
	updates, err := client.SubscribeStatus(ctx, interval)
	if err != nil {
		_ = write(wsNotification{
			JSONRPC: Version,
			Method:  "controller.error",
			Params:  map[string]string{"source": "status", "error": err.Error()},
		})
		return
	}
	for update := range updates {
		if update.Error != "" {
			if write(wsNotification{
				JSONRPC: Version,
				Method:  "controller.error",
				Params:  map[string]string{"source": "status", "error": update.Error},
			}) != nil {
				return
			}
			continue
		}
		if write(wsNotification{
			JSONRPC: Version,
			Method:  "controller.status",
			Params:  update,
		}) != nil {
			return
		}
	}
}

func ServeStreams(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	service *Service,
) error {
	return serveStreams(ctx, input, output, service, Access{Transport: "ipc"})
}

func serveStreams(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	service *Service,
	access Access,
) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxMessage)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		var request Request
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			if encodeErr := encoder.Encode(Response{
				JSONRPC: Version,
				Error:   &RPCError{Code: -32700, Message: "parse error: " + err.Error()},
			}); encodeErr != nil {
				return encodeErr
			}
			continue
		}
		response := service.dispatch(ctx, request, access)
		// Notifications have no ID and intentionally receive no response.
		if len(request.ID) == 0 {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func Call(
	ctx context.Context,
	address string,
	request Request,
) (Response, error) {
	if address == "" {
		address = DefaultListen
	}
	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return Response{}, err
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	}
	request.JSONRPC = Version
	if len(request.ID) == 0 {
		request.ID = json.RawMessage("1")
	}
	if err := json.NewEncoder(connection).Encode(request); err != nil {
		return Response{}, err
	}
	var response Response
	decoder := json.NewDecoder(io.LimitReader(connection, maxMessage))
	if err := decoder.Decode(&response); err != nil {
		return Response{}, err
	}
	if response.Error != nil {
		return response, response.Error
	}
	return response, nil
}

func Listen(address string) (net.Listener, error) {
	return ListenWithRemote(address, false)
}

// ListenWithRemote permits a non-loopback bind only after the caller has
// validated an explicit allow_remote setting and authentication token.
func ListenWithRemote(address string, allowRemote bool) (net.Listener, error) {
	address, err := validateListenAddress(address, allowRemote)
	if err != nil {
		return nil, err
	}
	return net.Listen("tcp", address)
}

// validateListenAddress is deliberately side-effect free so address-policy
// tests never bind a wildcard socket from Go's randomized test executable.
func validateListenAddress(address string, allowRemote bool) (string, error) {
	if address == "" {
		address = DefaultListen
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return "", err
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && !allowRemote {
		return "", fmt.Errorf(
			"refusing non-loopback IPC address %q; use an SSH tunnel or explicit proxy",
			address,
		)
	}
	return address, nil
}

func WithDefaultTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 15*time.Second)
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || string(raw) == "null" {
		raw = []byte("{}")
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return &RPCError{Code: -32602, Message: "invalid params: " + err.Error()}
	}
	return nil
}

func (service *Service) dispatchWebhookAdmin(
	ctx context.Context,
	method string,
	raw json.RawMessage,
) (any, error) {
	admin, err := service.webhookAdminService()
	if err != nil {
		return nil, err
	}
	switch method {
	case "controller.webhooks.status":
		var params struct{}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		return admin.WebhookStatus(ctx)
	case "controller.webhooks.pending", "controller.webhooks.dead":
		var params struct {
			Limit int `json:"limit,omitempty"`
		}
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		limit, err := normalizeWebhookListLimit(params.Limit)
		if err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		if method == "controller.webhooks.pending" {
			return admin.WebhookPending(ctx, limit)
		}
		return admin.WebhookDead(ctx, limit)
	case "controller.webhooks.replay", "controller.webhooks.clear":
		var params webhookSelectorParams
		if err := decodeParams(raw, &params); err != nil {
			return nil, err
		}
		selector, err := normalizeWebhookSelector(params)
		if err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		if method == "controller.webhooks.replay" {
			return admin.WebhookReplay(ctx, selector)
		}
		return admin.WebhookClearDead(ctx, selector)
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found"}
	}
}

func (service *Service) webhookAdminService() (WebhookAdminService, error) {
	if service.WebhookAdmin == nil {
		return nil, errors.New("outbound webhook delivery service is unavailable")
	}
	admin := service.WebhookAdmin()
	if admin == nil {
		return nil, errors.New("outbound webhook delivery service is unavailable")
	}
	return admin, nil
}

func normalizeWebhookListLimit(limit int) (int, error) {
	if limit == 0 {
		return 25, nil
	}
	if limit < 1 || limit > 100 {
		return 0, errors.New("delivery limit must be 1..100")
	}
	return limit, nil
}

func normalizeWebhookSelector(params webhookSelectorParams) (string, error) {
	deliveryID := strings.TrimSpace(params.DeliveryID)
	if params.All {
		if deliveryID != "" {
			return "", errors.New("delivery_id and all are mutually exclusive")
		}
		if !params.ConfirmAll {
			return "", errors.New("bulk operation requires all=true and confirm_all=true")
		}
		return "all", nil
	}
	if params.ConfirmAll {
		return "", errors.New("confirm_all is valid only when all=true")
	}
	if deliveryID == "" {
		return "", errors.New("delivery_id is required unless all=true")
	}
	if len(deliveryID) > 128 || strings.ContainsAny(deliveryID, "\r\n\t") || strings.EqualFold(deliveryID, "all") {
		return "", errors.New("delivery_id is invalid")
	}
	return deliveryID, nil
}

func parseOptionalTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("since must be RFC3339: %w", err)
	}
	return parsed, nil
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseProgramMode(value string) (controller.ProgramMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "idle":
		return controller.ProgramIdle, nil
	case "running":
		return controller.ProgramRunning, nil
	default:
		return "", errors.New("mode must be idle or running")
	}
}
