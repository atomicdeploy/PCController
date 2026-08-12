package control

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type macroPlaybackRequest struct {
	generation uint64
	opcode     byte
	payload    []byte
}

type macroPlaybackScript struct {
	runtime *Runtime

	mu                sync.Mutex
	requests          []macroPlaybackRequest
	queryCount        int
	streamPeriodMS    uint16
	failRun           bool
	failPlaybackQuery bool
	playing           chan struct{}
	playingOnce       sync.Once
}

func newMacroPlaybackScriptRunner(
	t *testing.T,
) (*Runtime, *MacroRunner, *macroPlaybackScript) {
	t.Helper()
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = &link.Session{}
	runtime.generation = 7
	runtime.port = ports.Info{
		Name: "COM7", IsUSB: true, VID: "1A86", PID: "7523",
		SerialNumber: "macro-timing-board",
	}
	runtime.hello = native.Hello{
		BoardKind:    native.BoardKindPCController,
		BuildHash:    0x12345678,
		Capabilities: native.CapabilityTimedMacroQueue,
	}
	runtime.connectionState = "connected"
	runtime.mu.Unlock()

	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "demo", Category: "test",
		Steps: []appconfig.MacroStep{{Kind: "relays-off"}},
	}}
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
	)
	// Keep best-effort front-panel presentation out of this protocol-order
	// harness; the scripted request seam deliberately belongs only to the
	// macro queue and stream guard.
	runner.presentOnce.Do(func() { runner.present = make(chan MacroState, 1) })
	script := &macroPlaybackScript{
		runtime: runtime, streamPeriodMS: 500, playing: make(chan struct{}),
	}
	runner.requestGeneration = script.request
	return runtime, runner, script
}

func (script *macroPlaybackScript) request(
	_ context.Context,
	generation uint64,
	opcode byte,
	payload []byte,
	_ byte,
) (native.Frame, error) {
	snapshot := script.runtime.Snapshot()
	if !snapshot.Connected || snapshot.Generation != generation {
		return native.Frame{}, fmt.Errorf("connection generation %d is no longer active", generation)
	}
	script.mu.Lock()
	defer script.mu.Unlock()
	script.requests = append(script.requests, macroPlaybackRequest{
		generation: generation, opcode: opcode, payload: append([]byte(nil), payload...),
	})
	switch opcode {
	case native.OpGetSettings:
		settings := native.DefaultSettings()
		settings.StreamPeriodMS = script.streamPeriodMS
		encoded, err := settings.Payload()
		return native.Frame{Opcode: native.OpSettings, Payload: encoded}, err
	case native.OpSetStream:
		if len(payload) != 2 {
			return native.Frame{}, errors.New("invalid SET_STREAM test payload")
		}
		script.streamPeriodMS = binary.LittleEndian.Uint16(payload)
		return native.Frame{Opcode: native.OpACK}, nil
	case native.OpMacroStep:
		if len(payload) == 1 && payload[0] == 1 {
			if script.failRun {
				return native.Frame{}, errors.New("forced RUN failure")
			}
			return native.Frame{Opcode: native.OpACK}, nil
		}
		if len(payload) == 1 && payload[0] == 2 {
			script.queryCount++
			if script.queryCount == 1 {
				return macroPlaybackStatusFrame(native.MacroStatus{
					Schema: native.MacroQueueSchema, State: native.MacroIdle,
				}), nil
			}
			script.playingOnce.Do(func() { close(script.playing) })
			if script.failPlaybackQuery {
				return native.Frame{}, errors.New("forced playback status failure")
			}
			status := native.MacroStatus{
				Schema: native.MacroQueueSchema, ID: 7,
				AcceptedSteps: 1, AcceptedBytes: 6, TotalSteps: 1,
				StartedAtUS: 1000,
			}
			if script.queryCount == 2 {
				status.State, status.Fill = native.MacroPlaying, 6
			} else {
				status.State, status.ExecutedSteps = native.MacroCompleted, 1
			}
			return macroPlaybackStatusFrame(status), nil
		}
	}
	return native.Frame{Opcode: native.OpACK}, nil
}

func macroPlaybackStatusFrame(status native.MacroStatus) native.Frame {
	payload := make([]byte, 21)
	payload[0] = native.EventMacro
	payload[1] = native.MacroQueueSchema
	payload[2] = status.State
	payload[3] = status.ID
	binary.LittleEndian.PutUint16(payload[4:6], status.AcceptedSteps)
	binary.LittleEndian.PutUint16(payload[6:8], status.ExecutedSteps)
	binary.LittleEndian.PutUint16(payload[8:10], status.AcceptedBytes)
	payload[10] = status.Fill
	payload[11] = status.Underruns
	payload[12] = status.DispatchErrors
	binary.LittleEndian.PutUint32(payload[13:17], status.StartedAtUS)
	binary.LittleEndian.PutUint16(payload[17:19], status.TotalSteps)
	binary.LittleEndian.PutUint16(payload[19:21], status.DroppedSteps)
	return native.Frame{Opcode: native.OpMacroStatus, Payload: payload}
}

func (script *macroPlaybackScript) requestSnapshot() []macroPlaybackRequest {
	script.mu.Lock()
	defer script.mu.Unlock()
	result := make([]macroPlaybackRequest, len(script.requests))
	copy(result, script.requests)
	return result
}

func macroStreamValues(requests []macroPlaybackRequest) []uint16 {
	var values []uint16
	for _, request := range requests {
		if request.opcode == native.OpSetStream && len(request.payload) == 2 {
			values = append(values, binary.LittleEndian.Uint16(request.payload))
		}
	}
	return values
}

