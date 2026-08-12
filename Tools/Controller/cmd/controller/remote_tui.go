package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/tui"
)

// remoteTUIIPC is a read/command facade over controller JSON-RPC. It never
// constructs, scans, or opens a local serial runtime; the remote primary stays
// the sole owner of the board transport.
type remoteTUIIPC struct {
	address string
	auth    string

	events chan control.Event
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// remoteSettingsWire intentionally omits native.Settings.UnmarshalJSON. An
// offline primary has no authenticated settings yet and truthfully publishes
// zero motion_break_ms; that is valid snapshot state even though it is not a
// valid settings-write payload.
type remoteSettingsWire native.Settings

type remotePortWire struct {
	Name         string `json:"name"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	Product      string `json:"product,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	FriendlyName string `json:"friendly_name,omitempty"`
	InstanceID   string `json:"instance_id,omitempty"`
}

type remoteSnapshotWire struct {
	Connected         bool                         `json:"connected"`
	Paused            bool                         `json:"paused"`
	Port              remotePortWire               `json:"port"`
	Hello             native.Hello                 `json:"hello"`
	Status            native.Status                `json:"status"`
	Settings          remoteSettingsWire           `json:"settings"`
	HaveStatus        bool                         `json:"have_status"`
	HaveSettings      bool                         `json:"have_settings"`
	StatusUpdated     time.Time                    `json:"status_updated,omitempty"`
	ConnectionState   string                       `json:"connection_state"`
	ConnectionReason  string                       `json:"connection_reason,omitempty"`
	ConnectionUpdated time.Time                    `json:"connection_updated,omitempty"`
	FrontPanel        native.FrontPanel            `json:"front_panel"`
	HaveFrontPanel    bool                         `json:"have_front_panel"`
	FrontPanelUpdated time.Time                    `json:"front_panel_updated,omitempty"`
	StatusLED         native.StatusLEDState        `json:"status_led"`
	HaveStatusLED     bool                         `json:"have_status_led"`
	StatusLEDUpdated  time.Time                    `json:"status_led_updated,omitempty"`
	ProgramState      control.ProgramStateSnapshot `json:"program_state"`
	RFLearning        control.RFLearnState         `json:"rf_learning"`
}

type remoteUISettingsWire struct {
	AppTitle        string                  `json:"app_title"`
	Tagline         string                  `json:"tagline"`
	SetupComplete   bool                    `json:"setup_complete"`
	WelcomeMelody   string                  `json:"welcome_melody"`
	SegmentScroll   appconfig.SegmentScroll `json:"segment_scroll"`
	PeripheralNames map[string]string       `json:"peripheral_names"`
}

type remoteRFPresentationWire struct {
	Config appconfig.RFConfig `json:"config"`
}

func newRemoteTUIIPC(parent context.Context, address, auth string) *remoteTUIIPC {
	ctx, cancel := context.WithCancel(parent)
	client := &remoteTUIIPC{
		address: strings.TrimSpace(address), auth: auth,
		events: make(chan control.Event, 128), cancel: cancel, done: make(chan struct{}),
	}
	go client.pollEvents(ctx)
	return client
}

func (client *remoteTUIIPC) Close() {
	client.once.Do(func() { client.cancel() })
	<-client.done
}

func (client *remoteTUIIPC) call(
	ctx context.Context,
	method string,
	params any,
	target any,
) error {
	return callPrimaryAtAuthenticated(ctx, client.address, client.auth, method, params, target)
}

func (client *remoteTUIIPC) Snapshot(ctx context.Context) (control.Snapshot, error) {
	var wire remoteSnapshotWire
	if err := client.call(ctx, "controller.snapshot", map[string]any{}, &wire); err != nil {
		return control.Snapshot{}, err
	}
	return control.Snapshot{
		Connected: wire.Connected, Paused: wire.Paused,
		Port: ports.Info{
			Name: wire.Port.Name, IsUSB: wire.Port.VID != "" || wire.Port.PID != "",
			VID: wire.Port.VID, PID: wire.Port.PID, Product: wire.Port.Product,
			Manufacturer: wire.Port.Manufacturer, SerialNumber: wire.Port.SerialNumber,
			FriendlyName: wire.Port.FriendlyName, InstanceID: wire.Port.InstanceID,
		},
		Hello: wire.Hello, Status: wire.Status, Settings: native.Settings(wire.Settings),
		HaveStatus: wire.HaveStatus, HaveSettings: wire.HaveSettings,
		StatusUpdated: wire.StatusUpdated, ConnectionState: wire.ConnectionState,
		ConnectionReason: wire.ConnectionReason, ConnectionUpdated: wire.ConnectionUpdated,
		FrontPanel: wire.FrontPanel, HaveFrontPanel: wire.HaveFrontPanel,
		FrontPanelUpdated: wire.FrontPanelUpdated, StatusLED: wire.StatusLED,
		HaveStatusLED: wire.HaveStatusLED, StatusLEDUpdated: wire.StatusLEDUpdated,
		ProgramState: wire.ProgramState, RFLearning: wire.RFLearning,
	}, nil
}

func (client *remoteTUIIPC) Execute(ctx context.Context, command string) (string, error) {
	var result struct {
		Output string `json:"output"`
	}
	err := client.call(
		ctx,
		"controller.command.execute",
		map[string]string{"command": command},
		&result,
	)
	return result.Output, err
}

func (client *remoteTUIIPC) CommandEngine(ctx context.Context) (*shell.Engine, error) {
	var catalog []shell.CommandDescriptor
	if err := client.call(ctx, "controller.command.catalog", map[string]any{}, &catalog); err != nil {
		return nil, err
	}
	engine := shell.New(200)
	for _, descriptor := range catalog {
		descriptor := descriptor
		command := shell.Command{
			Name: descriptor.Name, Aliases: append([]string(nil), descriptor.Aliases...),
			Usage: descriptor.Usage, Summary: descriptor.Summary, Group: descriptor.Group,
			Run: func(commandContext context.Context, args []string) (string, error) {
				words := append([]string{descriptor.Name}, args...)
				return client.Execute(commandContext, shell.Join(words))
			},
		}
		if err := engine.Register(command); err != nil {
			return nil, fmt.Errorf("register remote command %q: %w", descriptor.Name, err)
		}
	}
	if len(catalog) == 0 {
		return nil, errors.New("remote controller returned an empty command catalog")
	}
	return engine, nil
}

func (client *remoteTUIIPC) RFEntries(ctx context.Context) ([]native.RFEntry, error) {
	var result []native.RFEntry
	err := client.call(ctx, "controller.rf.list", map[string]any{}, &result)
	return result, err
}

func (client *remoteTUIIPC) UISettings(ctx context.Context) (remoteUISettingsWire, error) {
	var result remoteUISettingsWire
	err := client.call(ctx, "controller.ui.config.get", map[string]any{}, &result)
	return result, err
}

func (client *remoteTUIIPC) SaveUISettings(
	ctx context.Context,
	value appconfig.UI,
) (remoteUISettingsWire, error) {
	var result remoteUISettingsWire
	err := client.call(ctx, "controller.ui.config.set", map[string]any{
		"app_title":        value.AppTitle,
		"tagline":          value.Tagline,
		"setup_complete":   value.SetupComplete,
		"segment_scroll":   value.SegmentScroll,
		"peripheral_names": value.PeripheralNames,
	}, &result)
	return result, err
}

func (client *remoteTUIIPC) RFPresentation(ctx context.Context) (appconfig.RFConfig, error) {
	var result remoteRFPresentationWire
	err := client.call(ctx, "controller.rf.presentation", map[string]any{}, &result)
	return result.Config, err
}

func cloneRemoteNames(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func mergeRemoteHostUI(local appconfig.UI, remote remoteUISettingsWire) appconfig.UI {
	local.AppTitle = remote.AppTitle
	local.Tagline = remote.Tagline
	local.SetupComplete = remote.SetupComplete
	local.WelcomeMelody = remote.WelcomeMelody
	local.SegmentScroll = remote.SegmentScroll
	local.PeripheralNames = cloneRemoteNames(remote.PeripheralNames)
	return local
}

func (client *remoteTUIIPC) pollEvents(ctx context.Context) {
	defer close(client.done)
	defer close(client.events)
	var cursor uint64
	haveCursor := false
	for ctx.Err() == nil {
		if !haveCursor {
			requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			var latest struct {
				ID uint64 `json:"id"`
			}
			err := client.call(requestContext, "controller.event.latest", map[string]any{}, &latest)
			cancel()
			if err != nil {
				if !remoteRetry(ctx) {
					return
				}
				continue
			}
			cursor = latest.ID
			haveCursor = true
		}

		requestContext, cancel := context.WithTimeout(ctx, 4*time.Second)
		var event controllerapi.Event
		err := client.call(requestContext, "controller.event.next", map[string]any{
			"after_id": cursor, "stream": "activity", "timeout_ms": 2000,
		}, &event)
		cancel()
		if err != nil {
			// A bounded long poll normally ends without an event. Re-probe the
			// latest cursor so a restarted primary (whose IDs reset) recovers.
			probeContext, probeCancel := context.WithTimeout(ctx, 2*time.Second)
			var latest struct {
				ID uint64 `json:"id"`
			}
			probeErr := client.call(probeContext, "controller.event.latest", map[string]any{}, &latest)
			probeCancel()
			if probeErr == nil && latest.ID < cursor {
				cursor = latest.ID
			}
			continue
		}
		cursor = event.ID
		select {
		case client.events <- remoteControlEvent(event):
		case <-ctx.Done():
			return
		}
	}
}

func remoteRetry(ctx context.Context) bool {
	timer := time.NewTimer(500 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func remoteControlEvent(event controllerapi.Event) control.Event {
	return control.Event{
		ID: event.ID, Time: event.Time, Kind: event.Kind, Stream: event.Stream,
		Text: event.Text, Frame: native.Frame{Opcode: event.Opcode, Seq: event.Seq, Payload: event.Payload},
		Lifecycle: event.Lifecycle,
		Port: ports.Info{
			Name: event.Port.Name, IsUSB: event.Port.VID != "" || event.Port.PID != "",
			VID: event.Port.VID, PID: event.Port.PID, SerialNumber: event.Port.SerialNumber,
			Manufacturer: event.Port.Manufacturer, Product: event.Port.Product,
			FriendlyName: event.Port.FriendlyName, InstanceID: event.Port.InstanceID,
		},
		Reason: event.Reason, State: event.State, Gesture: event.Gesture,
		Source: event.Source, Target: event.Target, MessageType: event.MessageType,
		Action: event.Action, Metadata: event.Metadata,
		RFCode: event.RFCode, RFBits: event.RFBits, RFProtocol: event.RFProtocol,
		RFPulseUS: event.RFPulseUS, ResetCause: event.ResetCause, ResetCount: event.ResetCount,
	}
}

func runRemoteTUI(
	address, auth string,
	stdout io.Writer,
	store *appconfig.Store,
	consoleOptions *tuiConsoleOptions,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newRemoteTUIIPC(ctx, address, auth)
	defer client.Close()

	probeContext, probeCancel := context.WithTimeout(ctx, 8*time.Second)
	initial, err := client.Snapshot(probeContext)
	if err == nil {
		var engine *shell.Engine
		engine, err = client.CommandEngine(probeContext)
		if err == nil {
			probeCancel()
			remoteHostUI := remoteUISettingsWire{}
			haveRemoteHostUI := false
			remoteRF := appconfig.DefaultRFConfig()
			settingsContext, settingsCancel := context.WithTimeout(ctx, 4*time.Second)
			if value, settingsErr := client.UISettings(settingsContext); settingsErr == nil {
				remoteHostUI = value
				haveRemoteHostUI = true
			}
			if value, rfErr := client.RFPresentation(settingsContext); rfErr == nil {
				remoteRF = value
			}
			settingsCancel()

			clientUI := func() appconfig.UI {
				value := store.Current().UI
				if haveRemoteHostUI {
					value = mergeRemoteHostUI(value, remoteHostUI)
				}
				return value
			}
			saveClientUI := func(value appconfig.UI) error {
				// The remote host identity, display names, and segment policy are
				// never copied into this client's config as a side effect of an
				// appearance/terminal edit.
				current := store.Current().UI
				value.AppTitle = current.AppTitle
				value.Tagline = current.Tagline
				value.WelcomeMelody = current.WelcomeMelody
				value.SegmentScroll = current.SegmentScroll
				value.PeripheralNames = cloneRemoteNames(current.PeripheralNames)
				_, updateErr := store.UpdateUI(value)
				return updateErr
			}
			var saveRemoteHostUI func(appconfig.UI) error
			if haveRemoteHostUI {
				saveRemoteHostUI = func(value appconfig.UI) error {
					saveContext, saveCancel := context.WithTimeout(ctx, 5*time.Second)
					defer saveCancel()
					updated, updateErr := client.SaveUISettings(saveContext, value)
					if updateErr == nil {
						remoteHostUI = updated
					}
					return updateErr
				}
			}
			dummyRuntime := control.New(control.Options{})
			defer dummyRuntime.Close()
			program := tea.NewProgram(
				tui.NewApplicationWithOptions(dummyRuntime, engine, tui.Options{
					// Appearance and terminal preferences belong to this client and
					// are intentionally saved locally. Host integrations and RF
					// presentation data belong to the remote primary; until their
					// structured IPC mutations exist, leave those save hooks nil so
					// the full TUI reports the capability as unavailable instead of
					// silently changing this SSH client's unrelated configuration.
					UIConfig: clientUI,
					SaveUI:   saveClientUI,
					ApplyTUIConsole: func(value appconfig.TUIConsole) error {
						settings := consoleOptions.resolve(value)
						result, applyErr := consolewindow.Apply(settings)
						if applyErr != nil {
							return applyErr
						}
						if !result.Applied && settings.Enabled {
							return errors.New(result.Reason)
						}
						return nil
					},
					RFConfig: func() appconfig.RFConfig { return remoteRF },
					RFFetch:  client.RFEntries,
					Integrations: func() hostui.IntegrationStatus {
						return remoteTUIIntegrationStatus(address)
					},
					InstanceID: "remote-tui:" + strings.TrimSpace(address),
					WriteOSC:   func(payload string) error { return hostui.WriteOSC(stdout, payload) },
					Remote: &tui.RemoteBackend{
						Endpoint: strings.TrimSpace(address), InitialSnapshot: initial,
						Snapshot: client.Snapshot, Events: client.events,
						SaveHostUI: saveRemoteHostUI,
					},
					DisableWelcome: true,
				}),
				tea.WithAltScreen(),
				tea.WithMouseCellMotion(),
			)
			_, runErr := program.Run()
			_ = hostui.WriteOSC(stdout, "9;4;0;0")
			return runErr
		}
	}
	probeCancel()
	if err != nil {
		return fmt.Errorf("attach full TUI to IPC %s: %w", strings.TrimSpace(address), err)
	}
	return nil
}

func remoteTUIIntegrationStatus(address string) hostui.IntegrationStatus {
	endpoint := strings.TrimSpace(address)
	return hostui.IntegrationStatus{
		Messaging: hostui.ServiceStatus{
			Name: "Remote JSON-RPC IPC", Enabled: true, State: "running", Endpoint: endpoint,
			Detail: "full Bubble Tea client; serial remains owned by remote primary",
		},
		Discovery: hostui.ServiceStatus{
			Name: "mDNS + NetBIOS resolver", Enabled: true, State: "running", Endpoint: endpoint,
		},
	}
}
