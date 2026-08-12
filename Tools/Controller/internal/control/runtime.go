package control

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
)

type Options struct {
	Filter           ports.Filter
	BaudRate         int
	StartupWait      time.Duration
	RequestTimeout   time.Duration
	HelloAttempts    int
	ResetOnReconnect bool
}

type Snapshot struct {
	Connected         bool
	Generation        uint64
	Paused            bool
	Port              ports.Info
	Hello             native.Hello
	Status            native.Status
	Settings          native.Settings
	HaveStatus        bool
	HaveSettings      bool
	StatusUpdated     time.Time
	ConnectionState   string
	ConnectionReason  string
	ConnectionUpdated time.Time
	FrontPanel        native.FrontPanel
	HaveFrontPanel    bool
	FrontPanelUpdated time.Time
	StatusLED         native.StatusLEDState
	HaveStatusLED     bool
	StatusLEDUpdated  time.Time
	ProgramState      ProgramStateSnapshot
	RFLearning        RFLearnState
}

type Event struct {
	ID          uint64
	Time        time.Time
	Kind        string
	Stream      string
	Text        string
	Frame       native.Frame
	Lifecycle   string
	Port        ports.Info
	Reason      string
	State       string
	Gesture     string
	Source      string
	Target      string
	Targets     []string
	MessageType string
	Action      string
	Severity    string
	Correlation string
	Delivery    string
	Metadata    map[string]string
	RFCode      uint32
	RFBits      byte
	RFProtocol  byte
	RFPulseUS   uint16
	RFID        byte
	HaveRFID    bool
	ResetCause  byte
	ResetCount  uint32
}

// ActionEvidence is the canonical recorder input for both acknowledged host
// commands and successfully applied physical/RF board actions. Its MCU clock
// is authoritative; ObservedAt is informational and never drives playback.
type ActionEvidence struct {
	Opcode       byte      `json:"opcode"`
	Payload      []byte    `json:"payload,omitempty"`
	Source       byte      `json:"source"`
	SourceID     byte      `json:"source_id,omitempty"`
	BoardOrigin  bool      `json:"board_origin,omitempty"`
	DeviceMicros uint32    `json:"device_micros"`
	Timed        bool      `json:"timed"`
	ObservedAt   time.Time `json:"observed_at"`
	Generation   uint64    `json:"connection_generation"`
}

type rfGestureKey struct {
	code     uint32
	bits     byte
	protocol byte
}

const (
	rfHoldAfter        = 600 * time.Millisecond
	rfReleaseAfter     = 250 * time.Millisecond
	rfDoubleClickAfter = 400 * time.Millisecond
)

type rfGestureState struct {
	firstSeen  time.Time
	lastSeen   time.Time
	lastRepeat time.Time
	held       bool
	double     bool
	timer      *time.Timer
	event      native.DeviceEvent
}

type rfClickState struct {
	releasedAt time.Time
	timer      *time.Timer
	event      native.DeviceEvent
}

type Runtime struct {
	options Options

	mu                     sync.RWMutex
	session                *link.Session
	port                   ports.Info
	hello                  native.Hello
	status                 native.Status
	settings               native.Settings
	haveStatus             bool
	haveSettings           bool
	frontPanel             native.FrontPanel
	haveFrontPanel         bool
	frontPanelUpdated      time.Time
	statusLED              native.StatusLEDState
	haveStatusLED          bool
	statusLEDUpdated       time.Time
	statusUpdated          time.Time
	paused                 bool
	connecting             bool
	generation             uint64
	connectionState        string
	connectionReason       string
	connectionUpdated      time.Time
	reconnectEpoch         uint64
	resetIssued            bool
	deviceObserver         func(ports.Info, native.Hello)
	connectionReadyHandler func(ports.Info, native.Hello)
	beforeDisconnect       func(string)
	connectionObserverMu   sync.RWMutex
	connectionObservers    map[uint64]func(uint64, ports.Info, native.Hello)
	nextConnectionObserver uint64

	events chan Event

	eventMu     sync.Mutex
	eventLog    []Event
	activityLog []Event
	nextEventID uint64
	eventNotify chan struct{}
	rfMu        sync.Mutex
	rfGestures  map[rfGestureKey]*rfGestureState
	rfClicks    map[rfGestureKey]*rfClickState

	rfLearnMu    sync.RWMutex
	rfLearnState RFLearnState

	historyConfigureMu         sync.Mutex
	historyMu                  sync.RWMutex
	historyRetention           time.Duration
	historySampleEvery         time.Duration
	historyLastSample          time.Time
	statusHistory              []StatusSample
	timeline                   []TimelineEntry
	timelineLimit              int
	timelinePath               string
	historyWriteOnce           sync.Once
	historyWrites              chan TimelineEntry
	statusHistoryPath          string
	statusHistoryWriteOnce     sync.Once
	statusHistoryWrites        chan measurementWrite
	lcdPresenter               *LCDPresenter
	programState               *ProgramStateManager
	programStateSyncMu         sync.Mutex
	programmingMu              sync.Mutex
	programStateSent           bool
	programStateSentGeneration uint64
	programStateSentRevision   uint64
	programStateSentMode       ProgramMode
	macroRunner                *MacroRunner
	displayMu                  sync.Mutex
	lcdMessageCancel           context.CancelFunc

	actionObserverMu        sync.RWMutex
	actionObservers         map[uint64]func(ActionEvidence)
	nextActionObserver      uint64
	macroStatusObserverMu   sync.RWMutex
	macroStatusObservers    map[uint64]func(native.MacroStatus, uint64)
	nextMacroStatusObserver uint64
	hostMenuRequestMu       sync.RWMutex
	hostMenuRequestHandler  func(native.HostMenuContentRequest)
}

const programStateHeartbeatPeriod = 2 * time.Second

func New(options Options) *Runtime {
	options = normalizedOptions(options)
	runtime := &Runtime{
		options: options, events: make(chan Event, 512),
		eventNotify:        make(chan struct{}),
		connectionState:    "disconnected",
		connectionUpdated:  time.Now(),
		historyRetention:   24 * time.Hour,
		historySampleEvery: time.Second,
		timelineLimit:      2000,
	}
	runtime.programState = NewProgramStateManager(func(state ProgramStateSnapshot) {
		runtime.publishEvent(Event{
			Kind: "program.state", Lifecycle: "changed",
			State: string(state.Mode), Reason: state.Reason,
			Text: fmt.Sprintf("program state %s: %s", state.Mode, state.Reason),
		})
		go runtime.syncProgramState(state, "changed")
	})
	runtime.lcdPresenter = NewLCDPresenter(runtime)
	return runtime
}

func (runtime *Runtime) LCDPresenter() *LCDPresenter {
	return runtime.lcdPresenter
}

func (runtime *Runtime) ProgramState() ProgramStateSnapshot {
	return runtime.programState.Snapshot()
}

// MacroRunner returns the single runner registered by the command engine so
// interactive views observe the same library, recorder, and playback state.
func (runtime *Runtime) MacroRunner() *MacroRunner {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.macroRunner
}

func (runtime *Runtime) setMacroRunner(runner *MacroRunner) {
	runtime.mu.Lock()
	runtime.macroRunner = runner
	runtime.mu.Unlock()
}

func (runtime *Runtime) SetProgramState(owner string, mode ProgramMode, reason string) (ProgramStateSnapshot, error) {
	return runtime.programState.Set(owner, mode, reason)
}

func (runtime *Runtime) AcquireProgramState(owner, reason string) (*ProgramStateLease, ProgramStateSnapshot, error) {
	return runtime.programState.Acquire(owner, reason)
}

func normalizedOptions(options Options) Options {
	if options.BaudRate == 0 {
		options.BaudRate = link.DefaultBaudRate
	}
	if options.StartupWait == 0 {
		options.StartupWait = 1200 * time.Millisecond
	}
	if options.RequestTimeout == 0 {
		options.RequestTimeout = 1200 * time.Millisecond
	}
	if options.HelloAttempts == 0 {
		options.HelloAttempts = 3
	}
	return options
}

func (runtime *Runtime) Events() <-chan Event {
	return runtime.events
}

// ObserveActions registers a lightweight macro-recorder input. Host actions
// arrive in acknowledgement order; physical/RF actions arrive in board event
// order. Both use the same MCU timestamp domain. The release is idempotent.
func (runtime *Runtime) ObserveActions(observer func(ActionEvidence)) func() {
	if observer == nil {
		return func() {}
	}
	runtime.actionObserverMu.Lock()
	if runtime.actionObservers == nil {
		runtime.actionObservers = make(map[uint64]func(ActionEvidence))
	}
	runtime.nextActionObserver++
	id := runtime.nextActionObserver
	runtime.actionObservers[id] = observer
	runtime.actionObserverMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			runtime.actionObserverMu.Lock()
			delete(runtime.actionObservers, id)
			runtime.actionObserverMu.Unlock()
		})
	}
}