func waitMacroPlaybackTerminal(t *testing.T, runner *MacroRunner) MacroState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for runner.State().Running && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	state := runner.State()
	if state.Running {
		t.Fatal("macro playback did not reach a terminal state")
	}
	return state
}

func connectMacroRecordingTestRuntime(runtime *Runtime) {
	runtime.mu.Lock()
	runtime.session = &link.Session{}
	runtime.port = ports.Info{
		Name: "COM-test", IsUSB: true, VID: "1A86", PID: "7523",
		SerialNumber: "macro-test-board",
	}
	runtime.hello = native.Hello{BoardKind: 1, BuildHash: 0x12345678}
	runtime.connectionState = "connected"
	runtime.mu.Unlock()
}

type macroCaptureTestStore struct {
	mu     sync.RWMutex
	config appconfig.Config
	saved  chan struct{}
}

func newMacroCaptureTestRunner(t *testing.T) (*Runtime, *MacroRunner, *macroCaptureTestStore) {
	t.Helper()
	runtime := New(Options{})
	store := &macroCaptureTestStore{config: appconfig.Defaults(), saved: make(chan struct{}, 8)}
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro {
			store.mu.RLock()
			defer store.mu.RUnlock()
			return append([]appconfig.Macro(nil), store.config.Macros...)
		},
		func() appconfig.Config {
			store.mu.RLock()
			defer store.mu.RUnlock()
			return store.config
		},
		func(change func(*appconfig.Config) error) error {
			store.mu.Lock()
			defer store.mu.Unlock()
			before := len(store.config.Macros)
			if err := change(&store.config); err != nil {
				return err
			}
			if err := store.config.Validate(); err != nil {
				return err
			}
			if len(store.config.Macros) > before {
				store.saved <- struct{}{}
			}
			return nil
		},
	)
	runner.ackCapture = func(boardCaptureToken) error { return nil }
	return runtime, runner, store
}

func (store *macroCaptureTestStore) macros() []appconfig.Macro {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return append([]appconfig.Macro(nil), store.config.Macros...)
}

func waitRecordingInactive(t *testing.T, runner *MacroRunner) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for runner.RecordingState().Active && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runner.RecordingState().Active {
		t.Fatal("recording did not stop")
	}
}

func TestMacroPlaybackDisablesPeriodicStreamBeforeRunAndRestoresOnCompletion(t *testing.T) {
	runtime, runner, script := newMacroPlaybackScriptRunner(t)
	if _, err := runner.Start(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-script.playing:
	case <-time.After(time.Second):
		t.Fatal("playback did not query the running MCU queue")
	}
	ackPayload := []byte{native.OpRelayAllOff, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(ackPayload[2:], 1000)
	runtime.publishEvent(Event{
		Kind: "rx",
		Frame: native.Frame{
			Seq: native.MacroExecutionSequence, Opcode: native.OpACK,
			Payload: ackPayload,
		},
	})
	state := waitMacroPlaybackTerminal(t, runner)
	if state.Lifecycle != "completed" || !state.Faithful || state.EvidenceSteps != 1 {
		t.Fatalf("completed playback state=%#v", state)
	}

	requests := script.requestSnapshot()
	settingsIndex, disableIndex, runIndex, restoreIndex := -1, -1, -1, -1
	for index, request := range requests {
		switch {
		case request.opcode == native.OpGetSettings:
			settingsIndex = index
		case request.opcode == native.OpSetStream && len(request.payload) == 2 &&
			binary.LittleEndian.Uint16(request.payload) == 0:
			disableIndex = index
		case request.opcode == native.OpMacroStep && len(request.payload) == 1 && request.payload[0] == 1:
			runIndex = index
		case request.opcode == native.OpSetStream && len(request.payload) == 2 &&
			binary.LittleEndian.Uint16(request.payload) == 500:
			restoreIndex = index
		}
	}
	if settingsIndex < 0 || disableIndex <= settingsIndex || runIndex <= disableIndex || restoreIndex <= runIndex {
		t.Fatalf(
			"stream guard order settings=%d disable=%d run=%d restore=%d requests=%#v",
			settingsIndex, disableIndex, runIndex, restoreIndex, requests,
		)
	}
	if values := macroStreamValues(requests); len(values) != 2 || values[0] != 0 || values[1] != 500 {
		t.Fatalf("stream period sequence=%v", values)
	}
}

func TestMacroPlaybackRestoresPeriodicStreamOnCancelAndFailures(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		_, runner, script := newMacroPlaybackScriptRunner(t)
		if _, err := runner.Start(context.Background(), "demo"); err != nil {
			t.Fatal(err)
		}
		select {
		case <-script.playing:
		case <-time.After(time.Second):
			t.Fatal("playback did not enter running state")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := runner.Cancel(ctx); err != nil {
			t.Fatal(err)
		}
		state := runner.State()
		if state.Lifecycle != "cancelled" {
			t.Fatalf("cancel state=%#v", state)
		}
		if values := macroStreamValues(script.requestSnapshot()); len(values) != 2 || values[0] != 0 || values[1] != 500 {
			t.Fatalf("cancel stream sequence=%v", values)
		}
	})

	t.Run("playback failure", func(t *testing.T) {
		_, runner, script := newMacroPlaybackScriptRunner(t)
		script.failPlaybackQuery = true
		if _, err := runner.Start(context.Background(), "demo"); err != nil {
			t.Fatal(err)
		}
		state := waitMacroPlaybackTerminal(t, runner)
		if state.Lifecycle != "failed" || !strings.Contains(state.LastError, "forced playback status failure") {
			t.Fatalf("failed playback state=%#v", state)
		}
		if values := macroStreamValues(script.requestSnapshot()); len(values) != 2 || values[0] != 0 || values[1] != 500 {
			t.Fatalf("failure stream sequence=%v", values)
		}
	})

	t.Run("run failure", func(t *testing.T) {
		_, runner, script := newMacroPlaybackScriptRunner(t)
		script.failRun = true
		if _, err := runner.Start(context.Background(), "demo"); err == nil ||
			!strings.Contains(err.Error(), "forced RUN failure") {
			t.Fatalf("RUN failure err=%v", err)
		}
		state := runner.State()
		if state.Lifecycle != "failed" {
			t.Fatalf("RUN failure state=%#v", state)
		}
		if values := macroStreamValues(script.requestSnapshot()); len(values) != 2 || values[0] != 0 || values[1] != 500 {
			t.Fatalf("RUN failure stream sequence=%v", values)
		}
	})
}

