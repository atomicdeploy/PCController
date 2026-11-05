package appcore

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/lamp"
	"rayanlamp.local/app/internal/model"
)

var ErrStopped = errors.New("RayanLamp core is stopped")

type Core struct {
	configMu      sync.RWMutex
	cfg           config.Config
	client        *lamp.Client
	clientFactory func(config.Config) (*lamp.Client, error)
	clientEpoch   uint64

	stateMu sync.RWMutex
	state   model.Snapshot

	commandMu sync.Mutex

	diagnostics *diagnosticsState
	terminal    *deviceterminal.Manager
	// terminalDial is nil in production, where TerminalConnect uses the lamp
	// client's cached-resolution dialer. Keeping the seam private lets
	// lifecycle race tests stop a dial at an exact point without changing the
	// public API or the terminal manager.
	terminalDial  func(context.Context, *lamp.Client, int) (net.Conn, error)
	terminalOpMu  sync.Mutex
	terminalOps   map[uint64]terminalConnectOperation
	terminalOpID  uint64
	terminalEpoch uint64

	subsMu    sync.RWMutex
	subs      map[uint64]chan model.Event
	nextSubID atomic.Uint64

	lifecycleOpMu sync.Mutex
	lifecycleMu   sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	network       *networkRun
	generation    uint64
	started       bool
}

type networkRun struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type terminalConnectOperation struct {
	epoch  uint64
	cancel context.CancelFunc
	done   chan struct{}
}

func (r *networkRun) start(fn func(context.Context)) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn(r.ctx)
	}()
}

func (r *networkRun) stopAndWait() {
	if r == nil {
		return
	}
	r.cancel()
	r.wg.Wait()
}

func New(cfg config.Config) (*Core, error) {
	cfg = cloneConfig(cfg)
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	client, err := newLampClient(cfg)
	if err != nil {
		return nil, err
	}
	return &Core{
		cfg:           cfg,
		client:        client,
		clientFactory: newLampClient,
		clientEpoch:   1,
		state: model.Snapshot{
			State:     model.StateUnknown,
			DeviceURL: cfg.Lamp.BaseURL,
		},
		diagnostics: newDiagnosticsState(cfg.Lamp.BaseURL),
		terminal:    deviceterminal.New(deviceterminal.DefaultScrollbackBytes),
		terminalOps: make(map[uint64]terminalConnectOperation),
		subs:        make(map[uint64]chan model.Event),
	}, nil
}

// ValidateConfig performs the same normalization and lamp-client validation
// used by ApplyConfig without changing runtime state.
func ValidateConfig(cfg config.Config) error {
	cfg = cloneConfig(cfg)
	if err := cfg.Normalize(); err != nil {
		return err
	}
	_, err := newLampClient(cfg)
	return err
}

func cloneConfig(cfg config.Config) config.Config {
	cfg.Diagnostics.SafeEndpoints = append([]string(nil), cfg.Diagnostics.SafeEndpoints...)
	cfg.Diagnostics.ExpertEndpoints = append([]string(nil), cfg.Diagnostics.ExpertEndpoints...)
	return cfg
}

func newLampClient(cfg config.Config) (*lamp.Client, error) {
	return lamp.New(
		cfg.Lamp.BaseURL,
		lamp.WithHTTPClient(&http.Client{Timeout: cfg.Lamp.RequestTimeout.Value()}),
		lamp.WithReconnectPolicy(lamp.ReconnectPolicy{
			Initial:    cfg.Lamp.ReconnectMin.Value(),
			Maximum:    cfg.Lamp.ReconnectMax.Value(),
			Multiplier: 2,
			Jitter:     0.2,
			ResetAfter: time.Minute,
		}),
		lamp.WithUserAgent(buildinfo.Name+"/"+buildinfo.Version),
	)
}