func (runtime *Runtime) PublishHostEvent(kind, text string) {
	runtime.publish(kind, text, native.Frame{})
}

func (runtime *Runtime) PublishStructuredEvent(event Event) Event {
	return runtime.publishEvent(event)
}

// SetHostMenuRequestHandler installs the optional host-menu content responder. It
// receives only validated unsolicited schema-1 requests and never runs on the
// serial pump goroutine.
func (runtime *Runtime) SetHostMenuRequestHandler(handler func(native.HostMenuContentRequest)) {
	runtime.hostMenuRequestMu.Lock()
	runtime.hostMenuRequestHandler = handler
	runtime.hostMenuRequestMu.Unlock()
}

func (runtime *Runtime) dispatchHostMenuRequest(request native.HostMenuContentRequest) {
	runtime.hostMenuRequestMu.RLock()
	handler := runtime.hostMenuRequestHandler
	runtime.hostMenuRequestMu.RUnlock()
	if handler != nil {
		go handler(request)
	}
}

func (runtime *Runtime) LatestEventID() uint64 {
	runtime.eventMu.Lock()
	defer runtime.eventMu.Unlock()
	return runtime.nextEventID
}

func (runtime *Runtime) WaitEvent(
	ctx context.Context,
	afterID uint64,
	kind string,
) (Event, error) {
	return runtime.WaitEventFilter(ctx, afterID, kind, nil)
}

// WaitEventFilter supports the versionless opcode explorer path without
// teaching the host about each experimental opcode. A nil opcode accepts any
// frame; an exact opcode also matches currently unknown unsolicited frames.
func (runtime *Runtime) WaitEventFilter(
	ctx context.Context,
	afterID uint64,
	kind string,
	opcode *byte,
) (Event, error) {
	return runtime.WaitEventStreamFilter(ctx, afterID, kind, opcode, "")
}

// WaitEventStreamFilter selects an independent semantic stream. The activity
// ring is retained separately so animation frames and measurements cannot
// evict useful one-shot events before a UI or bridge consumer receives them.
func (runtime *Runtime) WaitEventStreamFilter(
	ctx context.Context,
	afterID uint64,
	kind string,
	opcode *byte,
	stream string,
) (Event, error) {
	stream = strings.ToLower(strings.TrimSpace(stream))
	if stream != "" && stream != EventStreamActivity && stream != EventStreamState &&
		stream != EventStreamTelemetry && stream != EventStreamDebug {
		return Event{}, fmt.Errorf("unknown event stream %q", stream)
	}
	for {
		runtime.eventMu.Lock()
		events := runtime.eventLog
		if stream == EventStreamActivity {
			events = runtime.activityLog
		}
		for _, event := range events {
			if event.ID > afterID &&
				(kind == "" || eventKindMatches(kind, event.Kind)) &&
				(stream == "" || event.Stream == stream) &&
				(opcode == nil || event.Frame.Opcode == *opcode) {
				runtime.eventMu.Unlock()
				return event, nil
			}
		}
		notify := runtime.eventNotify
		runtime.eventMu.Unlock()
		select {
		case <-ctx.Done():
			return Event{}, ctx.Err()
		case <-notify:
		}
	}
}

const (
	EventStreamActivity  = "activity"
	EventStreamState     = "state"
	EventStreamTelemetry = "telemetry"
	EventStreamDebug     = "debug"
)

// EventStreamForKind is the canonical cross-UI classification. Continuous
// streams still update live state immediately, but stay out of human activity
// feeds unless a caller explicitly subscribes to state/telemetry/debug.
func EventStreamForKind(kind string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "telemetry":
		return EventStreamTelemetry
	case "rx", "tx", "opcode", "action.applied":
		return EventStreamDebug
	case "front_panel.segment", "status_led.changed", "buzzer.note":
		return EventStreamState
	}
	if strings.HasPrefix(kind, "measurement.") || strings.HasSuffix(kind, ".measurement") ||
		strings.HasSuffix(kind, ".sample") {
		return EventStreamTelemetry
	}
	if strings.HasSuffix(kind, ".frame") {
		return EventStreamState
	}
	return EventStreamActivity
}

func IsActivityEvent(event Event) bool {
	stream := strings.ToLower(strings.TrimSpace(event.Stream))
	if stream == "" {
		stream = EventStreamForKind(event.Kind)
	}
	return stream == EventStreamActivity
}

func eventKindMatches(requested, actual string) bool {
	requested = strings.ToLower(strings.TrimSpace(requested))
	actual = strings.ToLower(strings.TrimSpace(actual))
	return requested == actual ||
		(requested != "" && strings.HasPrefix(actual, requested+"."))
}

func (runtime *Runtime) Snapshot() Snapshot {
	programState := runtime.ProgramState()
	rfLearning := runtime.RFLearnState()
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return Snapshot{
		Connected:         runtime.session != nil,
		Generation:        runtime.generation,
		Paused:            runtime.paused,
		Port:              runtime.port,
		Hello:             runtime.hello,
		Status:            runtime.status,
		Settings:          runtime.settings,
		HaveStatus:        runtime.haveStatus,
		HaveSettings:      runtime.haveSettings,
		StatusUpdated:     runtime.statusUpdated,
		ConnectionState:   runtime.connectionState,
		ConnectionReason:  runtime.connectionReason,
		ConnectionUpdated: runtime.connectionUpdated,
		FrontPanel:        runtime.frontPanel, HaveFrontPanel: runtime.haveFrontPanel,
		FrontPanelUpdated: runtime.frontPanelUpdated,
		StatusLED:         runtime.statusLED, HaveStatusLED: runtime.haveStatusLED,
		StatusLEDUpdated: runtime.statusLEDUpdated,
		ProgramState:     programState,
		RFLearning:       rfLearning,
	}
}

func (runtime *Runtime) SetFilter(filter ports.Filter) {
	runtime.mu.Lock()
	runtime.options.Filter = filter
	runtime.mu.Unlock()
}

func (runtime *Runtime) SetDeviceObserver(
	observer func(ports.Info, native.Hello),
) {
	runtime.mu.Lock()
	runtime.deviceObserver = observer
	runtime.mu.Unlock()
}

// SetConnectionReadyHandler installs an application-service hook that runs
// after authenticated HELLO on every initial connection/reconnect. Device
// identity persistence remains a separate observer.
func (runtime *Runtime) SetConnectionReadyHandler(handler func(ports.Info, native.Hello)) {
	runtime.mu.Lock()
	runtime.connectionReadyHandler = handler
	runtime.mu.Unlock()
}

// ObserveConnectionReady adds a non-exclusive service hook for every
// authenticated initial connection and reconnect. Unlike
// SetConnectionReadyHandler, independent subsystems cannot replace each
// other's callback. The release function is idempotent.
func (runtime *Runtime) ObserveConnectionReady(
	observer func(uint64, ports.Info, native.Hello),
) func() {
	if observer == nil {
		return func() {}
	}
	runtime.connectionObserverMu.Lock()
	if runtime.connectionObservers == nil {
		runtime.connectionObservers = make(map[uint64]func(uint64, ports.Info, native.Hello))
	}
	runtime.nextConnectionObserver++
	id := runtime.nextConnectionObserver
	runtime.connectionObservers[id] = observer
	runtime.connectionObserverMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			runtime.connectionObserverMu.Lock()
			delete(runtime.connectionObservers, id)
			runtime.connectionObserverMu.Unlock()
		})
	}
}

func (runtime *Runtime) notifyConnectionReady(
	generation uint64,
	port ports.Info,
	hello native.Hello,
) {
	runtime.connectionObserverMu.RLock()
	observers := make([]func(uint64, ports.Info, native.Hello), 0, len(runtime.connectionObservers))
	for _, observer := range runtime.connectionObservers {
		observers = append(observers, observer)
	}
	runtime.connectionObserverMu.RUnlock()
	for _, observer := range observers {
		go observer(generation, port, hello)
	}
}

// SetBeforeDisconnect installs one synchronous host-side fail-safe hook. The
// hook runs while the current serial session is still usable and must return
// promptly; it is never called for a transport that is already detached.
func (runtime *Runtime) SetBeforeDisconnect(observer func(string)) {
	runtime.mu.Lock()
	runtime.beforeDisconnect = observer
	runtime.mu.Unlock()
}

