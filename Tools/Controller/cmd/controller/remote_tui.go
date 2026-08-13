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
	callFn  func(context.Context, string, any, any) error
	retry   time.Duration

	events    chan control.Event
	ready     chan struct{}
	readyOnce sync.Once
	sessions  chan struct{}
	cancel    context.CancelFunc
	done      chan struct{}
	once      sync.Once
}

const remoteTUIInstanceLeaseSeconds = 45

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
	AppTitle               string                  `json:"app_title"`
	Tagline                string                  `json:"tagline"`
	SetupComplete          bool                    `json:"setup_complete"`
	WelcomeMelody          string                  `json:"welcome_melody"`
	StatusIntervalMS       int                     `json:"status_interval_ms"`
	MeasurementFreshnessMS int                     `json:"measurement_freshness_ms"`
	SegmentScroll          appconfig.SegmentScroll `json:"segment_scroll"`
	PeripheralNames        map[string]string       `json:"peripheral_names"`
}

type remoteRFPresentationWire struct {
	Config appconfig.RFConfig `json:"config"`
}

func newRemoteTUIIPC(parent context.Context, address, auth string) *remoteTUIIPC {
	ctx, cancel := context.WithCancel(parent)
	client := &remoteTUIIPC{
		address: strings.TrimSpace(address), auth: auth,
		events: make(chan control.Event, 128), ready: make(chan struct{}),
		sessions: make(chan struct{}, 1),
		cancel:   cancel, done: make(chan struct{}),
	}
	go client.pollEvents(ctx)
	return client
}

func (client *remoteTUIIPC) WaitEventCursor(ctx context.Context) error {
	if client == nil || client.ready == nil {
		return errors.New("remote TUI event cursor is unavailable")
	}
	select {
	case <-client.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
	if client.callFn != nil {
		return client.callFn(ctx, method, params, target)
	}
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
		"app_title":                value.AppTitle,
		"tagline":                  value.Tagline,
		"setup_complete":           value.SetupComplete,
		"status_interval_ms":       value.StatusIntervalMS,
		"measurement_freshness_ms": value.MeasurementFreshnessMS,
		"segment_scroll":           value.SegmentScroll,
		"peripheral_names":         value.PeripheralNames,
	}, &result)
	return result, err
}

func (client *remoteTUIIPC) RFPresentation(ctx context.Context) (appconfig.RFConfig, error) {
	var result remoteRFPresentationWire
	err := client.call(ctx, "controller.rf.presentation", map[string]any{}, &result)
	return result.Config, err
}

func (client *remoteTUIIPC) ReportInstance(
	ctx context.Context,
	instance hostui.AppInstance,
) (hostui.AppInstance, error) {
	var result hostui.AppInstance
	err := client.call(ctx, "controller.app.instance.report", instance, &result)
	return result, err
}

func (client *remoteTUIIPC) CommitNavigation(
	ctx context.Context, command hostui.NavigationCommand,
) (hostui.NavigationOutcome, error) {
	var result hostui.NavigationOutcome
	err := client.call(ctx, "controller.app.navigation.commit", command, &result)
	return result, err
}

func (client *remoteTUIIPC) RemoveInstance(ctx context.Context, id string) error {
	return client.call(
		ctx, "controller.app.instance.remove", map[string]string{"id": id}, nil,
	)
}

type remoteTUIInstanceLease struct {
	client   *remoteTUIIPC
	reporter *hostui.NavigationReporter
	self     hostui.InstanceSelf
	refresh  time.Duration

	mu       sync.Mutex
	reportMu sync.Mutex
	page     string
	title    string
	have     bool
	joined   bool
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
}

func newRemoteTUIInstanceLease(
	client *remoteTUIIPC,
	reporter *hostui.NavigationReporter,
	refresh time.Duration,
) *remoteTUIInstanceLease {
	if refresh <= 0 {
		refresh = 15 * time.Second
	}
	lease := &remoteTUIInstanceLease{
		client: client, reporter: reporter, self: hostui.CurrentProcessSelf(time.Now()),
		refresh: refresh, stop: make(chan struct{}), done: make(chan struct{}),
	}
	go lease.run()
	return lease
}

