package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"testing"
	"time"
)

func TestRecordingStateCapabilitiesCannotMutateActiveTake(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	started, err := runner.StartRecording("snapshot", "tests", "green", CaptureSelection{ControlKeys: []string{"relay.5"}})
	if err != nil {
		t.Fatal(err)
	}
	state := runner.RecordingState()
	if len(state.Capabilities.Controls) == 0 || len(state.Capabilities.Opcodes) == 0 {
		t.Fatal("recording capabilities missing")
	}
	want := state.Capabilities.clone()
	started.Capabilities.Controls[0].Name = "mutated-start"
	started.Capabilities.Opcodes[0].CaptureModes[0] = "mutated-start"
	started.CaptureSelection.ControlKeys[0] = "mutated-start"
	state.Capabilities.Controls[0].Name = "mutated"
	for i := range state.Capabilities.Controls {
		if len(state.Capabilities.Controls[i].Actions) > 0 {
			state.Capabilities.Controls[i].Actions[0] = appconfig.ActionDescriptor{}
		}
	}
	state.Capabilities.Opcodes[0].Name = "mutated"
	state.Capabilities.Opcodes[0].CaptureModes[0] = "mutated"
	gotJSON, _ := json.Marshal(runner.RecordingState().Capabilities)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatal("caller mutated capability state of active recording")
	}
	if runner.RecordingState().CaptureSelection.ControlKeys[0] != "relay.5" {
		t.Fatal("start response mutated active capture selection")
	}
	_, _ = runner.StopRecording(false)
}