// ApplyOptions updates PC-side transport settings. If a live connection would
// no longer match, it is closed cleanly and authenticated auto-reconnect is
// armed. It never writes controller EEPROM or any board setting.
func (runtime *Runtime) ApplyOptions(options Options) bool {
	options = normalizedOptions(options)
	runtime.mu.RLock()
	previous := runtime.options
	changed := previous != options
	runtime.mu.RUnlock()
	if !changed {
		return false
	}
	previous.ResetOnReconnect = options.ResetOnReconnect
	previous.Filter.Preferred = options.Filter.Preferred
	if previous == options {
		runtime.mu.Lock()
		runtime.options.ResetOnReconnect = options.ResetOnReconnect
		runtime.options.Filter.Preferred = options.Filter.Preferred
		runtime.mu.Unlock()
		runtime.publish(
			"config",
			fmt.Sprintf("reset_on_reconnect=%t applied without reconnect", options.ResetOnReconnect),
			native.Frame{},
		)
		return true
	}
	_ = runtime.detachReason(false, "connection configuration changed")
	runtime.mu.Lock()
	runtime.options = options
	runtime.paused = false
	runtime.connectionState = "reconnecting"
	runtime.connectionReason = "connection configuration changed"
	runtime.connectionUpdated = time.Now()
	runtime.reconnectEpoch++
	epoch := runtime.reconnectEpoch
	runtime.resetIssued = true // A configuration reload is not a USB reappearance.
	runtime.mu.Unlock()
	runtime.publish(
		"config",
		"connection configuration changed; authenticated reconnect armed",
		native.Frame{},
	)
	runtime.publishConnection(
		"reconnecting",
		runtime.Snapshot().Port,
		"connection configuration changed",
	)
	go runtime.autoReconnect(epoch)
	return true
}

func (runtime *Runtime) ResumeAuto() {
	runtime.mu.Lock()
	runtime.paused = false
	runtime.mu.Unlock()
}

// Reconnect deliberately releases the application UART, publishes the full
// lifecycle transition, and authenticates a fresh application HELLO. It is
// used after an acknowledged MCU reset; that expected transition must not
// consume the physical-reappearance DTR reset policy.
func (runtime *Runtime) Reconnect(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "reconnect requested by host"
	}
	closeErr := runtime.detachReason(false, reason)
	runtime.mu.Lock()
	runtime.paused = false
	runtime.connectionState = "reconnecting"
	runtime.connectionReason = reason
	runtime.connectionUpdated = time.Now()
	runtime.reconnectEpoch++
	epoch := runtime.reconnectEpoch
	runtime.resetIssued = true
	port := runtime.port
	runtime.mu.Unlock()
	runtime.publishConnection("reconnecting", port, reason)
	connectErr := runtime.EnsureConnected(ctx)
	if connectErr != nil {
		go runtime.autoReconnect(epoch)
	}
	return errors.Join(closeErr, connectErr)
}

func (runtime *Runtime) EnsureConnected(ctx context.Context) error {
	runtime.mu.Lock()
	if runtime.session != nil {
		runtime.mu.Unlock()
		return nil
	}
	if runtime.paused {
		runtime.mu.Unlock()
		return errors.New("automatic connection is paused")
	}
	if runtime.connecting {
		runtime.mu.Unlock()
		return nil
	}
	runtime.connecting = true
	options := runtime.options
	runtime.mu.Unlock()

	defer func() {
		runtime.mu.Lock()
		runtime.connecting = false
		runtime.mu.Unlock()
	}()

	result, err := link.AutoOpen(ctx, runtime.discoveryOptions(options))
	if err != nil {
		return err
	}
	runtime.attach(result)
	return nil
}

func (runtime *Runtime) Open(ctx context.Context, name string) error {
	if link.IsNetworkEndpoint(name) {
		runtime.mu.RLock()
		options := runtime.options
		runtime.mu.RUnlock()
		result, err := link.OpenAuthenticated(
			ctx,
			ports.Info{Name: name, Product: productidentity.DefaultAppTitle() + " Virtual Board"},
			link.DiscoveryOptions{
				BaudRate: options.BaudRate, StartupWait: options.StartupWait,
				RequestTimeout: options.RequestTimeout,
				HelloAttempts:  options.HelloAttempts,
				ResetAfterOpen: runtime.resetAfterOpen,
			},
		)
		if err != nil {
			return err
		}
		if runtime.currentSession() != nil {
			runtime.detachReason(false, "port changed by host")
		}
		runtime.mu.Lock()
		runtime.paused = false
		runtime.mu.Unlock()
		runtime.attach(result)
		return nil
	}
	selector, err := ports.ParseSelector(name)
	if err != nil {
		return err
	}
	// Opening the device already owned by this primary is an idempotent select,
	// not a second serial open. Secondary programmer clients use this path to
	// confirm an explicit COM/friendly-name/VID:PID selector before delegating.
	current := runtime.Snapshot()
	if current.Connected &&
		len(ports.Candidates([]ports.Info{current.Port}, selector)) == 1 {
		runtime.mu.Lock()
		runtime.paused = false
		runtime.mu.Unlock()
		return nil
	}
	all, err := ports.List()
	if err != nil {
		return err
	}
	candidates := ports.Candidates(all, selector)
	if len(candidates) == 0 {
		return fmt.Errorf("serial device %q was not found", name)
	}
	if len(candidates) > 1 {
		return &ports.AmbiguousError{
			Candidates: append([]ports.Info(nil), candidates...),
		}
	}

	runtime.mu.RLock()
	options := runtime.options
	runtime.mu.RUnlock()
	result, err := link.OpenAuthenticated(ctx, candidates[0], link.DiscoveryOptions{
		BaudRate: options.BaudRate, StartupWait: options.StartupWait,
		RequestTimeout: options.RequestTimeout,
		HelloAttempts:  options.HelloAttempts,
		ResetAfterOpen: runtime.resetAfterOpen,
	})
	if err != nil {
		return err
	}
	if runtime.currentSession() != nil {
		runtime.detachReason(false, "port changed by host")
	}
	runtime.mu.Lock()
	runtime.paused = false
	runtime.mu.Unlock()
	runtime.attach(result)
	return nil
}

func (runtime *Runtime) Close() error {
	runtime.cancelDisplaySchedules()
	return runtime.detach(true)
}

func (runtime *Runtime) Request(
	ctx context.Context,
	opcode byte,
	payload []byte,
	expected ...byte,
) (native.Frame, error) {
	runtime.mu.RLock()
	session := runtime.session
	generation := runtime.generation
	runtime.mu.RUnlock()
	if session == nil {
		return native.Frame{}, errors.New("device is not connected")
	}
	frame, err := session.Request(ctx, opcode, payload, expected...)
	if err != nil {
		return native.Frame{}, err
	}
	if !runtime.observeAtGeneration(frame, generation) {
		return native.Frame{}, fmt.Errorf("connection generation %d changed while the request was in flight", generation)
	}
	runtime.publishAcknowledgedHostAction(opcode, payload, frame, generation)
	return frame, nil
}

// requestAtGeneration pins one request to the authenticated session that
// produced an asynchronous lifecycle token. It can never fall through to a
// replacement board after reconnect, even if the old request completes late.
func (runtime *Runtime) requestAtGeneration(
	ctx context.Context,
	generation uint64,
	opcode byte,
	payload []byte,
	expected ...byte,
) (native.Frame, error) {
	runtime.mu.RLock()
	if runtime.generation != generation || runtime.session == nil {
		runtime.mu.RUnlock()
		return native.Frame{}, fmt.Errorf("connection generation %d is no longer active", generation)
	}
	session := runtime.session
	runtime.mu.RUnlock()

	frame, err := session.Request(ctx, opcode, payload, expected...)
	if err != nil {
		return native.Frame{}, err
	}
	runtime.mu.RLock()
	current := runtime.generation == generation && runtime.session == session
	runtime.mu.RUnlock()
	if !current {
		return native.Frame{}, fmt.Errorf("connection generation %d changed while the request was in flight", generation)
	}
	if !runtime.observeAtGeneration(frame, generation) {
		return native.Frame{}, fmt.Errorf("connection generation %d changed before its response could be observed", generation)
	}
	return frame, nil
}

func (runtime *Runtime) Command(
	ctx context.Context,
	opcode byte,
	payload []byte,
) error {
	snapshot := runtime.Snapshot()
	if !snapshot.Connected {
		return errors.New("device is not connected")
	}
	frame, err := runtime.requestAtGeneration(
		ctx, snapshot.Generation, opcode, payload, native.OpACK,
	)
	if err != nil {
		return err
	}
	runtime.publishAcknowledgedHostAction(opcode, payload, frame, snapshot.Generation)
	return nil
}

// publishAcknowledgedHostAction is the one recorder ingress for both typed
// Command calls and raw Request/ExchangeOpcode surfaces. Board SourceHost
// echoes remain filtered, so each accepted operation is recorded exactly once.
func (runtime *Runtime) publishAcknowledgedHostAction(
	opcode byte,
	payload []byte,
	frame native.Frame,
	generation uint64,
) bool {
	if frame.Opcode != native.OpACK ||
		!native.MacroPlaybackPayloadSemanticallyValid(opcode, payload) {
		return false
	}
	deviceMicros, timed := native.ResponseDeviceMicros(frame)
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: opcode, Payload: append([]byte(nil), payload...),
		Source: native.InputSourceHost, SourceID: 0xFF,
		DeviceMicros: deviceMicros, Timed: timed, ObservedAt: time.Now(),
		Generation: generation,
	})
	return true
}