func TestMacroPlaybackNeverRestoresStreamOnReplacementBoard(t *testing.T) {
	runtime, runner, script := newMacroPlaybackScriptRunner(t)
	if _, err := runner.Start(context.Background(), "demo"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-script.playing:
	case <-time.After(time.Second):
		t.Fatal("playback did not enter running state")
	}
	runtime.mu.Lock()
	runtime.session = nil
	runtime.generation = 8
	runtime.port.SerialNumber = "replacement-board"
	runtime.hello.BuildHash = 0x87654321
	runtime.mu.Unlock()
	// Wake the evidence wait immediately; the playback loop must fail closed
	// before either cleanup or stream restoration can reach generation 8.
	runtime.publishEvent(Event{Kind: "connection", Lifecycle: "replaced"})
	state := waitMacroPlaybackTerminal(t, runner)
	if state.Lifecycle != "failed" || !strings.Contains(state.LastError, "generation 7") {
		t.Fatalf("replacement playback state=%#v", state)
	}
	requests := script.requestSnapshot()
	if values := macroStreamValues(requests); len(values) != 1 || values[0] != 0 {
		t.Fatalf("replacement board received restoration: stream sequence=%v requests=%#v", values, requests)
	}
}

func TestMacroStreamLeaseAlsoRejectsSameGenerationIdentityReplacement(t *testing.T) {
	runtime, runner, script := newMacroPlaybackScriptRunner(t)
	snapshot := runtime.Snapshot()
	lease := &macroStreamLease{
		Generation: snapshot.Generation,
		Board:      captureBoardIdentity(snapshot.Port, snapshot.Hello),
		PeriodMS:   500,
	}
	runtime.mu.Lock()
	runtime.port.SerialNumber = "replacement-board"
	runtime.mu.Unlock()
	if err := runner.restoreMacroStream(lease); err != nil {
		t.Fatal(err)
	}
	if requests := script.requestSnapshot(); len(requests) != 0 {
		t.Fatalf("same-generation replacement received restoration: %#v", requests)
	}
}

func TestCompileMacroEncodesOrdinaryOpcodesWithExactOffsets(t *testing.T) {
	compiled, err := compileMacro(appconfig.Macro{
		ID: 7, Name: "demo",
		Steps: []appconfig.MacroStep{
			{AtUS: 0, Kind: "relay", Target: 5, Value: 1},
			{AtUS: 1250, Kind: "pwm", Target: 2, Value: 2048},
			{AtUS: 2500, Kind: "beep", FrequencyHz: 880, DurationMS: 25},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.durationUS != 2500 || len(compiled.steps) != 3 {
		t.Fatalf("unexpected compile summary: %#v", compiled)
	}
	offset := 0
	wantOpcodes := []byte{native.OpRelaySet, native.OpPWMSet, native.OpBuzzer}
	wantDue := []uint32{0, 1250, 2500}
	for index := range wantOpcodes {
		due := binary.LittleEndian.Uint32(compiled.stream[offset : offset+4])
		opcode := compiled.stream[offset+4]
		length := int(compiled.stream[offset+5])
		if due != wantDue[index] || opcode != wantOpcodes[index] {
			t.Fatalf("record %d got due/opcode %d/0x%02X", index, due, opcode)
		}
		offset += native.MacroRecordHeaderSize + length
		if compiled.completeSteps(offset) != index+1 {
			t.Fatalf("completeSteps(%d) did not include record %d", offset, index)
		}
	}
	if offset != len(compiled.stream) {
		t.Fatalf("decoded %d of %d bytes", offset, len(compiled.stream))
	}
}

func TestCompileMacroAcceptsLegacyBuzzerStepAsBeep(t *testing.T) {
	canonicalOpcode, canonicalPayload, canonicalErr := compileMacroCommand(appconfig.MacroStep{
		Kind: "beep", FrequencyHz: 880, DurationMS: 25,
	})
	legacyOpcode, legacyPayload, legacyErr := compileMacroCommand(appconfig.MacroStep{
		Kind: "buzzer", FrequencyHz: 880, DurationMS: 25,
	})
	if canonicalErr != nil || legacyErr != nil {
		t.Fatalf("canonical=%v legacy=%v", canonicalErr, legacyErr)
	}
	if canonicalOpcode != legacyOpcode || !bytes.Equal(canonicalPayload, legacyPayload) {
		t.Fatalf("compiled forms differ: canonical=0x%02X/% X legacy=0x%02X/% X", canonicalOpcode, canonicalPayload, legacyOpcode, legacyPayload)
	}
}

func TestMacroRecorderUsesWrappingMCUAcknowledgementDeltas(t *testing.T) {
	runtime := New(Options{})
	connectMacroRecordingTestRuntime(runtime)
	config := appconfig.Defaults()
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	if _, err := runner.StartRecording("lift", "motion", "purple"); err != nil {
		t.Fatal(err)
	}
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySet, Payload: []byte{5, 1},
		Source:       native.InputSourceHost,
		DeviceMicros: 0xFFFFFF00, Timed: true,
	})
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{2, 0x00, 0x08},
		Source:       native.InputSourceHost,
		DeviceMicros: 0x000000F4, Timed: true,
	})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if macro.Color != "violet" || len(macro.Steps) != 2 {
		t.Fatalf("unexpected recorded macro: %#v", macro)
	}
	if macro.Steps[0].AtUS != 0 || macro.Steps[1].AtUS != 500 {
		t.Fatalf("MCU wrap delta was not preserved: %#v", macro.Steps)
	}
	if len(config.Macros) != 1 || config.Macros[0].Name != "lift" {
		t.Fatalf("recording was not persisted: %#v", config.Macros)
	}
}