func (c *Core) Start(parent context.Context) error {
	if parent == nil {
		parent = context.Background()
	}
	c.lifecycleOpMu.Lock()
	defer c.lifecycleOpMu.Unlock()
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if c.started {
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(parent)
	c.started = true
	c.startNetworkLocked()
	return nil
}

func (c *Core) Stop() {
	c.lifecycleOpMu.Lock()
	defer c.lifecycleOpMu.Unlock()

	c.lifecycleMu.Lock()
	wasStarted := c.started
	network := c.network
	c.network = nil
	c.generation++
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.ctx = nil
	c.started = false
	c.lifecycleMu.Unlock()

	c.cancelTerminalConnectsLocked()
	network.stopAndWait()
	if wasStarted {
		c.closeIdleConnections()
	}
}

func (c *Core) Reconnect() error {
	c.lifecycleOpMu.Lock()
	defer c.lifecycleOpMu.Unlock()

	c.lifecycleMu.Lock()
	if !c.started || c.ctx == nil || c.ctx.Err() != nil {
		c.lifecycleMu.Unlock()
		return ErrStopped
	}
	network := c.network
	c.network = nil
	c.generation++
	if network != nil {
		network.cancel()
	}
	c.lifecycleMu.Unlock()

	c.cancelTerminalConnectsLocked()
	if network != nil {
		network.wg.Wait()
	}

	// Cancellation is deliberate here, so the WebSocket listener does not
	// classify it as a failed cached-address connection. Explicit recovery
	// (including resume and host-network changes) must invalidate .local
	// itself before starting the replacement generation.
	c.configMu.RLock()
	client := c.client
	c.configMu.RUnlock()
	if client != nil {
		client.CloseIdleConnections()
		client.InvalidateResolution()
	}

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.started || c.ctx == nil || c.ctx.Err() != nil {
		return ErrStopped
	}
	c.startNetworkLocked()
	return nil
}

func (c *Core) startNetworkLocked() {
	networkCtx, stop := context.WithCancel(c.ctx)
	run := &networkRun{ctx: networkCtx, cancel: stop}
	c.network = run
	c.generation++
	generation := c.generation

	c.configMu.RLock()
	cfg := c.cfg
	client := c.client
	c.configMu.RUnlock()

	if cfg.Lamp.WebSocket {
		run.start(func(ctx context.Context) {
			_ = client.Listen(ctx, lamp.Handlers{
				OnEvent: func(event model.Event) {
					c.handleLampEventForGeneration(generation, event)
				},
				OnConnection: func(status lamp.ConnectionStatus) {
					c.handleConnectionForGeneration(generation, status)
				},
			})
		})

		// The firmware sends one `get` through the socket after connection.
		// Use HTTP only if that initial event never arrives.
		run.start(func(ctx context.Context) {
			timer := time.NewTimer(2 * time.Second)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if c.Snapshot().State == model.StateUnknown {
					_, _ = c.refreshForGeneration(ctx, client, generation)
				}
			}
		})
	} else {
		run.start(func(ctx context.Context) {
			_, _ = c.refreshForGeneration(ctx, client, generation)
		})
	}

	if interval := cfg.Lamp.HTTPPoll.Value(); interval > 0 {
		run.start(func(ctx context.Context) {
			c.poll(ctx, client, generation, interval)
		})
	}
	run.start(func(ctx context.Context) {
		c.runDiagnostics(ctx, client, generation)
	})
}

func (c *Core) currentGenerationLocked(generation uint64) bool {
	return c.started && generation == c.generation
}

func (c *Core) handleLampEventForGeneration(generation uint64, event model.Event) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.currentGenerationLocked(generation) {
		return
	}
	c.handleLampEvent(event)
}

func (c *Core) handleConnectionForGeneration(generation uint64, status lamp.ConnectionStatus) {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.currentGenerationLocked(generation) {
		return
	}
	c.handleConnection(status)
}

func (c *Core) poll(ctx context.Context, client *lamp.Client, generation uint64, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// WebSocket events are authoritative. HTTP polling is the fallback,
			// not additional traffic while the stream is healthy.
			if !c.Snapshot().WebSocketOnline {
				_, _ = c.refreshForGeneration(ctx, client, generation)
			}
		}
	}
}

