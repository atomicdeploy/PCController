package localdevice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const (
	defaultReconnectMin   = 250 * time.Millisecond
	defaultReconnectMax   = 30 * time.Second
	defaultReconnectReset = 30 * time.Second
	defaultReadLimit      = int64(maxEventBytes)
	defaultEventBuffer    = 64
)

var (
	ErrManagerClosed     = errors.New("local device manager is closed")
	ErrGenerationChanged = errors.New("local device configuration changed during operation")
)

// ManagerConfig controls one immutable connection generation.
type ManagerConfig struct {
	BaseURL             string
	EnableEvents        bool
	RequestTimeout      time.Duration
	ReconnectMin        time.Duration
	ReconnectMax        time.Duration
	ReconnectResetAfter time.Duration
	ReadLimit           int64
}

// ManagerOptions provides transport bounds and deterministic test hooks.
type ManagerOptions struct {
	HTTPClient  *http.Client
	BodyLimit   int64
	UserAgent   string
	EventBuffer int
	Now         func() time.Time
	Sleep       func(context.Context, time.Duration) error
}

// ManagerSnapshot is an immutable, safe status view. Device display text is
// intentionally represented only by SnapshotInspection metadata.
type ManagerSnapshot struct {
	BaseURL              string               `json:"base_url"`
	HTTPReachable        bool                 `json:"http_reachable"`
	EventsConnected      bool                 `json:"events_connected"`
	HaveCapabilities     bool                 `json:"have_capabilities"`
	Capabilities         CapabilityInspection `json:"capabilities,omitempty"`
	HaveDeviceSnapshot   bool                 `json:"have_device_snapshot"`
	Device               SnapshotInspection   `json:"device,omitempty"`
	LastEvent            EventType            `json:"last_event,omitempty"`
	LastEventSequence    uint64               `json:"last_event_sequence,omitempty"`
	LastEventAt          time.Time            `json:"last_event_at,omitempty"`
	LastConnectedAt      time.Time            `json:"last_connected_at,omitempty"`
	ReconnectAttempt     int                  `json:"reconnect_attempt,omitempty"`
	LastError            string               `json:"last_error,omitempty"`
	UpdatedAt            time.Time            `json:"updated_at"`
	ConfigurationVersion uint64               `json:"configuration_version"`
}

// ManagerEvent carries a validated JSON event and the generation that read it.
type ManagerEvent struct {
	Event                Event     `json:"event"`
	ReceivedAt           time.Time `json:"received_at"`
	ConfigurationVersion uint64    `json:"configuration_version"`
}

type managerRuntime struct {
	config ManagerConfig
	client *Client
}

// Manager owns configuration changes, passive refresh, event reconnection,
// and cancellation of stale in-flight operations.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	options ManagerOptions
	events  chan ManagerEvent
	actions chan struct{}

	mu         sync.RWMutex
	closed     bool
	generation uint64
	genCancel  context.CancelFunc
	genContext context.Context
	runtime    managerRuntime
	status     ManagerSnapshot
	capability Capabilities
	device     Snapshot
	wg         sync.WaitGroup
	active     sync.WaitGroup
}

// NewManager validates the first generation before starting any goroutine.
func NewManager(parent context.Context, config ManagerConfig, options ManagerOptions) (*Manager, error) {
	if parent == nil {
		return nil, errors.New("local device manager requires a non-nil context")
	}
	options = normalizeManagerOptions(options)
	runtime, err := prepareManagerRuntime(config, options)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	manager := &Manager{
		ctx:     ctx,
		cancel:  cancel,
		done:    make(chan struct{}),
		options: options,
		events:  make(chan ManagerEvent, options.EventBuffer),
		actions: make(chan struct{}, 1),
	}
	manager.mu.Lock()
	manager.startGenerationLocked(runtime)
	manager.mu.Unlock()
	go manager.watchLifecycle()
	return manager, nil
}