func (lease *remoteTUIInstanceLease) Update(page, title string) error {
	if lease == nil || lease.client == nil || lease.reporter == nil {
		return errors.New("remote TUI instance reporting is unavailable")
	}
	lease.mu.Lock()
	previousPage, joined := lease.page, lease.joined
	lease.page = strings.ToLower(strings.TrimSpace(page))
	lease.title = strings.TrimSpace(title)
	lease.have = true
	lease.mu.Unlock()
	if err := lease.report(false); err != nil {
		return err
	}
	if !joined {
		lease.mu.Lock()
		lease.joined = true
		lease.mu.Unlock()
		return nil
	}
	if previousPage == lease.page {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	outcome, err := lease.client.CommitNavigation(ctx, hostui.NavigationCommand{
		Group: hostui.DefaultNavigationGroup, Source: lease.reporter.InstanceID(), Page: lease.page,
		OperationID: lease.reporter.NextOperationID(),
	})
	if err != nil {
		return err
	}
	lease.mu.Lock()
	lease.page = outcome.Page
	lease.mu.Unlock()
	return nil
}

func (lease *remoteTUIInstanceLease) report(catchUp bool) error {
	lease.reportMu.Lock()
	defer lease.reportMu.Unlock()
	lease.mu.Lock()
	if !lease.have {
		lease.mu.Unlock()
		return nil
	}
	page, title := lease.page, lease.title
	lease.mu.Unlock()
	var values map[string]string
	if catchUp {
		values = lease.reporter.NextCatchUpValues()
	} else {
		values = lease.reporter.NextValues()
	}
	values["terminal_title"] = title
	values["terminal_osc"] = "enabled"
	values["terminal_progress"] = "osc-9-4"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := lease.client.ReportInstance(ctx, hostui.AppInstance{
		ID: lease.reporter.InstanceID(), Surface: "tui", Page: page, State: "active",
		LeaseSeconds: remoteTUIInstanceLeaseSeconds, Values: values, Self: &lease.self,
	})
	return err
}

func (lease *remoteTUIInstanceLease) run() {
	defer close(lease.done)
	ticker := time.NewTicker(lease.refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = lease.report(false)
		case <-lease.client.sessions:
			_ = lease.report(true)
		case <-lease.stop:
			return
		}
	}
}

func (lease *remoteTUIInstanceLease) Close() {
	if lease == nil {
		return
	}
	lease.once.Do(func() { close(lease.stop) })
	<-lease.done
	if lease.client == nil || lease.reporter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = lease.client.RemoveInstance(ctx, lease.reporter.InstanceID())
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
	local.StatusIntervalMS = remote.StatusIntervalMS
	local.MeasurementFreshnessMS = remote.MeasurementFreshnessMS
	local.SegmentScroll = remote.SegmentScroll
	local.PeripheralNames = cloneRemoteNames(remote.PeripheralNames)
	return local
}