func (runtime *Runtime) publishActionEvidence(evidence ActionEvidence) {
	runtime.actionObserverMu.RLock()
	observers := make([]func(ActionEvidence), 0, len(runtime.actionObservers))
	for _, observer := range runtime.actionObservers {
		observers = append(observers, observer)
	}
	runtime.actionObserverMu.RUnlock()
	for _, observer := range observers {
		copyEvidence := evidence
		copyEvidence.Payload = append([]byte(nil), evidence.Payload...)
		observer(copyEvidence)
	}
}

// ObserveMacroStatuses subscribes board-local capture and playback services
// without competing for Runtime.Events. The callback receives a value copy in
// UART order; the returned release function is idempotent.
func (runtime *Runtime) ObserveMacroStatuses(observer func(native.MacroStatus, uint64)) func() {
	if observer == nil {
		return func() {}
	}
	runtime.macroStatusObserverMu.Lock()
	if runtime.macroStatusObservers == nil {
		runtime.macroStatusObservers = make(map[uint64]func(native.MacroStatus, uint64))
	}
	runtime.nextMacroStatusObserver++
	id := runtime.nextMacroStatusObserver
	runtime.macroStatusObservers[id] = observer
	runtime.macroStatusObserverMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			runtime.macroStatusObserverMu.Lock()
			delete(runtime.macroStatusObservers, id)
			runtime.macroStatusObserverMu.Unlock()
		})
	}
}

func (runtime *Runtime) publishBoardAction(event native.DeviceEvent, generation uint64) {
	if event.Type != native.EventAction {
		return
	}
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: event.ActionOpcode, Payload: append([]byte(nil), event.ActionPayload...),
		Source: event.Source, SourceID: event.SourceID, BoardOrigin: true,
		DeviceMicros: event.DeviceMicros, Timed: event.Timed, ObservedAt: time.Now(),
		Generation: generation,
	})
}

func (runtime *Runtime) publishMacroStatus(status native.MacroStatus, generation ...uint64) {
	value := runtime.Snapshot().Generation
	if len(generation) != 0 {
		value = generation[0]
	}
	runtime.macroStatusObserverMu.RLock()
	observers := make([]func(native.MacroStatus, uint64), 0, len(runtime.macroStatusObservers))
	for _, observer := range runtime.macroStatusObservers {
		observers = append(observers, observer)
	}
	runtime.macroStatusObserverMu.RUnlock()
	for _, observer := range observers {
		observer(status, value)
	}
}

func (runtime *Runtime) RefreshFrontPanel(ctx context.Context) (native.FrontPanel, error) {
	frame, err := runtime.Request(ctx, native.OpFrontPanelGet, nil, native.OpFrontPanel)
	if err != nil {
		return native.FrontPanel{}, err
	}
	return native.ParseFrontPanel(frame.Payload)
}

func (runtime *Runtime) WriteRaw(data []byte) error {
	session := runtime.currentSession()
	if session == nil {
		return errors.New("device is not connected")
	}
	return session.WriteRaw(data)
}

func (runtime *Runtime) PulseReset(ctx context.Context) error {
	return runtime.PulseResetFor(ctx, 120*time.Millisecond)
}

func (runtime *Runtime) PulseResetFor(ctx context.Context, duration time.Duration) error {
	session := runtime.currentSession()
	if session == nil {
		return errors.New("device is not connected")
	}
	runtime.publish("tx", "pulsing DTR reset", native.Frame{})
	return session.PulseReset(ctx, duration)
}

func (runtime *Runtime) RefreshStatus(ctx context.Context) (native.Status, error) {
	frame, err := runtime.Request(ctx, native.OpGetStatus, nil, native.OpStatus)
	if err != nil {
		return native.Status{}, err
	}
	return native.ParseStatus(frame.Payload)
}

func (runtime *Runtime) currentSession() *link.Session {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.session
}

func (runtime *Runtime) discoveryOptions(options Options) link.DiscoveryOptions {
	runtime.mu.RLock()
	allowPortRebind := runtime.connectionState == "reconnecting" &&
		runtime.session == nil
	runtime.mu.RUnlock()
	return link.DiscoveryOptions{
		Filter: options.Filter, BaudRate: options.BaudRate,
		StartupWait: options.StartupWait, RequestTimeout: options.RequestTimeout,
		HelloAttempts:   options.HelloAttempts,
		ResetAfterOpen:  runtime.resetAfterOpen,
		AllowPortRebind: allowPortRebind,
	}
}

// resetAfterOpen consumes the reconnect reset permit before pulsing. Failed
// authentication and subsequent HELLO retries therefore cannot create a reset
// storm. TCP transports never call this hook.
func (runtime *Runtime) resetAfterOpen(_ ports.Info) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.connectionState != "reconnecting" ||
		!runtime.options.ResetOnReconnect ||
		runtime.resetIssued {
		return false
	}
	runtime.resetIssued = true
	return true
}

func (runtime *Runtime) attach(result link.OpenResult) {
	runtime.mu.Lock()
	reconnected := runtime.connectionState == "reconnecting"
	runtime.generation++
	generation := runtime.generation
	runtime.session = result.Session
	runtime.port = result.Port
	runtime.hello = result.Hello
	runtime.haveStatus = false
	runtime.haveSettings = false
	runtime.haveFrontPanel = false
	runtime.frontPanel = native.FrontPanel{}
	runtime.frontPanelUpdated = time.Time{}
	runtime.connectionState = "connected"
	runtime.connectionReason = ""
	runtime.connectionUpdated = time.Now()
	runtime.reconnectEpoch++
	observer := runtime.deviceObserver
	ready := runtime.connectionReadyHandler
	runtime.mu.Unlock()

	if observer != nil {
		observer(result.Port, result.Hello)
	}
	if ready != nil {
		go ready(result.Port, result.Hello)
	}
	runtime.notifyConnectionReady(generation, result.Port, result.Hello)
	go runtime.pump(result.Session, generation)
	go runtime.syncProgramState(runtime.ProgramState(), "connected")
	go runtime.provisionDefaultStatusProfiles(generation)
	go runtime.programStateHeartbeat(generation)

	lifecycle := "connect"
	if reconnected {
		lifecycle = "reconnected"
	}
	runtime.publishConnection(
		lifecycle,
		result.Port,
		"",
	)
}

// provisionDefaultStatusProfiles installs the Go-owned factory table only
// into missing/corrupt EEPROM slots. Existing user profiles are never
// overwritten, and older firmware simply omits the capability.
func (runtime *Runtime) provisionDefaultStatusProfiles(generation uint64) {
	runtime.mu.RLock()
	connected := runtime.session != nil && runtime.generation == generation
	capabilities := runtime.hello.Capabilities
	timeout := runtime.options.RequestTimeout
	runtime.mu.RUnlock()
	if !connected || capabilities&native.CapabilityStatusProfiles == 0 {
		return
	}
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	if !runtime.provisionDefaultSettings(generation, timeout) {
		return
	}
	profiles := native.DefaultStatusProfiles(native.FactoryStatusBrightness)
	written := 0
	for condition, factory := range profiles {
		runtime.mu.RLock()
		current := runtime.session != nil && runtime.generation == generation
		runtime.mu.RUnlock()
		if !current {
			return
		}
		payload, _ := native.StatusProfileGetPayload(byte(condition))
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		frame, err := runtime.Request(ctx, native.OpStatusProfileGet, payload, native.OpStatusProfile)
		cancel()
		if err != nil {
			runtime.publishEvent(Event{Kind: "status_led.profile.provision", Lifecycle: "failed", Reason: err.Error(), Text: "factory status profile query failed: " + err.Error()})
			return
		}
		profile, err := native.ParseStatusProfile(frame.Payload)
		if err != nil {
			runtime.publishEvent(Event{Kind: "status_led.profile.provision", Lifecycle: "failed", Reason: err.Error(), Text: "factory status profile response failed validation: " + err.Error()})
			return
		}
		if profile.Persisted {
			continue
		}
		setPayload, err := native.StatusProfileSetPayload(byte(condition), factory)
		if err != nil {
			return
		}
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		err = runtime.Command(ctx, native.OpStatusProfileSet, setPayload)
		cancel()
		if err != nil {
			runtime.publishEvent(Event{Kind: "status_led.profile.provision", Lifecycle: "failed", Reason: err.Error(), Text: fmt.Sprintf("factory status profile %d write failed: %v", condition, err)})
			return
		}
		written++
	}
	if written != 0 {
		runtime.publishEvent(Event{
			Kind: "status_led.profile.provision", Lifecycle: "complete",
			Text:     fmt.Sprintf("installed %d missing host-owned factory status profiles", written),
			Metadata: map[string]string{"written": strconv.Itoa(written)},
		})
	}
}