func normalizeManagerOptions(options ManagerOptions) ManagerOptions {
	if options.EventBuffer <= 0 {
		options.EventBuffer = defaultEventBuffer
	}
	if options.EventBuffer > 4096 {
		options.EventBuffer = 4096
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	return options
}

// Events returns the bounded validated event stream. Slow consumers may miss
// events and can use passive Refresh to obtain current state.
func (manager *Manager) Events() <-chan ManagerEvent { return manager.events }

// Snapshot returns a deep copy of the safe manager status.
func (manager *Manager) Snapshot() ManagerSnapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	result := manager.status
	result.Capabilities.Actions = append([]ActionType(nil), result.Capabilities.Actions...)
	result.Capabilities.Events = append([]EventType(nil), result.Capabilities.Events...)
	return result
}

// Capabilities performs a fresh bounded capability GET on the active
// generation and updates the safe cached projection.
func (manager *Manager) Capabilities(ctx context.Context) (Capabilities, error) {
	client, generation, operationCtx, release, err := manager.beginOperation(ctx)
	if err != nil {
		return Capabilities{}, err
	}
	defer release()
	value, operationErr := client.Capabilities(operationCtx)
	known, reachable := operationReachability(operationErr)
	if !manager.finishCapabilities(generation, value, operationErr, known, reachable) {
		return Capabilities{}, manager.staleOperationError(generation, operationErr)
	}
	return value, operationErr
}

// Refresh passively fetches the snapshot endpoint. It sends no action and no
// WebSocket message.
func (manager *Manager) Refresh(ctx context.Context) (Snapshot, error) {
	client, generation, operationCtx, release, err := manager.beginOperation(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer release()
	value, operationErr := client.Snapshot(operationCtx)
	known, reachable := operationReachability(operationErr)
	if !manager.finishSnapshot(generation, value, operationErr, known, reachable) {
		return Snapshot{}, manager.staleOperationError(generation, operationErr)
	}
	return value, operationErr
}

// Action serializes typed writes and discards results from superseded
// configuration generations.
func (manager *Manager) Action(ctx context.Context, action Action) (ActionResult, error) {
	if err := action.Validate(); err != nil {
		return ActionResult{}, err
	}
	client, generation, operationCtx, release, err := manager.beginOperation(ctx)
	if err != nil {
		return ActionResult{}, err
	}
	defer release()
	select {
	case manager.actions <- struct{}{}:
		defer func() { <-manager.actions }()
	case <-operationCtx.Done():
		return ActionResult{}, manager.staleOperationError(generation, operationCtx.Err())
	}
	result, operationErr := client.Action(operationCtx, action)
	known, reachable := operationReachability(operationErr)
	var snapshot Snapshot
	haveSnapshot := result.Snapshot != nil
	if haveSnapshot {
		snapshot = *result.Snapshot
	}
	if !manager.finishAction(generation, snapshot, haveSnapshot, operationErr, known, reachable) {
		return ActionResult{}, manager.staleOperationError(generation, operationErr)
	}
	return result, operationErr
}

// Inspect returns only capability or snapshot safe projections.
func (manager *Manager) Inspect(ctx context.Context, resource string) (any, error) {
	switch resource {
	case InspectCapabilities:
		value, err := manager.Capabilities(ctx)
		if err != nil {
			return nil, err
		}
		return InspectCapabilityDocument(value), nil
	case InspectSnapshot:
		value, err := manager.Refresh(ctx)
		if err != nil {
			return nil, err
		}
		return InspectSnapshotDocument(value), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedInspection, resource)
	}
}

