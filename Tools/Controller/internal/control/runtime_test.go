package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type reconnectTestPort struct {
	closed chan struct{}
	dtr    []bool
}

type retryableCloseTestPort struct {
	readStarted chan struct{}
	readAborted chan struct{}
	startOnce   sync.Once
	abortOnce   sync.Once

	mu            sync.Mutex
	closeCalls    int
	closeFailures int
	closeErr      error
}

type immediateReadFailurePort struct {
	*retryableCloseTestPort
	readErr error
}

func (port *immediateReadFailurePort) Read([]byte) (int, error) {
	return 0, port.readErr
}

func newRetryableCloseTestPort(closeErr error) *retryableCloseTestPort {
	return &retryableCloseTestPort{
		readStarted:   make(chan struct{}),
		readAborted:   make(chan struct{}),
		closeFailures: 1,
		closeErr:      closeErr,
	}
}

func (*retryableCloseTestPort) SetMode(*serial.Mode) error { return nil }
func (port *retryableCloseTestPort) Read([]byte) (int, error) {
	port.startOnce.Do(func() { close(port.readStarted) })
	<-port.readAborted
	return 0, errors.New("overlapped read aborted")
}
func (*retryableCloseTestPort) Write(data []byte) (int, error) { return len(data), nil }
func (*retryableCloseTestPort) Drain() error                   { return nil }
func (*retryableCloseTestPort) ResetInputBuffer() error        { return nil }
func (*retryableCloseTestPort) ResetOutputBuffer() error       { return nil }
func (*retryableCloseTestPort) SetDTR(bool) error              { return nil }
func (*retryableCloseTestPort) SetRTS(bool) error              { return nil }
func (*retryableCloseTestPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*retryableCloseTestPort) SetReadTimeout(time.Duration) error { return nil }
func (port *retryableCloseTestPort) Close() error {
	port.mu.Lock()
	port.closeCalls++
	if port.closeFailures > 0 {
		port.closeFailures--
		err := port.closeErr
		port.mu.Unlock()
		return err
	}
	port.mu.Unlock()
	port.abortOnce.Do(func() { close(port.readAborted) })
	return nil
}
func (*retryableCloseTestPort) Break(time.Duration) error { return nil }

func newReconnectTestPort() *reconnectTestPort {
	return &reconnectTestPort{closed: make(chan struct{})}
}

func (*reconnectTestPort) SetMode(*serial.Mode) error { return nil }
func (port *reconnectTestPort) Read([]byte) (int, error) {
	<-port.closed
	return 0, errors.New("USB device removed")
}
func (*reconnectTestPort) Write(data []byte) (int, error) { return len(data), nil }
func (*reconnectTestPort) Drain() error                   { return nil }
func (*reconnectTestPort) ResetInputBuffer() error        { return nil }
func (*reconnectTestPort) ResetOutputBuffer() error       { return nil }
func (port *reconnectTestPort) SetDTR(value bool) error {
	port.dtr = append(port.dtr, value)
	return nil
}
func (*reconnectTestPort) SetRTS(bool) error { return nil }
func (*reconnectTestPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*reconnectTestPort) SetReadTimeout(time.Duration) error { return nil }
func (port *reconnectTestPort) Close() error {
	select {
	case <-port.closed:
	default:
		close(port.closed)
	}
	return nil
}
func (*reconnectTestPort) Break(time.Duration) error { return nil }