func (runtime *Runtime) provisionDefaultSettings(generation uint64, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	frame, err := runtime.Request(ctx, native.OpGetSettings, nil, native.OpSettings)
	cancel()
	if err != nil {
		runtime.publishEvent(Event{Kind: "settings.factory.provision", Lifecycle: "failed", Reason: err.Error(), Text: "factory settings query failed: " + err.Error()})
		return false
	}
	settings, err := native.ParseSettings(frame.Payload)
	if err != nil {
		runtime.publishEvent(Event{Kind: "settings.factory.provision", Lifecycle: "failed", Reason: err.Error(), Text: "factory settings response failed validation: " + err.Error()})
		return false
	}
	if settings.Persisted {
		return true
	}
	runtime.mu.RLock()
	current := runtime.session != nil && runtime.generation == generation
	runtime.mu.RUnlock()
	if !current {
		return false
	}
	factory := native.DefaultSettings()
	payload, err := factory.Payload()
	if err != nil {
		return false
	}
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	err = runtime.Command(ctx, native.OpSetSettings, payload)
	cancel()
	if err != nil {
		runtime.publishEvent(Event{Kind: "settings.factory.provision", Lifecycle: "failed", Reason: err.Error(), Text: "factory settings write failed: " + err.Error()})
		return false
	}
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	frame, err = runtime.Request(ctx, native.OpGetSettings, nil, native.OpSettings)
	cancel()
	if err != nil {
		return false
	}
	confirmed, err := native.ParseSettings(frame.Payload)
	factory.Persisted = true
	if err != nil || !confirmed.Persisted || confirmed != factory {
		reason := "read-back did not match canonical host defaults"
		if err != nil {
			reason = err.Error()
		}
		runtime.publishEvent(Event{Kind: "settings.factory.provision", Lifecycle: "failed", Reason: reason, Text: "factory settings verification failed: " + reason})
		return false
	}
	runtime.publishEvent(Event{Kind: "settings.factory.provision", Lifecycle: "complete", Text: "installed and verified missing host-owned factory settings"})
	return true
}

// BoardName reads the operator name from the CRC-backed settings exchange.
func (runtime *Runtime) BoardName(ctx context.Context) (native.BoardName, error) {
	snapshot := runtime.Snapshot()
	if !snapshot.Connected || snapshot.Hello.Capabilities&native.CapabilityBoardName == 0 {
		return native.BoardName{}, errors.New("connected firmware does not support persistent board names")
	}
	frame, err := runtime.Request(ctx, native.OpGetSettings, nil, native.OpSettings)
	if err != nil {
		return native.BoardName{}, err
	}
	return native.ParseBoardNameFromSettings(frame.Payload)
}

