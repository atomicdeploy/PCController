// Package controller is the reusable host API for a controller board.
//
// It owns authenticated serial discovery, the native framed protocol, live
// status, settings, learned RF records, and the same command engine used by the
// CLI and TUI. Applications can embed this package without starting a terminal
// UI or a subprocess.
package controller

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/shell"
)

// Protocol, configuration, and host-service aliases form the embeddable API's
// shared data model without duplicating their wire or validation definitions.
type (
	Hello                     = native.Hello
	Status                    = native.Status
	Settings                  = native.Settings
	BoardName                 = native.BoardName
	RFEntry                   = native.RFEntry
	RFConfig                  = appconfig.RFConfig
	RFCategory                = appconfig.RFCategory
	RFCodeKey                 = appconfig.RFCodeKey
	RFMetadata                = appconfig.RFMetadata
	TemperatureSensor         = native.TemperatureSensor
	DeviceEvent               = native.DeviceEvent
	PWMValues                 = native.PWMValues
	FrontPanel                = native.FrontPanel
	Melody                    = appconfig.Melody
	MelodyNote                = appconfig.MelodyNote
	StatusLEDEffect           = appconfig.StatusLEDEffect
	StatusLEDState            = native.StatusLEDState
	StatusProfile             = native.StatusProfile
	StatusEffectOptions       = native.StatusEffectOptions
	StatusLEDPolicy           = appconfig.StatusLEDPolicy
	StatusLEDVisual           = appconfig.StatusLEDVisual
	RGBColor                  = appconfig.RGBColor
	OutputStreamState         = control.OutputStreamState
	StatusSample              = control.StatusSample
	TimelineEntry             = control.TimelineEntry
	HistoryOptions            = control.HistoryOptions
	RFLearnMode               = control.RFLearnMode
	RFLearnOptions            = control.RFLearnOptions
	RFLearnState              = control.RFLearnState
	MenuPageInfo              = control.MenuPageInfo
	MenuCatalog               = control.MenuCatalog
	MenuLayout                = control.MenuLayout
	HostMenuDirectory         = native.HostMenuDirectory
	HostMenuDirectoryEntry    = native.HostMenuDirectoryEntry
	HostMenuContent           = native.HostMenuContent
	HostMenuState             = native.HostMenuState
	HostMenuContentRequest    = native.HostMenuContentRequest
	I2CTransferResult         = native.I2CTransferResult
	LCDPresentationOptions    = control.LCDPresentationOptions
	LCDPresentationState      = control.LCDPresentationState
	DisplayRepeat             = control.DisplayRepeat
	DisplayRequest            = control.DisplayRequest
	DisplayResult             = control.DisplayResult
	OSPolicy                  = hostos.Policy
	VirtualKeyPolicy          = hostos.VirtualKeyPolicy
	PowerPolicy               = hostos.PowerPolicy
	VirtualKeyRequest         = hostos.VirtualKeyRequest
	PowerRequest              = hostos.PowerRequest
	OSActionResult            = hostos.ActionResult
	SystemStatus              = hostos.SystemStatus
	CommandDescriptor         = shell.CommandDescriptor
	ToolchainSyncOptions      = programmer.ToolchainSyncOptions
	ToolchainSyncStep         = programmer.ToolchainSyncStep
	ToolchainSyncReport       = programmer.ToolchainSyncReport
	ToolchainProfile          = programmer.ToolchainProfile
	ToolchainPolicy           = programmer.ToolchainPolicy
	ToolchainLock             = programmer.ToolchainLock
	ToolchainCanary           = programmer.ToolchainCanary
	ToolchainResolution       = programmer.ToolchainResolution
	ToolchainChange           = programmer.ToolchainChange
	ToolchainResolveOptions   = programmer.ToolchainResolveOptions
	ToolchainBootstrapOptions = programmer.ToolchainBootstrapOptions
	ToolchainBootstrapReport  = programmer.ToolchainBootstrapReport
	ProgramMode               = control.ProgramMode
	ProgramStateOwner         = control.ProgramStateOwner
	ProgramStateSnapshot      = control.ProgramStateSnapshot
	ProgramStateLease         = control.ProgramStateLease
)

// RF learning modes select indefinite multi-code or bounded timer operation.
const (
	RFLearnIndefinite = control.RFLearnIndefinite
	RFLearnTimer      = control.RFLearnTimer
)

const (
	DisplayRepeatOnce     = control.DisplayRepeatOnce
	DisplayRepeatLoop     = control.DisplayRepeatLoop
	DisplayRepeatInterval = control.DisplayRepeatInterval
)

// ParseRFLearnMode accepts the canonical RF learning mode and documented aliases.
func ParseRFLearnMode(value string) (RFLearnMode, error) {
	return control.ParseRFLearnMode(value)
}

// RFAction identifies the peripheral domain invoked by a learned RF code.
type RFAction byte

// RFBehavior selects press, toggle, momentary, or motion behavior.
type RFBehavior byte

// RelayMotion selects the requested state of one interlocked motion side.
type RelayMotion byte

// RF mapping, motion, settings, and program-state constants mirror the native API.
const (
	RFActionNone  RFAction = RFAction(native.RFActionNone)
	RFActionKey   RFAction = RFAction(native.RFActionKey)
	RFActionMenu  RFAction = RFAction(native.RFActionMenu)
	RFActionRelay RFAction = RFAction(native.RFActionRelay)
	RFActionSide  RFAction = RFAction(native.RFActionSide)
	RFActionPWM   RFAction = RFAction(native.RFActionPWM)

	RFBehaviorPress     RFBehavior = RFBehavior(native.RFBehaviorPress)
	RFBehaviorToggle    RFBehavior = RFBehavior(native.RFBehaviorToggle)
	RFBehaviorMomentary RFBehavior = RFBehavior(native.RFBehaviorMomentary)
	RFBehaviorUp        RFBehavior = RFBehavior(native.RFBehaviorUp)
	RFBehaviorDown      RFBehavior = RFBehavior(native.RFBehaviorDown)
	RFBehaviorStop      RFBehavior = RFBehavior(native.RFBehaviorStop)

	RelayMotionStop RelayMotion = 0
	RelayMotionUp   RelayMotion = 1
	RelayMotionDown RelayMotion = 2

	SettingsSaveLastPage       byte = native.SettingsSaveLastPage
	SettingsStatusColorMask    byte = native.SettingsStatusColorMask
	SettingsVoltageDecimalMask byte = native.SettingsVoltageDecimalMask
	SettingsCurrentDecimalMask byte = native.SettingsCurrentDecimalMask
	SettingsDefaultDecimals    byte = native.SettingsDefaultDecimals
	MotionDoorAlways           byte = native.MotionDoorAlways
	MotionDoorClosedOnly       byte = native.MotionDoorClosedOnly
	MotionDoorOpenOnly         byte = native.MotionDoorOpenOnly
	MotionDoorNever            byte = native.MotionDoorNever
	ProgramIdle                     = control.ProgramIdle
	ProgramRunning                  = control.ProgramRunning
)

// RFMapping binds a learned record to one board action and behavior.
type RFMapping struct {
	Action   RFAction   `json:"action"`
	Value    byte       `json:"value"`
	Behavior RFBehavior `json:"behavior"`
}

// OutputOperation tracks an asynchronous melody or status-LED effect.
type OutputOperation struct {
	ID   uint64       `json:"id"`
	Kind string       `json:"kind"`
	Name string       `json:"name"`
	Done <-chan error `json:"-"`
}