func TestRecordingSelectionChangeBroadcastsAuthoritativeRefreshEvent(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("selection", "tests", "green"); err != nil {
		t.Fatal(err)
	}
	after := runner.runtime.LatestEventID()
	selection, _ := json.Marshal(CaptureSelection{ControlKeys: []string{"relay.5"}})
	if _, err := runner.recordingJSONCommand(context.Background(), "selection", hex.EncodeToString(selection)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	event, err := runner.runtime.WaitEvent(ctx, after, "macro.recording")
	if err != nil || event.Lifecycle != "selection-changed" || event.State != "recording" {
		t.Fatalf("selection change not broadcast: %+v %v", event, err)
	}
	if runner.RecordingState().CaptureSelection.ControlKeys[0] != "relay.5" {
		t.Fatal("refresh event did not refer to updated recorder state")
	}
	_, _ = runner.StopRecording(false)
}

func TestCaptureControlsOnlyAdvertiseImplementedSemanticMappings(t *testing.T) {
	controls := []appconfig.ControlDescriptor{
		{Key: "seat.a", Kind: "seat"}, {Key: "relay.5", Kind: "relay"}, {Key: "pwm.2", Kind: "mosfet"},
		{Key: "rgb", Kind: "rgb"}, {Key: "display", Kind: "display"}, {Key: "strip", Kind: "strip"},
		{Key: "buzzer", Kind: "buzzer"}, {Key: "relay.9", Kind: "relay"}, {Key: "pwm.16", Kind: "mosfet"},
		{Key: "pwm.02", Kind: "mosfet"}, {Key: "seat.c", Kind: "seat"},
	}
	got := selectableCaptureControls(controls)
	if len(got) != 3 || got[0].Key != "seat.a" || got[1].Key != "relay.5" || got[2].Key != "pwm.2" {
		t.Fatalf("unsupported capture controls advertised: %+v", got)
	}
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	for _, key := range []string{"rgb", "display", "strip", "buzzer", "pwm.16", "relay.9"} {
		if _, err := runner.validateCaptureSelection(CaptureSelection{ControlKeys: []string{key}}, macroModeAuto, false); err == nil {
			t.Fatalf("unsupported semantic selection %s accepted", key)
		}
	}
	_, advertised := appconfig.ProfileDescriptors("", false, nil, nil)
	if len(selectableCaptureControls(advertised)) != len(advertised) {
		t.Fatal("current profile contained an unimplemented capture control")
	}
}

func TestWholeEffectRepeatPreservesMixedSequenceOrderForBothExecutors(t *testing.T) {
	count := uint16(3)
	macro := appconfig.Macro{ID: 9, Name: "repeat", RepeatCount: &count, Steps: []appconfig.MacroStep{
		{Kind: "relay", Target: 4, Value: 1, DurationMS: 100},
		{AtUS: 150000, Kind: "pwm", Target: 2, Value: 30, DurationMS: 50},
	}}
	for _, mode := range []string{macroModeHost, macroModeMCU} {
		macro.Mode = mode
		compiled, err := compileMacro(macro)
		if err != nil || len(compiled.steps) != 9 {
			t.Fatalf("%s: %v %+v", mode, err, compiled.steps)
		}
		want := []uint32{0, 100000, 150000, 200000, 300000, 350000, 400000, 500000, 550000}
		for i, step := range compiled.steps {
			if step.dueUS != want[i] {
				t.Fatalf("%s step%d due%d want%d", mode, i, step.dueUS, want[i])
			}
		}
	}
	macro.RepeatIntervalMS = 199
	if _, err := compileMacro(macro); err == nil {
		t.Fatal("overlapping whole effects accepted")
	}
	zero := uint16(0)
	macro.RepeatCount = &zero
	if _, err := compileMacro(macro); err == nil {
		t.Fatal("explicit zero count accepted")
	}
	count = 1000
	macro.RepeatCount = &count
	macro.RepeatIntervalMS = 3600000
	if _, err := compileMacro(macro); err == nil {
		t.Fatal("repeat total overflow accepted")
	}
	macro.RepeatIntervalMS = 0
	macro.Steps = make([]appconfig.MacroStep, 66)
	for i := range macro.Steps {
		macro.Steps[i] = appconfig.MacroStep{AtUS: 1000, Kind: "relay", Target: 4, Value: 0}
	}
	if _, err := compileMacro(macro); err == nil {
		t.Fatal("repeated command capacity accepted")
	}
}

func TestCaptureSelectionDefaultsNoneAndTargetsOnlySelectedRelayAndPWM(t *testing.T) {
	config := appconfig.Defaults()
	config.Connection.LastDevice = &appconfig.DeviceIdentity{Port: "fixture", SerialNumber: "selection"}
	config.BoardProfiles = map[string]appconfig.BoardProfile{"serial:selection": {Key: "selection", Mode: appconfig.BoardModeCinemaSeatMotion}}
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("none", "tests", "green"); err != nil {
		t.Fatal(err)
	}
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{2, 1, 0}, ObservedAt: time.Now()})
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 0xff, Timed: true, DeviceMicros: 100})
	if len(runner.RecordingState().Preview) != 0 {
		t.Fatal("omitted selection defaulted to all")
	}
	if _, err := runner.StopRecording(false); err != nil {
		t.Fatal(err)
	}
	selection := CaptureSelection{ControlKeys: []string{"seat.a", "relay.5", "pwm.2"}}
	if _, err := runner.StartRecording("selected", "tests", "green", selection); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 0xff, Timed: true, DeviceMicros: 100, ObservedAt: base})
	runner.captureCommand(CommandEvidence{RelayEdge: true, RelayMask: 0, Timed: true, DeviceMicros: 1100, ObservedAt: base.Add(time.Millisecond)})
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{3, 4, 0}, ObservedAt: base.Add(2 * time.Millisecond)})
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMAllOff, ObservedAt: base.Add(3 * time.Millisecond)})
	state := runner.RecordingState()
	if len(state.Preview) != 5 || state.Preview[2].AtUS != 1000 || state.Preview[4].Kind != "pwm" || state.Preview[4].Target != 2 {
		t.Fatalf("partial take: %+v", state.Preview)
	}
	for _, step := range state.Preview {
		op, payload, err := compileMacroCommand(step)
		if err != nil {
			t.Fatal(err)
		}
		if op == native.OpRelaySet && (len(payload) != 2 || payload[0] != 4) {
			t.Fatalf("excluded relays overwritten: %+v", step)
		}
		if op == native.OpRelaySide && payload[0] != 0 {
			t.Fatal("excluded seat overwritten")
		}
	}
	state.CaptureSelection.ControlKeys[0] = "mutated"
	if runner.RecordingState().CaptureSelection.ControlKeys[0] == "mutated" {
		t.Fatal("mutable selection snapshot leaked")
	}
}

