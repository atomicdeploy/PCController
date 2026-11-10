// Package presentation defines the controller presentation seam. No serial,
// device HTTP, board ownership or authentication implementation lives here.
package presentation

import (
	"context"
	"time"
)

type Snapshot struct {
	ObservedAt                                          time.Time
	Generation                                          uint64
	Preview                                             bool
	CatalogRevision                                     string
	ControlOwner                                        ControlOwner
	Channels                                            []Channel
	Seats                                               []Seat
	Preferences                                         *Preferences
	HostMenu                                            []MenuItem
	TrayIcon, TrayTooltip                               string
	Connected, HavePanel, HaveStatus, Busy, CanNavigate bool
	ProfileAttached, ProfileConfigured                  bool
	HavePower, HaveTemperature                          bool
	Segments                                            [4]byte
	Brightness, PressedKeys                             byte
	LCDLine1, LCDLine2, Mode, Endpoint, Error           string
	SupplyMV, CurrentMA                                 int32
	TemperatureC                                        float64
	BusMV, PowerMW                                      int32
	TemperatureBTC                                      float64
	HaveTemperatureBT                                   bool
}

type Options struct {
	Title, ConfigPath string
	// WindowRequests are local presentation commands, never hardware actions.
	WindowRequests <-chan WindowRequest
	StartHidden    bool
	HideOnClose    bool   // Combined TUI sessions keep running when the GUI closes.
	PreviewPage    string // Offline-only capture entry point; never performs an action.
	PageChanges    <-chan struct{}
	PageVersion    func(string) uint64
	Updates        <-chan Snapshot
	Messages       <-chan Message
	Execute        func(context.Context, string) error
	OpenPage       func(string) error
	InvokeHostMenu func(context.Context, string) error
	ShowConsole    func() error
	ConsoleVisible func() bool
	Notify         func(context.Context, string, string, string) error
	Log            func(string, ...any)
	InvokeAction   func(context.Context, ControlOwner, string) error
	SetPWM         func(context.Context, ControlOwner, string, float64) error
	UpdateChannel  func(context.Context, string, string, map[string]any) error
	LoadPage       func(context.Context, string) (Page, error)
	SubmitPage     func(context.Context, string, string, map[string]string) (Page, error)
	MotionHold     func(context.Context, string, MotionHoldLease, string) (MotionHoldLease, error)
}

type WindowRequest struct {
	Action string // show, hide or toggle
	Result chan<- error
}

// Message is an already accepted host message, not an executable command.
type Message struct{ Text, Source string }

type Field struct {
	Key, Label, Value string
	Section           string
	Numeric           bool
	Min, Max          float64
	Choices           []string
	ChoiceLabels      []string
	ReadOnly          bool
	Hidden, Multiline bool
	Decimals          int
	FileFilter        string
	SaveFile          bool
	IconPicker        bool
	OptionalNumeric   bool
	WhenKey           string
	WhenValues        []string
}
type PageAction struct {
	ID, Label         string
	Section           string
	TimeoutSeconds    int
	Enabled, Confirm  bool
	SelectionRequired bool
	DiscardChanges    bool
	RequiresChanges   bool
	SelectionOffset   int // Non-zero requires an adjacent selected row (reorder).
}
type Page struct {
	Title   string
	Fields  []Field
	Columns []string
	Rows    [][]string
	Actions []PageAction
	// RowValues binds editors to stable row identities, never visual indices.
	RowValues map[string]map[string]string
	// DraftBaseline retains the authoritative values of drafts carried across
	// an unrelated successful action, so dismissal still warns about lost edits.
	DraftBaseline  map[string]string
	Output         string
	Owner          string
	Receipts       map[string]RFReceipt
	ClearSelection bool
	SelectedRow    string
	Timeline       []TimelineCue
}

type TimelineCue struct {
	Key, Label, Lane string
	AtUS             uint32
	DurationMS       uint16
}

type RFReceipt struct {
	Identity           string
	Count              uint64
	LastSeen           time.Time
	Learned, Connected bool
}

type Seat struct {
	Key, Name, State, StopAction string
	Side                         byte
	Known, Locked                bool
}

type Preferences struct {
	Revision, Theme, Locale                                                                          string
	ReduceMotion, ShowGraphs                                                                         bool
	ShowSupplyVoltage, ShowBusVoltage, ShowCurrent, ShowPower, ShowTemperatureLED, ShowTemperatureBT bool
	VoltageDecimals, CurrentDecimals, PowerDecimals, TemperatureDecimals                             int
}
type MotionHoldLease struct {
	ID         string    `json:"id"`
	Side       byte      `json:"side"`
	Generation uint64    `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Captured with the displayed controls, not read again after queuing input.
type ControlOwner struct {
	HostInstanceID string `json:"expected_host_instance_id"`
	Generation     uint64 `json:"expected_connection_generation"`
	BoardIdentity  string `json:"board_identity"`
	Revision       string `json:"expected_revision"`
	PWMRevision    string `json:"expected_pwm_configuration_revision"`
}

type Channel struct {
	Key, Name, Kind, State string
	Icon                   string
	Order                  int
	Known, Locked          bool
	Percent                float64
	Actions                []Action
}
type Action struct{ ID, Label, Verb string }

// MenuItem carries the existing host tray's canonical actions into any renderer.
type MenuItem struct {
	ID, Label                    string
	Disabled, Separator, Default bool
}
