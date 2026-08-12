package control

import (
	"context"
	"errors"
	"strconv"
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

type statusLEDTestPort struct {
	reads     chan []byte
	closed    chan struct{}
	closeOnce sync.Once
}

func newStatusLEDTestPort() *statusLEDTestPort {
	return &statusLEDTestPort{
		reads:  make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (*statusLEDTestPort) SetMode(*serial.Mode) error { return nil }
func (port *statusLEDTestPort) Read(data []byte) (int, error) {
	select {
	case encoded := <-port.reads:
		return copy(data, encoded), nil
	case <-port.closed:
		return 0, errors.New("test port closed")
	}
}
func (*statusLEDTestPort) Write(data []byte) (int, error) { return len(data), nil }
func (*statusLEDTestPort) Drain() error                   { return nil }
func (*statusLEDTestPort) ResetInputBuffer() error        { return nil }
func (*statusLEDTestPort) ResetOutputBuffer() error       { return nil }
func (*statusLEDTestPort) SetDTR(bool) error              { return nil }
func (*statusLEDTestPort) SetRTS(bool) error              { return nil }
func (*statusLEDTestPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*statusLEDTestPort) SetReadTimeout(time.Duration) error { return nil }
func (port *statusLEDTestPort) Close() error {
	port.closeOnce.Do(func() { close(port.closed) })
	return nil
}
func (*statusLEDTestPort) Break(time.Duration) error { return nil }

func (port *statusLEDTestPort) sendStatusLED(state native.StatusLEDState) error {
	encoded, err := native.Encode(native.Frame{
		Opcode: native.OpStatusLEDChanged,
		Payload: []byte{
			state.Red, state.Green, state.Blue, state.Brightness,
			state.Effect, state.Condition,
		},
	})
	if err != nil {
		return err
	}
	select {
	case port.reads <- encoded:
		return nil
	case <-port.closed:
		return errors.New("test port closed")
	}
}

func TestStatusLEDPumpSuppressesOnlyUnchangedFrames(t *testing.T) {
	runtime := New(Options{})
	port := newStatusLEDTestPort()
	session := link.NewForPort("STATUS-LED-TEST", port)
	pumpDone := make(chan struct{})
	go func() {
		runtime.pump(session, 0)
		close(pumpDone)
	}()
	defer func() {
		if err := session.Close(); err != nil {
			t.Error(err)
		}
		select {
		case <-pumpDone:
		case <-time.After(time.Second):
			t.Error("status LED pump did not stop")
		}
	}()

	state := native.StatusLEDState{
		Red: 1, Green: 2, Blue: 3, Brightness: 4,
		Effect: native.StatusEffectNone, Condition: 5,
	}
	cursor := runtime.LatestEventID()
	if err := port.sendStatusLED(state); err != nil {
		t.Fatal(err)
	}
	first := waitTestEvent(t, runtime, cursor, "status_led.changed")
	firstSnapshot := runtime.Snapshot()
	if !firstSnapshot.HaveStatusLED || firstSnapshot.StatusLED != state {
		t.Fatalf("initial status LED snapshot=%#v", firstSnapshot.StatusLED)
	}
	if firstSnapshot.StatusLEDRevision != 1 ||
		first.Metadata["revision"] != strconv.FormatUint(firstSnapshot.StatusLEDRevision, 10) {
		t.Fatalf("initial revision snapshot=%d event=%q",
			firstSnapshot.StatusLEDRevision, first.Metadata["revision"])
	}

	if err := port.sendStatusLED(state); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	_, err := runtime.WaitEvent(ctx, first.ID, "status_led.changed")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unchanged status LED event was published: %v", err)
	}
	duplicateSnapshot := runtime.Snapshot()
	if !duplicateSnapshot.StatusLEDUpdated.Equal(firstSnapshot.StatusLEDUpdated) {
		t.Fatalf("unchanged frame advanced status timestamp: before=%v after=%v",
			firstSnapshot.StatusLEDUpdated, duplicateSnapshot.StatusLEDUpdated)
	}
	if duplicateSnapshot.StatusLEDRevision != firstSnapshot.StatusLEDRevision {
		t.Fatalf("unchanged frame advanced revision: before=%d after=%d",
			firstSnapshot.StatusLEDRevision, duplicateSnapshot.StatusLEDRevision)
	}

	changes := []native.StatusLEDState{
		{Red: 6, Green: 2, Blue: 3, Brightness: 4, Effect: native.StatusEffectNone, Condition: 5},
		{Red: 6, Green: 7, Blue: 3, Brightness: 4, Effect: native.StatusEffectNone, Condition: 5},
		{Red: 6, Green: 7, Blue: 8, Brightness: 4, Effect: native.StatusEffectNone, Condition: 5},
		{Red: 6, Green: 7, Blue: 8, Brightness: 9, Effect: native.StatusEffectNone, Condition: 5},
		{Red: 6, Green: 7, Blue: 8, Brightness: 9, Effect: native.StatusEffectBreathe, Condition: 5},
		{Red: 6, Green: 7, Blue: 8, Brightness: 9, Effect: native.StatusEffectBreathe, Condition: 10},
	}
	cursor = first.ID
	for index, changed := range changes {
		if err := port.sendStatusLED(changed); err != nil {
			t.Fatal(err)
		}
		event := waitTestEvent(t, runtime, cursor, "status_led.changed")
		cursor = event.ID
		published, err := native.ParseStatusLEDState(event.Frame.Payload)
		if err != nil || published != changed {
			t.Fatalf("change %d event was lost/reordered: got=%#v err=%v want=%#v",
				index, published, err, changed)
		}
		snapshot := runtime.Snapshot()
		wantRevision := uint64(index + 2)
		if snapshot.StatusLED != changed || snapshot.StatusLEDRevision != wantRevision ||
			event.Metadata["revision"] != strconv.FormatUint(wantRevision, 10) {
			t.Fatalf("change %d state/revision mismatch: snapshot=%#v rev=%d event_rev=%q want=%#v rev=%d",
				index, snapshot.StatusLED, snapshot.StatusLEDRevision,
				event.Metadata["revision"], changed, wantRevision)
		}
	}
	if got, want := runtime.LatestEventID(), first.ID+uint64(len(changes)); got != want {
		t.Fatalf("status LED event count=%d, want %d (duplicate was published)", got, want)
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
		Name: "TEST-NOT-A-REAL-PORT", VID: "1A86", PID: "7523",
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
	disconnected, err := runtime.WaitEvent(ctx, after, "connection")
	if err != nil {
		t.Fatal(err)
	}
	if disconnected.Lifecycle != "disconnect" ||
		disconnected.Port.SerialNumber != "controller-1" {
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
		"rx": EventStreamDebug, "action.applied": EventStreamDebug,
		"front_panel.segment": EventStreamState,
		"status_led.changed":  EventStreamState, "buzzer.note": EventStreamState,
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