func TestPulseResetUsesRememberedPortBeforeAuthentication(t *testing.T) {
	port := newReconnectTestPort()
	previous := openResetSession
	openResetSession = func(context.Context, string, int) (*link.Session, error) {
		return link.NewForPort("COM4", port), nil
	}
	defer func() { openResetSession = previous }()

	runtime := New(Options{BaudRate: link.DefaultBaudRate})
	runtime.mu.Lock()
	runtime.paused = true
	runtime.mu.Unlock()
	if err := runtime.PulseResetPortFor(context.Background(), "COM4", time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(port.dtr) != 2 || !port.dtr[0] || port.dtr[1] {
		t.Fatalf("unexpected DTR sequence: %v", port.dtr)
	}
}

func TestPulseResetQuarantinesTemporaryCloseFailure(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	port := newRetryableCloseTestPort(cancelErr)
	session := link.NewForPort("COM4", port)
	previous := openResetSession
	openResetSession = func(context.Context, string, int) (*link.Session, error) {
		return session, nil
	}
	defer func() { openResetSession = previous }()

	runtime := New(Options{BaudRate: link.DefaultBaudRate})
	runtime.mu.Lock()
	runtime.paused = true
	runtime.mu.Unlock()
	if err := runtime.PulseResetPortFor(context.Background(), "COM4", time.Millisecond); !errors.Is(err, cancelErr) {
		t.Fatalf("pulse reset cleanup error = %v, want %v", err, cancelErr)
	}
	runtime.mu.RLock()
	retained := runtime.session
	state := runtime.connectionState
	paused := runtime.paused
	runtime.mu.RUnlock()
	if retained != session || state != "close_failed" || !paused {
		t.Fatalf("reset cleanup owner: session=%p state=%q paused=%v, want %p/close_failed/true", retained, state, paused, session)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("retry reset cleanup: %v", err)
	}
}

func TestPulseResetRejectsCloseQuarantine(t *testing.T) {
	tests := []struct {
		name    string
		install func(*Runtime, *link.Session)
	}{
		{
			name: "active failed-close owner",
			install: func(runtime *Runtime, session *link.Session) {
				runtime.session = session
				runtime.port = ports.Info{Name: "COM4", IsUSB: true}
			},
		},
		{
			name: "retained rejected owner",
			install: func(runtime *Runtime, session *link.Session) {
				runtime.retainedClose = []link.OpenResult{{
					Session: session,
					Port:    ports.Info{Name: "COM4", IsUSB: true},
				}}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			port := newReconnectTestPort()
			session := link.NewForPort("COM4", port)
			openCalled := false
			previous := openResetSession
			openResetSession = func(context.Context, string, int) (*link.Session, error) {
				openCalled = true
				return nil, errors.New("unexpected reset open")
			}
			defer func() { openResetSession = previous }()

			runtime := New(Options{BaudRate: link.DefaultBaudRate})
			runtime.mu.Lock()
			test.install(runtime, session)
			runtime.paused = true
			runtime.connectionState = "close_failed"
			runtime.connectionReason = "CancelIoEx failed"
			runtime.mu.Unlock()

			if err := runtime.PulseResetPortFor(context.Background(), "COM4", time.Millisecond); err == nil {
				t.Fatal("PulseResetPortFor accepted a quarantined serial owner")
			}
			if openCalled {
				t.Fatal("PulseResetPortFor opened a second transport during close quarantine")
			}
			if len(port.dtr) != 0 {
				t.Fatalf("PulseResetPortFor toggled quarantined transport DTR: %v", port.dtr)
			}
			if err := runtime.Close(); err != nil {
				t.Fatalf("release quarantined owner: %v", err)
			}
		})
	}
}

func TestCloseCancelsAndJoinsResetAttempt(t *testing.T) {
	previous := openResetSession
	resetStarted := make(chan struct{})
	openResetSession = func(ctx context.Context, _ string, _ int) (*link.Session, error) {
		close(resetStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	defer func() { openResetSession = previous }()

	runtime := New(Options{BaudRate: link.DefaultBaudRate})
	runtime.mu.Lock()
	runtime.paused = true
	runtime.mu.Unlock()
	resetDone := make(chan error, 1)
	go func() {
		resetDone <- runtime.PulseResetPortFor(
			context.Background(), "COM4", time.Hour,
		)
	}()
	select {
	case <-resetStarted:
	case <-time.After(time.Second):
		t.Fatal("reset attempt did not enter temporary transport open")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close while reset was pending: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join reset attempt")
	}
	select {
	case err := <-resetDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reset error = %v, want cancellation", err)
		}
	default:
		t.Fatal("Close returned before reset attempt exited")
	}
}

func TestDoorEventUpdatesSnapshotAndWakesWaiters(t *testing.T) {
	runtime := New(Options{})
	after := runtime.LatestEventID()
	result := make(chan Event, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		event, _ := runtime.WaitEvent(ctx, after, "door")
		result <- event
	}()

	frame := native.Frame{
		Opcode:  native.OpEvent,
		Seq:     0,
		Payload: []byte{native.EventDoor, 1},
	}
	runtime.observe(frame)
	kind, text := describeDeviceEvent(native.DeviceEvent{
		Type: native.EventDoor, DoorOpen: true,
	})
	runtime.publish(kind, text, frame)
	if !runtime.Snapshot().Status.DoorOpen {
		t.Fatal("door event did not update live snapshot")
	}
	select {
	case event := <-result:
		if event.Kind != "door" || event.ID <= after {
			t.Fatalf("unexpected event: %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("event waiter was not notified")
	}
}

func TestResetEventUpdatesSnapshotAndWakesWaiters(t *testing.T) {
	runtime := New(Options{})
	after := runtime.LatestEventID()
	frame := native.Frame{
		Opcode: native.OpEvent,
		Payload: []byte{
			native.EventReset, 0x0A, 0x78, 0x56, 0x34, 0x12,
		},
	}
	runtime.observe(frame)
	parsed, err := native.ParseDeviceEvent(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	kind, text := describeDeviceEvent(parsed)
	runtime.publish(kind, text, frame)
	snapshot := runtime.Snapshot()
	if snapshot.Status.ResetCause != 0x0A ||
		snapshot.Status.ResetCount != 0x12345678 {
		t.Fatalf("reset event did not update snapshot: %#v", snapshot.Status)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, "reset")
	if err != nil {
		t.Fatal(err)
	}
	if event.ResetCause != 0x0A || event.ResetCount != 0x12345678 {
		t.Fatalf("reset metadata not exposed: %#v", event)
	}
}

func TestAlertEventUpdatesHotSnapshotAndDescription(t *testing.T) {
	runtime := New(Options{})
	frame := native.Frame{
		Opcode:  native.OpEvent,
		Payload: []byte{native.EventAlert, native.AlertHot, 1},
	}
	runtime.observe(frame)
	if !runtime.Snapshot().Status.Hot {
		t.Fatal("HOT alert did not update the live snapshot")
	}
	parsed, err := native.ParseDeviceEvent(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	kind, text := describeDeviceEvent(parsed)
	if kind != "hot" || text != "temperature alert active" {
		t.Fatalf("alert description=%q %q", kind, text)
	}
}

func TestAppNavigationEventDescription(t *testing.T) {
	kind, text := describeDeviceEvent(native.DeviceEvent{
		Type: native.EventAppNavigation, AppTarget: "tui", AppPage: "events",
	})
	if kind != "app.page" || text != "board requested page events for tui" {
		t.Fatalf("navigation description=%q %q", kind, text)
	}
}

func TestUnplugReplugLifecycleAndOneResetPermit(t *testing.T) {
	runtime := New(Options{
		Filter:           ports.Filter{Port: "TEST-NOT-A-REAL-PORT"},
		ResetOnReconnect: true,
	})
	firstPort := newReconnectTestPort()
	firstSession := link.NewForPort("TEST-NOT-A-REAL-PORT", firstPort)
	info := ports.Info{
		Name: "TEST-NOT-A-REAL-PORT", IsUSB: true, VID: "1A86", PID: "7523",
		SerialNumber: "controller-1",
	}
	runtime.attach(link.OpenResult{
		Session: firstSession, Port: info,
		Hello: native.Hello{Name: "PCController"},
	})
	after := runtime.LatestEventID()
	_ = firstPort.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	disconnected, err := runtime.WaitEvent(ctx, after, "usb.disconnected")
	if err != nil {
		t.Fatal(err)
	}
	if disconnected.Lifecycle != "disconnect" ||
		disconnected.Port.SerialNumber != "controller-1" ||
		disconnected.State != "disconnected" {
		t.Fatalf("unexpected disconnect: %#v", disconnected)
	}
	reconnecting, err := runtime.WaitEvent(ctx, disconnected.ID, "connection")
	if err != nil {
		t.Fatal(err)
	}
	if reconnecting.Lifecycle != "reconnecting" ||
		reconnecting.State != "reconnecting" {
		t.Fatalf("unexpected reconnecting event: %#v", reconnecting)
	}
	if snapshot := runtime.Snapshot(); snapshot.Connected ||
		snapshot.ConnectionState != "reconnecting" ||
		snapshot.Port.Name != info.Name {
		t.Fatalf("unplug snapshot lost immediate state/identity: %#v", snapshot)
	}
	if !runtime.resetAfterOpen(info) {
		t.Fatal("first physical re-open did not receive reset permit")
	}
	if runtime.resetAfterOpen(info) {
		t.Fatal("second authentication/open attempt received a reset permit")
	}

	secondPort := newReconnectTestPort()
	secondSession := link.NewForPort(info.Name, secondPort)
	runtime.attach(link.OpenResult{
		Session: secondSession, Port: info,
		Hello: native.Hello{Name: "PCController"},
	})
	cursor := reconnecting.ID
	var reconnected Event
	for reconnected.Lifecycle != "reconnected" {
		reconnected, err = runtime.WaitEvent(ctx, cursor, "connection")
		if err != nil {
			t.Fatal(err)
		}
		cursor = reconnected.ID
	}
	if reconnected.Lifecycle != "reconnected" ||
		reconnected.State != "connected" {
		t.Fatalf("unexpected reconnected event: %#v", reconnected)
	}
	if !runtime.Snapshot().Connected {
		t.Fatal("replug did not update snapshot immediately")
	}
	_ = runtime.Close()
}

func TestHardwareProblemPersistsUntilAuthenticatedAttach(t *testing.T) {
	runtime := New(Options{Filter: ports.Filter{Port: "COM3"}})
	runtime.port = ports.Info{
		Name: "COM3", IsUSB: true, VID: "1A86", PID: "7523",
		InstanceID: `USB\VID_1A86&PID_7523\5&1330824A&0&2`,
	}
	problem := ports.HardwareProblem{
		Code:          ports.HardwareProblemUSBDescriptorFailure,
		Severity:      "error",
		OSProblemCode: 43,
		DeviceID:      `USB\VID_0000&PID_0002\5&1330824A&0&2`,
		ObservedAt:    time.Now(),
	}
	runtime.hardwareProblemScan = func(filter ports.Filter) ([]ports.HardwareProblem, error) {
		if filter.Port != "COM3" || filter.Preferred.InstanceID != runtime.port.InstanceID {
			t.Fatalf("hardware scan lost controller identity: %#v", filter)
		}
		return []ports.HardwareProblem{problem}, nil
	}
	if _, err := runtime.SetProgramState("test-player", ProgramRunning, "active playback"); err != nil {
		t.Fatal(err)
	}
	runtime.refreshHardwareProblems(true)
	snapshot := runtime.Snapshot()
	if len(snapshot.HardwareProblems) != 1 {
		t.Fatalf("hardware problem missing from snapshot: %#v", snapshot.HardwareProblems)
	}
	if snapshot.HardwareProblems[0].Impact != ports.HardwareImpactActiveOutcomeUnknown {
		t.Fatalf("active-use impact=%q", snapshot.HardwareProblems[0].Impact)
	}
	event, err := runtime.WaitEvent(context.Background(), 0, "hardware.problem")
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata["problem"] != ports.HardwareProblemUSBDescriptorFailure ||
		event.Metadata["impact"] != ports.HardwareImpactActiveOutcomeUnknown {
		t.Fatalf("hardware event lost semantic evidence: %#v", event)
	}

	// A device disappearing from the problem list is not proof of recovery.
	// Only an authenticated application HELLO may clear the warning.
	runtime.hardwareProblemScan = func(ports.Filter) ([]ports.HardwareProblem, error) { return nil, nil }
	runtime.refreshHardwareProblems(false)
	if len(runtime.Snapshot().HardwareProblems) != 1 {
		t.Fatal("disconnected scan cleared the hardware problem before authentication")
	}

	port := newReconnectTestPort()
	runtime.attach(link.OpenResult{
		Session: link.NewForPort("COM3", port),
		Port:    runtime.port,
		Hello:   native.Hello{Name: "PCController"},
	})
	if len(runtime.Snapshot().HardwareProblems) != 0 {
		t.Fatal("authenticated attach did not clear the hardware problem")
	}
	recovered, err := runtime.WaitEvent(context.Background(), event.ID, "hardware.recovered")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Lifecycle != "recovered" || recovered.State != "healthy" {
		t.Fatalf("unexpected recovery event: %#v", recovered)
	}
	_ = runtime.Close()
}

func TestAuthenticatedAttachRejectsStaleHardwareScan(t *testing.T) {
	runtime := New(Options{Filter: ports.Filter{Port: "COM3"}})
	runtime.port = ports.Info{
		Name: "COM3", IsUSB: true,
		InstanceID: `USB\VID_1A86&PID_7523\CONTROLLER`,
	}
	runtime.setHardwareProblems([]ports.HardwareProblem{{
		Code: ports.HardwareProblemUSBDescriptorFailure, Severity: "error",
		DeviceID: `USB\VID_0000&PID_0002\CONTROLLER`, ObservedAt: time.Now(),
	}})
	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	runtime.hardwareProblemScan = func(ports.Filter) ([]ports.HardwareProblem, error) {
		close(scanStarted)
		<-releaseScan
		return []ports.HardwareProblem{{
			Code: ports.HardwareProblemUSBDescriptorFailure, Severity: "error",
			DeviceID: `USB\VID_0000&PID_0002\CONTROLLER`, ObservedAt: time.Now(),
		}}, nil
	}
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		runtime.refreshHardwareProblems(false)
	}()
	<-scanStarted

	port := newReconnectTestPort()
	runtime.attach(link.OpenResult{
		Session: link.NewForPort("COM3", port), Port: runtime.port,
		Hello: native.Hello{Name: "PCController"},
	})
	close(releaseScan)
	<-scanDone
	if problems := runtime.Snapshot().HardwareProblems; len(problems) != 0 {
		t.Fatalf("stale scan resurrected a fault after authenticated HELLO: %#v", problems)
	}
	_ = runtime.Close()
}

func TestActiveUseAtTransportLossIncludesOutputStreams(t *testing.T) {
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.outputState = func() OutputStreamState { return OutputStreamState{EffectID: 42} }
	runtime.mu.Unlock()
	if !runtime.activeUseAtTransportLoss() {
		t.Fatal("active host-streamed status effect was omitted from transport-loss impact")
	}
}

func TestReconnectDiscoveryRebindsAuthenticatedUSBIdentity(t *testing.T) {
	runtime := New(Options{Filter: ports.Filter{Port: "COM4"}})
	runtime.mu.Lock()
	runtime.connectionState = "reconnecting"
	runtime.portRebindAllowed = true
	runtime.port = ports.Info{
		Name: "COM4", IsUSB: true, VID: "1A86", PID: "7523",
		FriendlyName: "USB-SERIAL CH340",
		InstanceID:   `USB\VID_1A86&PID_7523\OLD-PATH`,
	}
	options := runtime.options
	runtime.mu.Unlock()

	discovery := runtime.discoveryOptions(options)
	if !discovery.AllowPortRebind {
		t.Fatal("physical USB disappearance did not arm identity rebind")
	}
	candidates := ports.ReconnectCandidates([]ports.Info{
		{
			Name: "COM3", IsUSB: true, VID: "1A86", PID: "7523",
			FriendlyName: "USB-SERIAL CH340",
			InstanceID:   `USB\VID_1A86&PID_7523\NEW-PATH`,
		},
		{Name: "COM8", IsUSB: true, VID: "2341", PID: "0043"},
	}, discovery.Filter)
	if len(candidates) != 1 || candidates[0].Name != "COM3" {
		t.Fatalf("COM4 -> COM3 runtime rebind = %#v", candidates)
	}
}

func TestExplicitConnectionPathRemainsStrict(t *testing.T) {
	runtime := New(Options{Filter: ports.Filter{Port: "COM4"}})
	runtime.mu.Lock()
	runtime.connectionState = "reconnecting"
	runtime.portRebindAllowed = false
	runtime.port = ports.Info{
		Name: "COM4", IsUSB: true, VID: "1A86", PID: "7523",
	}
	options := runtime.options
	runtime.mu.Unlock()

	discovery := runtime.discoveryOptions(options)
	if discovery.AllowPortRebind {
		t.Fatal("explicit/configuration reconnect relaxed the selected COM port")
	}
	all := []ports.Info{{Name: "COM3", IsUSB: true, VID: "1A86", PID: "7523"}}
	if candidates := ports.Candidates(all, discovery.Filter); len(candidates) != 0 {
		t.Fatalf("strict COM4 selector matched COM3: %#v", candidates)
	}
}

func TestReconnectBackoffIsExponentialAndBounded(t *testing.T) {
	options := New(Options{}).options
	if options.ReconnectInitialDelay != 500*time.Millisecond ||
		options.ReconnectMaximumDelay != 15*time.Second {
		t.Fatalf("default reconnect policy = %+v", options)
	}
	var got []time.Duration
	delay := time.Duration(0)
	for index := 0; index < 8; index++ {
		delay = nextReconnectDelay(delay, 500*time.Millisecond, 15*time.Second)
		got = append(got, delay)
	}
	want := []time.Duration{
		500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 15 * time.Second, 15 * time.Second, 15 * time.Second,
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("retry %d delay=%s want=%s; sequence=%v", index, got[index], want[index], got)
		}
	}
}

func TestConnectionTransitionsAreChangedOnlyAndRejectStaleFailures(t *testing.T) {
	runtime := New(Options{})
	port := ports.Info{Name: "COM4", IsUSB: true, VID: "1A86", PID: "7523"}
	after := runtime.LatestEventID()
	if !runtime.publishConnection("reconnecting", port, "USB removed") {
		t.Fatal("first reconnecting transition was suppressed")
	}
	if runtime.publishConnection("reconnecting", port, "USB removed") {
		t.Fatal("duplicate reconnecting transition was published")
	}
	if got := runtime.LatestEventID(); got != after+1 {
		t.Fatalf("duplicate transition advanced event ID to %d, want %d", got, after+1)
	}
	if !runtime.publishUSBConnection(
		"usb.disconnected", "disconnect", port, "USB removed", "disconnected",
	) {
		t.Fatal("first USB disconnect transition was suppressed")
	}
	if runtime.publishUSBConnection(
		"usb.disconnected", "disconnect", port, "USB removed", "disconnected",
	) {
		t.Fatal("duplicate USB disconnect transition was published")
	}
	if !runtime.publishUSBConnection(
		"usb.reconnected", "reconnected", port, "", "connected",
	) {
		t.Fatal("USB reconnect transition was suppressed")
	}
	if !runtime.publishUSBConnection(
		"usb.disconnected", "disconnect", port, "USB removed", "disconnected",
	) {
		t.Fatal("new USB disconnect cycle was suppressed")
	}
	afterTransitions := runtime.LatestEventID()

	runtime.mu.Lock()
	runtime.reconnectEpoch = 9
	runtime.connectionState = "connected"
	runtime.connectionReason = ""
	runtime.session = &link.Session{}
	runtime.mu.Unlock()
	if runtime.publishReconnectFailure(8, "old COM4 scan failed") {
		t.Fatal("stale reconnect epoch published after connection")
	}
	if got := runtime.LatestEventID(); got != afterTransitions {
		t.Fatalf("stale reconnect event advanced event ID to %d", got)
	}
}

func TestHotResetPolicyChangeDoesNotDropLiveConnection(t *testing.T) {
	runtime := New(Options{})
	port := newReconnectTestPort()
	session := link.NewForPort("TEST", port)
	runtime.attach(link.OpenResult{
		Session: session,
		Port:    ports.Info{Name: "TEST"},
		Hello:   native.Hello{Name: "PCController"},
	})
	if !runtime.ApplyOptions(Options{ResetOnReconnect: true}) {
		t.Fatal("reset policy update was not applied")
	}
	if !runtime.Snapshot().Connected {
		t.Fatal("reset policy-only hot reload dropped the live transport")
	}
	_ = runtime.Close()
}

func TestDisconnectedRuntimeDropsPeerOwnedSnapshotValues(t *testing.T) {
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.port = ports.Info{Name: "COM18", SerialNumber: "controller-1"}
	runtime.hello = native.Hello{Name: "PCController", Capabilities: native.CapabilityINA219}
	runtime.status = native.Status{SupplyMV: 12220}
	runtime.settings = native.DefaultSettings()
	runtime.haveStatus = true
	runtime.haveSettings = true
	runtime.statusUpdated = time.Now()
	runtime.frontPanel = native.FrontPanel{MenuPage: 3}
	runtime.haveFrontPanel = true
	runtime.haveFrontPanelSegments = true
	runtime.statusLED = native.StatusLEDState{Brightness: 255}
	runtime.haveStatusLED = true
	runtime.clearPeerStateLocked()
	runtime.mu.Unlock()

	snapshot := runtime.Snapshot()
	if snapshot.Hello != (native.Hello{}) || snapshot.Status != (native.Status{}) ||
		snapshot.Settings != (native.Settings{}) || snapshot.HaveStatus || snapshot.HaveSettings ||
		snapshot.HaveFrontPanel || snapshot.HaveFrontPanelSegments || snapshot.HaveStatusLED ||
		!snapshot.StatusUpdated.IsZero() {
		t.Fatalf("disconnected runtime retained peer-owned state: %#v", snapshot)
	}
	if snapshot.Port.Name != "COM18" || snapshot.Port.SerialNumber != "controller-1" {
		t.Fatalf("reconnect identity was discarded: %#v", snapshot.Port)
	}
}

func TestSegmentChangedDoesNotPromotePartialStateToExactFrontPanel(t *testing.T) {
	runtime := New(Options{})
	runtime.observe(native.Frame{
		Opcode:  native.OpSegmentChanged,
		Payload: []byte{0x11, 0x22, 0x33, 0x44, 5},
	})
	snapshot := runtime.Snapshot()
	if snapshot.HaveFrontPanel {
		t.Fatal("five-byte segment update was promoted to an exact full-panel snapshot")
	}
	if !snapshot.HaveFrontPanelSegments {
		t.Fatal("five-byte segment update was not retained as partial segment authority")
	}
	if snapshot.FrontPanel.RawSegments != ([4]byte{0x11, 0x22, 0x33, 0x44}) ||
		snapshot.FrontPanel.Brightness != 5 || !snapshot.FrontPanel.SegmentsActive {
		t.Fatalf("partial segment fields were not retained: %#v", snapshot.FrontPanel)
	}

	runtime.mu.Lock()
	runtime.frontPanel = native.FrontPanel{Schema: 2, MenuPage: 3, ProgramMode: 7}
	runtime.haveFrontPanel = true
	runtime.mu.Unlock()
	runtime.observe(native.Frame{
		Opcode:  native.OpSegmentChanged,
		Payload: []byte{0x01, 0x02, 0x03, 0x04, 6},
	})
	snapshot = runtime.Snapshot()
	if !snapshot.HaveFrontPanel || snapshot.FrontPanel.MenuPage != 3 ||
		snapshot.FrontPanel.ProgramMode != 7 {
		t.Fatalf("segment update invalidated or fabricated exact fields: %#v", snapshot)
	}
	if !snapshot.HaveFrontPanelSegments {
		t.Fatal("exact panel snapshot lost segment authority")
	}
	if snapshot.FrontPanel.RawSegments != ([4]byte{0x01, 0x02, 0x03, 0x04}) ||
		snapshot.FrontPanel.Brightness != 6 {
		t.Fatalf("exact snapshot did not absorb changed segment fields: %#v", snapshot.FrontPanel)
	}
}

func TestRememberedPreferredDeviceChangeDoesNotDropLiveConnection(t *testing.T) {
	runtime := New(Options{})
	port := newReconnectTestPort()
	session := link.NewForPort("TEST", port)
	runtime.attach(link.OpenResult{
		Session: session,
		Port:    ports.Info{Name: "TEST"},
		Hello:   native.Hello{Name: "PCController"},
	})
	if !runtime.ApplyOptions(Options{
		Filter: ports.Filter{
			Preferred: ports.Identity{
				Port: "TEST", InstanceID: "USB\\TEST",
			},
		},
	}) {
		t.Fatal("preferred-device update was not applied")
	}
	if !runtime.Snapshot().Connected {
		t.Fatal("preferred-device persistence dropped the live transport")
	}
	_ = runtime.Close()
}

func TestOpenAlreadyConnectedSelectorIsIdempotent(t *testing.T) {
	runtime := New(Options{})
	port := newReconnectTestPort()
	session := link.NewForPort("COM18", port)
	info := ports.Info{
		Name: "COM18", VID: "1A86", PID: "7523",
		FriendlyName: "USB-SERIAL CH340",
	}
	runtime.attach(link.OpenResult{
		Session: session, Port: info,
		Hello: native.Hello{Name: "PCController"},
	})
	if err := runtime.Open(context.Background(), "COM18"); err != nil {
		t.Fatalf("idempotent open attempted a second serial handle: %v", err)
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Connected || snapshot.Port.Name != "COM18" {
		t.Fatalf("idempotent open changed the live device: %#v", snapshot)
	}
	_ = runtime.Close()
}

func TestOpenAuthenticationFailurePreservesActiveSession(t *testing.T) {
	runtime := New(Options{})
	port := newReconnectTestPort()
	session := link.NewForPort("COM3", port)
	runtime.attach(link.OpenResult{
		Session: session,
		Port:    ports.Info{Name: "COM3", IsUSB: true},
		Hello:   native.Hello{Name: "PCController"},
	})
	authErr := errors.New("replacement HELLO timed out")
	runtime.openAuthenticated = func(context.Context, ports.Info, link.DiscoveryOptions) (link.OpenResult, error) {
		return link.OpenResult{}, authErr
	}

	if err := runtime.Open(context.Background(), "tcp://127.0.0.1:8787"); !errors.Is(err, authErr) {
		t.Fatalf("replacement error = %v, want %v", err, authErr)
	}
	if current := runtime.currentSession(); current != session {
		t.Fatalf("failed replacement changed active session to %p, want %p", current, session)
	}
	if snapshot := runtime.Snapshot(); !snapshot.Connected || snapshot.Port.Name != "COM3" {
		t.Fatalf("failed replacement changed active snapshot: %#v", snapshot)
	}
	select {
	case <-port.closed:
		t.Fatal("failed replacement closed the healthy active transport")
	default:
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("close preserved active session: %v", err)
	}
}

func TestCloseCancelsAndJoinsDirectOpen(t *testing.T) {
	runtime := New(Options{})
	openStarted := make(chan struct{})
	runtime.openAuthenticated = func(ctx context.Context, _ ports.Info, _ link.DiscoveryOptions) (link.OpenResult, error) {
		close(openStarted)
		<-ctx.Done()
		return link.OpenResult{}, ctx.Err()
	}

	openDone := make(chan error, 1)
	go func() {
		openDone <- runtime.Open(context.Background(), "tcp://127.0.0.1:8787")
	}()
	select {
	case <-openStarted:
	case <-time.After(time.Second):
		t.Fatal("direct Open did not begin authentication")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close while direct Open was pending: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel and join direct Open")
	}
	select {
	case err := <-openDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("direct Open error = %v, want cancellation", err)
		}
	default:
		t.Fatal("Close returned before direct Open authentication exited")
	}
}

func TestCloseEpochRejectsDirectOpenSuccessAndReleasesTransport(t *testing.T) {
	port := newReconnectTestPort()
	session := link.NewForPort("TCP", port)
	runtime := New(Options{})
	openStarted := make(chan struct{})
	observed := make(chan struct{}, 1)
	runtime.SetDeviceObserver(func(ports.Info, native.Hello) { observed <- struct{}{} })
	runtime.openAuthenticated = func(ctx context.Context, _ ports.Info, _ link.DiscoveryOptions) (link.OpenResult, error) {
		close(openStarted)
		<-ctx.Done()
		return link.OpenResult{
			Session: session,
			Port:    ports.Info{Name: "tcp://127.0.0.1:8787"},
			Hello:   native.Hello{Name: "PCController"},
		}, nil
	}

	openDone := make(chan error, 1)
	go func() {
		openDone <- runtime.Open(context.Background(), "tcp://127.0.0.1:8787")
	}()
	select {
	case <-openStarted:
	case <-time.After(time.Second):
		t.Fatal("direct Open did not begin authentication")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close rejected direct-open result: %v", err)
	}
	select {
	case err := <-openDone:
		if err == nil {
			t.Fatal("direct Open reported success after Close won the epoch")
		}
	default:
		t.Fatal("Close returned before direct Open rejected its result")
	}
	select {
	case <-port.closed:
	default:
		t.Fatal("Close returned before rejected direct-open transport was released")
	}
	select {
	case <-observed:
		t.Fatal("rejected direct Open emitted a ready observer event")
	default:
	}
	if snapshot := runtime.Snapshot(); snapshot.Connected || !snapshot.Paused {
		t.Fatalf("close/direct-open race snapshot = %#v", snapshot)
	}
}

func TestCloseDrainsRejectedDirectOpenCleanupOwner(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	port := newRetryableCloseTestPort(cancelErr)
	session := link.NewForPort("TCP", port)
	runtime := New(Options{})
	openStarted := make(chan struct{})
	runtime.openAuthenticated = func(ctx context.Context, _ ports.Info, _ link.DiscoveryOptions) (link.OpenResult, error) {
		close(openStarted)
		<-ctx.Done()
		return link.OpenResult{
			Session: session,
			Port:    ports.Info{Name: "tcp://127.0.0.1:8787"},
			Hello:   native.Hello{Name: "PCController"},
		}, nil
	}

	openDone := make(chan error, 1)
	go func() {
		openDone <- runtime.Open(context.Background(), "tcp://127.0.0.1:8787")
	}()
	select {
	case <-openStarted:
	case <-time.After(time.Second):
		t.Fatal("direct Open did not begin authentication")
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close did not retry rejected direct-open cleanup: %v", err)
	}
	select {
	case err := <-openDone:
		if !errors.Is(err, cancelErr) {
			t.Fatalf("direct Open cleanup error = %v, want %v", err, cancelErr)
		}
	default:
		t.Fatal("Close returned before rejected cleanup owner was quarantined")
	}
	port.mu.Lock()
	closeCalls := port.closeCalls
	port.mu.Unlock()
	if closeCalls != 2 {
		t.Fatalf("transport Close calls = %d, want rejection plus barrier retry", closeCalls)
	}
	if current := runtime.currentSession(); current != nil {
		t.Fatalf("Close retained rejected direct-open owner %p", current)
	}
}

func TestApplyOptionsPreservesExplicitPause(t *testing.T) {
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.paused = true
	runtime.mu.Unlock()
	openCalled := make(chan struct{}, 1)
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		select {
		case openCalled <- struct{}{}:
		default:
		}
		return link.OpenResult{}, errors.New("unexpected reconnect")
	}

	if !runtime.ApplyOptions(Options{BaudRate: 57600}) {
		t.Fatal("transport option update was not applied")
	}
	if snapshot := runtime.Snapshot(); !snapshot.Paused || snapshot.ConnectionState != "disconnected" {
		t.Fatalf("option update resumed an explicitly paused runtime: %#v", snapshot)
	}
	select {
	case <-openCalled:
		t.Fatal("option update launched a reconnect attempt while paused")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestConnectRacingCloseCannotReopenAfterCloseBarrier(t *testing.T) {
	port := newReconnectTestPort()
	session := link.NewForPort("COM3", port)
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = session
	runtime.port = ports.Info{Name: "COM3", IsUSB: true}
	runtime.connectionState = "connected"
	runtime.mu.Unlock()
	disconnectStarted := make(chan struct{})
	releaseDisconnect := make(chan struct{})
	runtime.SetBeforeDisconnect(func(string) {
		close(disconnectStarted)
		<-releaseDisconnect
	})
	openCalled := false
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalled = true
		return link.OpenResult{}, errors.New("unexpected reopen")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case <-disconnectStarted:
	case <-time.After(time.Second):
		t.Fatal("close did not reach its transport barrier")
	}
	connectDone := make(chan error, 1)
	go func() { connectDone <- runtime.Connect(context.Background()) }()
	select {
	case err := <-connectDone:
		if err == nil {
			t.Fatal("connection racing Close was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("connection racing Close did not fail promptly")
	}
	close(releaseDisconnect)
	if err := <-closeDone; err != nil {
		t.Fatalf("close: %v", err)
	}
	if openCalled {
		t.Fatal("connection racing Close opened a replacement transport")
	}
	if snapshot := runtime.Snapshot(); !snapshot.Paused || snapshot.Connected {
		t.Fatalf("close barrier snapshot = %#v", snapshot)
	}
}

func TestCloseRejectsConnectionBeforeCancellationSlotAdmission(t *testing.T) {
	tests := []struct {
		name string
		run  func(*Runtime) error
	}{
		{name: "Connect", run: func(runtime *Runtime) error {
			return runtime.Connect(context.Background())
		}},
		{name: "Reconnect", run: func(runtime *Runtime) error {
			return runtime.Reconnect(context.Background(), "test reconnect")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := New(Options{})
			admissionReached := make(chan struct{})
			releaseAdmission := make(chan struct{})
			runtime.beforeConnectAdmission = func() {
				close(admissionReached)
				<-releaseAdmission
			}
			openCalled := false
			runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
				openCalled = true
				return link.OpenResult{}, errors.New("unexpected open")
			}

			connectDone := make(chan error, 1)
			go func() { connectDone <- test.run(runtime) }()
			select {
			case <-admissionReached:
			case <-time.After(time.Second):
				t.Fatal("connection did not reach cancellation-slot admission")
			}
			closeDone := make(chan error, 1)
			go func() { closeDone <- runtime.Close() }()
			deadline := time.Now().Add(time.Second)
			for {
				_, closing := runtime.closeBarrierState()
				if closing {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Close did not establish its barrier")
				}
				time.Sleep(time.Millisecond)
			}
			close(releaseAdmission)

			select {
			case err := <-connectDone:
				if err == nil {
					t.Fatal("connection admitted after Close established its barrier")
				}
			case <-time.After(time.Second):
				t.Fatal("connection did not reject close-barrier admission")
			}
			select {
			case err := <-closeDone:
				if err != nil {
					t.Fatalf("Close: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Close remained blocked after rejecting late connection admission")
			}
			if openCalled {
				t.Fatal("late connection admission entered autoOpen")
			}
		})
	}
}

func TestCloseKeepsBarrierSetUntilOpenMutexIsReleased(t *testing.T) {
	runtime := New(Options{})
	barrierUnlocked := make(chan struct{})
	releaseClose := make(chan struct{})
	runtime.afterCloseOpenUnlock = func() {
		close(barrierUnlocked)
		<-releaseClose
	}
	openCalled := false
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalled = true
		return link.OpenResult{}, errors.New("unexpected reopen")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- runtime.Close() }()
	select {
	case <-barrierUnlocked:
	case <-time.After(time.Second):
		t.Fatal("Close did not release openMu")
	}
	connectDone := make(chan error, 1)
	go func() { connectDone <- runtime.Connect(context.Background()) }()
	select {
	case err := <-connectDone:
		if err == nil {
			t.Fatal("Connect overlapping the close return boundary was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("Connect overlapping the close return boundary did not fail promptly")
	}
	if openCalled {
		t.Fatal("overlapping Connect opened a transport before Close returned")
	}
	close(releaseClose)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestApplyOptionsPreservesRetainedCloseQuarantine(t *testing.T) {
	port := newReconnectTestPort()
	session := link.NewForPort("COM4", port)
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.retainedClose = []link.OpenResult{{
		Session: session,
		Port:    ports.Info{Name: "COM4", IsUSB: true},
	}}
	runtime.paused = true
	runtime.connectionState = "close_failed"
	runtime.connectionReason = "cleanup failed"
	runtime.mu.Unlock()
	openCalled := make(chan struct{}, 1)
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalled <- struct{}{}
		return link.OpenResult{}, errors.New("unexpected reconnect")
	}

	runtime.ResumeAuto()
	if !runtime.ApplyOptions(Options{BaudRate: 57600}) {
		t.Fatal("transport option update was not applied")
	}
	if snapshot := runtime.Snapshot(); !snapshot.Paused || snapshot.ConnectionState != "close_failed" {
		t.Fatalf("option update cleared retained-close quarantine: %#v", snapshot)
	}
	select {
	case <-openCalled:
		t.Fatal("option update reconnected with a retained close owner")
	case <-time.After(50 * time.Millisecond):
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("release retained owner: %v", err)
	}
}

func TestReconnectRejectsRetainedCloseOwner(t *testing.T) {
	port := newReconnectTestPort()
	session := link.NewForPort("COM4", port)
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.retainedClose = []link.OpenResult{{
		Session: session,
		Port:    ports.Info{Name: "COM4", IsUSB: true},
	}}
	runtime.paused = true
	runtime.connectionState = "close_failed"
	runtime.connectionReason = "cleanup failed"
	runtime.mu.Unlock()
	openCalled := false
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalled = true
		return link.OpenResult{}, errors.New("unexpected reconnect")
	}

	if err := runtime.Reconnect(context.Background(), "test reconnect"); err == nil {
		t.Fatal("Reconnect accepted a retained close owner")
	}
	if openCalled {
		t.Fatal("Reconnect opened while a retained close owner remained")
	}
	if snapshot := runtime.Snapshot(); !snapshot.Paused || snapshot.ConnectionState != "close_failed" {
		t.Fatalf("Reconnect cleared retained-close quarantine: %#v", snapshot)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("release retained owner: %v", err)
	}
}

func TestCloseCancelsInflightReconnectAndReleasesTransport(t *testing.T) {
	port := newReconnectTestPort()
	opened := make(chan struct{})
	runtime := New(Options{})
	observed := make(chan struct{}, 1)
	runtime.SetDeviceObserver(func(ports.Info, native.Hello) { observed <- struct{}{} })
	runtime.autoOpen = func(ctx context.Context, _ link.DiscoveryOptions) (link.OpenResult, error) {
		session := link.NewForPort("COM3", port)
		close(opened)
		<-ctx.Done()
		return link.OpenResult{
			Session: session,
			Port:    ports.Info{Name: "COM3", IsUSB: true},
			Hello:   native.Hello{Name: "PCController"},
		}, nil
	}

	connectDone := make(chan error, 1)
	go func() { connectDone <- runtime.EnsureConnected(context.Background()) }()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("reconnect did not acquire the transport")
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	select {
	case <-port.closed:
	default:
		t.Fatal("close returned before the reconnect transport was released")
	}
	select {
	case err := <-connectDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("reconnect error = %v, want cancellation", err)
		}
	default:
		t.Fatal("close returned before the reconnect attempt ended")
	}
	select {
	case <-observed:
		t.Fatal("cancelled reconnect attached and invoked the device observer")
	default:
	}
	if snapshot := runtime.Snapshot(); snapshot.Connected || !snapshot.Paused ||
		snapshot.ConnectionState != "disconnected" || snapshot.ConnectionReason != "closed by host" {
		t.Fatalf("close snapshot = %#v", snapshot)
	}
}

func TestRuntimeCloseRetainsSessionUntilRetrySucceeds(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	port := newRetryableCloseTestPort(cancelErr)
	session := link.NewForPort("COM3", port)
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = session
	runtime.port = ports.Info{Name: "COM3", IsUSB: true, VID: "1A86", PID: "7523"}
	runtime.hello = native.Hello{Name: "PCController"}
	runtime.connectionState = "connected"
	runtime.generation = 1
	runtime.mu.Unlock()
	pumpExited := make(chan struct{})
	go func() {
		runtime.pump(session, 1)
		close(pumpExited)
	}()
	select {
	case <-port.readStarted:
	case <-time.After(time.Second):
		t.Fatal("runtime session did not start its pending read")
	}

	firstClose := make(chan error, 1)
	go func() { firstClose <- runtime.Close() }()
	select {
	case err := <-firstClose:
		if !errors.Is(err, cancelErr) {
			t.Fatalf("first runtime close error = %v, want %v", err, cancelErr)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime close waited for the pending read after cancellation failed")
	}

	runtime.mu.RLock()
	retained := runtime.session
	state := runtime.connectionState
	runtime.mu.RUnlock()
	if retained != session || state != "close_failed" {
		t.Fatalf("failed close ownership: session=%p state=%q, want %p close_failed", retained, state, session)
	}
	if snapshot := runtime.Snapshot(); !snapshot.Paused {
		t.Fatalf("failed close did not keep automatic open paused: %#v", snapshot)
	}

	openCalled := false
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalled = true
		return link.OpenResult{}, errors.New("unexpected open")
	}
	if err := runtime.EnsureConnected(context.Background()); err == nil {
		t.Fatal("EnsureConnected accepted a new open while failed close retained the session")
	}
	if err := runtime.Open(context.Background(), "COM3"); err == nil {
		t.Fatal("Open accepted a new transport while failed close retained the session")
	}
	runtime.mu.RLock()
	epoch := runtime.reconnectEpoch
	runtime.mu.RUnlock()
	runtime.autoReconnect(epoch)
	if openCalled {
		t.Fatal("failed close allowed EnsureConnected/Open/autoReconnect to open another transport")
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("retry runtime close: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("repeated runtime close after success: %v", err)
	}
	port.mu.Lock()
	closeCalls := port.closeCalls
	port.mu.Unlock()
	if closeCalls != 2 {
		t.Fatalf("transport close calls = %d, want failed attempt plus successful retry", closeCalls)
	}
	if current := runtime.currentSession(); current != nil {
		t.Fatalf("successful retry retained session %p", current)
	}
	if snapshot := runtime.Snapshot(); snapshot.Connected || !snapshot.Paused ||
		snapshot.ConnectionState != "disconnected" {
		t.Fatalf("successful retry snapshot = %#v", snapshot)
	}
	select {
	case <-pumpExited:
	case <-time.After(time.Second):
		t.Fatal("stale pump did not exit after successful retry closed Session.Done")
	}
}

func TestAsyncTransportCloseFailurePausesRuntimeUntilRetry(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	port := &immediateReadFailurePort{
		retryableCloseTestPort: newRetryableCloseTestPort(cancelErr),
		readErr:                errors.New("serial read failed"),
	}
	session := link.NewForPort("COM3", port)
	runtime := New(Options{})
	runtime.attach(link.OpenResult{
		Session: session,
		Port:    ports.Info{Name: "COM3", IsUSB: true},
		Hello:   native.Hello{Name: "PCController"},
	})

	deadline := time.Now().Add(time.Second)
	for {
		snapshot := runtime.Snapshot()
		if snapshot.ConnectionState == "close_failed" {
			if !snapshot.Paused || !snapshot.Connected {
				t.Fatalf("asynchronous close failure snapshot = %#v, want paused retained ownership", snapshot)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("asynchronous close failure was not published: %#v", snapshot)
		}
		time.Sleep(time.Millisecond)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("retry asynchronous close: %v", err)
	}
}

func TestEnsureConnectedQuarantinesAuthenticationCleanupOwner(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	authErr := errors.New("HELLO timed out")
	port := newRetryableCloseTestPort(cancelErr)
	session := link.NewForPort("COM3", port)
	select {
	case <-port.readStarted:
	case <-time.After(time.Second):
		t.Fatal("pending authentication read did not start")
	}
	if err := session.Close(); !errors.Is(err, cancelErr) {
		t.Fatalf("synthetic authentication cleanup error = %v, want %v", err, cancelErr)
	}

	runtime := New(Options{})
	openCalls := 0
	runtime.autoOpen = func(context.Context, link.DiscoveryOptions) (link.OpenResult, error) {
		openCalls++
		return link.OpenResult{
			Session: session,
			Port:    ports.Info{Name: "COM3", IsUSB: true},
		}, errors.Join(authErr, cancelErr)
	}
	if err := runtime.EnsureConnected(context.Background()); !errors.Is(err, authErr) || !errors.Is(err, cancelErr) {
		t.Fatalf("EnsureConnected error = %v, want authentication and cleanup errors", err)
	}
	runtime.mu.RLock()
	retained := runtime.session
	state := runtime.connectionState
	runtime.mu.RUnlock()
	if retained != session || state != "close_failed" {
		t.Fatalf("authentication quarantine: session=%p state=%q, want %p close_failed", retained, state, session)
	}
	if err := runtime.EnsureConnected(context.Background()); err == nil {
		t.Fatal("quarantined authentication cleanup allowed another connection")
	}
	if openCalls != 1 {
		t.Fatalf("auto-open calls = %d, want 1 while cleanup owner is quarantined", openCalls)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("release authentication cleanup quarantine: %v", err)
	}
	if runtime.currentSession() != nil {
		t.Fatal("authentication cleanup quarantine retained after successful retry")
	}
}

func TestEnsureConnectedQuarantinesRejectedAttachCleanupOwner(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	port := newRetryableCloseTestPort(cancelErr)
	session := link.NewForPort("COM3", port)
	opened := make(chan struct{})
	runtime := New(Options{})
	runtime.autoOpen = func(ctx context.Context, _ link.DiscoveryOptions) (link.OpenResult, error) {
		close(opened)
		<-ctx.Done()
		return link.OpenResult{
			Session: session,
			Port:    ports.Info{Name: "COM3", IsUSB: true},
			Hello:   native.Hello{Name: "PCController"},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	connected := make(chan error, 1)
	go func() { connected <- runtime.EnsureConnected(ctx) }()
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("connection attempt did not open its transport")
	}
	cancel()
	select {
	case err := <-connected:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, cancelErr) {
			t.Fatalf("rejected attach error = %v, want cancellation and cleanup errors", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled connection attempt did not return")
	}
	runtime.mu.RLock()
	retained := runtime.session
	state := runtime.connectionState
	runtime.mu.RUnlock()
	if retained != session || state != "close_failed" {
		t.Fatalf("rejected attach quarantine: session=%p state=%q, want %p close_failed", retained, state, session)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("release rejected attach quarantine: %v", err)
	}
	if runtime.currentSession() != nil {
		t.Fatal("rejected attach quarantine retained after successful retry")
	}
}

func TestRuntimeCloseDrainsEveryRetainedCleanupOwner(t *testing.T) {
	cancelErr := errors.New("CancelIoEx failed")
	activePort := newRetryableCloseTestPort(cancelErr)
	activePort.closeFailures = 0
	activeSession := link.NewForPort("COM3", activePort)
	retainedPort := newRetryableCloseTestPort(cancelErr)
	retainedSession := link.NewForPort("COM4", retainedPort)
	for name, started := range map[string]<-chan struct{}{
		"active": activePort.readStarted, "retained": retainedPort.readStarted,
	} {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("%s cleanup owner did not start its pending read", name)
		}
	}

	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = activeSession
	runtime.port = ports.Info{Name: "COM3", IsUSB: true}
	runtime.connectionState = "connected"
	runtime.mu.Unlock()
	ownershipErr := errors.New("concurrent authentication cleanup failed")
	if err := runtime.retainFailedOpen(link.OpenResult{
		Session: retainedSession,
		Port:    ports.Info{Name: "COM4", IsUSB: true},
	}, ownershipErr); !errors.Is(err, ownershipErr) {
		t.Fatalf("retain cleanup owner error = %v, want %v", err, ownershipErr)
	}
	runtime.mu.RLock()
	owned := runtime.session
	retainedCount := len(runtime.retainedClose)
	runtime.mu.RUnlock()
	if owned != activeSession || retainedCount != 1 {
		t.Fatalf("ownership after quarantine: active=%p retained=%d, want %p/1", owned, retainedCount, activeSession)
	}

	if err := runtime.Close(); !errors.Is(err, cancelErr) {
		t.Fatalf("first close error = %v, want retained cancellation error", err)
	}
	runtime.mu.RLock()
	owned = runtime.session
	retainedCount = len(runtime.retainedClose)
	state := runtime.connectionState
	runtime.mu.RUnlock()
	if owned != nil || retainedCount != 1 || state != "close_failed" {
		t.Fatalf("first close ownership: active=%p retained=%d state=%q, want nil/1/close_failed", owned, retainedCount, state)
	}
	if err := runtime.Open(context.Background(), "tcp://127.0.0.1:8787"); err == nil {
		t.Fatal("Open accepted a new transport while a cleanup owner remained")
	}

	if err := runtime.Close(); err != nil {
		t.Fatalf("retry retained cleanup: %v", err)
	}
	runtime.mu.RLock()
	retainedCount = len(runtime.retainedClose)
	state = runtime.connectionState
	runtime.mu.RUnlock()
	if retainedCount != 0 || state != "disconnected" {
		t.Fatalf("successful drain: retained=%d state=%q, want 0/disconnected", retainedCount, state)
	}
	activePort.mu.Lock()
	activeCalls := activePort.closeCalls
	activePort.mu.Unlock()
	retainedPort.mu.Lock()
	retainedCalls := retainedPort.closeCalls
	retainedPort.mu.Unlock()
	if activeCalls != 1 || retainedCalls != 2 {
		t.Fatalf("close calls: active=%d retained=%d, want 1/2", activeCalls, retainedCalls)
	}
}

func TestRFReceiveInfersDownAndTimedUp(t *testing.T) {
	runtime := New(Options{})
	after := runtime.LatestEventID()
	runtime.observeRFGesture(native.DeviceEvent{
		Type:   native.EventRFReceived,
		RFCode: 0x123456, RFBits: 24, RFProtocol: 1,
		RFPulseUS: 350, RFLearnedID: 4,
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	down, err := runtime.WaitEvent(ctx, after, "rf.gesture")
	if err != nil {
		t.Fatal(err)
	}
	if down.Gesture != "down" || down.RFCode != 0x123456 ||
		!down.HaveRFID || down.RFID != 4 {
		t.Fatalf("unexpected inferred down: %#v", down)
	}
	up, err := runtime.WaitEvent(ctx, down.ID, "rf.gesture")
	if err != nil {
		t.Fatal(err)
	}
	if up.Gesture != "up" || up.RFCode != down.RFCode {
		t.Fatalf("unexpected inferred up: %#v", up)
	}
}

func TestRFShortReleaseProducesDelayedSingleClick(t *testing.T) {
	runtime := New(Options{})
	event := native.DeviceEvent{
		Type:   native.EventRFReceived,
		RFCode: 0x111111, RFBits: 24, RFProtocol: 1,
		RFLearnedID: 0xFF,
	}
	after := runtime.LatestEventID()
	runtime.observeRFGesture(event)
	down := waitTestEvent(t, runtime, after, "rf.gesture")
	key := rfGestureKey{code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol}
	state := activeRFState(t, runtime, key)
	runtime.finishRFGesture(key, state)
	up := waitTestEvent(t, runtime, down.ID, "rf.gesture")
	if up.Gesture != "up" {
		t.Fatalf("release gesture=%q", up.Gesture)
	}
	click := waitTestEvent(t, runtime, up.ID, "rf.gesture")
	if click.Gesture != "click" {
		t.Fatalf("short release gesture=%q", click.Gesture)
	}
}

func TestRFSecondShortPressProducesDoubleClickWithoutSingle(t *testing.T) {
	runtime := New(Options{})
	event := native.DeviceEvent{
		Type:   native.EventRFReceived,
		RFCode: 0x222222, RFBits: 24, RFProtocol: 1,
		RFLearnedID: 7,
	}
	key := rfGestureKey{code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol}
	after := runtime.LatestEventID()

	runtime.observeRFGesture(event)
	firstDown := waitTestEvent(t, runtime, after, "rf.gesture")
	runtime.finishRFGesture(key, activeRFState(t, runtime, key))
	firstUp := waitTestEvent(t, runtime, firstDown.ID, "rf.gesture")

	runtime.observeRFGesture(event)
	secondDown := waitTestEvent(t, runtime, firstUp.ID, "rf.gesture")
	if secondDown.Gesture != "down" {
		t.Fatalf("second press gesture=%q", secondDown.Gesture)
	}
	runtime.finishRFGesture(key, activeRFState(t, runtime, key))
	secondUp := waitTestEvent(t, runtime, secondDown.ID, "rf.gesture")
	double := waitTestEvent(t, runtime, secondUp.ID, "rf.gesture")
	if secondUp.Gesture != "up" || double.Gesture != "double-click" {
		t.Fatalf("second release=%q final=%q", secondUp.Gesture, double.Gesture)
	}
}

func TestRFRepeatAccelerationBoundaries(t *testing.T) {
	tests := []struct {
		held time.Duration
		want time.Duration
	}{
		{0, 150 * time.Millisecond},
		{1999 * time.Millisecond, 150 * time.Millisecond},
		{2 * time.Second, 100 * time.Millisecond},
		{3999 * time.Millisecond, 100 * time.Millisecond},
		{4 * time.Second, 60 * time.Millisecond},
		{10 * time.Second, 60 * time.Millisecond},
	}
	for _, test := range tests {
		if got := rfRepeatInterval(test.held); got != test.want {
			t.Fatalf("held %v interval=%v want=%v", test.held, got, test.want)
		}
	}
}

func TestRFHoldSuppressesSingleClick(t *testing.T) {
	runtime := New(Options{})
	event := native.DeviceEvent{
		Type:   native.EventRFReceived,
		RFCode: 0x333333, RFBits: 24, RFProtocol: 1,
		RFLearnedID: 9,
	}
	key := rfGestureKey{code: event.RFCode, bits: event.RFBits, protocol: event.RFProtocol}
	after := runtime.LatestEventID()
	runtime.observeRFGesture(event)
	down := waitTestEvent(t, runtime, after, "rf.gesture")
	state := activeRFState(t, runtime, key)
	runtime.rfMu.Lock()
	state.firstSeen = time.Now().Add(-rfHoldAfter)
	runtime.rfMu.Unlock()
	runtime.observeRFGesture(event)
	hold := waitTestEvent(t, runtime, down.ID, "rf.gesture")
	if hold.Gesture != "hold" {
		t.Fatalf("long press gesture=%q", hold.Gesture)
	}
	runtime.finishRFGesture(key, activeRFState(t, runtime, key))
	up := waitTestEvent(t, runtime, hold.ID, "rf.gesture")
	if up.Gesture != "up" {
		t.Fatalf("hold release gesture=%q", up.Gesture)
	}
	ctx, cancel := context.WithTimeout(
		context.Background(),
		rfDoubleClickAfter+50*time.Millisecond,
	)
	defer cancel()
	if extra, err := runtime.WaitEvent(ctx, up.ID, "rf.gesture"); err == nil {
		t.Fatalf("hold emitted unexpected post-release event: %#v", extra)
	}
}

func TestActivityStreamIsRetainedSeparatelyFromContinuousFrames(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	after := runtime.LatestEventID()
	for index := 0; index < 600; index++ {
		runtime.PublishStructuredEvent(Event{Kind: "status_led.changed", Text: "frame"})
	}
	activity := runtime.PublishStructuredEvent(Event{Kind: "door", Text: "door opened"})
	for index := 0; index < 600; index++ {
		runtime.PublishStructuredEvent(Event{Kind: "front_panel.segment", Text: "frame"})
	}
	if activity.Stream != EventStreamActivity {
		t.Fatalf("activity stream=%q", activity.Stream)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	retained, err := runtime.WaitEventStreamFilter(ctx, after, "", nil, EventStreamActivity)
	if err != nil {
		t.Fatal(err)
	}
	if retained.ID != activity.ID || retained.Kind != "door" {
		t.Fatalf("retained activity=%#v want=%#v", retained, activity)
	}
	state, err := runtime.WaitEventStreamFilter(ctx, activity.ID, "", nil, EventStreamState)
	if err != nil {
		t.Fatal(err)
	}
	if state.Kind != "front_panel.segment" || state.Stream != EventStreamState {
		t.Fatalf("state event=%#v", state)
	}
}

func TestEventStreamClassification(t *testing.T) {
	tests := map[string]string{
		"door": EventStreamActivity, "telemetry": EventStreamTelemetry,
		"rx": EventStreamDebug, "front_panel.segment": EventStreamState,
		"status_led.changed": EventStreamState, "buzzer.note": EventStreamState,
		"illumination.changed": EventStreamState, "settings.changed": EventStreamState,
		"sensor.sample": EventStreamTelemetry, "animation.frame": EventStreamState,
	}
	for kind, expected := range tests {
		if got := EventStreamForKind(kind); got != expected {
			t.Errorf("EventStreamForKind(%q)=%q want %q", kind, got, expected)
		}
	}
}

func activeRFState(
	t *testing.T,
	runtime *Runtime,
	key rfGestureKey,
) *rfGestureState {
	t.Helper()
	runtime.rfMu.Lock()
	defer runtime.rfMu.Unlock()
	state := runtime.rfGestures[key]
	if state == nil {
		t.Fatal("RF gesture state is missing")
	}
	if state.timer != nil {
		state.timer.Stop()
	}
	return state
}

func waitTestEvent(
	t *testing.T,
	runtime *Runtime,
	after uint64,
	kind string,
) Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, kind)
	if err != nil {
		t.Fatal(err)
	}
	return event
}