func TestMacroRecorderCombinesPanelAndRFWithoutDuplicatingHostEcho(t *testing.T) {
	runtime := New(Options{})
	connectMacroRecordingTestRuntime(runtime)
	config := appconfig.Defaults()
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	if _, err := runner.StartRecording("mixed", "motion", "green"); err != nil {
		t.Fatal(err)
	}
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 1},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1000, Timed: true,
	})
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{4, 0xFF, 0x0F},
		Source: native.InputSourceRF, SourceID: 3, BoardOrigin: true,
		DeviceMicros: 2750, Timed: true,
	})
	// This is the board echo for a host ACK, not a third activation.
	runner.captureAction(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{4, 0xFF, 0x0F},
		Source: native.InputSourceHost, BoardOrigin: true,
		DeviceMicros: 2750, Timed: true,
	})
	state := runner.RecordingState()
	if state.Steps != 2 || state.PanelSteps != 1 || state.RFSteps != 1 ||
		state.HostSteps != 0 || state.LastAtUS != 1750 ||
		state.LastDeltaUS != 1750 || state.LastOpcode != native.OpPWMSet ||
		state.LastSource != native.InputSourceRF {
		t.Fatalf("unexpected mixed-source recorder state: %#v", state)
	}
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(macro.Steps) != 2 || macro.Steps[0].AtUS != 0 ||
		macro.Steps[1].AtUS != 1750 || macro.Steps[0].Kind != "motion" ||
		macro.Steps[1].Kind != "pwm" {
		t.Fatalf("unexpected mixed-source macro: %#v", macro)
	}
	shown, err := macroCommand(context.Background(), runner, []string{"show", "mixed"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shown, "at_us=0") ||
		!strings.Contains(shown, "at_us=1750") ||
		!strings.Contains(shown, "delta_us=1750") {
		t.Fatalf("exact MCU offsets/deltas missing from macro show:\n%s", shown)
	}
}

func TestMacroRecorderOrdersConcurrentHostAndPanelEvidenceAcrossRollover(t *testing.T) {
	runtime, runner, _ := newMacroCaptureTestRunner(t)
	connectMacroRecordingTestRuntime(runtime)
	if _, err := runner.StartRecording("ordered", "mixed", "blue"); err != nil {
		t.Fatal(err)
	}
	// The pump publishes the later panel edge first. The requester goroutine
	// then publishes the earlier ACK edge after micros wrapped.
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 1},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 0x00000100, Timed: true, Generation: 0,
	})
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelayAllOff, Payload: nil,
		Source:       native.InputSourceHost,
		DeviceMicros: 0xFFFFFF00, Timed: true, Generation: 0,
	})
	macro, err := runner.StopRecording(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(macro.Steps) != 2 || macro.Steps[0].Kind != "relays-off" ||
		macro.Steps[0].AtUS != 0 || macro.Steps[1].Kind != "motion" ||
		macro.Steps[1].AtUS != 512 {
		t.Fatalf("concurrent MCU evidence was not wrap-ordered: %#v", macro.Steps)
	}
}

func TestHostRecordingStopsOnPinnedBoardReplacement(t *testing.T) {
	runtime, runner, _ := newMacroCaptureTestRunner(t)
	connectMacroRecordingTestRuntime(runtime)
	if _, err := runner.StartRecording("pinned", "test", "green"); err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.generation++
	runtime.port.SerialNumber = "replacement-board"
	runtime.mu.Unlock()
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelayAllOff, Source: native.InputSourceHost,
		DeviceMicros: 500, Timed: true, Generation: 1,
	})
	state := runner.RecordingState()
	if state.Active || !strings.Contains(state.LastError, "pinned board connection changed") {
		t.Fatalf("replacement board did not fail closed: %#v", state)
	}
}