func (c *Core) refreshForGeneration(
	ctx context.Context,
	client *lamp.Client,
	generation uint64,
) (model.LampState, error) {
	state, err := client.Get(ctx)
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	if !c.currentGenerationLocked(generation) {
		return state, err
	}
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			c.setNetworkError(err)
		}
		return model.StateUnknown, err
	}
	c.mergeState(state, "http", false, true)
	return state, nil
}

func (c *Core) Snapshot() model.Snapshot {
	c.stateMu.RLock()
	defer c.stateMu.RUnlock()
	return c.state
}

func (c *Core) Config() config.Config {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	return cloneConfig(c.cfg)
}

func (c *Core) ApplyConfig(cfg config.Config) error {
	cfg = cloneConfig(cfg)
	if err := cfg.Normalize(); err != nil {
		return err
	}

	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	c.lifecycleOpMu.Lock()
	defer c.lifecycleOpMu.Unlock()

	c.lifecycleMu.Lock()
	c.configMu.RLock()
	lampChanged := c.cfg.Lamp != cfg.Lamp
	c.configMu.RUnlock()
	if !lampChanged {
		c.configMu.Lock()
		c.cfg = cloneConfig(cfg)
		c.configMu.Unlock()
		c.lifecycleMu.Unlock()
		return nil
	}
	c.lifecycleMu.Unlock()

	factory := c.clientFactory
	if factory == nil {
		factory = newLampClient
	}
	client, err := factory(cfg)
	if err != nil {
		return err
	}

	c.lifecycleMu.Lock()
	network := c.network
	c.network = nil
	c.generation++
	if network != nil {
		network.cancel()
	}
	c.lifecycleMu.Unlock()
	c.cancelTerminalConnectsLocked()
	if network != nil {
		network.wg.Wait()
	}
	c.closeIdleConnections()

	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()
	c.configMu.Lock()
	c.cfg = cloneConfig(cfg)
	c.client = client
	c.clientEpoch++
	c.configMu.Unlock()

	c.stateMu.Lock()
	previousState := c.state.State
	c.state = model.Snapshot{
		State:     model.StateUnknown,
		DeviceURL: cfg.Lamp.BaseURL,
	}
	snapshot := c.state
	c.stateMu.Unlock()
	c.resetDiagnostics(cfg.Lamp.BaseURL)
	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: previousState,
	})

	if c.started && c.ctx != nil && c.ctx.Err() == nil {
		c.startNetworkLocked()
	}
	return nil
}

func (c *Core) closeIdleConnections() {
	if c == nil {
		return
	}
	c.configMu.RLock()
	client := c.client
	c.configMu.RUnlock()
	if client != nil {
		client.CloseIdleConnections()
	}
}

func (c *Core) Refresh(ctx context.Context) (model.LampState, error) {
	client, epoch, err := c.currentClient()
	if err != nil {
		return model.StateUnknown, err
	}
	state, err := client.Get(ctx)
	if err != nil {
		c.updateClientEpoch(epoch, func() {
			if !errors.Is(err, context.Canceled) {
				c.setNetworkError(err)
			}
		})
		return model.StateUnknown, err
	}
	c.updateClientEpoch(epoch, func() {
		c.mergeState(state, "http", false, true)
	})
	return state, nil
}

func (c *Core) On(ctx context.Context) (model.LampState, error) {
	return c.set(ctx, model.StateOn)
}

func (c *Core) Off(ctx context.Context) (model.LampState, error) {
	return c.set(ctx, model.StateOff)
}

func (c *Core) Toggle(ctx context.Context) (model.LampState, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()

	current := c.Snapshot().State
	if current == model.StateUnknown {
		var err error
		current, err = c.Refresh(ctx)
		if err != nil {
			return model.StateUnknown, err
		}
	}
	if current == model.StateOn {
		return c.setLocked(ctx, model.StateOff)
	}
	return c.setLocked(ctx, model.StateOn)
}

