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
	Channels                                            []Channel
	HostMenu                                            []MenuItem
	TrayIcon, TrayTooltip                               string
	Connected, HavePanel, HaveStatus, Busy, CanNavigate bool
	HavePower, HaveTemperature                          bool
	Segments                                            [4]byte
	Brightness, PressedKeys                             byte
	LCDLine1, LCDLine2, Mode, Endpoint, Error           string
	SupplyMV, CurrentMA                                 int32
	TemperatureC                                        float64
}

type Options struct {
	Title, ConfigPath string
	PreviewPage       string // Offline-only capture entry point; never performs an action.
	PageChanges       <-chan struct{}
	Updates           <-chan Snapshot
	Messages          <-chan Message
	Execute           func(context.Context, string) error
	OpenPage          func(string) error
	InvokeHostMenu    func(context.Context, string) error
	ShowConsole       func() error
	ConsoleVisible    func() bool
	Notify            func(context.Context, string, string, string) error
	Log               func(string, ...any)
	InvokeAction      func(context.Context, string) error
	SetPWM            func(context.Context, string, float64) error
	UpdateChannel     func(context.Context, string, string, map[string]any) error
	LoadPage          func(context.Context, string) (Page, error)
	SubmitPage        func(context.Context, string, string, map[string]string) (Page, error)
}

// Message is an already accepted host message, not an executable command.
type Message struct{ Text, Source string }

type Field struct {
	Key, Label, Value string
	Section           string
	Numeric           bool
	Min, Max          float64
	Choices           []string
	ReadOnly          bool
}
type PageAction struct {
	ID, Label        string
	Enabled, Confirm bool
}
type Page struct {
	Title   string
	Fields  []Field
	Columns []string
	Rows    [][]string
	Actions []PageAction
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