// Update atomically starts a validated generation. The old generation is
// cancelled and can no longer mutate status or return a successful operation.
func (manager *Manager) Update(config ManagerConfig) error {
	runtime, err := prepareManagerRuntime(config, manager.options)
	if err != nil {
		return err
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || manager.ctx.Err() != nil {
		return ErrManagerClosed
	}
	if manager.genCancel != nil {
		manager.genCancel()
	}
	manager.startGenerationLocked(runtime)
	return nil
}

// Close is idempotent and waits for all network and delivery activity.
func (manager *Manager) Close() error {
	manager.cancel()
	<-manager.done
	return nil
}

func (manager *Manager) watchLifecycle() {
	<-manager.ctx.Done()
	manager.mu.Lock()
	if !manager.closed {
		manager.closed = true
		if manager.genCancel != nil {
			manager.genCancel()
		}
	}
	manager.mu.Unlock()
	manager.wg.Wait()
	manager.active.Wait()
	close(manager.events)
	close(manager.done)
}

func (manager *Manager) startGenerationLocked(runtime managerRuntime) {
	manager.generation++
	generation := manager.generation
	ctx, cancel := context.WithCancel(manager.ctx)
	manager.genCancel = cancel
	manager.genContext = ctx
	manager.runtime = runtime
	manager.capability = Capabilities{}
	manager.device = Snapshot{}
	now := manager.options.Now().UTC()
	manager.status = ManagerSnapshot{
		BaseURL:              runtime.client.BaseURL(),
		UpdatedAt:            now,
		ConfigurationVersion: generation,
	}
	manager.wg.Add(1)
	go func() {
		defer manager.wg.Done()
		manager.runGeneration(ctx, generation, runtime)
	}()
}

func prepareManagerRuntime(config ManagerConfig, options ManagerOptions) (managerRuntime, error) {
	if config.RequestTimeout <= 0 {
		config.RequestTimeout = defaultRequestTimeout
	}
	if config.ReconnectMin <= 0 {
		config.ReconnectMin = defaultReconnectMin
	}
	if config.ReconnectMax <= 0 {
		config.ReconnectMax = defaultReconnectMax
	}
	if config.ReconnectMax < config.ReconnectMin {
		return managerRuntime{}, errors.New("local device reconnect maximum must not be below minimum")
	}
	if config.ReconnectResetAfter <= 0 {
		config.ReconnectResetAfter = defaultReconnectReset
	}
	if config.ReadLimit <= 0 {
		config.ReadLimit = defaultReadLimit
	}
	if config.ReadLimit < 256 || config.ReadLimit > 1<<20 {
		return managerRuntime{}, errors.New("local device event read limit must be between 256 bytes and 1 MiB")
	}
	client, err := NewClient(config.BaseURL, ClientOptions{
		HTTPClient: options.HTTPClient,
		Timeout:    config.RequestTimeout,
		BodyLimit:  options.BodyLimit,
		UserAgent:  options.UserAgent,
	})
	if err != nil {
		return managerRuntime{}, err
	}
	return managerRuntime{config: config, client: client}, nil
}

func (manager *Manager) runGeneration(
	ctx context.Context,
	generation uint64,
	runtime managerRuntime,
) {
	defer runtime.client.CloseIdleConnections()
	manager.refreshGeneration(ctx, generation, runtime.client, true)
	if !runtime.config.EnableEvents || ctx.Err() != nil {
		<-ctx.Done()
		return
	}
	failures := 0
	for ctx.Err() == nil {
		connectedAt, err := manager.readConnection(ctx, generation, runtime)
		if ctx.Err() != nil {
			return
		}
		if !connectedAt.IsZero() && manager.options.Now().Sub(connectedAt) >= runtime.config.ReconnectResetAfter {
			failures = 0
		}
		failures++
		delay := reconnectDelay(runtime.config.ReconnectMin, runtime.config.ReconnectMax, failures)
		manager.updateStatus(generation, func(status *ManagerSnapshot) {
			status.EventsConnected = false
			status.ReconnectAttempt = failures
			if err != nil {
				status.LastError = err.Error()
			}
		})
		if manager.options.Sleep(ctx, delay) != nil {
			return
		}
		manager.refreshGeneration(ctx, generation, runtime.client, false)
	}
}

func (manager *Manager) refreshGeneration(
	ctx context.Context,
	generation uint64,
	client *Client,
	includeCapabilities bool,
) {
	var failures []error
	if includeCapabilities {
		capabilities, err := client.Capabilities(ctx)
		if err != nil {
			failures = append(failures, err)
		} else {
			manager.updateCapabilities(generation, capabilities)
		}
	}
	device, err := client.Snapshot(ctx)
	if err != nil {
		failures = append(failures, err)
	} else {
		manager.updateDevice(generation, device)
	}
	manager.updateStatus(generation, func(status *ManagerSnapshot) {
		if len(failures) == 0 {
			status.HTTPReachable = true
			status.LastError = ""
			return
		}
		status.LastError = errors.Join(failures...).Error()
		known, reachable := operationReachability(failures[0])
		if known {
			status.HTTPReachable = reachable
		}
	})
}

func (manager *Manager) readConnection(
	ctx context.Context,
	generation uint64,
	runtime managerRuntime,
) (time.Time, error) {
	dialContext, cancelDial := context.WithTimeout(ctx, runtime.config.RequestTimeout)
	httpClient := *runtime.client.http
	httpClient.Timeout = 0
	connection, response, err := websocket.Dial(dialContext, runtime.client.EventsURL(), &websocket.DialOptions{
		HTTPClient:      &httpClient,
		CompressionMode: websocket.CompressionDisabled,
	})
	cancelDial()
	if err != nil {
		if response != nil && response.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
		}
		return time.Time{}, fmt.Errorf("local device event connect: %w", err)
	}
	defer connection.CloseNow()
	connection.SetReadLimit(runtime.config.ReadLimit)
	connectedAt := manager.options.Now().UTC()
	manager.updateStatus(generation, func(status *ManagerSnapshot) {
		status.EventsConnected = true
		status.ReconnectAttempt = 0
		status.LastConnectedAt = connectedAt
		status.LastError = ""
	})
	for {
		messageType, payload, err := connection.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return connectedAt, ctx.Err()
			}
			return connectedAt, fmt.Errorf("local device event read: %w", err)
		}
		if messageType != websocket.MessageText {
			continue
		}
		event, err := ParseEvent(payload)
		if err != nil {
			continue
		}
		receivedAt := manager.options.Now().UTC()
		if !manager.applyEvent(generation, event, receivedAt) {
			continue
		}
		manager.emit(generation, ManagerEvent{
			Event:                event,
			ReceivedAt:           receivedAt,
			ConfigurationVersion: generation,
		})
	}
}