func (c *Core) set(ctx context.Context, target model.LampState) (model.LampState, error) {
	c.commandMu.Lock()
	defer c.commandMu.Unlock()
	return c.setLocked(ctx, target)
}

func (c *Core) setLocked(ctx context.Context, target model.LampState) (model.LampState, error) {
	client, epoch, err := c.currentClient()
	if err != nil {
		return model.StateUnknown, err
	}
	command := target.String()
	c.setPending(command)

	var state model.LampState
	switch target {
	case model.StateOn:
		state, err = client.On(ctx)
	case model.StateOff:
		state, err = client.Off(ctx)
	default:
		err = errors.New("invalid target lamp state")
	}
	if err != nil {
		c.updateClientEpoch(epoch, func() {
			c.clearPending(err)
		})
		return model.StateUnknown, err
	}
	c.updateClientEpoch(epoch, func() {
		c.mergeState(state, "command", false, true)
	})
	return state, nil
}

func (c *Core) FetchDiagnostic(ctx context.Context, endpoint string) ([]byte, error) {
	return c.fetchDiagnosticRaw(ctx, endpoint)
}

// TerminalStatus returns connection metadata for the optional Telnet-style
// device terminal. It contains no transcript data.
func (c *Core) TerminalStatus() deviceterminal.Snapshot {
	if c == nil || c.terminal == nil {
		return deviceterminal.Snapshot{
			State: deviceterminal.StateDisconnected,
			Port:  deviceterminal.DefaultPort,
		}
	}
	return c.decorateTerminalSnapshot(c.terminal.Snapshot())
}

func (c *Core) decorateTerminalSnapshot(
	snapshot deviceterminal.Snapshot,
) deviceterminal.Snapshot {
	if c == nil {
		return snapshot
	}
	c.configMu.RLock()
	cfg := c.cfg
	c.configMu.RUnlock()
	// A connected/connecting manager snapshot describes the target it
	// actually dialed. Only disconnected or incomplete metadata should be
	// decorated from the currently configured target; otherwise a config race
	// could make an old live connection look as though it reached the new
	// host.
	useConfiguredHost := snapshot.State == deviceterminal.StateDisconnected ||
		snapshot.Host == ""
	useConfiguredPort := snapshot.State == deviceterminal.StateDisconnected ||
		snapshot.Port <= 0
	if useConfiguredHost {
		if parsed, err := url.Parse(cfg.Lamp.BaseURL); err == nil &&
			parsed.Hostname() != "" {
			snapshot.Host = parsed.Hostname()
		}
	}
	if useConfiguredPort && cfg.Lamp.TelnetPort > 0 {
		snapshot.Port = cfg.Lamp.TelnetPort
	}
	return snapshot
}