func (client *remoteTUIIPC) pollEvents(ctx context.Context) {
	defer close(client.done)
	defer close(client.events)
	var cursor uint64
	haveCursor := false
	connectedOnce := false
	resetOnConnect := false
	for ctx.Err() == nil {
		if !haveCursor {
			requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
			var latest struct {
				ID uint64 `json:"id"`
			}
			err := client.call(requestContext, "controller.event.latest", map[string]any{}, &latest)
			cancel()
			if err != nil {
				if connectedOnce {
					resetOnConnect = true
				}
				if !client.remoteRetry(ctx) {
					return
				}
				continue
			}
			reconnected := connectedOnce && resetOnConnect
			if reconnected {
				if !client.emitSessionReset(ctx) {
					return
				}
			}
			cursor = latest.ID
			haveCursor = true
			connectedOnce = true
			resetOnConnect = false
			client.readyOnce.Do(func() { close(client.ready) })
			if reconnected {
				client.notifySession()
			}
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
			if probeErr != nil {
				// Transport/authentication failures are not long-poll timeouts.
				// Re-enter discovery after a bounded delay instead of spinning
				// RPC, sticky-name resolution, and Bubble Tea goroutines.
				haveCursor = false
				resetOnConnect = connectedOnce
				if !client.remoteRetry(ctx) {
					return
				}
				continue
			}
			if probeErr == nil && latest.ID < cursor {
				if !client.emitSessionReset(ctx) {
					return
				}
				cursor = latest.ID
				client.notifySession()
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

func (client *remoteTUIIPC) notifySession() {
	if client == nil || client.sessions == nil {
		return
	}
	select {
	case client.sessions <- struct{}{}:
	default:
	}
}

func (client *remoteTUIIPC) emitSessionReset(ctx context.Context) bool {
	select {
	case client.events <- control.Event{
		Kind: "client.navigation.session.reset", Source: "remote-ipc", Target: "tui",
	}:
		return true
	case <-ctx.Done():
		return false
	}
}

func (client *remoteTUIIPC) remoteRetry(ctx context.Context) bool {
	delay := client.retry
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}
	timer := time.NewTimer(delay)
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
	syncNavigation bool,
) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := newRemoteTUIIPC(ctx, address, auth)
	defer client.Close()
	navigationReporter, err := hostui.NewNavigationReporter(syncNavigation, "")
	if err != nil {
		return fmt.Errorf("create remote TUI instance identity: %w", err)
	}
	instanceLease := newRemoteTUIInstanceLease(client, navigationReporter, 0)
	defer instanceLease.Close()

	probeContext, probeCancel := context.WithTimeout(ctx, 8*time.Second)
	initial, err := client.Snapshot(probeContext)
	initialReceivedAt := time.Now()
	if err == nil {
		var engine *shell.Engine
		engine, err = client.CommandEngine(probeContext)
		if err == nil {
			if syncNavigation {
				cursorContext, cursorCancel := context.WithTimeout(ctx, 3*time.Second)
				cursorErr := client.WaitEventCursor(cursorContext)
				cursorCancel()
				if cursorErr != nil {
					probeCancel()
					return fmt.Errorf("establish remote TUI navigation event cursor: %w", cursorErr)
				}
			}
			probeCancel()
			remoteHostUI := remoteUISettingsWire{}
			haveRemoteHostUI := false
			var remoteHostUIMu sync.RWMutex
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
				remoteHostUIMu.RLock()
				defer remoteHostUIMu.RUnlock()
				if haveRemoteHostUI {
					value = mergeRemoteHostUI(value, remoteHostUI)
				}
				return value
			}
			loadRemoteHostUI := func(loadContext context.Context) (appconfig.UI, error) {
				updated, loadErr := client.UISettings(loadContext)
				if loadErr != nil {
					return appconfig.UI{}, loadErr
				}
				remoteHostUIMu.Lock()
				remoteHostUI = updated
				haveRemoteHostUI = true
				remoteHostUIMu.Unlock()
				return mergeRemoteHostUI(store.Current().UI, updated), nil
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
						remoteHostUIMu.Lock()
						remoteHostUI = updated
						haveRemoteHostUI = true
						remoteHostUIMu.Unlock()
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
					InstanceID:      navigationReporter.InstanceID(),
					NavigationSync:  syncNavigation,
					NavigationGroup: hostui.DefaultNavigationGroup,
					ReportTerminal:  instanceLease.Update,
					WriteOSC:        func(payload string) error { return hostui.WriteOSC(stdout, payload) },
					Remote: &tui.RemoteBackend{
						Endpoint:                  strings.TrimSpace(address),
						InitialSnapshot:           initial,
						InitialSnapshotReceivedAt: initialReceivedAt,
						Snapshot:                  client.Snapshot, Events: client.events,
						SaveHostUI: saveRemoteHostUI,
						LoadHostUI: loadRemoteHostUI,
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
