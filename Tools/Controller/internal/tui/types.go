package tui

import (
	"context"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/portowner"
	"pccontroller.local/controller/internal/productidentity"
)

type Page int

const (
	PageDashboard Page = iota
	PageOutputs
	PageMenus
	PageBoardSettings
	PageAppSettings
	PageRF
	PageProgramming
	PageAutomations
	PageEvents
	PageConsole
	pageCount
)

type pageDefinition struct {
	Key   string
	Short string
	Title string
}

var pageDefinitions = [...]pageDefinition{
	{"1", "Dashboard", "Dashboard & Live Measurements"},
	{"2", "Control", "Relays, Motion & PWM Control"},
	{"3", "Menus", "Board Display Menus"},
	{"4", "Board", "Board EEPROM Settings"},
	{"5", "App", "HOST Settings"},
	{"6", "RF", "433 MHz Learn & Mapping"},
	{"7", "Program", "Programming & Urboot/Urclock"},
	{"8", "Automate", "Automations & Macros"},
	{"9", "Events", "History, Graphs & Timeline"},
	{"0", "Console", "Command Console"},
}

type Preferences struct {
	AppTitle            string
	Tagline             string
	PollInterval        time.Duration
	EventLogLimit       int
	HistoryWindow       time.Duration
	VoltageDecimals     int
	CurrentDecimals     int
	PowerDecimals       int
	TemperatureDecimals int
	Visible             map[string]bool
}

func defaultPreferences() Preferences {
	return Preferences{
		AppTitle:            productidentity.Title(""),
		Tagline:             productidentity.DefaultFirstRunLine(),
		PollInterval:        250 * time.Millisecond,
		EventLogLimit:       500,
		HistoryWindow:       6 * time.Hour,
		VoltageDecimals:     2,
		CurrentDecimals:     1,
		PowerDecimals:       2,
		TemperatureDecimals: 2,
		Visible: map[string]bool{
			"supply": true, "bus": true, "current": true, "power": true,
			"temperature_led": true, "temperature_bt": true,
			"io": true, "diagnostics": true, "graphs": true,
		},
	}
}

func preferencesFromUI(value appconfig.UI) Preferences {
	result := defaultPreferences()
	result.AppTitle = productidentity.Title(value.AppTitle)
	result.Tagline = strings.TrimSpace(value.Tagline)
	if result.Tagline == "" {
		result.Tagline = productidentity.DefaultFirstRunLine()
	}
	if value.StatusIntervalMS >= 100 {
		result.PollInterval = time.Duration(value.StatusIntervalMS) * time.Millisecond
	}
	if value.EventLogLimit >= 50 {
		result.EventLogLimit = value.EventLogLimit
	}
	if value.HistoryHours > 0 {
		result.HistoryWindow = time.Duration(value.HistoryHours) * time.Hour
	}
	if value.VoltageDecimals >= 0 && value.VoltageDecimals <= 4 {
		result.VoltageDecimals = value.VoltageDecimals
	}
	if value.CurrentDecimals >= 0 && value.CurrentDecimals <= 4 {
		result.CurrentDecimals = value.CurrentDecimals
	}
	if value.PowerDecimals >= 0 && value.PowerDecimals <= 4 {
		result.PowerDecimals = value.PowerDecimals
	}
	if value.TemperatureDecimals >= 0 && value.TemperatureDecimals <= 2 {
		result.TemperatureDecimals = value.TemperatureDecimals
	}
	result.Visible = map[string]bool{
		"supply":          value.ShowSupplyVoltage,
		"bus":             value.ShowBusVoltage,
		"current":         value.ShowCurrent,
		"power":           value.ShowPower,
		"temperature_led": value.ShowTemperatureLED,
		"temperature_bt":  value.ShowTemperatureBT,
		"io":              value.ShowIO,
		"diagnostics":     value.ShowDiagnostics,
		"graphs":          value.ShowGraphs,
	}
	return result
}

type measurementSample struct {
	At          time.Time
	SupplyMV    int32
	BusMV       int32
	CurrentMA   int32
	PowerMW     int32
	TLEDCenti   int16
	TBTCenti    int16
	HaveSupply  bool
	HaveBus     bool
	HaveCurrent bool
	HavePower   bool
	HaveTLED    bool
	HaveTBT     bool
}

type timelineEntry struct {
	At        time.Time
	Kind      string
	Text      string
	Important bool
}

