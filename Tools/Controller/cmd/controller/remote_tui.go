package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/lanresolver"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/tui"
)

// remoteTUIIPC is a read/command facade over controller JSON-RPC. It never
// constructs, scans, or opens a local serial runtime; the remote primary stays
// the sole owner of the board transport.
type remoteTUIIPC struct {
	ctx     context.Context
	address string
	auth    string
	callFn  func(context.Context, string, any, any) error
	retry   time.Duration

	events                chan control.Event
	live                  chan tui.RemoteLiveUpdate
	liveRaw               chan tui.RemoteLiveUpdate
	liveRates             chan time.Duration
	flushRates            chan time.Duration
	ready                 chan struct{}
	readyOnce             sync.Once
	sessions              chan struct{}
	cancel                context.CancelFunc
	done                  chan struct{}
	once                  sync.Once
	liveOnce              sync.Once
	liveDone              chan struct{}
	flushDone             chan struct{}
	liveMu                sync.Mutex
	liveRate              time.Duration
	lastStatus            native.Status
	haveLastStatus        bool
	lastStatusForwardedAt time.Time
	liveRequestID         uint64
	liveStateCursor       uint64
	liveRequestedAfter    uint64
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
		ctx: ctx, address: strings.TrimSpace(address), auth: auth,
		events: make(chan control.Event, 128), ready: make(chan struct{}),
		live: make(chan tui.RemoteLiveUpdate, 1), liveRaw: make(chan tui.RemoteLiveUpdate, 1),
		liveRates: make(chan time.Duration, 1), flushRates: make(chan time.Duration, 1),
		sessions: make(chan struct{}, 1),
		cancel:   cancel, done: make(chan struct{}), liveRate: 50 * time.Millisecond,
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
	if client.liveDone != nil {
		<-client.liveDone
	}
	if client.flushDone != nil {
		<-client.flushDone
	}
}

func (client *remoteTUIIPC) StartLive() {
	if client == nil {
		return
	}
	client.liveOnce.Do(func() {
		client.liveDone = make(chan struct{})
		client.flushDone = make(chan struct{})
		go client.pollLive(client.ctx)
		go client.flushLive(client.ctx)
	})
}