// StatusUpdate contains one subscribed telemetry result or its polling error.
type StatusUpdate struct {
	Time   time.Time `json:"time"`
	Status Status    `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// Options configures discovery, transport, tooling, automation, and host policy.
type Options struct {
	Port             string                 `json:"port,omitempty"`
	VID              string                 `json:"vid,omitempty"`
	PID              string                 `json:"pid,omitempty"`
	Name             string                 `json:"name,omitempty"`
	PreferredDevice  *PortInfo              `json:"preferred_device,omitempty"`
	BaudRate         int                    `json:"baud_rate,omitempty"`
	StartupWait      time.Duration          `json:"startup_wait,omitempty"`
	RequestTimeout   time.Duration          `json:"request_timeout,omitempty"`
	HelloAttempts    int                    `json:"hello_attempts,omitempty"`
	ResetOnReconnect bool                   `json:"reset_on_reconnect,omitempty"`
	ProjectPath      string                 `json:"project_path,omitempty"`
	FQBN             string                 `json:"fqbn,omitempty"`
	ToolchainCLI     string                 `json:"toolchain_cli,omitempty"`
	Avrdude          string                 `json:"avrdude,omitempty"`
	AvrdudeConf      string                 `json:"avrdude_conf,omitempty"`
	Programmer       string                 `json:"programmer,omitempty"`
	Macros           []Macro                `json:"macros,omitempty"`
	Melodies         []Melody               `json:"melodies,omitempty"`
	StatusEffects    []StatusLEDEffect      `json:"status_effects,omitempty"`
	Scripts          map[string]string      `json:"scripts,omitempty"`
	Automations      []Automation           `json:"automations,omitempty"`
	MotionDoorPolicy string                 `json:"motion_door_policy,omitempty"`
	LCDPresentation  LCDPresentationOptions `json:"lcd_presentation,omitempty"`
	RF               RFConfig               `json:"rf"`
	OSActions        OSPolicy               `json:"os_actions"`
}

// Macro describes a host-owned, MCU-timed sequence of peripheral operations.
type Macro struct {
	ID                  byte        `json:"id"`
	Name                string      `json:"name"`
	Category            string      `json:"category,omitempty"`
	Color               string      `json:"color,omitempty"`
	Label               string      `json:"label,omitempty"`
	LCDMessage          string      `json:"lcd_message,omitempty"`
	TimingToleranceUS   uint32      `json:"timing_tolerance_us,omitempty"`
	KeepOutputsOnCancel bool        `json:"keep_outputs_on_cancel,omitempty"`
	Steps               []MacroStep `json:"steps"`
}

// MacroStep describes one timestamped operation within a Macro.
type MacroStep struct {
	AtUS        uint32 `json:"at_us,omitempty"`
	Kind        string `json:"kind"`
	Target      byte   `json:"target,omitempty"`
	Value       uint16 `json:"value,omitempty"`
	DurationMS  uint16 `json:"duration_ms,omitempty"`
	FrequencyHz uint16 `json:"frequency_hz,omitempty"`
	Text        string `json:"text,omitempty"`
	Destination string `json:"destination,omitempty"`
	Code        uint32 `json:"code,omitempty"`
	Bits        byte   `json:"bits,omitempty"`
	Protocol    byte   `json:"protocol,omitempty"`
	PulseUS     uint16 `json:"pulse_us,omitempty"`
	Red         byte   `json:"red,omitempty"`
	Green       byte   `json:"green,omitempty"`
	Blue        byte   `json:"blue,omitempty"`
	Brightness  byte   `json:"brightness,omitempty"`
	Opcode      byte   `json:"opcode,omitempty"`
	PayloadHex  string `json:"payload_hex,omitempty"`
}

// Automation maps an event match to one or more host-side actions.
type Automation struct {
	Name       string             `json:"name"`
	Enabled    bool               `json:"enabled"`
	CooldownMS int                `json:"cooldown_ms,omitempty"`
	Match      AutomationMatch    `json:"match"`
	Actions    []AutomationAction `json:"actions"`
}

// AutomationMatch selects the event properties that trigger an automation.
type AutomationMatch struct {
	Kind       string  `json:"kind"`
	Lifecycle  string  `json:"lifecycle,omitempty"`
	State      string  `json:"state,omitempty"`
	Contains   string  `json:"contains,omitempty"`
	Key        byte    `json:"key,omitempty"`
	Gesture    string  `json:"gesture,omitempty"`
	Source     string  `json:"source,omitempty"`
	RFID       *byte   `json:"rf_id,omitempty"`
	RFCode     *uint32 `json:"rf_code,omitempty"`
	RFProtocol byte    `json:"rf_protocol,omitempty"`
}

// AutomationAction describes one command, macro, integration, or OS action.
type AutomationAction struct {
	Type       string      `json:"type"`
	Command    string      `json:"command,omitempty"`
	Macro      string      `json:"macro,omitempty"`
	Executable string      `json:"executable,omitempty"`
	Args       []string    `json:"args,omitempty"`
	Script     string      `json:"script,omitempty"`
	Event      string      `json:"event,omitempty"`
	RF         *RFTransmit `json:"rf,omitempty"`
	VirtualKey string      `json:"virtual_key,omitempty"`
	HoldMS     int         `json:"hold_ms,omitempty"`
	Power      string      `json:"power,omitempty"`
	Confirm    string      `json:"confirm,omitempty"`
}

// RFTransmit contains the complete waveform metadata for one 433 MHz send action.
type RFTransmit struct {
	Code     uint32 `json:"code"`
	Bits     byte   `json:"bits"`
	Protocol byte   `json:"protocol"`
	PulseUS  uint16 `json:"pulse_us,omitempty"`
	Repeats  byte   `json:"repeats,omitempty"`
}

// RFEntryView combines one MCU record with host-owned display metadata.
type RFEntryView struct {
	ID          byte   `json:"id"`
	Code        uint32 `json:"code"`
	CodeDisplay string `json:"code_display"`
	Bits        byte   `json:"bits"`
	Protocol    byte   `json:"protocol"`
	PulseUS     uint16 `json:"pulse_us"`
	ActionKind  byte   `json:"action_kind"`
	ActionValue byte   `json:"action_value"`
	Behavior    byte   `json:"behavior"`
	Name        string `json:"name,omitempty"`
	Category    string `json:"category,omitempty"`
}

// PortInfo identifies one serial candidate with stable USB metadata when available.
type PortInfo struct {
	Name         string `json:"name"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	Product      string `json:"product,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	FriendlyName string `json:"friendly_name,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
}

// Snapshot is a point-in-time view of connection, board, and front-panel state.
type Snapshot struct {
	Connected         bool                 `json:"connected"`
	Paused            bool                 `json:"paused"`
	Port              PortInfo             `json:"port"`
	Hello             Hello                `json:"hello"`
	Status            Status               `json:"status"`
	Settings          Settings             `json:"settings"`
	HaveStatus        bool                 `json:"have_status"`
	HaveSettings      bool                 `json:"have_settings"`
	StatusUpdated     time.Time            `json:"status_updated,omitempty"`
	ConnectionState   string               `json:"connection_state"`
	ConnectionReason  string               `json:"connection_reason,omitempty"`
	ConnectionUpdated time.Time            `json:"connection_updated,omitempty"`
	ProgramState      ProgramStateSnapshot `json:"program_state"`
	RFLearning        RFLearnState         `json:"rf_learning"`
	FrontPanel        FrontPanel           `json:"front_panel"`
	HaveFrontPanel    bool                 `json:"have_front_panel"`
	FrontPanelUpdated time.Time            `json:"front_panel_updated,omitempty"`
	StatusLED         StatusLEDState       `json:"status_led"`
	HaveStatusLED     bool                 `json:"have_status_led"`
	StatusLEDUpdated  time.Time            `json:"status_led_updated,omitempty"`
}

// Event is the normalized event envelope shared by embedders and bridge clients.
type Event struct {
	ID          uint64            `json:"id"`
	Time        time.Time         `json:"time"`
	Kind        string            `json:"kind"`
	Stream      string            `json:"stream"`
	Text        string            `json:"text"`
	Opcode      byte              `json:"opcode,omitempty"`
	Seq         byte              `json:"seq,omitempty"`
	Payload     []byte            `json:"payload,omitempty"`
	Device      *DeviceEvent      `json:"device,omitempty"`
	Lifecycle   string            `json:"lifecycle,omitempty"`
	Port        PortInfo          `json:"port,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	State       string            `json:"state,omitempty"`
	Key         byte              `json:"key,omitempty"`
	Gesture     string            `json:"gesture,omitempty"`
	Source      string            `json:"source,omitempty"`
	Target      string            `json:"target,omitempty"`
	MessageType string            `json:"message_type,omitempty"`
	Action      string            `json:"action,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	SourceID    *byte             `json:"source_id,omitempty"`
	RFID        *byte             `json:"rf_id,omitempty"`
	RFCode      uint32            `json:"rf_code,omitempty"`
	RFBits      byte              `json:"rf_bits,omitempty"`
	RFProtocol  byte              `json:"rf_protocol,omitempty"`
	RFPulseUS   uint16            `json:"rf_pulse_us,omitempty"`
	ResetCause  byte              `json:"reset_cause,omitempty"`
	ResetCount  uint32            `json:"reset_count,omitempty"`
}

// OpcodeFrame is the raw, versionless UART exchange result. Payload is kept
// opaque so clients can query firmware additions before the host understands
// their schema.
type OpcodeFrame struct {
	Opcode     byte   `json:"opcode"`
	Name       string `json:"name"`
	Sequence   byte   `json:"sequence"`
	Payload    []byte `json:"payload,omitempty"`
	PayloadHex string `json:"payload_hex,omitempty"`
}