// SetBoardName writes and independently reads back the operator name.
// Empty names deliberately clear the value while retaining a valid record.
func (runtime *Runtime) SetBoardName(ctx context.Context, name string) (native.BoardName, error) {
	if err := native.ValidateBoardName(name); err != nil {
		return native.BoardName{}, err
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Connected || snapshot.Hello.Capabilities&native.CapabilityBoardName == 0 {
		return native.BoardName{}, errors.New("connected firmware does not support persistent board names")
	}
	frame, err := runtime.Request(ctx, native.OpGetSettings, nil, native.OpSettings)
	if err != nil {
		return native.BoardName{}, fmt.Errorf("read settings before board-name write: %w", err)
	}
	settings, err := native.ParseSettings(frame.Payload)
	if err != nil {
		return native.BoardName{}, fmt.Errorf("validate settings before board-name write: %w", err)
	}
	payload, err := native.SettingsWithBoardNamePayload(settings, name)
	if err != nil {
		return native.BoardName{}, err
	}
	if err := runtime.Command(ctx, native.OpSetSettings, payload); err != nil {
		return native.BoardName{}, err
	}
	confirmed, err := runtime.BoardName(ctx)
	if err != nil {
		return native.BoardName{}, fmt.Errorf("read back board name: %w", err)
	}
	if !confirmed.Persisted || confirmed.Name != name {
		return native.BoardName{}, fmt.Errorf("board name readback=%q persisted=%t; wanted %q", confirmed.Name, confirmed.Persisted, name)
	}
	return confirmed, nil
}

// syncProgramState mirrors the latest host-owned semantic state after HELLO
// and after every state change. Serialization prevents stale concurrent state
// changes from becoming the board's final value; older firmware simply omits
// the capability and continues to interoperate.
func (runtime *Runtime) syncProgramState(snapshot ProgramStateSnapshot, lifecycle string) {
	runtime.programStateSyncMu.Lock()
	defer runtime.programStateSyncMu.Unlock()

	current := runtime.ProgramState()
	if current.Revision != snapshot.Revision {
		snapshot = current
	}
	runtime.mu.RLock()
	connected := runtime.session != nil
	capabilities := runtime.hello.Capabilities
	generation := runtime.generation
	timeout := runtime.options.RequestTimeout
	runtime.mu.RUnlock()
	if !connected || capabilities&native.CapabilityProgramState == 0 {
		return
	}
	if lifecycle != "heartbeat" && runtime.programStateSent &&
		runtime.programStateSentGeneration == generation &&
		runtime.programStateSentRevision == snapshot.Revision &&
		runtime.programStateSentMode == snapshot.Mode {
		return
	}
	payload := native.ProgramStatePayload(snapshot.Mode == ProgramRunning)
	if timeout <= 0 {
		timeout = 1200 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	err := runtime.Command(ctx, native.OpProgramState, payload)
	cancel()
	if err != nil {
		runtime.publishEvent(Event{
			Kind: "program.state.sync", Lifecycle: "failed",
			State: string(snapshot.Mode), Reason: err.Error(),
			Text: fmt.Sprintf("program state %s sync failed: %v", snapshot.Mode, err),
		})
		return
	}
	runtime.programStateSent = true
	runtime.programStateSentGeneration = generation
	runtime.programStateSentRevision = snapshot.Revision
	runtime.programStateSentMode = snapshot.Mode
	if lifecycle != "heartbeat" {
		runtime.publishEvent(Event{
			Kind: "program.state.sync", Lifecycle: lifecycle,
			State: string(snapshot.Mode),
			Text:  fmt.Sprintf("program state %s sent to board", snapshot.Mode),
		})
	}
}

// programStateHeartbeat keeps firmware's host-presence watchdog truthful even
// when no telemetry consumer is subscribed. It sends no status query and ends
// automatically when this authenticated connection generation changes.
func (runtime *Runtime) programStateHeartbeat(generation uint64) {
	ticker := time.NewTicker(programStateHeartbeatPeriod)
	defer ticker.Stop()
	for range ticker.C {
		runtime.mu.RLock()
		active := runtime.generation == generation && runtime.session != nil
		runtime.mu.RUnlock()
		if !active {
			return
		}
		runtime.syncProgramState(runtime.ProgramState(), "heartbeat")
	}
}

func (runtime *Runtime) detach(pause bool) error {
	reason := "connection replaced"
	if pause {
		reason = "closed by host"
	}
	return runtime.detachReason(pause, reason)
}

func (runtime *Runtime) detachReason(pause bool, reason string) error {
	runtime.mu.RLock()
	attached := runtime.session != nil
	beforeDisconnect := runtime.beforeDisconnect
	runtime.mu.RUnlock()
	if attached && beforeDisconnect != nil {
		beforeDisconnect(reason)
	}
	if attached && runtime.lcdPresenter != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		_ = runtime.lcdPresenter.PrepareDisconnect(ctx)
		cancel()
	}
	runtime.mu.Lock()
	session := runtime.session
	port := runtime.port
	runtime.generation++
	runtime.session = nil
	runtime.hello = native.Hello{}
	runtime.paused = pause
	runtime.reconnectEpoch++
	runtime.connectionState = "disconnected"
	runtime.connectionReason = reason
	runtime.connectionUpdated = time.Now()
	runtime.mu.Unlock()
	if session == nil {
		return nil
	}
	err := session.Close()
	runtime.publishConnection("disconnect", port, reason)
	return err
}

func (runtime *Runtime) pump(session *link.Session, generation uint64) {
	disconnectReason := "transport closed"
	for {
		select {
		case event := <-session.Events():
			// A replaced session may still have buffered frames when its read
			// goroutine unwinds. Reject them before any cache, action, or macro
			// observer can relabel the frame with the new connection generation.
			if !runtime.sessionGenerationCurrent(session, generation) {
				return
			}
			if event.Err != nil {
				disconnectReason = event.Err.Error()
				runtime.publish("error", event.Err.Error(), native.Frame{})
			} else {
				if !runtime.observeAtGeneration(event.Frame, generation) {
					return
				}
				kind := "rx"
				text := fmt.Sprintf(
					"%s seq=%d payload=% X",
					native.OpcodeName(event.Frame.Opcode),
					event.Frame.Seq,
					event.Frame.Payload,
				)
				var parsedDevice *native.DeviceEvent
				var parsedSegments *native.SegmentState
				var parsedBuzzer *native.BuzzerState
				var parsedStatusLED *native.StatusLEDState
				var rfMappingRequired bool
				var rfCaptured uint32
				var hostMenuRequest *native.HostMenuContentRequest
				var hostMenuState *native.HostMenuState
				if event.Frame.Opcode == native.OpStatus {
					kind = "telemetry"
					text = "live status updated"
				} else if event.Frame.Opcode == native.OpHelloResp {
					kind = "identity"
					text = "application HELLO received"
					// Identity bytes include fixed-width build metadata and are
					// useful only to a protocol debugger. Ordinary UI/IPC events
					// deliberately omit the raw HELLO payload.
					event.Frame.Payload = nil
				} else if event.Frame.Opcode == native.OpEvent {
					if parsed, err := native.ParseDeviceEvent(event.Frame.Payload); err == nil {
						rfMappingRequired, rfCaptured = runtime.observeRFLearningEvent(parsed)
						kind, text = describeDeviceEvent(parsed)
						parsedDevice = &parsed
						runtime.publishBoardAction(parsed, generation)
						if parsed.Macro != nil {
							runtime.publishMacroStatus(*parsed.Macro, generation)
						}
					} else {
						kind = "error"
						text = err.Error()
					}
				} else if event.Frame.Opcode == native.OpSegmentChanged {
					if state, err := native.ParseSegmentState(event.Frame.Payload); err == nil {
						kind = "front_panel.segment"
						text = "seven-segment display changed"
						parsedSegments = &state
					} else {
						kind, text = "error", err.Error()
					}
				} else if event.Frame.Opcode == native.OpBuzzerChanged {
					if state, err := native.ParseBuzzerState(event.Frame.Payload); err == nil {
						kind = "buzzer.note"
						text = fmt.Sprintf("buzzer note %d Hz for %d ms", state.FrequencyHz, state.DurationMS)
						parsedBuzzer = &state
					} else {
						kind, text = "error", err.Error()
					}
				} else if event.Frame.Opcode == native.OpStatusLEDChanged {
					if state, err := native.ParseStatusLEDState(event.Frame.Payload); err == nil {
						kind = "status_led.changed"
						text = fmt.Sprintf("status LED changed to #%02X%02X%02X", state.Red, state.Green, state.Blue)
						parsedStatusLED = &state
					} else {
						kind, text = "error", err.Error()
					}
				} else if event.Frame.Opcode == native.OpHostMenuRequest {
					if request, err := native.ParseHostMenuContentRequest(event.Frame.Payload); err == nil {
						kind = "menu.content.request"
						text = fmt.Sprintf("host-menu content requested node=0x%02X generation=%d reason=%d attempt=%d", request.ID, request.Generation, request.Reason, request.Attempt)
						hostMenuRequest = &request
					} else {
						kind, text = "error", err.Error()
					}
				} else if event.Frame.Opcode == native.OpHostMenuState {
					if state, err := native.ParseHostMenuState(event.Frame.Payload); err == nil {
						kind = "menu.content.applied"
						text = fmt.Sprintf("host-menu board state node=0x%02X generation=%d phase=%d attempt=%d revision=%d", state.ActiveID, state.Generation, state.Phase, state.Attempt, state.Revision)
						hostMenuState = &state
					} else {
						kind, text = "error", err.Error()
					}
				}
				if hostMenuRequest != nil {
					runtime.publishEvent(Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "request",
						Metadata: map[string]string{
							"generation": strconv.Itoa(int(hostMenuRequest.Generation)),
							"node_id":    fmt.Sprintf("0x%02X", hostMenuRequest.ID),
							"reason":     strconv.Itoa(int(hostMenuRequest.Reason)),
							"attempt":    strconv.Itoa(int(hostMenuRequest.Attempt)),
						},
					})
				} else if hostMenuState != nil {
					runtime.publishEvent(Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "reaction",
						Metadata: map[string]string{
							"generation": strconv.Itoa(int(hostMenuState.Generation)),
							"node_id":    fmt.Sprintf("0x%02X", hostMenuState.ActiveID),
							"phase":      strconv.Itoa(int(hostMenuState.Phase)),
							"attempt":    strconv.Itoa(int(hostMenuState.Attempt)),
							"revision":   strconv.Itoa(int(hostMenuState.Revision)),
						},
					})
				} else if parsedSegments != nil {
					runtime.publishEvent(Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "event",
						Metadata: map[string]string{
							"raw_segments": fmt.Sprintf("%02X%02X%02X%02X", parsedSegments.RawSegments[0], parsedSegments.RawSegments[1], parsedSegments.RawSegments[2], parsedSegments.RawSegments[3]),
							"brightness":   strconv.Itoa(int(parsedSegments.Brightness)),
						},
					})
				} else if parsedBuzzer != nil {
					runtime.publishEvent(Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "event",
						Metadata: map[string]string{
							"frequency_hz": strconv.Itoa(int(parsedBuzzer.FrequencyHz)),
							"duration_ms":  strconv.Itoa(int(parsedBuzzer.DurationMS)),
							"muted":        strconv.FormatBool(parsedBuzzer.Muted),
						},
					})
				} else if parsedStatusLED != nil {
					runtime.publishEvent(Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "event",
						Metadata: map[string]string{
							"red":        strconv.Itoa(int(parsedStatusLED.Red)),
							"green":      strconv.Itoa(int(parsedStatusLED.Green)),
							"blue":       strconv.Itoa(int(parsedStatusLED.Blue)),
							"hex":        fmt.Sprintf("#%02X%02X%02X", parsedStatusLED.Red, parsedStatusLED.Green, parsedStatusLED.Blue),
							"brightness": strconv.Itoa(int(parsedStatusLED.Brightness)),
							"effect":     strconv.Itoa(int(parsedStatusLED.Effect)),
							"condition":  strconv.Itoa(int(parsedStatusLED.Condition)),
						},
					})
				} else if parsedDevice != nil {
					deviceEvent := Event{
						Kind: kind, Text: text, Frame: event.Frame,
						Source: "board", Target: "host", MessageType: "event",
					}
					if parsedDevice.Type == native.EventAppNavigation {
						deviceEvent.Target = "app.clients"
						deviceEvent.Action = "navigate"
						deviceEvent.Metadata = map[string]string{
							"page": parsedDevice.AppPage, "value": parsedDevice.AppPage,
							"target_instance": parsedDevice.AppTarget,
						}
					} else if parsedDevice.Type == native.EventAction {
						deviceEvent.Metadata = map[string]string{
							"connection_generation": strconv.FormatUint(generation, 10),
							"device_micros":         strconv.FormatUint(uint64(parsedDevice.DeviceMicros), 10),
							"source":                inputSourceName(parsedDevice.Source),
							"source_id":             strconv.Itoa(int(parsedDevice.SourceID)),
							"opcode":                fmt.Sprintf("0x%02X", parsedDevice.ActionOpcode),
							"opcode_name":           native.OpcodeName(parsedDevice.ActionOpcode),
							"payload":               fmt.Sprintf("%X", parsedDevice.ActionPayload),
						}
					}
					runtime.publishEvent(deviceEvent)
				} else {
					runtime.publish(kind, text, event.Frame)
				}
				if rfMappingRequired && parsedDevice != nil {
					runtime.publishRFMappingRequired(*parsedDevice, rfCaptured)
				}
				if parsedDevice != nil &&
					parsedDevice.Type == native.EventRFReceived {
					runtime.observeRFGesture(*parsedDevice)
				}
				if hostMenuRequest != nil {
					runtime.dispatchHostMenuRequest(*hostMenuRequest)
				}
			}
		case <-session.Done():
			runtime.mu.Lock()
			owned := false
			var port ports.Info
			var epoch uint64
			if runtime.generation == generation && runtime.session == session {
				owned = true
				port = runtime.port
				runtime.session = nil
				runtime.hello = native.Hello{}
				runtime.connectionState = "reconnecting"
				runtime.connectionReason = disconnectReason
				runtime.connectionUpdated = time.Now()
				runtime.reconnectEpoch++
				epoch = runtime.reconnectEpoch
				runtime.resetIssued = false
			}
			runtime.mu.Unlock()
			if owned {
				runtime.markRFLearningDisconnected("device disconnected")
				runtime.publishConnection("disconnect", port, disconnectReason)
				runtime.publishConnection("reconnecting", port, disconnectReason)
				go runtime.autoReconnect(epoch)
			}
			return
		}
	}
}

func (runtime *Runtime) sessionGenerationCurrent(
	session *link.Session,
	generation uint64,
) bool {
	runtime.mu.RLock()
	defer runtime.mu.RUnlock()
	return runtime.session == session && runtime.generation == generation
}