type FrontPanelState struct {
	Segments         string
	RawSegments      [4]byte
	HasRawSegments   bool
	DecimalMask      byte
	Blink            bool
	CategorySelector bool
	Brightness       byte
	LCDLine1         string
	LCDLine2         string
	LCDBacklight     bool
	MenuID           byte
	MenuName         string
	Submode          string
	PressedKeys      byte
	InputSource      string
	Exact            bool
	StatusLED        native.StatusLEDState
	HaveStatusLED    bool
}

// RemoteLiveUpdate is one coalescible high-rate patch from the primary host.
// Status measurements and the composed status-light frame share this bounded
// path so a slow terminal always renders the newest state instead of building
// an unbounded animation backlog.
type RemoteLiveUpdate struct {
	Status              native.Status
	HaveStatus          bool
	StatusUpdated       time.Time
	StatusReceivedAt    time.Time
	StatusLED           native.StatusLEDState
	HaveStatusLED       bool
	StatusLEDUpdated    time.Time
	StatusLEDReceivedAt time.Time
	ConnectionChange    bool
	Connected           bool
	Error               string
}

// RemoteBackend supplies the live board state and activity stream for a TUI
// attached to another controller host. Command execution deliberately remains
// on the injected shell.Engine so the remote command catalog, completion,
// history, prompt, and console are identical to a locally owned TUI.
//
// A remote backend never owns or scans a local serial port. Snapshot polling
// and Events are expected to travel over the authenticated controller IPC.
type RemoteBackend struct {
	Endpoint                  string
	InitialSnapshot           control.Snapshot
	InitialSnapshotReceivedAt time.Time
	Snapshot                  func(context.Context) (control.Snapshot, error)
	Events                    <-chan control.Event
	Live                      <-chan RemoteLiveUpdate
	// SetLiveInterval switches both producer measurement demand and the bounded
	// client-to-render flush between an active 20 Hz view and low-rate idle view.
	SetLiveInterval func(time.Duration)
	// SaveHostUI persists the host-owned UI subset (identity and peripheral
	// names) through the remote primary's structured IPC contract. Client
	// appearance and terminal preferences continue to use Options.SaveUI.
	SaveHostUI func(appconfig.UI) error
}

type Options struct {
	UIConfig           func() appconfig.UI
	SaveUI             func(appconfig.UI) error
	ApplyTUIConsole    func(appconfig.TUIConsole) error
	HostIntegrations   func() appconfig.Integrations
	SaveIntegrations   func(appconfig.Integrations) error
	BuzzerRuntime      func() appconfig.BuzzerRuntimeStatus
	RFConfig           func() appconfig.RFConfig
	SaveRF             func(appconfig.RFConfig) error
	RFFetch            func(context.Context) ([]native.RFEntry, error)
	RFApplyOrder       func(context.Context, []native.RFEntry) error
	RFReplaceSupport   func() control.RFReplaceSupport
	RFProbeReplace     func(context.Context) (control.RFReplaceSupport, error)
	HostMenus          *hostmenu.Manager
	PushHostPanel      func(hostmenu.Snapshot) error
	ReleaseHostPanel   func() error
	FrontPanel         func() FrontPanelState
	FrontPanelKey      func(key int, phase string) error
	MirrorLCD          func(line1, line2 string) error
	Integrations       func() hostui.IntegrationStatus
	Notifier           hostui.Notifier
	AppActions         <-chan hostui.AppAction
	InstanceID         string
	NavigationSync     bool
	NavigationGroup    string
	SetNavigationSync  func(bool)
	NavigationIdentity func() (string, uint64)
	ReportPage         func(string) error
	ReportTerminal     func(page, title string) error
	// ReportTerminalAsync keeps network-backed instance reporting out of the
	// Bubble Tea update loop. The callback owns coalescing and error delivery.
	ReportTerminalAsync func(page, title string)
	// CommitNavigation asynchronously submits a local page intent to the
	// coordinator. Presence reports and coordinator-applied pages must never
	// pass through this callback.
	CommitNavigation func(page string)
	WriteOSC         func(payload string) error
	AckAppAction     func(hostui.ActionAck) error
	Remote           *RemoteBackend
	Preview          *control.Snapshot
	ForceWelcome     bool
	DisableWelcome   bool
	MarkWelcomed     func()
	WelcomeMelody    func(context.Context) error
	PortOwnerActions portowner.Actions
	NetworkDiscovery func(context.Context) ([]discovery.Instance, error)
	OpenNetwork      func(string) error
	Debug            bool
}