func TestHostACKRecordingPreservesVariableDisplayAndStatusEffect(t *testing.T) {
	display, err := native.DisplayTextPayload(native.DisplayLCD, 250, "hello")
	if err != nil {
		t.Fatal(err)
	}
	effect, err := native.StatusEffectPayload(native.StatusEffectOptions{
		Kind: native.StatusEffectBreathe, Red: 1, Green: 2, Blue: 3,
		Brightness: 200, MinimumBrightness: 20, PeriodMS: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, evidence := range []ActionEvidence{
		{Opcode: native.OpDisplayText, Payload: display, Source: native.InputSourceHost},
		{Opcode: native.OpStatusEffect, Payload: effect, Source: native.InputSourceHost},
	} {
		step, ok := recordedMacroStep(evidence)
		if !ok {
			t.Fatalf("variable host ACK action was not recordable: %#v", evidence)
		}
		opcode, payload, compileErr := compileMacroCommand(step)
		if compileErr != nil || opcode != evidence.Opcode || !strings.EqualFold(
			hex.EncodeToString(payload), hex.EncodeToString(evidence.Payload),
		) {
			t.Fatalf("variable action was not lossless: step=%#v payload=% X err=%v", step, payload, compileErr)
		}
		evidence.BoardOrigin = true
		if _, accepted := recordedMacroStep(evidence); accepted {
			t.Fatalf("variable payload entered compact board Action evidence: %#v", evidence)
		}
	}
}

func TestRawOpcodeACKUsesSharedExactEvidenceIngress(t *testing.T) {
	runtime := New(Options{})
	var captured []ActionEvidence
	release := runtime.ObserveActions(func(evidence ActionEvidence) {
		captured = append(captured, evidence)
	})
	defer release()
	for index, test := range []struct {
		opcode  byte
		payload []byte
		atUS    uint32
	}{
		{native.OpRelayAllOff, nil, 0x01020304},
		{native.OpRFTx, []byte{1, 2, 3, 4, 24, 1, 0x5E, 1}, 0xA1B2C3D4},
	} {
		ack := native.Frame{Opcode: native.OpACK, Payload: []byte{
			test.opcode, 0, byte(test.atUS), byte(test.atUS >> 8),
			byte(test.atUS >> 16), byte(test.atUS >> 24),
		}}
		if !runtime.publishAcknowledgedHostAction(test.opcode, test.payload, ack, 7) {
			t.Fatalf("case %d was not published", index)
		}
	}
	if len(captured) != 2 || captured[0].DeviceMicros != 0x01020304 ||
		captured[1].DeviceMicros != 0xA1B2C3D4 ||
		len(captured[0].Payload) != 0 || len(captured[1].Payload) != 8 {
		t.Fatalf("raw opcode ACK evidence lost exact timestamp/payload: %#v", captured)
	}
}

func TestBoardLocalReplayPublishesLiveProgressAndTerminalState(t *testing.T) {
	_, runner, store := newMacroCaptureTestRunner(t)
	const board = "transport=macro-test-board;vid=1A86;pid=7523;kind=1;build=12345678"
	store.mu.Lock()
	store.config.Macros = []appconfig.Macro{{
		ID: 3, Name: "door cycle", Category: "motion",
		CaptureBoard: board, CaptureID: 7,
		Steps: []appconfig.MacroStep{
			{Kind: "relays-off"}, {AtUS: 500, Kind: "relays-off"},
		},
	}}
	store.mu.Unlock()
	playing := native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroPlaying,
		ID: 7, TotalSteps: 2, StartedAtUS: 5000,
	}
	runner.handleBoardMacroStatusAtGeneration(playing, 9, board)
	state := runner.State()
	if !state.Running || state.Name != "door cycle" ||
		state.Lifecycle != "local-playing" || state.DeviceStartedAtUS != 5000 ||
		state.Step != 0 || state.StepCount != 2 {
		t.Fatalf("local replay start was not observable: %#v", state)
	}
	playing.ExecutedSteps = 1
	runner.handleBoardMacroStatusAtGeneration(playing, 9, board)
	if state = runner.State(); state.Step != 1 || state.Device.ExecutedSteps != 1 {
		t.Fatalf("local replay progress was not observable: %#v", state)
	}
	playing.State = native.MacroCompleted
	playing.ExecutedSteps = 2
	runner.handleBoardMacroStatusAtGeneration(playing, 9, board)
	state = runner.State()
	if state.Running || state.Lifecycle != "local-completed" ||
		state.Step != 2 || !state.Faithful || state.DeviceStartedAtUS != 5000 {
		t.Fatalf("local replay completion was not observable: %#v", state)
	}
}

func TestBoardRecordingStatusAutoStartsProvisionalHostMacro(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	runtime.publishMacroStatus(native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroRecording, ID: 7,
		StartedAtUS: 1000,
	})
	state := runner.RecordingState()
	if !state.Active || !state.BoardOwned || state.BoardID != 7 ||
		state.ID != 7 || state.Name != "Board capture 7" || state.Category != "board" {
		t.Fatalf("unexpected board recording state: %#v", state)
	}
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{1, 2},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1100, Timed: true,
	})
	if state = runner.RecordingState(); state.Steps != 1 || state.PanelSteps != 1 {
		t.Fatalf("board action was not captured: %#v", state)
	}
	runner.recordMu.RLock()
	live := append([]appconfig.MacroStep(nil), runner.recordMacro.Steps...)
	runner.recordMu.RUnlock()
	recoveredRecord, err := native.EncodeMacroRecord(100, native.OpRelaySide, []byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := decodeMacroCaptureStream(recoveredRecord)
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeRecordedMacroSteps(live, recovered)
	if len(merged) != 1 || merged[0].AtUS != 100 {
		t.Fatalf("live and recovered board-relative step did not deduplicate: %#v", merged)
	}
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
}