func (runtime *Runtime) autoReconnect(epoch uint64) {
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	changes, watchErr := ports.WatchChanges(watchContext)
	if watchErr != nil {
		runtime.publish(
			"error",
			"serial device notifications unavailable; using safety retry: "+
				watchErr.Error(),
			native.Frame{},
		)
	}
	activityCheck := time.NewTicker(500 * time.Millisecond)
	defer activityCheck.Stop()
	safetyRetry := time.NewTimer(30 * time.Second)
	defer safetyRetry.Stop()
	attempt := true
	for {
		runtime.mu.RLock()
		active := runtime.reconnectEpoch == epoch &&
			runtime.connectionState == "reconnecting" &&
			!runtime.paused &&
			runtime.session == nil
		options := runtime.options
		runtime.mu.RUnlock()
		if !active {
			return
		}

		if attempt {
			attempt = false
			timeout := options.StartupWait +
				time.Duration(options.HelloAttempts)*options.RequestTimeout +
				2*time.Second
			if timeout < 3*time.Second {
				timeout = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := runtime.EnsureConnected(ctx)
			cancel()
			if err == nil && runtime.Snapshot().Connected {
				return
			}
			if err != nil {
				reason := err.Error()
				runtime.mu.Lock()
				changed := runtime.reconnectEpoch == epoch &&
					runtime.connectionState == "reconnecting" &&
					runtime.connectionReason != reason
				if changed {
					runtime.connectionReason = reason
					runtime.connectionUpdated = time.Now()
				}
				port := runtime.port
				runtime.mu.Unlock()
				if changed {
					runtime.publishConnection("reconnecting", port, reason)
				}
			}
		}

		select {
		case _, ok := <-changes:
			if !ok {
				changes = nil
			}
			attempt = true
		case <-activityCheck.C:
			// Re-check epoch/pause state without periodically enumerating.
		case <-safetyRetry.C:
			attempt = true
			safetyRetry.Reset(30 * time.Second)
		}
	}
}

func (runtime *Runtime) observe(frame native.Frame) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.observeLocked(frame)
}

func (runtime *Runtime) observeAtGeneration(frame native.Frame, generation uint64) bool {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.generation != generation || runtime.session == nil {
		return false
	}
	runtime.observeLocked(frame)
	return true
}

func (runtime *Runtime) observeLocked(frame native.Frame) {
	switch frame.Opcode {
	case native.OpStatus:
		if status, err := native.ParseStatus(frame.Payload); err == nil {
			now := time.Now()
			runtime.status = status
			runtime.haveStatus = true
			runtime.statusUpdated = now
			runtime.recordStatus(now, status)
		}
	case native.OpSettings:
		if settings, err := native.ParseSettings(frame.Payload); err == nil {
			runtime.settings = settings
			runtime.haveSettings = true
		}
	case native.OpFrontPanel:
		if panel, err := native.ParseFrontPanel(frame.Payload); err == nil {
			runtime.frontPanel = panel
			runtime.haveFrontPanel = true
			runtime.frontPanelUpdated = time.Now()
		}
	case native.OpSegmentChanged:
		if state, err := native.ParseSegmentState(frame.Payload); err == nil {
			if runtime.frontPanel.Schema == 0 {
				runtime.frontPanel.Schema = 2
			}
			runtime.frontPanel.RawSegments = state.RawSegments
			runtime.frontPanel.Brightness = state.Brightness
			runtime.frontPanel.SegmentsActive = true
			runtime.haveFrontPanel = true
			runtime.frontPanelUpdated = time.Now()
		}
	case native.OpStatusLEDChanged:
		if state, err := native.ParseStatusLEDState(frame.Payload); err == nil {
			runtime.statusLED = state
			runtime.haveStatusLED = true
			runtime.statusLEDUpdated = time.Now()
		}
	case native.OpEvent:
		if event, err := native.ParseDeviceEvent(frame.Payload); err == nil {
			switch event.Type {
			case native.EventDoor:
				runtime.status.DoorOpen = event.DoorOpen
				runtime.statusUpdated = time.Now()
			case native.EventBluetooth:
				runtime.status.BluetoothState = event.Bluetooth
				runtime.statusUpdated = time.Now()
			case native.EventPWMChannel:
				runtime.status.PWMChannel = event.PWMChannel
				runtime.statusUpdated = time.Now()
			case native.EventReset:
				runtime.status.ResetCause = event.ResetCause
				runtime.status.ResetCount = event.ResetCount
				runtime.statusUpdated = time.Now()
			case native.EventRelay:
				runtime.status.ActiveRelays = event.RelayMask
				runtime.statusUpdated = time.Now()
			case native.EventAlert:
				if event.AlertKind == native.AlertHot {
					runtime.status.Hot = event.AlertActive
					runtime.statusUpdated = time.Now()
				}
			}
		}
	}
}

func describeDeviceEvent(event native.DeviceEvent) (string, string) {
	switch event.Type {
	case native.EventKey:
		gesture := NormalizeGesture(event.Gesture)
		if gesture == "" {
			gesture = fmt.Sprintf("gesture-%d", event.Gesture)
		}
		source := inputSourceName(event.Source)
		if source == "" {
			source = fmt.Sprintf("source-%d", event.Source)
		}
		if event.SourceID != 0xFF {
			return "key", fmt.Sprintf(
				"key %d %s source=%s id=%d",
				event.Key+1,
				gesture,
				source,
				event.SourceID,
			)
		}
		return "key", fmt.Sprintf(
			"key %d %s source=%s",
			event.Key+1,
			gesture,
			source,
		)
	case native.EventDoor:
		if event.DoorOpen {
			return "door", "door opened"
		}
		return "door", "door closed"
	case native.EventBluetooth:
		return "bluetooth", fmt.Sprintf("Bluetooth indicator state %d", event.Bluetooth)
	case native.EventPWMChannel:
		return "pwm", fmt.Sprintf("automatic PWM channel changed to %d", event.PWMChannel)
	case native.EventRFLearned:
		return "rf.learn.capture", fmt.Sprintf("RF learned entry %d; mapping=unmapped", event.RFID)
	case native.EventMacro:
		if event.Macro != nil {
			state := map[byte]string{
				native.MacroIdle:      "idle",
				native.MacroBuffering: "buffering",
				native.MacroPlaying:   "playing",
				native.MacroCancelled: "cancelled",
				native.MacroCompleted: "completed",
				native.MacroFailed:    "failed",
				native.MacroRecording: "recording",
				native.MacroCaptured:  "captured",
				native.MacroExported:  "exported",
			}[event.Macro.State]
			if state == "" {
				state = fmt.Sprintf("state-%d", event.Macro.State)
			}
			return "macro", fmt.Sprintf(
				"macro %d %s steps=%d/%d buffer=%dB underruns=%d errors=%d",
				event.Macro.ID, state, event.Macro.ExecutedSteps,
				event.Macro.TotalSteps, event.Macro.Fill,
				event.Macro.Underruns, event.Macro.DispatchErrors,
			)
		}
		state := map[byte]string{
			native.MacroEventStarted:   "started",
			native.MacroEventStep:      "advanced",
			native.MacroEventCancelled: "cancelled",
			native.MacroEventCompleted: "completed",
		}[event.MacroState]
		if state == "" {
			state = fmt.Sprintf("state-%d", event.MacroState)
		}
		return "macro", fmt.Sprintf("macro %d %s", event.MacroID, state)
	case native.EventReset:
		return "reset", fmt.Sprintf(
			"controller boot/reset cause=0x%02X count=%d",
			event.ResetCause,
			event.ResetCount,
		)
	case native.EventRFReceived:
		learned := "unlearned"
		if event.RFLearnedID != 0xFF {
			learned = fmt.Sprintf("learned-id=%d", event.RFLearnedID)
		}
		return "rf.receive", fmt.Sprintf(
			"RF received code=%d bits=%d protocol=%d pulse=%dus %s",
			event.RFCode,
			event.RFBits,
			event.RFProtocol,
			event.RFPulseUS,
			learned,
		)
	case native.EventRFLearning:
		state, kind := "unknown", "rf.learn"
		switch event.RFLearnState {
		case native.RFLearningEnded:
			state, kind = "ended", "rf.learn.ended"
		case native.RFLearningCancelled:
			state, kind = "cancelled", "rf.learn.cancelled"
		case native.RFLearningFull:
			state, kind = "storage full", "rf.learn.full"
		case native.RFLearningStarted:
			state, kind = "started", "rf.learn.started"
		case native.RFLearningProgress:
			state, kind = "progress", "rf.learn.progress"
		}
		mode := "indefinite"
		if event.RFLearnMode == native.RFLearnModeTimer {
			mode = "timer"
		}
		return kind, fmt.Sprintf(
			"RF learning %s mode=%s stored=%d configured=%ds remaining=%ds",
			state, mode, event.RFLearnCount,
			event.RFLearnTotalSeconds, event.RFLearnRemainingSeconds,
		)
	case native.EventRelay:
		return "relay", fmt.Sprintf(
			"relay outputs changed; active mask=0x%02X",
			event.RelayMask,
		)
	case native.EventAlert:
		state := "cleared"
		if event.AlertActive {
			state = "active"
		}
		if event.AlertKind == native.AlertHot {
			return "hot", "temperature alert " + state
		}
		return "fault", "firmware fault " + state
	case native.EventAppNavigation:
		return "app.page", fmt.Sprintf(
			"board requested page %s for %s",
			event.AppPage,
			event.AppTarget,
		)
	case native.EventAction:
		source := inputSourceName(event.Source)
		if source == "" {
			source = fmt.Sprintf("source-%d", event.Source)
		}
		return "action.applied", fmt.Sprintf(
			"%s action %s payload=% X at MCU %d us",
			source, native.OpcodeName(event.ActionOpcode), event.ActionPayload,
			event.DeviceMicros,
		)
	default:
		return "event", fmt.Sprintf("device event %d payload=% X", event.Type, event.Raw)
	}
}