// TerminalConnect manually opens the configured lamp's Telnet-style port. It
// never auto-connects during normal application startup because the audited
// firmware accepts only one client and exposes no authentication.
func (c *Core) TerminalConnect(
	ctx context.Context,
) (deviceterminal.Snapshot, error) {
	if c == nil || c.terminal == nil {
		return deviceterminal.Snapshot{}, ErrStopped
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// Serialize only target capture and operation registration with lifecycle
	// changes. The potentially slow dial runs without lifecycleOpMu, allowing
	// ApplyConfig, Reconnect, and Stop to cancel it promptly.
	c.lifecycleOpMu.Lock()
	c.configMu.RLock()
	cfg := c.cfg
	client := c.client
	terminalDial := c.terminalDial
	c.configMu.RUnlock()
	if client == nil {
		c.lifecycleOpMu.Unlock()
		return c.TerminalStatus(), ErrStopped
	}
	parsed, err := url.Parse(cfg.Lamp.BaseURL)
	if err != nil || parsed.Hostname() == "" {
		c.lifecycleOpMu.Unlock()
		return c.TerminalStatus(), errors.New("lamp terminal hostname is invalid")
	}
	timeout := cfg.Lamp.RequestTimeout.Value()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	dialContext, cancel := context.WithTimeout(ctx, timeout)
	operationID, operation := c.registerTerminalConnect(cancel)
	c.lifecycleOpMu.Unlock()

	snapshot, err := c.terminal.Connect(
		dialContext,
		parsed.Hostname(),
		cfg.Lamp.TelnetPort,
		func(connectContext context.Context) (net.Conn, error) {
			if terminalDial != nil {
				return terminalDial(connectContext, client, cfg.Lamp.TelnetPort)
			}
			return client.DialTCP(connectContext, cfg.Lamp.TelnetPort)
		},
	)
	cancel()
	if !c.completeTerminalConnect(operationID, operation) {
		if err == nil {
			err = context.Canceled
		}
		snapshot = c.terminal.Snapshot()
	}
	return c.decorateTerminalSnapshot(snapshot), err
}

func (c *Core) registerTerminalConnect(
	cancel context.CancelFunc,
) (uint64, terminalConnectOperation) {
	c.terminalOpMu.Lock()
	defer c.terminalOpMu.Unlock()
	c.terminalOpID++
	operation := terminalConnectOperation{
		epoch:  c.terminalEpoch,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	c.terminalOps[c.terminalOpID] = operation
	return c.terminalOpID, operation
}

func (c *Core) completeTerminalConnect(
	id uint64,
	operation terminalConnectOperation,
) bool {
	c.terminalOpMu.Lock()
	registered, ok := c.terminalOps[id]
	delete(c.terminalOps, id)
	current := ok &&
		registered.epoch == operation.epoch &&
		operation.epoch == c.terminalEpoch
	c.terminalOpMu.Unlock()

	if !current && c.terminal != nil {
		c.terminal.Disconnect()
	}
	close(operation.done)
	return current
}

// cancelTerminalConnectsLocked invalidates every operation registered before
// the lifecycle change, cancels both the appcore and manager dial contexts,
// and joins their short completion paths. The caller holds lifecycleOpMu, so a
// new TerminalConnect cannot capture a target until the lifecycle change has
// committed.
func (c *Core) cancelTerminalConnectsLocked() {
	if c == nil {
		return
	}
	c.terminalOpMu.Lock()
	c.terminalEpoch++
	operations := make([]terminalConnectOperation, 0, len(c.terminalOps))
	for _, operation := range c.terminalOps {
		operations = append(operations, operation)
	}
	c.terminalOpMu.Unlock()

	for _, operation := range operations {
		operation.cancel()
	}
	if c.terminal != nil {
		c.terminal.Disconnect()
	}
	for _, operation := range operations {
		<-operation.done
	}
}

func (c *Core) TerminalDisconnect() deviceterminal.Snapshot {
	if c == nil || c.terminal == nil {
		return deviceterminal.Snapshot{
			State: deviceterminal.StateDisconnected,
			Port:  deviceterminal.DefaultPort,
		}
	}
	return c.decorateTerminalSnapshot(c.terminal.Disconnect())
}

func (c *Core) TerminalWrite(
	ctx context.Context,
	data []byte,
) (deviceterminal.Snapshot, error) {
	if c == nil || c.terminal == nil {
		return deviceterminal.Snapshot{}, ErrStopped
	}
	snapshot, err := c.terminal.Write(ctx, data)
	return c.decorateTerminalSnapshot(snapshot), err
}

func (c *Core) TerminalRead(
	ctx context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (deviceterminal.ReadResult, error) {
	if c == nil || c.terminal == nil {
		return deviceterminal.ReadResult{}, ErrStopped
	}
	result, err := c.terminal.Read(ctx, after, limit, wait)
	result.Snapshot = c.decorateTerminalSnapshot(result.Snapshot)
	return result, err
}

func (c *Core) currentClient() (*lamp.Client, uint64, error) {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	if c.client == nil {
		return nil, 0, ErrStopped
	}
	return c.client, c.clientEpoch, nil
}

func (c *Core) updateClientEpoch(epoch uint64, update func()) bool {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	if c.clientEpoch != epoch {
		return false
	}
	update()
	return true
}

func (c *Core) handleLampEvent(event model.Event) {
	switch event.Kind {
	case model.EventSnapshot:
		c.mergeState(event.Snapshot.State, "websocket", true, true)
	default:
		c.stateMu.Lock()
		c.state.LastEvent = event.Raw
		snapshot := c.state
		c.stateMu.Unlock()
		event.Snapshot = snapshot
		c.publish(event)
	}
}

func (c *Core) handleConnection(status lamp.ConnectionStatus) {
	c.stateMu.Lock()
	c.state.WebSocketOnline = status.Online
	c.state.ReconnectAttempt = status.Attempt
	if status.Online {
		c.state.LastConnected = status.At
		c.state.LastError = ""
	} else if status.Err != nil && !errors.Is(status.Err, context.Canceled) {
		c.state.LastError = status.Err.Error()
	}
	snapshot := c.state
	c.stateMu.Unlock()
	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: snapshot.State,
	})
}

func (c *Core) mergeState(state model.LampState, source string, remote, reachable bool) {
	if state == model.StateUnknown {
		return
	}
	now := time.Now().UTC()
	c.stateMu.Lock()
	previous := c.state.State
	initial := previous == model.StateUnknown
	changed := previous != state
	c.state.State = state
	c.state.HTTPReachable = c.state.HTTPReachable || reachable
	c.state.LastError = ""
	c.state.PendingCommand = ""
	c.state.LastSource = source
	c.state.LastEvent = state.String()
	if changed {
		c.state.LastChanged = now
	}
	snapshot := c.state
	c.stateMu.Unlock()

	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: previous,
		InitialSync:   initial,
		FromRemote:    remote,
	})
}