func TestBoardCaptureSealsAfterOverflowActionAndDeduplicatesLifecycle(t *testing.T) {
	runtime, runner, store := newMacroCaptureTestRunner(t)
	retained, err := native.EncodeMacroRecord(100, native.OpRelaySide, []byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	fetchStarted := make(chan boardCaptureToken, 1)
	releaseFetch := make(chan struct{})
	var fetchCalls atomic.Int32
	runner.fetchCapture = func(token boardCaptureToken) ([]byte, error) {
		fetchCalls.Add(1)
		fetchStarted <- token
		<-releaseFetch
		return retained, nil
	}

	status := native.MacroStatus{
		Schema: native.MacroQueueSchema, ID: 7, StartedAtUS: 1000,
	}
	status.State = native.MacroRecording
	runtime.publishMacroStatus(status)
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 1},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1100, Timed: true,
	})
	// Firmware guarantees this final, non-retained action precedes Captured.
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: native.OpPWMSet, Payload: []byte{2, 0, 8},
		Source: native.InputSourceRF, BoardOrigin: true,
		DeviceMicros: 1200, Timed: true,
	})
	status.State = native.MacroCaptured
	status.AcceptedSteps = 1
	status.DroppedSteps = 1
	runtime.publishMacroStatus(status)
	select {
	case token := <-fetchStarted:
		if token.ID != 7 || token.StartedAtUS != 1000 {
			t.Fatalf("unexpected capture token: %#v", token)
		}
	case <-time.After(time.Second):
		t.Fatal("capture recovery did not start")
	}

	// Repeated lifecycle reports must not start another fetch/save. Evidence
	// received after the synchronous seal must not enter the recording.
	runtime.publishMacroStatus(status)
	runtime.publishActionEvidence(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{1, 2},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1300, Timed: true,
	})
	time.Sleep(10 * time.Millisecond)
	if calls := fetchCalls.Load(); calls != 1 {
		t.Fatalf("duplicate Captured status started %d recoveries", calls)
	}
	close(releaseFetch)
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("sealed board capture was not saved")
	}

	macros := store.macros()
	if len(macros) != 1 || len(macros[0].Steps) != 2 ||
		macros[0].Steps[0].AtUS != 100 || macros[0].Steps[1].AtUS != 200 ||
		macros[0].CaptureDroppedSteps != 1 || macros[0].CaptureMissingSteps != 0 {
		t.Fatalf("overflow capture was not faithful: %#v", macros)
	}
}

func TestConnectedBoardCaptureStreamsBeyondRetainedRingUntilExplicitStop(t *testing.T) {
	runtime, runner, store := newMacroCaptureTestRunner(t)
	connectMacroRecordingTestRuntime(runtime)
	const (
		board = "transport=macro-test-board;vid=1A86;pid=7523;kind=1;build=12345678"
		steps = 20 // RelaySide records are 8 bytes: 160 bytes > 127-byte ring.
	)
	var retained []byte
	for index := 0; index < 15; index++ {
		record, err := native.EncodeMacroRecord(
			uint32((index+1)*100), native.OpRelaySide, []byte{0, 1},
		)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, record...)
	}
	runner.fetchCapture = func(token boardCaptureToken) ([]byte, error) {
		if token.Board != board || token.ID != 7 || token.StartedAtUS != 1000 {
			t.Fatalf("unexpected capture token: %#v", token)
		}
		return retained, nil
	}
	status := native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroRecording,
		ID: 7, StartedAtUS: 1000,
	}
	runner.handleBoardMacroStatusAtGeneration(status, 0, board)
	for index := 0; index < steps; index++ {
		runtime.publishActionEvidence(ActionEvidence{
			Opcode: native.OpRelaySide, Payload: []byte{0, 1},
			Source: native.InputSourcePhysical, BoardOrigin: true,
			DeviceMicros: uint32(1100 + index*100), Timed: true,
		})
		if index == 15 {
			status.State = native.MacroCaptured
			status.AcceptedSteps = 15
			status.AcceptedBytes = uint16(len(retained))
			status.Fill = byte(len(retained))
			status.TotalSteps = 15
			status.DroppedSteps = 1
			runner.handleBoardMacroStatusAtGeneration(status, 0, board)
			if state := runner.RecordingState(); !state.Active || state.Steps != 16 {
				t.Fatalf("full retained ring stopped connected streaming: %#v", state)
			}
		}
	}
	if state := runner.RecordingState(); !state.Active || state.Steps != steps ||
		state.DroppedSteps != 1 {
		t.Fatalf("connected long capture state=%#v", state)
	}
	runner.queryCaptureStatus = func(generation uint64) (native.MacroStatus, error) {
		if generation != 0 {
			t.Fatalf("capture queried generation %d", generation)
		}
		return status, nil
	}
	if _, err := runner.StopBoardCapture(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("connected long capture was not saved after explicit stop")
	}
	macros := store.macros()
	if len(macros) != 1 || len(macros[0].Steps) != steps ||
		macros[0].CaptureDroppedSteps != 1 || macros[0].CaptureMissingSteps != 0 ||
		macros[0].Steps[steps-1].AtUS != uint32(steps*100) {
		t.Fatalf("connected long capture was not lossless: %#v", macros)
	}
}

func TestOfflineBoardCaptureReportsBoundedPrefixAsTruncated(t *testing.T) {
	_, runner, store := newMacroCaptureTestRunner(t)
	var retained []byte
	for index := 0; index < 15; index++ {
		record, err := native.EncodeMacroRecord(
			uint32((index+1)*100), native.OpRelaySide, []byte{0, 1},
		)
		if err != nil {
			t.Fatal(err)
		}
		retained = append(retained, record...)
	}
	runner.fetchCapture = func(boardCaptureToken) ([]byte, error) { return retained, nil }
	status := native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroCaptured,
		ID: 8, StartedAtUS: 2000, AcceptedSteps: 15,
		AcceptedBytes: uint16(len(retained)), Fill: byte(len(retained)),
		TotalSteps: 15, DroppedSteps: 1,
	}
	runner.handleBoardMacroStatusAtGeneration(status, 0, "offline-board")
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("offline retained capture was not recovered")
	}
	macros := store.macros()
	if len(macros) != 1 || len(macros[0].Steps) != 15 ||
		macros[0].CaptureDroppedSteps != 1 || macros[0].CaptureMissingSteps != 1 {
		t.Fatalf("offline truncation was not reported truthfully: %#v", macros)
	}
}