func TestCaptureSelectionUpfrontValidationAndORRawOpcode(t *testing.T) {
	config := appconfig.Defaults()
	runner := macroTestRunner(&config, nil)
	if _, err := runner.StartRecording("bad", "", "", CaptureSelection{ControlKeys: []string{"seat.nonexistent"}}); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := runner.validateCaptureSelection(CaptureSelection{Opcodes: []uint16{uint16(native.OpPWMSet)}}, macroModeMCU, true); err == nil {
		t.Fatal("nonrelay board-retained selection accepted")
	}
	selection := CaptureSelection{ControlKeys: []string{"pwm.2"}, Opcodes: []uint16{uint16(native.OpBuzzer)}}
	encodedSelection, _ := json.Marshal(selection)
	var wire struct {
		Opcodes []int `json:"opcodes"`
	}
	if err := json.Unmarshal(encodedSelection, &wire); err != nil || len(wire.Opcodes) != 1 || wire.Opcodes[0] != int(native.OpBuzzer) {
		t.Fatalf("opcodes must be a numeric JSON array: %s %v", encodedSelection, err)
	}
	if _, err := runner.validateCaptureSelection(CaptureSelection{Opcodes: []uint16{256}}, macroModeAuto, false); err == nil {
		t.Fatal("opcode overflow accepted")
	}
	if _, err := runner.validateCaptureSelection(CaptureSelection{Opcodes: []uint16{uint16(native.OpRelaySide)}}, macroModeAuto, false); err == nil {
		t.Fatal("unadvertised semantic motion opcode accepted")
	}
	if _, err := runner.StartRecording("or", "", "", selection); err != nil {
		t.Fatal(err)
	}
	runner.captureCommand(CommandEvidence{Opcode: native.OpBuzzer, Payload: native.BuzzerPayload(440, 25), ObservedAt: time.Now()})
	runner.captureCommand(CommandEvidence{Opcode: native.OpPWMSet, Payload: []byte{2, 1, 0}, ObservedAt: time.Now()})
	if len(runner.RecordingState().Preview) != 2 {
		t.Fatal("OR-selection did not capture both sources")
	}
	steps := selectedRelaySteps(100, 0, 0xff, true, appconfig.BoardModeCinemaSeatMotion, CaptureSelection{Opcodes: []uint16{uint16(native.OpRelaySet)}})
	if len(steps) != 1 || steps[0].Kind != "relay-mask" || steps[0].Value != 255 {
		t.Fatalf("explicit broad opcode lost exact mask: %+v", steps)
	}
	if steps = selectedRelaySteps(100, 0, 255, true, appconfig.BoardModeCinemaSeatMotion, CaptureSelection{Opcodes: []uint16{uint16(native.OpRelayAllOff)}}); len(steps) != 0 {
		t.Fatal("all-off selector captured an on edge")
	}
}

func TestRecordingJSONAppendInheritsMetadataAndExposesSelectionCapabilities(t *testing.T) {
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{{ID: 3, Name: "existing", Color: "green", Mode: "auto", Steps: []appconfig.MacroStep{{Kind: "pwm", Target: 2, Value: 1}}}}
	runner := macroTestRunner(&config, nil)
	request := RecordingRequest{Reference: "effect:3", CaptureMode: "automatic", CaptureSelection: CaptureSelection{ControlKeys: []string{"pwm.2"}}}
	data, _ := json.Marshal(request)
	response, err := runner.recordingJSONCommand(context.Background(), "append-json", hex.EncodeToString(data))
	if err != nil {
		t.Fatal(err)
	}
	var state MacroRecordingState
	if err = json.Unmarshal([]byte(response), &state); err != nil {
		t.Fatal(err)
	}
	if state.Name != "existing" || state.CaptureSelection.All || len(state.CaptureSelection.ControlKeys) != 1 || len(state.Capabilities.Opcodes) == 0 {
		t.Fatalf("contract: %+v", state)
	}
	request.Name = "overwrite"
	data, _ = json.Marshal(request)
	if _, err = runner.recordingJSONCommand(context.Background(), "append-json", hex.EncodeToString(data)); err == nil {
		t.Fatal("append overwrote metadata")
	}
	if err = decodeCaptureJSON(hex.EncodeToString([]byte(`{"all":true,"typo":1}`)), &CaptureSelection{}); err == nil {
		t.Fatal("unknown selector silently ignored")
	}
	var step appconfig.MacroStep
	if err = json.Unmarshal([]byte(`{"kind":"relay","repeat_count":2}`), &step); err == nil {
		t.Fatal("retired step repeat silently ignored")
	}
}