func (c *Core) setPending(command string) {
	c.stateMu.Lock()
	c.state.PendingCommand = command
	snapshot := c.state
	c.stateMu.Unlock()
	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: snapshot.State,
	})
}

func (c *Core) clearPending(err error) {
	c.stateMu.Lock()
	c.state.PendingCommand = ""
	if err != nil {
		c.state.LastError = err.Error()
	}
	snapshot := c.state
	c.stateMu.Unlock()
	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: snapshot.State,
	})
}

func (c *Core) setNetworkError(err error) {
	c.stateMu.Lock()
	c.state.HTTPReachable = false
	if err != nil {
		c.state.LastError = err.Error()
	}
	snapshot := c.state
	c.stateMu.Unlock()
	c.publish(model.Event{
		Kind:          model.EventSnapshot,
		Snapshot:      snapshot,
		PreviousState: snapshot.State,
	})
}

func (c *Core) Subscribe(buffer int) (<-chan model.Event, func()) {
	if buffer < 1 {
		buffer = 1
	}
	id := c.nextSubID.Add(1)
	channel := make(chan model.Event, buffer)
	c.subsMu.Lock()
	c.subs[id] = channel
	c.subsMu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			c.subsMu.Lock()
			delete(c.subs, id)
			c.subsMu.Unlock()
		})
	}
	return channel, unsubscribe
}

func (c *Core) publish(event model.Event) {
	c.subsMu.RLock()
	channels := make([]chan model.Event, 0, len(c.subs))
	for _, channel := range c.subs {
		channels = append(channels, channel)
	}
	c.subsMu.RUnlock()

	for _, channel := range channels {
		select {
		case channel <- event:
		default:
			// Subscribers care about the most recent state. Drop one stale
			// entry before publishing the new event.
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- event:
			default:
			}
		}
	}
}

func (c *Core) String() string {
	snapshot := c.Snapshot()
	return fmt.Sprintf("%s (%s)", snapshot.State, snapshot.DeviceURL)
}