func (manager *Manager) applyEvent(generation uint64, event Event, receivedAt time.Time) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation ||
		event.Sequence <= manager.status.LastEventSequence {
		return false
	}
	manager.status.HTTPReachable = true
	manager.status.LastEvent = event.Type
	manager.status.LastEventSequence = event.Sequence
	manager.status.LastEventAt = receivedAt
	manager.status.LastError = ""
	manager.status.UpdatedAt = receivedAt
	var device *Snapshot
	if event.Snapshot != nil {
		device = event.Snapshot
	} else if event.Result != nil && event.Result.Snapshot != nil {
		device = event.Result.Snapshot
	}
	if device != nil {
		manager.device = *device
		manager.status.HaveDeviceSnapshot = true
		manager.status.Device = InspectSnapshotDocument(*device)
	}
	return true
}

func (manager *Manager) beginOperation(
	caller context.Context,
) (*Client, uint64, context.Context, func(), error) {
	if caller == nil {
		return nil, 0, nil, nil, errors.New("local device operation requires a non-nil context")
	}
	manager.mu.Lock()
	if manager.closed || manager.ctx.Err() != nil || manager.runtime.client == nil || manager.genContext == nil {
		manager.mu.Unlock()
		return nil, 0, nil, nil, ErrManagerClosed
	}
	client := manager.runtime.client
	generation := manager.generation
	generationContext := manager.genContext
	manager.active.Add(1)
	manager.mu.Unlock()
	operationContext, cancel := context.WithCancel(caller)
	stopGenerationCancellation := context.AfterFunc(generationContext, cancel)
	var once sync.Once
	release := func() {
		once.Do(func() {
			stopGenerationCancellation()
			cancel()
			manager.active.Done()
		})
	}
	return client, generation, operationContext, release, nil
}