func (runtime *Runtime) observeRFGesture(event native.DeviceEvent) {
	now := time.Now()
	key := rfGestureKey{
		code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol,
	}
	gesture := ""
	var expiredClick *native.DeviceEvent
	runtime.rfMu.Lock()
	if runtime.rfGestures == nil {
		runtime.rfGestures = make(map[rfGestureKey]*rfGestureState)
	}
	if runtime.rfClicks == nil {
		runtime.rfClicks = make(map[rfGestureKey]*rfClickState)
	}
	state := runtime.rfGestures[key]
	if state == nil || now.Sub(state.lastSeen) > rfReleaseAfter {
		if state != nil && state.timer != nil {
			state.timer.Stop()
		}
		double := false
		if pending := runtime.rfClicks[key]; pending != nil {
			pending.timer.Stop()
			delete(runtime.rfClicks, key)
			if now.Sub(pending.releasedAt) <= rfDoubleClickAfter {
				double = true
			} else {
				value := pending.event
				expiredClick = &value
			}
		}
		state = &rfGestureState{
			firstSeen: now, lastSeen: now, lastRepeat: now, event: event,
			double: double,
		}
		runtime.rfGestures[key] = state
		gesture = "down"
	} else {
		state.lastSeen = now
		state.event = event
		if !state.held && now.Sub(state.firstSeen) >= rfHoldAfter {
			state.held = true
			state.lastRepeat = now
			gesture = "hold"
		} else if state.held &&
			now.Sub(state.lastRepeat) >= rfRepeatInterval(now.Sub(state.firstSeen)) {
			state.lastRepeat = now
			gesture = "repeat"
		}
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(rfReleaseAfter, func() {
		runtime.finishRFGesture(key, state)
	})
	runtime.rfMu.Unlock()
	if expiredClick != nil {
		runtime.publishRFGesture(*expiredClick, "click")
	}
	if gesture != "" {
		runtime.publishRFGesture(event, gesture)
	}
}

func (runtime *Runtime) finishRFGesture(
	key rfGestureKey,
	state *rfGestureState,
) {
	runtime.rfMu.Lock()
	if runtime.rfGestures[key] != state {
		runtime.rfMu.Unlock()
		return
	}
	delete(runtime.rfGestures, key)
	event := state.event
	held := state.held
	double := state.double
	if !held && !double {
		if runtime.rfClicks == nil {
			runtime.rfClicks = make(map[rfGestureKey]*rfClickState)
		}
		click := &rfClickState{releasedAt: time.Now(), event: event}
		click.timer = time.AfterFunc(rfDoubleClickAfter, func() {
			runtime.finishRFClick(key, click)
		})
		runtime.rfClicks[key] = click
	}
	runtime.rfMu.Unlock()
	runtime.publishRFGesture(event, "up")
	if double {
		runtime.publishRFGesture(event, "double-click")
	}
}

func (runtime *Runtime) finishRFClick(
	key rfGestureKey,
	click *rfClickState,
) {
	runtime.rfMu.Lock()
	if runtime.rfClicks[key] != click {
		runtime.rfMu.Unlock()
		return
	}
	delete(runtime.rfClicks, key)
	event := click.event
	runtime.rfMu.Unlock()
	runtime.publishRFGesture(event, "click")
}

func rfRepeatInterval(heldFor time.Duration) time.Duration {
	switch {
	case heldFor >= 4*time.Second:
		return 60 * time.Millisecond
	case heldFor >= 2*time.Second:
		return 100 * time.Millisecond
	default:
		return 150 * time.Millisecond
	}
}

func (runtime *Runtime) publishRFGesture(
	event native.DeviceEvent,
	gesture string,
) {
	learned := "unlearned"
	haveID := event.RFLearnedID != 0xFF
	if haveID {
		learned = fmt.Sprintf("learned-id=%d", event.RFLearnedID)
	}
	runtime.publishEvent(Event{
		Kind: "rf.gesture",
		Text: fmt.Sprintf(
			"RF %s code=%d bits=%d protocol=%d %s",
			gesture,
			event.RFCode,
			event.RFBits,
			event.RFProtocol,
			learned,
		),
		Gesture: gesture, Source: "rf",
		RFCode: event.RFCode, RFBits: event.RFBits,
		RFProtocol: event.RFProtocol, RFPulseUS: event.RFPulseUS,
		RFID: event.RFLearnedID, HaveRFID: haveID,
	})
}

func (runtime *Runtime) publish(kind, text string, frame native.Frame) {
	event := Event{Kind: kind, Text: text, Frame: frame}
	// Frames reaching this helper came from the device-facing serial pump. Keep
	// that provenance in every normalized event envelope, not only in the
	// special host-menu branches, so REST/RPC/WebSocket consumers can distinguish
	// board activity from host-generated notices.
	if frame.Opcode != 0 {
		event.Source = "board"
		event.Target = "host"
	}
	if frame.Opcode == native.OpEvent {
		if parsed, err := native.ParseDeviceEvent(frame.Payload); err == nil &&
			parsed.Type == native.EventReset {
			event.ResetCause = parsed.ResetCause
			event.ResetCount = parsed.ResetCount
		}
	}
	runtime.publishEvent(event)
}

func (runtime *Runtime) publishConnection(lifecycle string, port ports.Info, reason string) {
	state := lifecycle
	if lifecycle == "connect" || lifecycle == "reconnected" {
		state = "connected"
	} else if lifecycle == "disconnect" {
		state = "disconnected"
	}
	text := lifecycle
	if port.Name != "" {
		text += " " + port.Name
	}
	if reason != "" {
		text += ": " + reason
	}
	runtime.publishEvent(Event{
		Kind: "connection", Text: text,
		Lifecycle: lifecycle, Port: port, Reason: reason, State: state,
	})
	// Keep transport transitions first-class for every consumer of the common
	// event stream (Web UI, TUI, IPC, API, and relays), rather than making each
	// surface infer USB state from a generic connection string.
	usbKind := ""
	switch lifecycle {
	case "disconnect":
		usbKind = "usb.disconnected"
	case "reconnecting":
		usbKind = "usb.reconnecting"
	case "connect", "reconnected":
		if port.IsUSB {
			usbKind = "usb.reconnected"
		}
	}
	if usbKind != "" && port.IsUSB {
		runtime.publishEvent(Event{
			Kind: usbKind, Text: text, Lifecycle: lifecycle, Port: port,
			Reason: reason, State: state, Source: "host", Target: "app.clients",
		})
	}
}

func (runtime *Runtime) publishEvent(event Event) Event {
	event.Stream = strings.ToLower(strings.TrimSpace(event.Stream))
	if event.Stream == "" {
		event.Stream = EventStreamForKind(event.Kind)
	}
	if event.Metadata != nil {
		metadata := make(map[string]string, len(event.Metadata))
		for key, value := range event.Metadata {
			metadata[key] = value
		}
		event.Metadata = metadata
	}
	runtime.eventMu.Lock()
	runtime.nextEventID++
	event.ID = runtime.nextEventID
	event.Time = time.Now()
	runtime.eventLog = append(runtime.eventLog, event)
	if len(runtime.eventLog) > 512 {
		runtime.eventLog = append([]Event(nil), runtime.eventLog[len(runtime.eventLog)-512:]...)
	}
	if event.Stream == EventStreamActivity {
		runtime.activityLog = append(runtime.activityLog, event)
		if len(runtime.activityLog) > 512 {
			runtime.activityLog = append([]Event(nil), runtime.activityLog[len(runtime.activityLog)-512:]...)
		}
	}
	close(runtime.eventNotify)
	runtime.eventNotify = make(chan struct{})
	runtime.eventMu.Unlock()
	runtime.recordTimeline(event)
	select {
	case runtime.events <- event:
	default:
		select {
		case <-runtime.events:
		default:
		}
		select {
		case runtime.events <- event:
		default:
		}
	}
	return event
}