func TestBoardCaptureReconnectGenerationCannotSaveOldRecovery(t *testing.T) {
	runtime, runner, store := newMacroCaptureTestRunner(t)
	runtime.mu.Lock()
	runtime.generation = 10
	runtime.mu.Unlock()
	record, err := native.EncodeMacroRecord(50, native.OpRelaySide, []byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	fetchStarted := make(chan boardCaptureToken, 1)
	releaseFetch := make(chan struct{})
	runner.fetchCapture = func(token boardCaptureToken) ([]byte, error) {
		fetchStarted <- token
		<-releaseFetch
		return record, nil
	}
	status := native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroRecording,
		ID: 4, StartedAtUS: 4000,
	}
	runtime.publishMacroStatus(status)
	status.State = native.MacroCaptured
	status.AcceptedSteps = 1
	runtime.publishMacroStatus(status)
	select {
	case token := <-fetchStarted:
		if token.Generation != 10 {
			t.Fatalf("old capture used generation %d", token.Generation)
		}
	case <-time.After(time.Second):
		t.Fatal("old capture recovery did not start")
	}

	runtime.mu.Lock()
	runtime.generation = 11
	runtime.mu.Unlock()
	close(releaseFetch)
	waitRecordingInactive(t, runner)
	if macros := store.macros(); len(macros) != 0 {
		t.Fatalf("stale generation saved against replacement board: %#v", macros)
	}

	// The replacement board may reuse the same uint8 ID and starts cleanly.
	runner.fetchCapture = func(token boardCaptureToken) ([]byte, error) {
		if token.Generation != 11 || token.StartedAtUS != 9000 {
			t.Fatalf("replacement capture token=%#v", token)
		}
		return record, nil
	}
	status.StartedAtUS = 9000
	status.State = native.MacroRecording
	runtime.publishMacroStatus(status)
	status.State = native.MacroCaptured
	runtime.publishMacroStatus(status)
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("replacement generation capture was not saved")
	}
	macros := store.macros()
	if len(macros) != 1 || macros[0].Name != "Board capture 4" ||
		len(macros[0].Steps) != 1 || macros[0].Steps[0].AtUS != 50 {
		t.Fatalf("replacement capture was not isolated: %#v", macros)
	}
}

func TestConnectActivelyRecoversCaptureCompletedWhileHostWasAbsent(t *testing.T) {
	runtime, runner, store := newMacroCaptureTestRunner(t)
	runtime.mu.Lock()
	runtime.generation = 22
	runtime.mu.Unlock()
	record, err := native.EncodeMacroRecord(375, native.OpRelaySide, []byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	var queries atomic.Int32
	runner.queryCaptureStatus = func(generation uint64) (native.MacroStatus, error) {
		queries.Add(1)
		if generation != 22 {
			t.Fatalf("status query generation=%d", generation)
		}
		return native.MacroStatus{
			Schema: native.MacroQueueSchema, State: native.MacroCaptured,
			ID: 9, StartedAtUS: 7000, AcceptedSteps: 1,
		}, nil
	}
	runner.fetchCapture = func(token boardCaptureToken) ([]byte, error) {
		if token.Generation != 22 || token.ID != 9 || token.StartedAtUS != 7000 {
			t.Fatalf("offline recovery token=%#v", token)
		}
		return record, nil
	}

	// A profile without macro support must not receive an unsupported query.
	runner.recoverBoardCaptureOnConnect(22, ports.Info{}, native.Hello{})
	if queries.Load() != 0 {
		t.Fatal("macro status queried without advertised support")
	}
	runner.recoverBoardCaptureOnConnect(22, ports.Info{}, native.Hello{
		Capabilities: native.CapabilityTimedMacroQueue,
	})
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("offline board capture was not recovered on connect")
	}
	macros := store.macros()
	if queries.Load() != 1 || len(macros) != 1 ||
		macros[0].Name != "Board capture 9" || macros[0].RecordingSource != "board" ||
		len(macros[0].Steps) != 1 || macros[0].Steps[0].AtUS != 375 {
		t.Fatalf("offline reconnect recovery=%#v queries=%d", macros, queries.Load())
	}
}

