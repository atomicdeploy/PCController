package control

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
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

	mu             sync.Mutex
	requests       []macroPlaybackRequest
	queryCount     int
	streamPeriodMS uint16
	failRun        bool
	playing        chan struct{}
	playingOnce    sync.Once
}

func newMacroPlaybackScriptRunner(t *testing.T) (*Runtime, *MacroRunner, *macroPlaybackScript) {
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
		Name: "PCController", BoardKind: native.BoardKindPCController,
		BuildHash: 0x12345678, BuildTimestamp: 0x01020304,
		Capabilities: native.CapabilityTimedMacroQueue,
	}
	runtime.connectionState = "connected"
	runtime.mu.Unlock()

	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{
		ID: 7, Name: "demo", Category: "test", Mode: macroModeMCU,
		Steps: []appconfig.MacroStep{{Kind: "relays-off"}},
	}}
	runner := NewMacroRunner(
		runtime,
		func() []appconfig.Macro { return config.Macros },
		func() appconfig.Config { return config },
	)
	// Keep the best-effort presenter out of this protocol-order harness; the
	// request seam intentionally owns only queue and stream-guard traffic.
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
	if !snapshot.Connected || snapshot.ConnectionGeneration != generation {
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
			script.playingOnce.Do(func() { close(script.playing) })
			status := native.MacroStatus{
				ID: 7, AcceptedSteps: 1, AcceptedBytes: 6,
				TotalSteps: 1, StartedAtUS: 1000,
			}
			if script.queryCount == 1 {
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
	payload := make([]byte, 18)
	payload[0] = native.EventMacro
	payload[1] = status.State
	payload[2] = status.ID
	binary.LittleEndian.PutUint16(payload[3:5], status.AcceptedSteps)
	binary.LittleEndian.PutUint16(payload[5:7], status.ExecutedSteps)
	binary.LittleEndian.PutUint16(payload[7:9], status.AcceptedBytes)
	payload[9] = status.Fill
	payload[10] = status.Underruns
	payload[11] = status.DispatchErrors
	binary.LittleEndian.PutUint32(payload[12:16], status.StartedAtUS)
	binary.LittleEndian.PutUint16(payload[16:18], status.TotalSteps)
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
		t.Fatalf("stream guard order settings=%d disable=%d run=%d restore=%d requests=%#v", settingsIndex, disableIndex, runIndex, restoreIndex, requests)
	}
}

func TestMacroPlaybackRestoresPeriodicStreamWhenRunFails(t *testing.T) {
	_, runner, script := newMacroPlaybackScriptRunner(t)
	script.failRun = true
	if _, err := runner.Start(context.Background(), "demo"); err == nil ||
		!strings.Contains(err.Error(), "forced RUN failure") {
		t.Fatalf("RUN failure err=%v", err)
	}
	if state := runner.State(); state.Lifecycle != "failed" {
		t.Fatalf("RUN failure state=%#v", state)
	}
	if values := macroStreamValues(script.requestSnapshot()); len(values) != 2 || values[0] != 0 || values[1] != 500 {
		t.Fatalf("RUN failure stream sequence=%v", values)
	}
}

func TestMacroPlaybackRestoresPeriodicStreamWhenCancelled(t *testing.T) {
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
	if state := runner.State(); state.Lifecycle != "cancelled" {
		t.Fatalf("cancel state=%#v", state)
	}
	if values := macroStreamValues(script.requestSnapshot()); len(values) != 2 || values[0] != 0 || values[1] != 500 {
		t.Fatalf("cancel stream sequence=%v", values)
	}
}

func TestMacroStreamLeaseNeverRestoresOnReplacementBoard(t *testing.T) {
	runtime, runner, script := newMacroPlaybackScriptRunner(t)
	snapshot := runtime.Snapshot()
	lease, err := runner.pauseMacroStream(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	runtime.mu.Lock()
	runtime.port.SerialNumber = "replacement-board"
	runtime.mu.Unlock()
	if err := runner.restoreMacroStream(lease); err != nil {
		t.Fatal(err)
	}
	if values := macroStreamValues(script.requestSnapshot()); len(values) != 1 || values[0] != 0 {
		t.Fatalf("replacement board received restoration: %v", values)
	}
}