func (manager *Manager) finishCapabilities(
	generation uint64,
	value Capabilities,
	err error,
	known bool,
	reachable bool,
) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return false
	}
	manager.finishReachabilityLocked(err, known, reachable)
	if err == nil {
		manager.capability = cloneCapabilities(value)
		manager.status.HaveCapabilities = true
		manager.status.Capabilities = InspectCapabilityDocument(value)
	}
	manager.status.UpdatedAt = manager.options.Now().UTC()
	return true
}

func (manager *Manager) finishSnapshot(
	generation uint64,
	value Snapshot,
	err error,
	known bool,
	reachable bool,
) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return false
	}
	manager.finishReachabilityLocked(err, known, reachable)
	if err == nil {
		manager.device = value
		manager.status.HaveDeviceSnapshot = true
		manager.status.Device = InspectSnapshotDocument(value)
	}
	manager.status.UpdatedAt = manager.options.Now().UTC()
	return true
}

func (manager *Manager) finishAction(
	generation uint64,
	value Snapshot,
	haveSnapshot bool,
	err error,
	known bool,
	reachable bool,
) bool {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return false
	}
	manager.finishReachabilityLocked(err, known, reachable)
	if err == nil && haveSnapshot {
		manager.device = value
		manager.status.HaveDeviceSnapshot = true
		manager.status.Device = InspectSnapshotDocument(value)
	}
	manager.status.UpdatedAt = manager.options.Now().UTC()
	return true
}

func (manager *Manager) finishReachabilityLocked(err error, known, reachable bool) {
	if known {
		manager.status.HTTPReachable = reachable
	}
	if err == nil {
		manager.status.LastError = ""
	} else {
		manager.status.LastError = err.Error()
	}
}

func (manager *Manager) updateCapabilities(generation uint64, value Capabilities) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return
	}
	manager.capability = cloneCapabilities(value)
	manager.status.HaveCapabilities = true
	manager.status.Capabilities = InspectCapabilityDocument(value)
	manager.status.UpdatedAt = manager.options.Now().UTC()
}

func (manager *Manager) updateDevice(generation uint64, value Snapshot) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return
	}
	manager.device = value
	manager.status.HaveDeviceSnapshot = true
	manager.status.Device = InspectSnapshotDocument(value)
	manager.status.UpdatedAt = manager.options.Now().UTC()
}

func (manager *Manager) updateStatus(generation uint64, update func(*ManagerSnapshot)) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.closed || generation != manager.generation {
		return
	}
	update(&manager.status)
	manager.status.UpdatedAt = manager.options.Now().UTC()
}

func (manager *Manager) emit(generation uint64, event ManagerEvent) {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.closed || generation != manager.generation {
		return
	}
	select {
	case manager.events <- event:
	default:
	}
}

func (manager *Manager) staleOperationError(generation uint64, cause error) error {
	manager.mu.RLock()
	var stale error
	if manager.closed || manager.ctx.Err() != nil {
		stale = ErrManagerClosed
	} else if generation != manager.generation {
		stale = ErrGenerationChanged
	}
	manager.mu.RUnlock()
	if stale == nil {
		return cause
	}
	if cause == nil {
		return stale
	}
	return errors.Join(stale, cause)
}

func operationReachability(err error) (known, reachable bool) {
	if err == nil {
		return true, true
	}
	var statusError *HTTPStatusError
	if errors.As(err, &statusError) {
		return true, true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false, false
	}
	return true, false
}

func reconnectDelay(minimum, maximum time.Duration, attempt int) time.Duration {
	if attempt <= 1 {
		return minimum
	}
	delay := minimum
	for index := 1; index < attempt; index++ {
		if delay >= maximum || delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