func (client *remoteTUIIPC) SetLiveInterval(interval time.Duration) {
	if client == nil {
		return
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	client.liveMu.Lock()
	if client.liveRate == interval {
		client.liveMu.Unlock()
		return
	}
	client.liveRate = interval
	client.liveMu.Unlock()
	select {
	case client.liveRates <- interval:
	default:
		select {
		case <-client.liveRates:
		default:
		}
		select {
		case client.liveRates <- interval:
		default:
		}
	}
	queueLatestDuration(client.flushRates, interval)
}

func queueLatestDuration(output chan time.Duration, value time.Duration) {
	select {
	case output <- value:
		return
	default:
	}
	select {
	case <-output:
	default:
	}
	select {
	case output <- value:
	default:
	}
}

func (client *remoteTUIIPC) currentLiveInterval() time.Duration {
	client.liveMu.Lock()
	defer client.liveMu.Unlock()
	return client.liveRate
}

func (client *remoteTUIIPC) nextLiveRequest() (uint64, uint64) {
	client.liveMu.Lock()
	defer client.liveMu.Unlock()
	client.liveRequestID++
	client.liveRequestedAfter = client.liveStateCursor
	return client.liveRequestID, client.liveStateCursor
}

func (client *remoteTUIIPC) acceptLiveAcknowledgement(latestID uint64) bool {
	client.liveMu.Lock()
	defer client.liveMu.Unlock()
	if latestID < client.liveRequestedAfter {
		// The primary restarted and its event epoch reset. Adopt the new retained
		// cursor; the caller refreshes one authoritative snapshot before accepting
		// subsequent state frames from this epoch.
		client.liveStateCursor = latestID
		client.liveRequestedAfter = latestID
		return true
	}
	return false
}

func (client *remoteTUIIPC) advanceLiveStateCursor(id uint64) bool {
	client.liveMu.Lock()
	defer client.liveMu.Unlock()
	if id != 0 && id <= client.liveStateCursor {
		return false
	}
	if id != 0 {
		client.liveStateCursor = id
	}
	return true
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

func (client *remoteTUIIPC) ReportInstance(
	ctx context.Context,
	instance hostui.AppInstance,
) (hostui.AppInstance, error) {
	var result hostui.AppInstance
	err := client.call(ctx, "controller.app.instance.report", instance, &result)
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
	lease.page = strings.ToLower(strings.TrimSpace(page))
	lease.title = strings.TrimSpace(title)
	lease.have = true
	lease.mu.Unlock()
	return lease.report(false)
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

type remoteLiveRead struct {
	data []byte
	err  error
}

type remoteLiveMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

func (client *remoteTUIIPC) pollLive(ctx context.Context) {
	defer close(client.liveDone)
	for ctx.Err() == nil {
		if err := client.liveSession(ctx); err != nil && ctx.Err() == nil {
			client.publishLive(tui.RemoteLiveUpdate{
				ConnectionChange: true, Connected: false, Error: err.Error(),
			})
			if !client.remoteRetry(ctx) {
				return
			}
		}
	}
}

func (client *remoteTUIIPC) flushLive(ctx context.Context) {
	defer close(client.flushDone)
	defer close(client.live)
	interval := client.currentLiveInterval()
	timer := time.NewTimer(interval)
	defer timer.Stop()
	var pending tui.RemoteLiveUpdate
	havePending := false
	for {
		select {
		case <-ctx.Done():
			return
		case update := <-client.liveRaw:
			mergeRemoteLiveUpdate(&pending, update)
			havePending = true
		case interval = <-client.flushRates:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(interval)
		case <-timer.C:
			if havePending {
				select {
				case client.live <- pending:
					pending = tui.RemoteLiveUpdate{}
					havePending = false
				default:
				}
			}
			timer.Reset(interval)
		}
	}
}

func (client *remoteTUIIPC) liveSession(ctx context.Context) error {
	target := url.URL{Scheme: "ws", Host: client.address, Path: "/ipc"}
	header := http.Header{}
	if strings.TrimSpace(client.auth) != "" {
		header.Set("Authorization", "Bearer "+client.auth)
	}
	connection, _, err := websocket.Dial(ctx, target.String(), &websocket.DialOptions{
		HTTPClient: lanresolver.HTTPClient(), HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(1024 * 1024)

	writeSubscription := func(interval time.Duration) (uint64, error) {
		requestID, afterID := client.nextLiveRequest()
		request := map[string]any{
			"jsonrpc": "2.0", "id": requestID,
			"method": "controller.subscribe",
			"params": map[string]any{
				"topics":      []string{"state", "status"},
				"interval_ms": interval.Milliseconds(),
				"after_id":    afterID,
			},
		}
		encoded, encodeErr := json.Marshal(request)
		if encodeErr != nil {
			return 0, encodeErr
		}
		writeContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		return requestID, connection.Write(writeContext, websocket.MessageText, encoded)
	}
	pendingRequestID, err := writeSubscription(client.currentLiveInterval())
	if err != nil {
		return err
	}
	currentInterval := client.currentLiveInterval()
	ackTimer := time.NewTimer(3 * time.Second)
	defer ackTimer.Stop()
	acknowledgement := ackTimer.C

	reads := make(chan remoteLiveRead, 1)
	go func() {
		for ctx.Err() == nil {
			messageType, data, readErr := connection.Read(ctx)
			if readErr == nil && messageType != websocket.MessageText {
				continue
			}
			select {
			case reads <- remoteLiveRead{data: data, err: readErr}:
			case <-ctx.Done():
			}
			if readErr != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case interval := <-client.liveRates:
			currentInterval = interval
			pendingRequestID, err = writeSubscription(interval)
			if err != nil {
				return err
			}
			if !ackTimer.Stop() {
				select {
				case <-ackTimer.C:
				default:
				}
			}
			ackTimer.Reset(3 * time.Second)
			acknowledgement = ackTimer.C
		case <-acknowledgement:
			return errors.New("remote live subscription acknowledgement timed out")
		case read := <-reads:
			if read.err != nil {
				return read.err
			}
			acknowledged, err := client.consumeLiveMessage(read.data, pendingRequestID)
			if err != nil {
				return err
			}
			if acknowledged {
				if !ackTimer.Stop() {
					select {
					case <-ackTimer.C:
					default:
					}
				}
				acknowledgement = nil
				client.publishLive(tui.RemoteLiveUpdate{
					ConnectionChange: true, Connected: true,
				})
				var response struct {
					Result struct {
						LatestID uint64 `json:"latest_id"`
					} `json:"result"`
				}
				if json.Unmarshal(read.data, &response) == nil {
					reset, resetErr := client.consumeLiveEpochReset(response.Result.LatestID)
					if resetErr != nil {
						return resetErr
					}
					if reset {
						pendingRequestID, err = writeSubscription(currentInterval)
						if err != nil {
							return err
						}
						ackTimer.Reset(3 * time.Second)
						acknowledgement = ackTimer.C
					}
				}
			}
		}
	}
}

func (client *remoteTUIIPC) consumeLiveMessage(data []byte, pendingRequestID uint64) (bool, error) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return false, fmt.Errorf("decode remote live stream: %w", err)
	}
	if len(envelope.ID) != 0 {
		var responseID uint64
		if json.Unmarshal(envelope.ID, &responseID) != nil || responseID != pendingRequestID {
			return false, nil
		}
		if len(envelope.Error) != 0 && string(envelope.Error) != "null" {
			return false, fmt.Errorf("remote live subscription rejected: %s", envelope.Error)
		}
		var acknowledgement struct {
			Subscribed bool   `json:"subscribed"`
			LatestID   uint64 `json:"latest_id"`
		}
		if json.Unmarshal(envelope.Result, &acknowledgement) != nil || !acknowledgement.Subscribed {
			return false, errors.New("remote live subscription was not acknowledged")
		}
		return true, nil
	}
	var message remoteLiveMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return false, fmt.Errorf("decode remote live notification: %w", err)
	}
	now := time.Now()
	switch message.Method {
	case "controller.status":
		var update controllerapi.StatusUpdate
		if json.Unmarshal(message.Params, &update) != nil || update.Error != "" {
			return false, nil
		}
		client.liveMu.Lock()
		identical := client.haveLastStatus && client.lastStatus == update.Status
		forward := !identical || now.Sub(client.lastStatusForwardedAt) >= 500*time.Millisecond
		if forward {
			client.lastStatus = update.Status
			client.haveLastStatus = true
			client.lastStatusForwardedAt = now
		}
		client.liveMu.Unlock()
		if !forward {
			return false, nil
		}
		client.publishLive(tui.RemoteLiveUpdate{
			Status: update.Status, HaveStatus: true,
			StatusUpdated: update.Time, StatusReceivedAt: now,
			ConnectionChange: true, Connected: true,
		})
	case "controller.state":
		var event controllerapi.Event
		if json.Unmarshal(message.Params, &event) != nil ||
			event.Opcode != native.OpStatusLEDChanged {
			return false, nil
		}
		if !client.advanceLiveStateCursor(event.ID) {
			return false, nil
		}
		state, err := native.ParseStatusLEDState(event.Payload)
		if err != nil {
			return false, nil
		}
		client.publishLive(tui.RemoteLiveUpdate{
			StatusLED: state, HaveStatusLED: true,
			StatusLEDUpdated: event.Time, StatusLEDReceivedAt: now,
			ConnectionChange: true, Connected: true,
		})
	}
	return false, nil
}

func (client *remoteTUIIPC) consumeLiveEpochReset(latestID uint64) (bool, error) {
	if !client.acceptLiveAcknowledgement(latestID) {
		return false, nil
	}
	requestContext, cancel := context.WithTimeout(client.ctx, 3*time.Second)
	defer cancel()
	snapshot, err := client.Snapshot(requestContext)
	if err != nil {
		return false, fmt.Errorf("refresh remote live baseline after primary restart: %w", err)
	}
	now := time.Now()
	client.publishLive(tui.RemoteLiveUpdate{
		Status: snapshot.Status, HaveStatus: snapshot.HaveStatus,
		StatusUpdated: snapshot.StatusUpdated, StatusReceivedAt: now,
		StatusLED: snapshot.StatusLED, HaveStatusLED: snapshot.HaveStatusLED,
		StatusLEDUpdated: snapshot.StatusLEDUpdated, StatusLEDReceivedAt: now,
		ConnectionChange: true, Connected: true,
	})
	return true, nil
}

func (client *remoteTUIIPC) publishLive(update tui.RemoteLiveUpdate) {
	select {
	case client.liveRaw <- update:
		return
	default:
	}
	var pending tui.RemoteLiveUpdate
	select {
	case pending = <-client.liveRaw:
	default:
	}
	mergeRemoteLiveUpdate(&pending, update)
	select {
	case client.liveRaw <- pending:
	default:
	}
}

func mergeRemoteLiveUpdate(pending *tui.RemoteLiveUpdate, update tui.RemoteLiveUpdate) {
	if pending == nil {
		return
	}
	if update.HaveStatus {
		pending.Status = update.Status
		pending.HaveStatus = true
		pending.StatusUpdated = update.StatusUpdated
		pending.StatusReceivedAt = update.StatusReceivedAt
	}
	if update.HaveStatusLED {
		pending.StatusLED = update.StatusLED
		pending.HaveStatusLED = true
		pending.StatusLEDUpdated = update.StatusLEDUpdated
		pending.StatusLEDReceivedAt = update.StatusLEDReceivedAt
	}
	if update.ConnectionChange {
		pending.ConnectionChange = true
		pending.Connected = update.Connected
		pending.Error = update.Error
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
	client.StartLive()
	navigationReporter, err := hostui.NewNavigationReporter(syncNavigation, "")
	if err != nil {
		return fmt.Errorf("create remote TUI instance identity: %w", err)
	}
	instanceLease := newRemoteTUIInstanceLease(client, navigationReporter, 0)
	defer instanceLease.Close()

	probeContext, probeCancel := context.WithTimeout(ctx, 8*time.Second)
	initial, err := client.Snapshot(probeContext)
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
						Live:            client.live,
						SetLiveInterval: client.SetLiveInterval,
						SaveHostUI:      saveRemoteHostUI,
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