// TextMessage is a typed envelope shared by IPC, WebSocket, webhooks, the host
// bridge, and LCD presentation. Action is descriptive; it is never executed
// implicitly. Remote command execution uses the authenticated execute method.
type TextMessage struct {
	Source   string            `json:"source"`
	Target   string            `json:"target"`
	Type     string            `json:"type"`
	Text     string            `json:"text"`
	Line1    string            `json:"line1,omitempty"`
	Line2    string            `json:"line2,omitempty"`
	Action   string            `json:"action,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Client owns one controller runtime, command engine, and host integration state.
type Client struct {
	runtime        *control.Runtime
	engine         *shell.Engine
	engineMu       sync.Mutex
	optionsMu      sync.RWMutex
	commandOptions control.CommandOptions
	macroMu        sync.RWMutex
	macros         []appconfig.Macro
	outputMu       sync.RWMutex
	melodies       []appconfig.Melody
	statusEffects  []appconfig.StatusLEDEffect
	outputs        *control.OutputScheduler
	hostMu         sync.RWMutex
	scripts        map[string]string
	automations    []appconfig.Automation
	safety         appconfig.Safety
	rfConfig       appconfig.RFConfig
	osPolicy       hostos.Policy
	events         chan Event
	done           chan struct{}
	doneOnce       sync.Once
}

// New creates a client that owns its serial and background-service lifecycle.
func New(options Options) *Client {
	baud := options.BaudRate
	if baud == 0 {
		baud = link.DefaultBaudRate
	}
	fqbn := options.FQBN
	if fqbn == "" {
		fqbn = programmer.DefaultFQBN()
	}
	runtime := control.New(control.Options{
		Filter: ports.Filter{
			Port:      options.Port,
			VID:       options.VID,
			PID:       options.PID,
			Name:      options.Name,
			Preferred: internalPortIdentity(options.PreferredDevice),
		},
		BaudRate:         baud,
		StartupWait:      options.StartupWait,
		RequestTimeout:   options.RequestTimeout,
		HelloAttempts:    options.HelloAttempts,
		ResetOnReconnect: options.ResetOnReconnect,
	})
	client := &Client{
		runtime:  runtime,
		macros:   toAppMacros(options.Macros),
		melodies: cloneMelodies(options.Melodies),
		statusEffects: append(
			[]appconfig.StatusLEDEffect(nil),
			options.StatusEffects...,
		),
		scripts:     cloneStringMap(options.Scripts),
		automations: toAppAutomations(options.Automations),
		safety: appconfig.Safety{
			MotionDoorPolicy: normalizedMotionDoorPolicy(options.MotionDoorPolicy),
		},
		rfConfig: cloneRFConfigOrDefault(options.RF),
		osPolicy: cloneOSPolicyOrDefault(options.OSActions),
		events:   make(chan Event, 256),
		done:     make(chan struct{}),
	}
	client.outputs = control.NewOutputScheduler(runtime)
	_ = runtime.LCDPresenter().Configure(options.LCDPresentation)
	client.commandOptions = control.CommandOptions{
		ProjectPath:      options.ProjectPath,
		FQBN:             fqbn,
		ArduinoCLI:       options.ToolchainCLI,
		Avrdude:          options.Avrdude,
		AvrdudeConf:      options.AvrdudeConf,
		Programmer:       options.Programmer,
		HostConfig:       client.currentHostConfig,
		UpdateHostConfig: client.updateHostConfig,
		Outputs:          client.outputs,
	}
	client.engine = control.NewCommandEngine(runtime, control.CommandOptions{
		ProjectPath:      options.ProjectPath,
		FQBN:             fqbn,
		Macros:           client.currentMacros,
		ArduinoCLI:       options.ToolchainCLI,
		Avrdude:          options.Avrdude,
		AvrdudeConf:      options.AvrdudeConf,
		Programmer:       options.Programmer,
		HostConfig:       client.currentHostConfig,
		UpdateHostConfig: client.updateHostConfig,
		Resolve:          client.currentCommandOptions,
		Outputs:          client.outputs,
	})
	automationContext, cancelAutomations := context.WithCancel(context.Background())
	go func() {
		<-client.done
		cancelAutomations()
	}()
	go control.RunAutomations(
		automationContext,
		runtime,
		client.engine,
		client.currentHostConfig,
	)
	go client.forwardEvents()
	return client
}

// AttachSharedRuntime creates an API/IPC facade around a runtime and command
// engine that are already owned by the current process. It lets an interactive
// TUI or shell be the one and only serial owner while secondary processes use
// JSON-RPC. The caller remains responsible for closing the runtime.
//
// This constructor is intended for sibling packages in this module. External
// embedders should normally use New, which owns its complete lifecycle.
func AttachSharedRuntime(
	runtime *control.Runtime,
	engine *shell.Engine,
) *Client {
	if runtime == nil {
		panic("controller: shared runtime is nil")
	}
	if engine == nil {
		panic("controller: shared command engine is nil")
	}
	return &Client{
		runtime: runtime,
		engine:  engine,
		outputs: control.NewOutputScheduler(runtime),
		events:  make(chan Event),
		done:    make(chan struct{}),
	}
}

// ApplyHostOptions atomically refreshes runtime and host-owned configuration.
func (client *Client) ApplyHostOptions(options Options) bool {
	baud := options.BaudRate
	if baud == 0 {
		baud = link.DefaultBaudRate
	}
	changed := client.runtime.ApplyOptions(control.Options{
		Filter: ports.Filter{
			Port:      options.Port,
			VID:       options.VID,
			PID:       options.PID,
			Name:      options.Name,
			Preferred: internalPortIdentity(options.PreferredDevice),
		},
		BaudRate: baud, StartupWait: options.StartupWait,
		RequestTimeout:   options.RequestTimeout,
		HelloAttempts:    options.HelloAttempts,
		ResetOnReconnect: options.ResetOnReconnect,
	})
	client.SetMacros(options.Macros)
	client.SetOutputDefinitions(options.Melodies, options.StatusEffects)
	_ = client.ConfigureLCDPresentation(options.LCDPresentation)
	client.hostMu.Lock()
	client.scripts = cloneStringMap(options.Scripts)
	client.automations = toAppAutomations(options.Automations)
	client.safety.MotionDoorPolicy = normalizedMotionDoorPolicy(
		options.MotionDoorPolicy,
	)
	client.rfConfig = cloneRFConfigOrDefault(options.RF)
	client.osPolicy = cloneOSPolicyOrDefault(options.OSActions)
	client.hostMu.Unlock()
	fqbn := options.FQBN
	if fqbn == "" {
		fqbn = programmer.DefaultFQBN()
	}
	client.optionsMu.Lock()
	client.commandOptions = control.CommandOptions{
		ProjectPath: options.ProjectPath, FQBN: fqbn,
		ArduinoCLI: options.ToolchainCLI, Avrdude: options.Avrdude,
		AvrdudeConf:      options.AvrdudeConf,
		Programmer:       options.Programmer,
		HostConfig:       client.currentHostConfig,
		UpdateHostConfig: client.updateHostConfig,
		Outputs:          client.outputs,
	}
	client.optionsMu.Unlock()
	return changed
}

func internalPortIdentity(info *PortInfo) ports.Identity {
	if info == nil {
		return ports.Identity{}
	}
	return ports.Identity{
		Port: info.Name, VID: info.VID, PID: info.PID,
		SerialNumber: info.SerialNumber,
		Name:         firstNonempty(info.FriendlyName, info.Product),
		InstanceID:   info.InstanceID,
	}
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func cloneOSPolicyOrDefault(value hostos.Policy) hostos.Policy {
	if value.VirtualKeys.MinIntervalMS == 0 && value.VirtualKeys.HoldMS == 0 &&
		len(value.VirtualKeys.Allowed) == 0 && len(value.Power.Allowed) == 0 {
		return hostos.DefaultPolicy()
	}
	return hostos.ClonePolicy(value)
}

func (client *Client) currentHostConfig() appconfig.Config {
	client.hostMu.RLock()
	scripts := cloneStringMap(client.scripts)
	automations := cloneAppAutomations(client.automations)
	safety := client.safety
	rfConfig := cloneRFConfigOrDefault(client.rfConfig)
	osPolicy := hostos.ClonePolicy(client.osPolicy)
	client.hostMu.RUnlock()
	config := appconfig.Defaults()
	config.Macros = client.currentMacros()
	config.Scripts = scripts
	config.Automations = automations
	config.Melodies = client.currentMelodies()
	config.StatusEffects = client.currentStatusEffects()
	config.Safety = safety
	config.RF = rfConfig
	config.OSActions = osPolicy
	return config
}

func (client *Client) updateHostConfig(
	change func(*appconfig.Config) error,
) error {
	if change == nil {
		return errors.New("host configuration update callback is nil")
	}
	config := client.currentHostConfig()
	if err := change(&config); err != nil {
		return err
	}
	if err := config.Validate(); err != nil {
		return err
	}
	client.SetOutputDefinitions(config.Melodies, config.StatusEffects)
	client.macroMu.Lock()
	client.macros = cloneAppMacros(config.Macros)
	client.macroMu.Unlock()
	client.hostMu.Lock()
	client.scripts = cloneStringMap(config.Scripts)
	client.automations = cloneAppAutomations(config.Automations)
	client.safety = config.Safety
	client.rfConfig = cloneRFConfigOrDefault(config.RF)
	client.osPolicy = hostos.ClonePolicy(config.OSActions)
	client.hostMu.Unlock()
	return nil
}

func (client *Client) currentCommandOptions() control.CommandOptions {
	client.optionsMu.RLock()
	defer client.optionsMu.RUnlock()
	return client.commandOptions
}

// SetMacros replaces the host-owned macro catalog with a defensive copy.
func (client *Client) SetMacros(macros []Macro) {
	client.macroMu.Lock()
	client.macros = toAppMacros(macros)
	client.macroMu.Unlock()
}

// SetOutputDefinitions replaces the host-owned melody and LED-effect catalogs.
func (client *Client) SetOutputDefinitions(
	melodies []Melody,
	effects []StatusLEDEffect,
) {
	client.outputMu.Lock()
	client.melodies = cloneMelodies(melodies)
	client.statusEffects = append(
		[]appconfig.StatusLEDEffect(nil),
		effects...,
	)
	client.outputMu.Unlock()
}

func (client *Client) currentMelodies() []appconfig.Melody {
	client.outputMu.RLock()
	defer client.outputMu.RUnlock()
	return cloneMelodies(client.melodies)
}

func (client *Client) currentStatusEffects() []appconfig.StatusLEDEffect {
	client.outputMu.RLock()
	defer client.outputMu.RUnlock()
	return append(
		[]appconfig.StatusLEDEffect(nil),
		client.statusEffects...,
	)
}

func (client *Client) currentMacros() []appconfig.Macro {
	client.macroMu.RLock()
	defer client.macroMu.RUnlock()
	return cloneAppMacros(client.macros)
}

func cloneAppMacros(source []appconfig.Macro) []appconfig.Macro {
	result := make([]appconfig.Macro, len(source))
	for index, macro := range source {
		result[index] = macro
		result[index].Steps = append([]appconfig.MacroStep(nil), macro.Steps...)
	}
	return result
}

func toAppMacros(macros []Macro) []appconfig.Macro {
	result := make([]appconfig.Macro, len(macros))
	for index, macro := range macros {
		result[index] = appconfig.Macro{
			ID: macro.ID, Name: macro.Name, Category: macro.Category,
			Color: macro.Color, Label: macro.Label, LCDMessage: macro.LCDMessage,
			TimingToleranceUS:   macro.TimingToleranceUS,
			KeepOutputsOnCancel: macro.KeepOutputsOnCancel,
			Steps:               make([]appconfig.MacroStep, len(macro.Steps)),
		}
		for stepIndex, step := range macro.Steps {
			result[index].Steps[stepIndex] = appconfig.MacroStep{
				AtUS: step.AtUS, Kind: step.Kind,
				Target: step.Target, Value: step.Value,
				DurationMS: step.DurationMS, FrequencyHz: step.FrequencyHz,
				Text: step.Text, Destination: step.Destination,
				Code: step.Code, Bits: step.Bits, Protocol: step.Protocol,
				PulseUS: step.PulseUS, Red: step.Red, Green: step.Green,
				Blue: step.Blue, Brightness: step.Brightness,
				Opcode: step.Opcode, PayloadHex: step.PayloadHex,
			}
		}
	}
	return result
}

func toAppAutomations(source []Automation) []appconfig.Automation {
	result := make([]appconfig.Automation, len(source))
	for index, automation := range source {
		result[index] = appconfig.Automation{
			Name: automation.Name, Enabled: automation.Enabled,
			CooldownMS: automation.CooldownMS,
			Match: appconfig.AutomationMatch{
				Kind: automation.Match.Kind, Lifecycle: automation.Match.Lifecycle,
				State: automation.Match.State, Contains: automation.Match.Contains,
				Key: automation.Match.Key, Gesture: automation.Match.Gesture,
				Source: automation.Match.Source, RFID: automation.Match.RFID,
				RFCode:     automation.Match.RFCode,
				RFProtocol: automation.Match.RFProtocol,
			},
			Actions: make([]appconfig.AutomationAction, len(automation.Actions)),
		}
		for actionIndex, action := range automation.Actions {
			result[index].Actions[actionIndex] = appconfig.AutomationAction{
				Type: action.Type, Command: action.Command, Macro: action.Macro,
				Executable: action.Executable,
				Args:       append([]string(nil), action.Args...),
				Script:     action.Script,
				Event:      action.Event,
				VirtualKey: action.VirtualKey,
				HoldMS:     action.HoldMS,
				Power:      action.Power,
				Confirm:    action.Confirm,
			}
			if action.RF != nil {
				result[index].Actions[actionIndex].RF = &appconfig.RFTransmit{
					Code: action.RF.Code, Bits: action.RF.Bits,
					Protocol: action.RF.Protocol, PulseUS: action.RF.PulseUS,
					Repeats: action.RF.Repeats,
				}
			}
		}
	}
	return result
}

func cloneAppAutomations(source []appconfig.Automation) []appconfig.Automation {
	result := make([]appconfig.Automation, len(source))
	for index, automation := range source {
		result[index] = automation
		result[index].Actions = make(
			[]appconfig.AutomationAction,
			len(automation.Actions),
		)
		for actionIndex, action := range automation.Actions {
			result[index].Actions[actionIndex] = action
			result[index].Actions[actionIndex].Args =
				append([]string(nil), action.Args...)
			if action.RF != nil {
				rf := *action.RF
				result[index].Actions[actionIndex].RF = &rf
			}
		}
		if automation.Match.RFID != nil {
			id := *automation.Match.RFID
			result[index].Match.RFID = &id
		}
		if automation.Match.RFCode != nil {
			code := *automation.Match.RFCode
			result[index].Match.RFCode = &code
		}
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneMelodies(source []appconfig.Melody) []appconfig.Melody {
	result := make([]appconfig.Melody, len(source))
	for index, melody := range source {
		result[index] = melody
		result[index].Notes = append(
			[]appconfig.MelodyNote(nil),
			melody.Notes...,
		)
	}
	return result
}

func cloneRFConfigOrDefault(source appconfig.RFConfig) appconfig.RFConfig {
	if strings.TrimSpace(source.DisplayRadix) == "" {
		source = appconfig.DefaultRFConfig()
	}
	result := source
	result.Categories = append([]appconfig.RFCategory(nil), source.Categories...)
	result.Metadata = append([]appconfig.RFMetadata(nil), source.Metadata...)
	return result
}

// ConfigureRFPresentation replaces host-only RF names, categories, and radix.
func (client *Client) ConfigureRFPresentation(config RFConfig) {
	client.hostMu.Lock()
	client.rfConfig = cloneRFConfigOrDefault(config)
	client.hostMu.Unlock()
}

// RFPresentation returns a defensive copy of host-owned RF presentation data.
func (client *Client) RFPresentation() RFConfig {
	client.hostMu.RLock()
	defer client.hostMu.RUnlock()
	return cloneRFConfigOrDefault(client.rfConfig)
}

func normalizedMotionDoorPolicy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "open", "closed", "never", "always":
		return value
	default:
		return "always"
	}
}

// Connect resumes automatic discovery and authenticates the selected board.
func (client *Client) Connect(ctx context.Context) error {
	client.runtime.ResumeAuto()
	return client.runtime.EnsureConnected(ctx)
}

// Open connects directly to a named serial port and authenticates the board.
func (client *Client) Open(ctx context.Context, port string) error {
	return client.runtime.Open(ctx, port)
}

// SetDeviceObserver receives authenticated device identities after connection.
func (client *Client) SetDeviceObserver(
	observer func(PortInfo, Hello),
) {
	if observer == nil {
		client.runtime.SetDeviceObserver(nil)
		return
	}
	client.runtime.SetDeviceObserver(func(info ports.Info, hello native.Hello) {
		observer(publicPortInfo(info), hello)
	})
}

// Close stops active output streams and intentionally closes the serial port.
func (client *Client) Close() error {
	client.outputs.StopAll()
	return client.runtime.Close()
}

// PulseResetFor briefly asserts the adapter reset lines, then reauthenticates
// the native application after Urboot has released the UART.
func (client *Client) PulseResetFor(
	ctx context.Context,
	duration time.Duration,
) error {
	if duration <= 0 || duration > 5*time.Second {
		return errors.New("reset pulse must be positive and at most five seconds")
	}
	if err := client.runtime.PulseResetFor(ctx, duration); err != nil {
		return err
	}
	return client.runtime.Reconnect(ctx, "DTR reset pulse completed over IPC")
}

// Shutdown closes the serial port and releases background event forwarding.
// A shutdown client must not be reused.
func (client *Client) Shutdown() error {
	client.outputs.Close()
	_ = hostos.DefaultExecutor.ReleaseAll()
	err := client.runtime.Close()
	client.doneOnce.Do(func() { close(client.done) })
	return err
}

// Execute runs one command through the same engine exposed by every host surface.
func (client *Client) Execute(ctx context.Context, command string) (string, error) {
	client.engineMu.Lock()
	defer client.engineMu.Unlock()
	return client.engine.Execute(ctx, command)
}

// CommandCatalog exposes the same discoverable command contract used by the
// shell, primary IPC, REST, WebSocket RPC, and the C-compatible library.
func (client *Client) CommandCatalog() []CommandDescriptor {
	client.engineMu.Lock()
	defer client.engineMu.Unlock()
	return client.engine.Catalog()
}

// ProgramState returns the host-owned application state. Hardware conditions
// such as the enclosure door are intentionally not state owners.
func (client *Client) ProgramState() ProgramStateSnapshot {
	return client.runtime.ProgramState()
}

// SetProgramState updates host-owned Idle or Running state for a named owner.
func (client *Client) SetProgramState(
	owner string,
	mode ProgramMode,
	reason string,
) (ProgramStateSnapshot, error) {
	return client.runtime.SetProgramState(owner, mode, reason)
}

// AcquireProgramState holds Running state until the returned lease is released.
func (client *Client) AcquireProgramState(
	owner string,
	reason string,
) (*ProgramStateLease, ProgramStateSnapshot, error) {
	return client.runtime.AcquireProgramState(owner, reason)
}

// Status requests one fresh native telemetry snapshot from the board.
func (client *Client) Status(ctx context.Context) (Status, error) {
	return client.runtime.RefreshStatus(ctx)
}

// SubscribeStatus polls only while the returned subscription context is
// alive. Merely keeping the serial protocol connected never starts polling.
func (client *Client) SubscribeStatus(
	ctx context.Context,
	interval time.Duration,
) (<-chan StatusUpdate, error) {
	if interval < 50*time.Millisecond || interval > time.Minute {
		return nil, errors.New("status subscription interval must be 50ms..1m")
	}
	updates := make(chan StatusUpdate, 1)
	go func() {
		defer close(updates)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case at := <-ticker.C:
				status, err := client.Status(ctx)
				update := StatusUpdate{Time: at, Status: status}
				if err != nil {
					update.Error = err.Error()
				}
				select {
				case updates <- update:
				default:
					select {
					case <-updates:
					default:
					}
					select {
					case updates <- update:
					default:
					}
				}
			}
		}
	}()
	return updates, nil
}

// ConfigureHistory updates bounded telemetry retention and persistence policy.
func (client *Client) ConfigureHistory(options HistoryOptions) error {
	return client.runtime.ConfigureHistory(options)
}

// ConfigureLCDPresentation updates host-owned LCD discovery and fallback policy.
func (client *Client) ConfigureLCDPresentation(
	options LCDPresentationOptions,
) error {
	return client.runtime.LCDPresenter().Configure(options)
}

// MirrorLCDPrompt publishes two prompt lines through the LCD presenter.
func (client *Client) MirrorLCDPrompt(line1, line2 string) {
	client.runtime.LCDPresenter().MirrorPrompt(line1, line2)
}

// ShowLCDPriority temporarily replaces ordinary LCD content by event priority.
func (client *Client) ShowLCDPriority(
	kind, line1, line2 string,
	hold time.Duration,
) bool {
	return client.runtime.LCDPresenter().ShowPriority(
		kind,
		line1,
		line2,
		hold,
	)
}

// LCDPresentationState returns the current address, visibility, and fallback state.
func (client *Client) LCDPresentationState() LCDPresentationState {
	return client.runtime.LCDPresenter().State()
}

// StatusHistory returns retained telemetry at or after since.
func (client *Client) StatusHistory(since time.Time) []StatusSample {
	return client.runtime.StatusHistory(since)
}

// Timeline returns retained device and host events in chronological order.
func (client *Client) Timeline(since time.Time, limit int) []TimelineEntry {
	return client.runtime.Timeline(since, limit)
}

// SetMenuPage navigates directly to one stable board menu ID.
func (client *Client) SetMenuPage(ctx context.Context, page byte) error {
	return client.runtime.Command(ctx, native.OpMenuSetPage, []byte{page})
}

// BoardName reads the operator-assigned MCU EEPROM name.
func (client *Client) BoardName(ctx context.Context) (BoardName, error) {
	return client.runtime.BoardName(ctx)
}

// SetBoardName persists and verifies up to eight printable ASCII characters.
// Passing an empty string clears the name.
func (client *Client) SetBoardName(ctx context.Context, name string) (BoardName, error) {
	return client.runtime.SetBoardName(ctx, name)
}

// SetMenuPageByName resolves a live catalog name or ID before navigating.
func (client *Client) SetMenuPageByName(
	ctx context.Context,
	reference string,
) (MenuPageInfo, error) {
	catalog, err := client.MenuCatalog(ctx)
	if err != nil {
		return MenuPageInfo{}, err
	}
	page, err := control.ResolveMenuPageIn(catalog.Pages, reference)
	if err != nil {
		return MenuPageInfo{}, err
	}
	if err := client.SetMenuPage(ctx, page.ID); err != nil {
		return MenuPageInfo{}, err
	}
	return page, nil
}

// MenuCatalog queries the board-authoritative page directory and active page.
func (client *Client) MenuCatalog(ctx context.Context) (MenuCatalog, error) {
	return control.QueryMenuCatalog(ctx, client.runtime)
}

// MenuLayout returns the persisted MCU layout when capability bit 23 is
// present, or a clearly marked read-only catalog-order fallback on older
// firmware.
func (client *Client) MenuLayout(ctx context.Context) (MenuLayout, error) {
	return control.QueryMenuLayout(ctx, client.runtime)
}

// SetMenuLayout validates a complete stable-ID permutation and visibility
// mask, writes MCU EEPROM, and verifies the board's exact readback.
func (client *Client) SetMenuLayout(ctx context.Context, layout MenuLayout) (MenuLayout, error) {
	return control.PersistMenuLayout(ctx, client.runtime, layout)
}

// ReplaceHostMenuDirectory installs one volatile generation on firmware that
// advertises capability bit 24. It never writes MCU EEPROM.
func (client *Client) ReplaceHostMenuDirectory(ctx context.Context, directory HostMenuDirectory) error {
	return client.runtime.ReplaceHostMenuDirectory(ctx, directory)
}

// PushHostMenuContent supplies one volatile host-menu value to the front panel.
func (client *Client) PushHostMenuContent(ctx context.Context, content HostMenuContent) error {
	return client.runtime.PushHostMenuContent(ctx, content)
}

// HostMenuState queries the active host-menu generation and selection.
func (client *Client) HostMenuState(ctx context.Context) (HostMenuState, error) {
	return client.runtime.HostMenuState(ctx)
}

// MenuPages returns the built-in stable menu metadata known to this host build.
func MenuPages() []MenuPageInfo { return control.MenuPages() }

// I2CTransfer exposes the firmware's bounded cooperative I2C lease to Go API
// consumers. MCU-owned sensor/PWM service remains authoritative outside the
// short requested lease.
func (client *Client) I2CTransfer(
	ctx context.Context,
	address, leaseSeconds byte,
	write []byte,
	readLength byte,
) (I2CTransferResult, error) {
	return control.TransferI2C(ctx, client.runtime, address, leaseSeconds, write, readLength)
}

// ScanI2C probes all valid seven-bit addresses through the cooperative lease.
func (client *Client) ScanI2C(ctx context.Context) ([]byte, error) {
	return control.ScanI2C(ctx, client.runtime)
}

// RescanLCD probes configured LCD addresses and returns the detected address.
func (client *Client) RescanLCD(ctx context.Context) (byte, error) {
	return client.runtime.LCDPresenter().RescanPhysical(ctx)
}

// SetRelay controls the human-facing R1..R8 relay number. The firmware remains
// the authority for the R1..R4 motion interlock.
func (client *Client) SetRelay(
	ctx context.Context,
	relayNumber byte,
	active bool,
) error {
	if relayNumber < 1 || relayNumber > 8 {
		return fmt.Errorf("relay number %d is outside R1..R8", relayNumber)
	}
	payload, err := native.RelayPayload(relayNumber-1, active)
	if err != nil {
		return err
	}
	if active && relayNumber <= 4 {
		if err := client.requireMotionAllowed(ctx); err != nil {
			return err
		}
	}
	return client.runtime.Command(ctx, native.OpRelaySet, payload)
}

// SetMotionSide requests interlocked Up, Down, or Stop for side 1 or 2.
func (client *Client) SetMotionSide(
	ctx context.Context,
	sideNumber byte,
	motion RelayMotion,
) error {
	if sideNumber < 1 || sideNumber > 2 {
		return fmt.Errorf("motion side %d is outside 1..2", sideNumber)
	}
	payload, err := native.RelaySidePayload(
		sideNumber-1,
		byte(motion),
	)
	if err != nil {
		return err
	}
	if motion != RelayMotionStop {
		if err := client.requireMotionAllowed(ctx); err != nil {
			return err
		}
	}
	return client.runtime.Command(ctx, native.OpRelaySide, payload)
}

// ToggleRelay returns the newly requested state. It obtains a fresh status
// first; an R1..R4 off-to-on transition is rejected unless that same reply
// confirms the enclosure door is open.
func (client *Client) ToggleRelay(
	ctx context.Context,
	relayNumber byte,
) (bool, error) {
	if relayNumber < 1 || relayNumber > 8 {
		return false, fmt.Errorf(
			"relay number %d is outside R1..R8",
			relayNumber,
		)
	}
	status, err := client.runtime.RefreshStatus(ctx)
	if err != nil {
		return false, fmt.Errorf("query relay state for toggle: %w", err)
	}
	active := status.ActiveRelays&(1<<(relayNumber-1)) == 0
	if active && relayNumber <= 4 {
		if err := client.checkMotionDoorPolicy(status); err != nil {
			return false, err
		}
	}
	payload, err := native.RelayPayload(relayNumber-1, active)
	if err != nil {
		return false, err
	}
	if err := client.runtime.Command(ctx, native.OpRelaySet, payload); err != nil {
		return false, err
	}
	return active, nil
}

func (client *Client) requireMotionAllowed(ctx context.Context) error {
	policy := client.motionDoorPolicy()
	if policy == "always" {
		return nil
	}
	if policy == "never" {
		return errors.New("motion command rejected by PC safety policy (never)")
	}
	status, err := client.runtime.RefreshStatus(ctx)
	if err != nil {
		return fmt.Errorf(
			"motion command rejected because the door policy could not be verified: %w",
			err,
		)
	}
	return client.checkMotionDoorPolicy(status)
}

func (client *Client) motionDoorPolicy() string {
	client.hostMu.RLock()
	defer client.hostMu.RUnlock()
	return normalizedMotionDoorPolicy(client.safety.MotionDoorPolicy)
}

func (client *Client) checkMotionDoorPolicy(status Status) error {
	switch policy := client.motionDoorPolicy(); policy {
	case "always":
		return nil
	case "open":
		if status.DoorOpen {
			return nil
		}
		return errors.New("motion command rejected while enclosure door is closed")
	case "closed":
		if !status.DoorOpen {
			return nil
		}
		return errors.New("motion command rejected while enclosure door is open")
	case "never":
		return errors.New("motion command rejected by PC safety policy (never)")
	default:
		return fmt.Errorf("motion command rejected: unknown PC safety policy %q", policy)
	}
}

// AllRelaysOff immediately requests the firmware's all-relays-off path.
func (client *Client) AllRelaysOff(ctx context.Context) error {
	return client.runtime.Command(ctx, native.OpRelayAllOff, nil)
}

// SetPWMChannel controls a native logical PWM channel 0..15 at 0..4095.
func (client *Client) SetPWMChannel(
	ctx context.Context,
	channel byte,
	value uint16,
) error {
	payload, err := native.PWMSetPayload(channel, value)
	if err != nil {
		return err
	}
	return client.runtime.Command(ctx, native.OpPWMSet, payload)
}

// AllPWMOff requests zero for every logical PWM channel.
func (client *Client) AllPWMOff(ctx context.Context) error {
	return client.runtime.Command(ctx, native.OpPWMAllOff, nil)
}

// PWMValues requests all logical PWM values and controller availability.
func (client *Client) PWMValues(ctx context.Context) (PWMValues, error) {
	frame, err := client.runtime.Request(
		ctx,
		native.OpPWMGet,
		nil,
		native.OpPWMValues,
	)
	if err != nil {
		return PWMValues{}, err
	}
	return native.ParsePWMValues(frame.Payload)
}

// SetStatusRGB replaces the base status color and cancels an active overlay.
func (client *Client) SetStatusRGB(
	ctx context.Context,
	red, green, blue, brightness byte,
) error {
	client.outputs.OverrideStatusEffect()
	return client.outputs.SetStatusBase(ctx, red, green, blue, brightness)
}

// SetStatusRGBBase updates the host state-policy color without canceling a
// user-requested overlay effect. The scheduler restores it after the overlay.
func (client *Client) SetStatusRGBBase(
	ctx context.Context,
	red, green, blue, brightness byte,
) error {
	return client.outputs.SetStatusBase(ctx, red, green, blue, brightness)
}

// OutputState returns active melody and status-effect operation metadata.
func (client *Client) OutputState() OutputStreamState {
	return client.outputs.State()
}

// PlayTone sends one bounded buzzer tone after stopping streamed melody playback.
func (client *Client) PlayTone(
	ctx context.Context,
	frequencyHz, durationMS uint16,
) error {
	if durationMS == 0 {
		return errors.New("tone duration must be nonzero")
	}
	if frequencyHz != 0 && (frequencyHz < 20 || frequencyHz > 20000) {
		return fmt.Errorf("tone frequency must be 0 or 20..20000 Hz")
	}
	client.outputs.StopMelody()
	return client.runtime.Command(
		ctx,
		native.OpBuzzer,
		native.BuzzerPayload(frequencyHz, durationMS),
	)
}

// StartMelody begins asynchronous playback; zero repeats until StopMelody.
func (client *Client) StartMelody(
	ctx context.Context,
	melody Melody,
	repeats int,
) (OutputOperation, error) {
	operation, err := client.outputs.StartMelody(ctx, melody, repeats)
	return publicOutputOperation(operation), err
}

// ConfiguredMelodies returns the effective named melody catalog.
func (client *Client) ConfiguredMelodies() []Melody {
	return appconfig.EffectiveMelodies(client.currentHostConfig())
}

// StartConfiguredMelody begins a named melody; zero repeats until stopped.
func (client *Client) StartConfiguredMelody(
	ctx context.Context,
	name string,
	repeats int,
) (OutputOperation, error) {
	for _, melody := range client.ConfiguredMelodies() {
		if strings.EqualFold(strings.TrimSpace(melody.Name), strings.TrimSpace(name)) {
			return client.StartMelody(ctx, melody, repeats)
		}
	}
	return OutputOperation{}, fmt.Errorf("configured melody %q was not found", name)
}

// StopMelody cancels active host-streamed melody playback.
func (client *Client) StopMelody() bool {
	return client.outputs.StopMelody()
}

// StartStatusLEDEffect begins one asynchronous host-defined LED overlay.
func (client *Client) StartStatusLEDEffect(
	ctx context.Context,
	effect StatusLEDEffect,
) (OutputOperation, error) {
	operation, err := client.outputs.StartStatusEffect(ctx, effect)
	return publicOutputOperation(operation), err
}

// ConfiguredStatusLEDEffects returns the effective named LED-effect catalog.
func (client *Client) ConfiguredStatusLEDEffects() []StatusLEDEffect {
	return appconfig.EffectiveStatusLEDEffects(client.currentHostConfig())
}

// StartConfiguredStatusLEDEffect begins a named effect from the effective catalog.
func (client *Client) StartConfiguredStatusLEDEffect(
	ctx context.Context,
	name string,
) (OutputOperation, error) {
	for _, effect := range client.ConfiguredStatusLEDEffects() {
		if strings.EqualFold(strings.TrimSpace(effect.Name), strings.TrimSpace(name)) {
			return client.StartStatusLEDEffect(ctx, effect)
		}
	}
	return OutputOperation{}, fmt.Errorf(
		"configured status LED effect %q was not found",
		name,
	)
}

// StopStatusLEDEffect cancels the active host-defined LED overlay.
func (client *Client) StopStatusLEDEffect() bool {
	return client.outputs.StopStatusEffect()
}

// TransmitRF sends a validated 433 MHz code one or more times.
func (client *Client) TransmitRF(
	ctx context.Context,
	code uint32,
	bits, protocol byte,
	pulseUS uint16,
	repeats byte,
) error {
	if repeats == 0 {
		repeats = 1
	}
	if repeats > 20 {
		return fmt.Errorf("RF repeats %d is outside 1..20", repeats)
	}
	payload, err := native.RFTxPayload(code, bits, protocol, pulseUS)
	if err != nil {
		return err
	}
	for repeat := byte(0); repeat < repeats; repeat++ {
		if err := client.runtime.Command(ctx, native.OpRFTx, payload); err != nil {
			return err
		}
	}
	return nil
}

func publicOutputOperation(operation control.StreamOperation) OutputOperation {
	return OutputOperation{
		ID: operation.ID, Kind: operation.Kind,
		Name: operation.Name, Done: operation.Done,
	}
}

// BeginRFLearn starts the bounded timer-learning convenience form.
func (client *Client) BeginRFLearn(ctx context.Context, timeout time.Duration) error {
	_, err := client.runtime.StartRFLearning(ctx, control.RFLearnOptions{
		Mode: control.RFLearnTimer, Timeout: timeout,
	})
	return err
}

// StartRFLearning begins indefinite multi-code or bounded timer learning.
func (client *Client) StartRFLearning(
	ctx context.Context,
	options RFLearnOptions,
) (RFLearnState, error) {
	return client.runtime.StartRFLearning(ctx, options)
}

// RFLearningState returns the latest host-tracked learning lifecycle.
func (client *Client) RFLearningState() RFLearnState {
	return client.runtime.RFLearnState()
}

// CancelRFLearn ends the current learning session with an explicit reason.
func (client *Client) CancelRFLearn(ctx context.Context) error {
	return client.runtime.CancelRFLearning(ctx, "cancelled through API")
}

// RemoveLearnedRF clears one EEPROM-backed learned record by stable slot ID.
func (client *Client) RemoveLearnedRF(ctx context.Context, id byte) error {
	return client.runtime.Command(ctx, native.OpRFLearnRemove, []byte{id})
}

// ClearLearnedRF clears every EEPROM-backed learned RF record.
func (client *Client) ClearLearnedRF(ctx context.Context) error {
	return client.runtime.Command(ctx, native.OpRFLearnClear, nil)
}

// MapLearnedRF updates one learned slot while preserving motion safety rules.
func (client *Client) MapLearnedRF(
	ctx context.Context,
	id byte,
	mapping RFMapping,
) error {
	if mapping.Action == RFActionRelay && mapping.Value < 4 {
		return errors.New(
			"learned RF can map directly only to user relays R5..R8; " +
				"use RFActionSide up/down/stop for door-gated motion",
		)
	}
	payload, err := native.RFMappingPayload(
		id,
		byte(mapping.Action),
		mapping.Value,
		byte(mapping.Behavior),
	)
	if err != nil {
		return err
	}
	return control.NewRFReplaceService(client.runtime).UpdateMapping(
		ctx, payload[0], payload[1], payload[2], payload[3],
	)
}

// Snapshot returns the latest cached connection and board state without polling.
func (client *Client) Snapshot() Snapshot {
	snapshot := client.runtime.Snapshot()
	return Snapshot{
		Connected: snapshot.Connected,
		Paused:    snapshot.Paused,
		Port: PortInfo{
			Name:         snapshot.Port.Name,
			VID:          snapshot.Port.VID,
			PID:          snapshot.Port.PID,
			Product:      snapshot.Port.Product,
			Manufacturer: snapshot.Port.Manufacturer,
			SerialNumber: snapshot.Port.SerialNumber,
			FriendlyName: snapshot.Port.FriendlyName,
			InstanceID:   snapshot.Port.InstanceID,
		},
		Hello:             snapshot.Hello,
		Status:            snapshot.Status,
		Settings:          snapshot.Settings,
		HaveStatus:        snapshot.HaveStatus,
		HaveSettings:      snapshot.HaveSettings,
		StatusUpdated:     snapshot.StatusUpdated,
		ConnectionState:   snapshot.ConnectionState,
		ConnectionReason:  snapshot.ConnectionReason,
		ConnectionUpdated: snapshot.ConnectionUpdated,
		ProgramState:      snapshot.ProgramState,
		RFLearning:        snapshot.RFLearning,
		FrontPanel:        snapshot.FrontPanel,
		HaveFrontPanel:    snapshot.HaveFrontPanel,
		FrontPanelUpdated: snapshot.FrontPanelUpdated,
		StatusLED:         snapshot.StatusLED,
		HaveStatusLED:     snapshot.HaveStatusLED,
		StatusLEDUpdated:  snapshot.StatusLEDUpdated,
	}
}

// SetSegmentText presents static or scrolling text through the firmware's
// native display path. Text longer than four cells scrolls using dwell as its
// per-window speed; an empty value releases the host segment presentation.
func (client *Client) SetSegmentText(
	ctx context.Context,
	text string,
	dwell time.Duration,
) error {
	if dwell < 0 || dwell > time.Duration(^uint16(0))*time.Millisecond {
		return errors.New("segment dwell must fit 0..65535 milliseconds")
	}
	payload, err := native.DisplayTextPayload(
		native.DisplaySegments,
		uint16(dwell/time.Millisecond),
		text,
	)
	if err != nil {
		return err
	}
	return client.runtime.Command(ctx, native.OpDisplayText, payload)
}

// PresentDisplay sends an arbitrary segment/LCD message through the shared
// primary runtime. Long segment text scrolls automatically; callers can force
// a marquee and select once, explicit loop, or interval scheduling.
func (client *Client) PresentDisplay(
	ctx context.Context,
	request DisplayRequest,
) (DisplayResult, error) {
	return client.runtime.PresentDisplay(ctx, request)
}

// RefreshFrontPanel returns the board's exact cached display/LCD/key state and
// updates Snapshot for every UI consuming the shared primary instance.
func (client *Client) RefreshFrontPanel(ctx context.Context) (FrontPanel, error) {
	return client.runtime.RefreshFrontPanel(ctx)
}

// Events returns the lossy, bounded stream of normalized runtime events.
func (client *Client) Events() <-chan Event {
	return client.events
}

// NextEvent waits for the first matching event newer than afterID.
func (client *Client) NextEvent(
	ctx context.Context,
	afterID uint64,
	kind string,
) (Event, error) {
	event, err := client.runtime.WaitEvent(ctx, afterID, kind)
	if err != nil {
		return Event{}, err
	}
	return publicEvent(event), nil
}

// NextEventStream waits on activity, state, telemetry, or debug without
// allowing a high-rate stream to displace retained activity events.
func (client *Client) NextEventStream(
	ctx context.Context,
	afterID uint64,
	kind string,
	stream string,
) (Event, error) {
	event, err := client.runtime.WaitEventStreamFilter(ctx, afterID, kind, nil, stream)
	if err != nil {
		return Event{}, err
	}
	return publicEvent(event), nil
}

// NextOpcodeEvent waits for an unsolicited raw frame, optionally filtered by
// exact opcode. It is the long-polling counterpart to WebSocket/Socket.IO's
// `opcodes` subscription topic.
func (client *Client) NextOpcodeEvent(
	ctx context.Context,
	afterID uint64,
	kind string,
	opcode *byte,
) (Event, error) {
	event, err := client.runtime.WaitEventFilter(ctx, afterID, kind, opcode)
	if err != nil {
		return Event{}, err
	}
	return publicEvent(event), nil
}

// ExchangeOpcode sends one opaque UART request and waits for the caller's
// expected opcode. The default expectation is ACK, but experimental queries
// can name any response opcode without a host update.
func (client *Client) ExchangeOpcode(
	ctx context.Context,
	opcode byte,
	payload []byte,
	expectedOpcode byte,
) (OpcodeFrame, error) {
	if len(payload) > native.MaxPayload {
		return OpcodeFrame{}, native.ErrPayloadTooLong
	}
	frame, err := client.runtime.Request(ctx, opcode, payload, expectedOpcode)
	if err != nil {
		return OpcodeFrame{}, err
	}
	return OpcodeFrame{
		Opcode: frame.Opcode, Name: native.OpcodeName(frame.Opcode),
		Sequence: frame.Seq, Payload: append([]byte(nil), frame.Payload...),
		PayloadHex: strings.ToUpper(hex.EncodeToString(frame.Payload)),
	}, nil
}

// SendTextMessage validates and routes a typed host, bridge, board, or LCD message.
func (client *Client) SendTextMessage(
	ctx context.Context,
	message TextMessage,
) (Event, error) {
	message.Source = strings.ToLower(strings.TrimSpace(message.Source))
	message.Target = strings.ToLower(strings.TrimSpace(message.Target))
	message.Type = strings.ToLower(strings.TrimSpace(message.Type))
	message.Text = strings.TrimSpace(message.Text)
	message.Action = strings.TrimSpace(message.Action)
	if !oneOf(message.Source, "client", "server", "bridge", "board", "lcd", "host", "ipc", "rest", "webhook", "websocket", "socket_io") {
		return Event{}, fmt.Errorf("unsupported message source %q", message.Source)
	}
	if !oneOf(message.Target, "client", "server", "bridge", "board", "lcd", "host", "all") {
		return Event{}, fmt.Errorf("unsupported message target %q", message.Target)
	}
	if message.Type == "" || len(message.Type) > 32 {
		return Event{}, errors.New("message type must contain 1..32 characters")
	}
	for _, character := range message.Type {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= '0' && character <= '9') &&
			!strings.ContainsRune("._-", character) {
			return Event{}, errors.New("message type may contain a-z, 0-9, dot, dash, and underscore")
		}
	}
	if message.Text == "" && message.Line1 == "" && message.Line2 == "" {
		return Event{}, errors.New("message text or LCD lines are required")
	}
	if len(message.Text) > 4096 || len(message.Action) > 512 {
		return Event{}, errors.New("message text/action exceeds protocol limit")
	}
	if len(message.Metadata) > 64 {
		return Event{}, errors.New("message metadata exceeds 64 entries")
	}
	for key, value := range message.Metadata {
		if strings.TrimSpace(key) == "" || len(key) > 64 || len(value) > 1024 {
			return Event{}, errors.New("message metadata keys/values exceed limits")
		}
	}
	if message.Target == "lcd" || message.Target == "board" {
		line1, line2 := message.Line1, message.Line2
		if line1 == "" && line2 == "" {
			line1, line2 = splitLCDText(message.Text)
		}
		payload, err := native.DisplayTextPayload(
			native.DisplayLCD,
			0,
			lcdASCII(line1)+lcdASCII(line2),
		)
		if err != nil {
			return Event{}, err
		}
		if err := client.runtime.Command(ctx, native.OpDisplayText, payload); err != nil {
			return Event{}, err
		}
	}
	event := client.runtime.PublishStructuredEvent(control.Event{
		Kind: "message", Text: message.Text,
		Source: message.Source, Target: message.Target,
		MessageType: message.Type, Action: message.Action,
		Metadata: message.Metadata,
	})
	return publicEvent(event), nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func splitLCDText(value string) (string, string) {
	value = strings.ReplaceAll(value, "\r", "")
	parts := strings.SplitN(value, "\n", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	runes := []rune(value)
	if len(runes) <= 16 {
		return value, ""
	}
	return string(runes[:16]), string(runes[16:])
}

func lcdASCII(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if builder.Len() >= 16 {
			break
		}
		if character < 0x20 || character > 0x7E {
			character = '?'
		}
		builder.WriteRune(character)
	}
	for builder.Len() < 16 {
		builder.WriteByte(' ')
	}
	return builder.String()
}

// LatestEventID returns the most recently published runtime event ID.
func (client *Client) LatestEventID() uint64 {
	return client.runtime.LatestEventID()
}

// EmitHostEvent publishes an integration/lifecycle event without touching the
// serial transport. It is intended for embedders implementing webhooks,
// notifications, or other host-side bridges.
func (client *Client) EmitHostEvent(kind, text string) {
	client.runtime.PublishHostEvent(kind, text)
}

// EmitHostActionEvent publishes a source-tagged host integration event without
// touching the serial transport. Metadata is copied by the runtime.
func (client *Client) EmitHostActionEvent(
	kind, text, source, action string,
	metadata map[string]string,
) Event {
	event := client.runtime.PublishStructuredEvent(control.Event{
		Kind: kind, Text: text, Source: source, Action: action,
		Metadata: metadata,
	})
	return publicEvent(event)
}

// SyncToolchain updates installed cores/libraries and ensures every
// PCController dependency through the controller-owned runner.
// It never opens, resets, or programs a board.
func (client *Client) SyncToolchain(
	ctx context.Context,
	options ToolchainSyncOptions,
	output io.Writer,
) (ToolchainSyncReport, error) {
	if strings.TrimSpace(options.ToolchainCLI) == "" {
		options.ToolchainCLI = client.currentCommandOptions().ArduinoCLI
	}
	return programmer.SyncToolchain(ctx, options, output)
}

// BootstrapToolchain installs one resolved, profile-local firmware toolchain.
// It does not replace an unrelated global dependency installation. Callers may
// supply the latest resolved profile or an intentionally selected rollback lock.
func BootstrapToolchain(
	ctx context.Context,
	options ToolchainBootstrapOptions,
	output io.Writer,
) (ToolchainBootstrapReport, error) {
	return programmer.BootstrapToolchain(ctx, options, output)
}

// LoadToolchainPolicy reads the latest-compatible dependency policy.
func LoadToolchainPolicy(path string) (ToolchainPolicy, error) {
	return programmer.LoadToolchainPolicy(path)
}

// LoadToolchainLock reads an exact, hash-bearing resolved dependency lock.
func LoadToolchainLock(path string) (ToolchainLock, error) {
	return programmer.LoadToolchainLock(path)
}

// ResolveToolchainPolicy resolves stable dependencies without device I/O.
func ResolveToolchainPolicy(
	ctx context.Context,
	policy ToolchainPolicy,
	options ToolchainResolveOptions,
) (ToolchainResolution, error) {
	return programmer.ResolveToolchainPolicy(ctx, policy, options)
}

// CompareToolchainLocks reports substantive changes between exact locks.
func CompareToolchainLocks(current, resolved ToolchainLock) []ToolchainChange {
	return programmer.CompareToolchainLocks(current, resolved)
}

// UpdateToolchainLock atomically writes a changed exact lock without timestamp churn.
func UpdateToolchainLock(path string, current, resolved ToolchainLock) (bool, error) {
	return programmer.UpdateToolchainLock(path, current, resolved)
}

// SetBeforeDisconnectHook lets the primary host bridge release momentary or
// latched PC inputs before an intentional port close removes the UART session.
func (client *Client) SetBeforeDisconnectHook(hook func(string)) {
	client.runtime.SetBeforeDisconnect(hook)
}

// HostSystemStatus returns local OS/network data without touching the serial port.
func (client *Client) HostSystemStatus() (SystemStatus, error) {
	return hostos.Status(ports.EnumerationSource())
}

// PressVirtualKey emits one allowlisted Windows virtual key with a guaranteed release.
func (client *Client) PressVirtualKey(
	ctx context.Context,
	request VirtualKeyRequest,
) (OSActionResult, error) {
	policy := client.currentHostConfig().OSActions.VirtualKeys
	result, err := hostos.DefaultExecutor.PressVirtualKey(ctx, policy, request)
	if err != nil {
		client.EmitHostEvent("os.virtual-key.audit", "Go API denied: "+err.Error())
		return OSActionResult{}, err
	}
	client.EmitHostEvent("os.virtual-key.audit", "Go API "+result.Detail)
	return result, nil
}

// RequestPowerAction invokes a confirmed, allowlisted OS power action.
func (client *Client) RequestPowerAction(
	ctx context.Context,
	request PowerRequest,
) (OSActionResult, error) {
	request.Automation = false
	policy := client.currentHostConfig().OSActions.Power
	result, err := hostos.DefaultExecutor.Power(ctx, policy, request)
	if err != nil {
		client.EmitHostEvent("os.power.audit", "Go API denied: "+err.Error())
		return OSActionResult{}, err
	}
	client.EmitHostEvent("os.power.audit", "Go API "+result.Detail)
	return result, nil
}

// Temperatures lists detected DS18B20 identities, roles, and current readings.
func (client *Client) Temperatures(
	ctx context.Context,
	rescan bool,
) ([]TemperatureSensor, error) {
	var payload []byte
	if rescan {
		payload = []byte{1}
	}
	frame, err := client.runtime.Request(
		ctx,
		native.OpTemperatureList,
		payload,
		native.OpTemperatures,
	)
	if err != nil {
		return nil, err
	}
	return native.ParseTemperatures(frame.Payload)
}

// ListLearned retrieves every learned RF record using bounded pagination.
func (client *Client) ListLearned(ctx context.Context) ([]RFEntry, error) {
	cursor := byte(0)
	var result []RFEntry
	for pageNumber := 0; pageNumber < 86; pageNumber++ {
		frame, err := client.runtime.Request(
			ctx,
			native.OpRFLearnList,
			[]byte{cursor},
			native.OpRFEntries,
		)
		if err != nil {
			return nil, err
		}
		page, err := native.ParseRFEntries(frame.Payload)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Entries...)
		if page.NextCursor == 0xFF {
			return result, nil
		}
		if page.NextCursor == cursor {
			return nil, fmt.Errorf("RF list cursor did not advance from %d", cursor)
		}
		cursor = page.NextCursor
	}
	return nil, fmt.Errorf("RF list exceeded pagination safety limit")
}

// ListLearnedDetailed joins learned records with host names and categories.
func (client *Client) ListLearnedDetailed(ctx context.Context) ([]RFEntryView, error) {
	entries, err := client.ListLearned(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	config := client.RFPresentation()
	result := make([]RFEntryView, 0, len(entries))
	for _, entry := range entries {
		metadata, _ := config.MetadataFor(appconfig.RFCodeKey{
			Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol,
		})
		result = append(result, RFEntryView{
			ID: entry.ID, Code: entry.Code,
			CodeDisplay: appconfig.FormatRFCode(entry.Code, config.DisplayRadix),
			Bits:        entry.Bits, Protocol: entry.Protocol, PulseUS: entry.PulseUS,
			ActionKind: entry.ActionKind, ActionValue: entry.ActionValue,
			Behavior: entry.Behavior, Name: metadata.Name, Category: metadata.Category,
		})
	}
	return result, nil
}

// ListPorts enumerates serial candidates with available USB identity metadata.
func ListPorts() ([]PortInfo, error) {
	list, err := ports.List()
	if err != nil {
		return nil, err
	}
	result := make([]PortInfo, 0, len(list))
	for _, port := range list {
		result = append(result, PortInfo{
			Name:         port.Name,
			VID:          port.VID,
			PID:          port.PID,
			Product:      port.Product,
			Manufacturer: port.Manufacturer,
			SerialNumber: port.SerialNumber,
			FriendlyName: port.FriendlyName,
			InstanceID:   port.InstanceID,
		})
	}
	return result, nil
}

func publicPortInfo(info ports.Info) PortInfo {
	return PortInfo{
		Name: info.Name, VID: info.VID, PID: info.PID,
		Product: info.Product, Manufacturer: info.Manufacturer,
		SerialNumber: info.SerialNumber,
		FriendlyName: info.FriendlyName,
		InstanceID:   info.InstanceID,
	}
}

func (client *Client) forwardEvents() {
	for {
		var event control.Event
		select {
		case <-client.done:
			close(client.events)
			return
		case event = <-client.runtime.Events():
		}
		forwarded := publicEvent(event)
		select {
		case client.events <- forwarded:
		default:
			select {
			case <-client.events:
			default:
			}
			select {
			case client.events <- forwarded:
			default:
			}
		}
	}
}

func publicEvent(event control.Event) Event {
	forwarded := Event{
		ID: event.ID, Time: event.Time,
		Kind: event.Kind, Stream: event.Stream, Text: event.Text,
		Opcode: event.Frame.Opcode, Seq: event.Frame.Seq,
		Payload:   append([]byte(nil), event.Frame.Payload...),
		Lifecycle: event.Lifecycle,
		Port: PortInfo{
			Name: event.Port.Name, VID: event.Port.VID, PID: event.Port.PID,
			Product: event.Port.Product, Manufacturer: event.Port.Manufacturer,
			SerialNumber: event.Port.SerialNumber,
			FriendlyName: event.Port.FriendlyName,
			InstanceID:   event.Port.InstanceID,
		},
		Reason: event.Reason, State: event.State,
		Gesture: event.Gesture, Source: event.Source,
		Target: event.Target, MessageType: event.MessageType, Action: event.Action,
		Metadata: cloneStringMap(event.Metadata),
		RFCode:   event.RFCode, RFBits: event.RFBits,
		RFProtocol: event.RFProtocol, RFPulseUS: event.RFPulseUS,
		ResetCause: event.ResetCause, ResetCount: event.ResetCount,
	}
	if event.HaveRFID {
		id := event.RFID
		forwarded.RFID = &id
	}
	if event.Frame.Opcode == native.OpEvent {
		if parsed, err := native.ParseDeviceEvent(event.Frame.Payload); err == nil {
			forwarded.Device = &parsed
			if parsed.Type == native.EventKey {
				forwarded.Key = parsed.Key + 1
				forwarded.Gesture = control.NormalizeGesture(parsed.Gesture)
				forwarded.Source = map[byte]string{
					native.InputSourcePhysical: "physical",
					native.InputSourceRF:       "rf",
					native.InputSourceHost:     "host",
				}[parsed.Source]
				if parsed.SourceID != 0xFF {
					id := parsed.SourceID
					forwarded.SourceID = &id
				}
			}
			if parsed.Type == native.EventRFLearned {
				id := parsed.RFID
				forwarded.RFID = &id
			}
			if parsed.Type == native.EventRFReceived {
				forwarded.RFCode = parsed.RFCode
				forwarded.RFBits = parsed.RFBits
				forwarded.RFProtocol = parsed.RFProtocol
				forwarded.RFPulseUS = parsed.RFPulseUS
				if parsed.RFLearnedID != 0xFF {
					id := parsed.RFLearnedID
					forwarded.RFID = &id
				}
			}
		}
	}
	return forwarded
}
