package control

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type reconnectTestPort struct {
	closed chan struct{}
}

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
func (*reconnectTestPort) SetDTR(bool) error              { return nil }
func (*reconnectTestPort) SetRTS(bool) error              { return nil }
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

func TestBuzzerChangedEventPreservesCompactAndTimestampedSemantics(t *testing.T) {
	compact := buzzerChangedEvent(native.Frame{
		Opcode:  native.OpBuzzerChanged,
		Payload: []byte{0xB8, 0x01, 0xDC, 0x00, 0},
	})
	if compact.Kind != "buzzer.note" || compact.Stream != EventStreamState ||
		compact.Metadata["frequency_hz"] != "440" ||
		compact.Metadata["duration_ms"] != "220" ||
		compact.Metadata["muted"] != "false" ||
		compact.Metadata["timed"] != "false" {
		t.Fatalf("compact buzzer event=%#v", compact)
	}
	if _, exists := compact.Metadata["device_micros"]; exists {
		t.Fatalf("compact buzzer event invented a device timestamp: %#v", compact.Metadata)
	}

	timestamped := buzzerChangedEvent(native.Frame{
		Opcode:  native.OpBuzzerChanged,
		Payload: []byte{0x70, 0x03, 125, 0, 1, 0x78, 0x56, 0x34, 0x12},
	})
	if timestamped.Kind != "buzzer.note" ||
		timestamped.Metadata["frequency_hz"] != "880" ||
		timestamped.Metadata["duration_ms"] != "125" ||
		timestamped.Metadata["muted"] != "true" ||
		timestamped.Metadata["timed"] != "true" ||
		timestamped.Metadata["device_micros"] != "305419896" {
		t.Fatalf("timestamped buzzer event=%#v", timestamped)
	}
}

func TestMalformedBuzzerPushIsDebugDiagnosticNotNotificationSpam(t *testing.T) {
	event := buzzerChangedEvent(native.Frame{
		Opcode:  native.OpBuzzerChanged,
		Payload: []byte{0xB8, 0x01, 0xDC, 0x00, 0, 0},
	})
	if event.Kind != "protocol.invalid" || event.Stream != EventStreamDebug ||
		event.Metadata["opcode"] != "BUZZER_CHANGED" ||
		event.Metadata["payload_bytes"] != "6" {
		t.Fatalf("malformed buzzer diagnostic=%#v", event)
	}
	if _, important := hostui.NotificationForImportantEvent(hostui.ImportantEvent{
		Kind: event.Kind, Message: event.Text, AppTitle: "PCController",
	}); important {
		t.Fatal("malformed repeated buzzer push became an operator toast")
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
	runtime.statusLED = native.StatusLEDState{Brightness: 255}
	runtime.haveStatusLED = true
	runtime.clearPeerStateLocked()
	runtime.mu.Unlock()

	snapshot := runtime.Snapshot()
	if snapshot.Hello != (native.Hello{}) || snapshot.Status != (native.Status{}) ||
		snapshot.Settings != (native.Settings{}) || snapshot.HaveStatus || snapshot.HaveSettings ||
		snapshot.HaveFrontPanel || snapshot.HaveStatusLED || !snapshot.StatusUpdated.IsZero() {
		t.Fatalf("disconnected runtime retained peer-owned state: %#v", snapshot)
	}
	if snapshot.Port.Name != "COM18" || snapshot.Port.SerialNumber != "controller-1" {
		t.Fatalf("reconnect identity was discarded: %#v", snapshot.Port)
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
