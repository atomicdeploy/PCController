// Package ipcjson implements newline-delimited JSON-RPC 2.0 over loopback TCP
// or separate input/output streams. It deliberately avoids platform-specific transports,
// so the same automation works on Windows, Linux, and macOS.
package ipcjson

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
)

const (
	Version       = "2.0"
	APIVersion    = 1
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

func (rpcError *RPCError) Error() string {
	return rpcError.Message
}

// Access records transport provenance for authorization and message tagging.
// Remote means a non-loopback network peer, not merely a WebSocket client.
type Access struct {
	Remote        bool
	Transport     string
	authenticated bool
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
	AuthToken        string
	AllowedOrigins   []string
	InboundWebhooks  bool
	AppAction        func(hostui.AppAction) error
	Shutdown         func()
	HostConfig       func() appconfig.Config
	UpdateHostConfig func(func(*appconfig.Config) error) error
	BridgeList       func() any
	BridgeCall       func(context.Context, string, Request) (Response, error)
	mu               sync.Mutex
}

// LocalDeviceService is the narrow host-owned surface exposed to IPC. Browser
// clients cannot choose an upstream URL or bypass the manager's network and
// payload bounds.
type LocalDeviceService interface {
	Status() any
	Action(context.Context, string, string, int) (any, error)
	Inspect(context.Context, string) (any, error)
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
	return service.dispatch(ctx, request, Access{
		Remote: true, Transport: transport, authenticated: true,
	})
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
	if !access.authenticated && !service.authorized(request.Auth) {
		response.Error = &RPCError{Code: -32001, Message: "authentication required"}
		return response
	}
	if err := service.authorizeAccess(access, request.Method, request.Params); err != nil {
		response.Error = &RPCError{Code: -32003, Message: err.Error()}
		return response
	}
	if request.Method == "controller.event.next" {
		var params struct {
			AfterID   uint64 `json:"after_id"`
			Kind      string `json:"kind,omitempty"`
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
		event, err := service.Client.NextEvent(waitContext, params.AfterID, params.Kind)
		if err != nil {
			response.Error = &RPCError{Code: -32000, Message: err.Error()}
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

	// The shell engine tracks history, and device operations share one serial
	// request stream. Serialize RPC calls while allowing snapshot reads to
	// complete independently in future versions.
	service.mu.Lock()
	defer service.mu.Unlock()

	var result any
	var err error
	switch request.Method {
	case "controller.ping":
		result = map[string]any{
			"ok": true, "jsonrpc": Version, "api_version": APIVersion,
		}
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
			"local_device": config.LocalDevice,
			"data_hub":     config.DataHub,
		}
	case "controller.integrations.local.set":
		var params struct {
			LocalDevice appconfig.LocalDevice `json:"local_device"`
			DataHub     appconfig.DataHub     `json:"data_hub"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.UpdateHostConfig == nil {
				err = errors.New("persistent host configuration is unavailable")
			} else {
				err = service.UpdateHostConfig(func(value *appconfig.Config) error {
					value.Integrations.LocalDevice = params.LocalDevice
					value.Integrations.DataHub = params.DataHub
					return nil
				})
				if err == nil {
					config := service.hostConfig().Integrations
					result = map[string]any{
						"local_device": config.LocalDevice,
						"data_hub":     config.DataHub,
					}
				}
			}
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
	case "controller.execute", "controller.command.execute":
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
				output, err = service.Client.Execute(ctx, command)
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
			TimeoutMS  int64 `json:"timeout_ms,omitempty"`
			Indefinite bool  `json:"indefinite,omitempty"`
			Multiple   bool  `json:"multiple,omitempty"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			result, err = service.Client.StartRFLearning(
				ctx,
				controller.RFLearnOptions{
					Timeout:    time.Duration(params.TimeoutMS) * time.Millisecond,
					Indefinite: params.Indefinite,
					Multiple:   params.Multiple,
				},
			)
		}
	case "controller.rf.learn.status":
		result = service.Client.RFLearningState()
	case "controller.rf.learn.cancel":
		err = service.Client.CancelRFLearn(ctx)
		result = map[string]bool{"cancelled": err == nil}
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
	case "controller.message.send":
		var message controller.TextMessage
		if err = decodeParams(request.Params, &message); err == nil {
			message = tagInboundMessage(message, access.Transport)
			result, err = service.Client.SendTextMessage(ctx, message)
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
			Page string `json:"page"`
		}
		if err = decodeParams(request.Params, &params); err == nil {
			if service.AppAction == nil {
				err = errors.New("primary app page routing is unavailable")
			} else {
				err = service.AppAction(hostui.AppAction{
					Kind: "app.page", Value: params.Page, Source: "ipc",
				})
				result = map[string]bool{"accepted": err == nil}
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

func (service *Service) hostConfig() appconfig.Config {
	if service.HostConfig != nil {
		return service.HostConfig()
	}
	return appconfig.Defaults()
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
	if !access.Remote {
		return nil
	}
	config := service.hostConfig()
	if !config.IPC.AllowRemote {
		service.auditRemote(access, operation, capability, false)
		return errors.New("remote network access is disabled")
	}
	if !remoteCapabilityAllowed(config.IPC.RemotePolicy, capability) {
		service.auditRemote(access, operation, capability, false)
		return fmt.Errorf("remote capability %s is disabled", capability)
	}
	if capability != capabilityRead && capability != capabilityEvents {
		service.auditRemote(access, operation, capability, true)
	}
	return nil
}

func (service *Service) auditRemote(
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
	transport := strings.ToLower(strings.TrimSpace(access.Transport))
	if transport == "" {
		transport = "network"
	}
	service.Client.EmitHostEvent(
		"security.remote."+decision,
		fmt.Sprintf("%s %s capability=%s method=%s", transport, decision, capability, method),
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
	case "controller.connect", "controller.open", "controller.port.open",
		"controller.close", "controller.port.close":
		return capabilityConnection
	case "controller.reset.lines", "controller.reset", "controller.port.reset":
		return capabilityReset
	case "controller.quit", "controller.exit":
		return capabilityShutdown
	case "controller.message.send":
		return capabilityMessages
	case "controller.host_menu.config", "controller.host_menu.config.get",
		"controller.os.policy", "controller.bridge.list":
		return capabilityRead
	case "controller.host_menu.configure", "controller.host_menu.config.set",
		"controller.os.configure", "controller.lcd.presentation.configure",
		"controller.app.page":
		return capabilityHostConfig
	case "controller.os.key", "controller.virtual_key":
		return capabilityVirtualKeys
	case "controller.os.power":
		return capabilityPowerActions
	case "controller.bridge.call":
		return capabilityBridgeCalls
	case "controller.device.status", "controller.device.action",
		"controller.device.inspect", "controller.integrations.local.get",
		"controller.integrations.local.set":
		return capabilityIntegrations
	case "controller.execute", "controller.command.execute":
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
	case "controller.ping", "controller.snapshot", "controller.status",
		"controller.command.catalog", "controller.program_state.get", "controller.program-state.get",
		"controller.temperatures", "controller.menu.list", "controller.menu.current",
		"controller.menu.layout.get", "controller.host_menu.state",
		"controller.rf.list", "controller.rf.presentation",
		"controller.rf.learn.status", "controller.history.status",
		"controller.history.timeline", "controller.lcd.presentation.status",
		"controller.ports", "controller.os.status", "controller.system.status",
		"controller.discovery.scan":
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
		if len(words) >= 2 && (words[1] == "list" || words[1] == "show" || words[1] == "status" ||
			(words[1] == "record" && len(words) >= 3 && words[2] == "status")) {
			return capabilityRead
		}
		if len(words) >= 2 && (words[1] == "create" || words[1] == "delete" || words[1] == "remove" || words[1] == "record") {
			return capabilityHostConfig
		}
		return capabilityBoard
	case "automation":
		if len(words) >= 2 && words[1] == "list" {
			return capabilityRead
		}
		return capabilityAutomations
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
	case "boot", "program", "programmer", "firmware", "flash", "upload",
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
		case "status", "policy":
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
	if request == nil {
		return Access{Remote: true, Transport: transport}
	}
	return accessFromAddress(stringAddress(request.RemoteAddr), transport)
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
	IntervalMS int      `json:"interval_ms,omitempty"`
	AfterID    uint64   `json:"after_id,omitempty"`
}

func websocketMux(serverContext context.Context, service *Service) http.Handler {
	webSocketPath := strings.TrimSpace(service.WebSocketPath)
	if webSocketPath == "" {
		webSocketPath = "/ipc"
	}
	socketIOPath := strings.TrimSpace(service.SocketIOPath)
	if socketIOPath == "" {
		socketIOPath = "/socket.io/"
	}
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
	mux.HandleFunc("/api/v1/ui-config", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := productidentity.Title(service.hostConfig().UI.AppTitle)
		writeHTTPJSON(writer, http.StatusOK, map[string]any{
			"name":           name,
			"api_version":    APIVersion,
			"websocket_path": webSocketPath,
			"socket_io_path": socketIOPath,
			"auth_required":  strings.TrimSpace(service.currentAuthToken()) != "",
			"integrations": map[string]bool{
				"local_device": service.hostConfig().Integrations.LocalDevice.Enabled,
				"data_hub":     service.hostConfig().Integrations.DataHub.Enabled,
			},
		})
	})
	mux.HandleFunc("/api/v1/rpc", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		serveHTTPRPC(writer, request, service, accessFromHTTPRequest(request, "rest"))
	})
	mux.HandleFunc("/api/v1/snapshot", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/commands", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/program-state", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/menu/catalog", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/menu/layout", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/host-menus", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/os/status", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/os/key", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/os/power", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/command", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/messages", func(writer http.ResponseWriter, request *http.Request) {
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
		message = tagInboundMessage(message, "ipc")
		event, err := service.Client.SendTextMessage(request.Context(), message)
		if err != nil {
			writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(writer, http.StatusAccepted, event)
	})
	mux.HandleFunc("/api/v1/bridges", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/bridges/call", func(writer http.ResponseWriter, request *http.Request) {
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
	mux.HandleFunc("/api/v1/webhooks/inbound", func(writer http.ResponseWriter, request *http.Request) {
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
			accessFromHTTPRequest(request, "websocket"),
		)
	})
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeHTTPJSON(writer, http.StatusOK, map[string]any{
			"ok": true,
			"service": productidentity.ServiceName(
				service.hostConfig().UI.AppTitle,
				"IPC",
			),
			"api_version": APIVersion,
		})
	})
	if service.IntegrationProxy != nil {
		mux.Handle("/api/v1/integrations/", http.HandlerFunc(func(
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
	if service.WebUI != nil && webSocketPath != "/" && socketIOPath != "/" {
		mux.Handle("/", service.WebUI)
	}
	return mux
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
			if message.Metadata == nil {
				message.Metadata = make(map[string]string)
			}
		}
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
	metadataCount := 2
	for key, values := range query {
		if metadataCount >= 48 {
			break
		}
		message.Metadata["query."+key] = truncateText(strings.Join(values, ","), 1024)
		metadataCount++
	}
	for key, values := range request.Header {
		if metadataCount >= 64 || strings.EqualFold(key, "Authorization") ||
			strings.EqualFold(key, "X-PCController-Token") {
			continue
		}
		message.Metadata["header."+strings.ToLower(key)] =
			truncateText(strings.Join(values, ","), 1024)
		metadataCount++
	}
	if message.Text == "" {
		message.Text = request.Method + " " + request.URL.RequestURI()
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
	message = tagInboundMessage(message, "webhook")
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
	var rpcRequest Request
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMessage))
	if err := decoder.Decode(&rpcRequest); err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, Response{
			JSONRPC: Version,
			Error:   &RPCError{Code: -32700, Message: "parse error: " + err.Error()},
		})
		return
	}
	// The HTTP layer has already authenticated the request without copying the
	// secret into its JSON body.
	rpcRequest.Auth = service.currentAuthToken()
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
		&websocket.AcceptOptions{OriginPatterns: origins},
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
		rpcRequest.Auth = service.currentAuthToken()
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
					"interval_ms": normalized.IntervalMS,
					"latest_id":   service.Client.LatestEventID(),
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
					"topics": normalized.Topics, "interval_ms": normalized.IntervalMS,
					"latest_id": service.Client.LatestEventID(),
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
				message = tagInboundMessage(message, "websocket")
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
					JSONRPC: Version, Method: "controller.execute",
					Params: encoded, Auth: service.currentAuthToken(),
				}, access)
				_ = writeEvent("command.response", response)
			case "rpc":
				var rpcRequest Request
				if err := json.Unmarshal(raw, &rpcRequest); err != nil {
					_ = writeEvent("error", map[string]string{"error": err.Error()})
					continue
				}
				rpcRequest.Auth = service.currentAuthToken()
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

func (service *Service) authorized(token string) bool {
	expected := strings.TrimSpace(service.currentAuthToken())
	if expected == "" {
		return true
	}
	provided := strings.TrimSpace(token)
	return len(expected) == len(provided) &&
		subtle.ConstantTimeCompare([]byte(expected), []byte(provided)) == 1
}

func (service *Service) currentAuthToken() string {
	if service.HostConfig != nil {
		return service.HostConfig().IPC.AuthToken
	}
	return service.AuthToken
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
	if strings.TrimSpace(service.currentAuthToken()) == "" {
		return true
	}
	token := strings.TrimSpace(request.Header.Get("X-PCController-Token"))
	if token == "" {
		authorization := strings.TrimSpace(request.Header.Get("Authorization"))
		if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
			token = strings.TrimSpace(authorization[7:])
		}
	}
	// Web browsers cannot set custom headers on a native WebSocket handshake.
	// Query-token support is therefore available, but header authentication is
	// preferred because URLs may be logged by intermediaries.
	if token == "" {
		token = request.URL.Query().Get("access_token")
	}
	if service.authorized(token) {
		return true
	}
	writer.Header().Set("WWW-Authenticate", "Bearer")
	writeHTTPJSON(writer, http.StatusUnauthorized, map[string]string{
		"error": "authentication required",
	})
	return false
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
		value.Topics = []string{"events"}
	}
	seen := make(map[string]bool)
	result := make([]string, 0, len(value.Topics))
	for _, topic := range value.Topics {
		topic = strings.ToLower(strings.TrimSpace(topic))
		if topic == "telemetry" {
			topic = "status"
		}
		if topic != "events" && topic != "status" {
			return wsSubscription{}, fmt.Errorf("unknown subscription topic %q", topic)
		}
		if !seen[topic] {
			seen[topic] = true
			result = append(result, topic)
		}
	}
	value.Topics = result
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
			go streamWebSocketEvents(ctx, client, afterID, write)
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

func streamWebSocketEvents(
	ctx context.Context,
	client *controller.Client,
	afterID uint64,
	write func(any) error,
) {
	for ctx.Err() == nil {
		event, err := client.NextEvent(ctx, afterID, "")
		if err != nil {
			return
		}
		afterID = event.ID
		if err := write(wsNotification{
			JSONRPC: Version,
			Method:  "controller.event",
			Params:  event,
		}); err != nil {
			return
		}
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
	if address == "" {
		address = DefaultListen
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if host != "127.0.0.1" && host != "localhost" && host != "::1" && !allowRemote {
		return nil, fmt.Errorf(
			"refusing non-loopback IPC address %q; use an SSH tunnel or explicit proxy",
			address,
		)
	}
	return net.Listen("tcp", address)
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