func TestRetainedCaptureReconnectIsDurablyDeduplicatedAndAcknowledged(t *testing.T) {
	runtime, runner, store := newMacroCaptureTestRunner(t)
	record, err := native.EncodeMacroRecord(375, native.OpRelaySide, []byte{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	runner.fetchCapture = func(boardCaptureToken) ([]byte, error) { return record, nil }
	var acknowledgements atomic.Int32
	runner.ackCapture = func(token boardCaptureToken) error {
		if token.ID != 9 || token.StartedAtUS != 7000 {
			t.Fatalf("acknowledgement token=%#v", token)
		}
		acknowledgements.Add(1)
		return nil
	}
	status := native.MacroStatus{
		Schema: native.MacroQueueSchema, State: native.MacroRecording,
		ID: 9, StartedAtUS: 7000, AcceptedSteps: 1,
	}
	const board = "transport=USB-BOARD-A;vid=1A86;pid=7523;kind=1;build=01234567"
	runtime.mu.Lock()
	runtime.generation = 1
	runtime.mu.Unlock()
	runner.handleBoardMacroStatusAtGeneration(status, 1, board)
	status.State = native.MacroCaptured
	runner.handleBoardMacroStatusAtGeneration(status, 1, board)
	select {
	case <-store.saved:
	case <-time.After(time.Second):
		t.Fatal("first retained capture was not saved")
	}
	deadline := time.Now().Add(time.Second)
	for acknowledgements.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	macros := store.macros()
	if len(macros) != 1 || len(macros[0].CaptureImportKey) != 64 ||
		macros[0].CaptureBoard != board || macros[0].CaptureID != 9 ||
		macros[0].CaptureStartedAtUS != 7000 {
		t.Fatalf("durable capture identity missing: %#v", macros)
	}

	// The retained Exported state is intentionally queryable after reconnect.
	// It must acknowledge the same capture again without creating a duplicate.
	runtime.mu.Lock()
	runtime.generation = 2
	runtime.mu.Unlock()
	status.State = native.MacroExported
	runner.handleBoardMacroStatusAtGeneration(status, 2, board)
	deadline = time.Now().Add(time.Second)
	for acknowledgements.Load() != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if acknowledgements.Load() != 2 {
		t.Fatalf("recovered capture acknowledgement count=%d", acknowledgements.Load())
	}
	if macros = store.macros(); len(macros) != 1 {
		t.Fatalf("reconnect duplicated retained capture: %#v", macros)
	}
	if runner.RecordingState().Active {
		t.Fatal("deduplicated retained capture left a provisional recording active")
	}
}

func TestBoardCaptureRejectsActionFromAnotherConnectionGeneration(t *testing.T) {
	_, runner, _ := newMacroCaptureTestRunner(t)
	token := boardCaptureToken{Generation: 7, ID: 4, StartedAtUS: 1000, Board: "board-A"}
	if _, err := runner.startRecording(&token, "Board capture 4", "board", "green"); err != nil {
		t.Fatal(err)
	}
	runner.captureAction(ActionEvidence{
		Opcode: native.OpRelaySide, Payload: []byte{0, 1},
		Source: native.InputSourcePhysical, BoardOrigin: true,
		DeviceMicros: 1100, Timed: true, Generation: 8,
	})
	if state := runner.RecordingState(); state.Steps != 0 {
		t.Fatalf("replacement-generation action entered capture: %#v", state)
	}
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
}

func TestCaptureStreamDecodeMergeAndMetadataUpdate(t *testing.T) {
	first, err := native.EncodeMacroRecord(100, native.OpRelaySide, []byte{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := native.EncodeMacroRecord(250, native.OpPWMSet, []byte{2, 0x00, 0x08})
	if err != nil {
		t.Fatal(err)
	}
	steps, err := decodeMacroCaptureStream(append(first, second...))
	if err != nil {
		t.Fatal(err)
	}
	merged := mergeRecordedMacroSteps(steps[:1], steps)
	if len(merged) != 2 || merged[0].AtUS != 100 || merged[1].AtUS != 250 ||
		merged[0].Kind != "motion" || merged[1].Kind != "pwm" {
		t.Fatalf("unexpected recovered steps: %#v", merged)
	}

	runtime := New(Options{})
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "Board capture 7", Category: "board", Color: "green",
		RecordingSource: "board", Steps: merged,
	}}
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	category, color := "motion", "purple"
	updated, err := runner.UpdateMetadata("7", "Night lift", &category, &color)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "Night lift" || updated.Category != "motion" ||
		updated.Color != "violet" || updated.RecordingSource != "board" ||
		len(updated.Steps) != 2 {
		t.Fatalf("metadata update changed capture data: %#v", updated)
	}
	preserved, err := runner.UpdateMetadata("7", "Night lift renamed", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Category != "motion" || preserved.Color != "violet" {
		t.Fatalf("rename erased omitted metadata: %#v", preserved)
	}
	clear := "-"
	cleared, err := runner.UpdateMetadata("7", "Night lift renamed", &clear, &clear)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Category != "" || cleared.Color != "" {
		t.Fatalf("explicit metadata clear was ignored: %#v", cleared)
	}
}

func TestMacroExplicitSafeCancelOverridesBeginKeepPreference(t *testing.T) {
	if payload := native.MacroQueueCancelPayload(false); len(payload) != 1 || payload[0] != 0 {
		t.Fatalf("safe cancel must be explicit zero, got %v", payload)
	}
}

func TestObservedPlaybackCountHasNoOffByOne(t *testing.T) {
	if observedExecutionCount(0) != 0 || observedExecutionCount(1) != 1 ||
		observedExecutionCount(7) != 7 || observedExecutionCount(70000) != 65535 {
		t.Fatal("observed playback count is not the exact acknowledged-step count")
	}
}

func TestRecordedMacroStepRequiresCanonicalFixedPayloadShape(t *testing.T) {
	for _, evidence := range []ActionEvidence{
		{Opcode: native.OpRelayAllOff, Payload: []byte{1}},
		{Opcode: native.OpPWMAllOff, Payload: []byte{0}},
		{Opcode: native.OpRelaySet, Payload: []byte{5}},
		{Opcode: native.OpStatusEffect, Payload: []byte{1, 2, 3}},
	} {
		if step, ok := recordedMacroStep(evidence); ok {
			t.Fatalf("malformed/playback-only evidence accepted: %#v => %#v", evidence, step)
		}
	}
}

func TestMacroMetadataAndMonitorCommandsUseTheSharedRunner(t *testing.T) {
	runtime := New(Options{})
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "old-name", Category: "Test", Color: "violet",
		Steps: []appconfig.MacroStep{{Kind: "relay", Target: 5, Value: 1}},
	}}
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
		func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	)
	ctx := context.Background()
	if output, err := macroCommand(ctx, runner, []string{"rename", "7", "new-name"}); err != nil || !strings.Contains(output, "new-name") {
		t.Fatalf("rename output=%q err=%v", output, err)
	}
	if output, err := macroCommand(ctx, runner, []string{"category", "new-name", "Diagnostics"}); err != nil || !strings.Contains(output, "Diagnostics") {
		t.Fatalf("category output=%q err=%v", output, err)
	}
	if got := config.Macros[0]; got.Name != "new-name" || got.Category != "Diagnostics" {
		t.Fatalf("metadata did not persist: %#v", got)
	}
	if output, err := macroCommand(ctx, runner, []string{"monitor"}); err != nil || !strings.Contains(output, "playback=") || !strings.Contains(output, "recording=") {
		t.Fatalf("monitor output=%q err=%v", output, err)
	}
	if _, err := macroCommand(ctx, runner, []string{"rename", "7", ""}); err == nil {
		t.Fatal("empty rename was accepted")
	}
}
