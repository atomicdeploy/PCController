package control

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

func (runner *MacroRunner) UpdateProfile(reference, name, category, color string) (appconfig.Macro, error) {
	macro, err := runner.find(reference)
	if err != nil {
		return macro, err
	}
	if runner.updateHostConfig == nil {
		return macro, errors.New("macro persistence is unavailable")
	}
	if state := runner.State(); state.Running && state.ID == macro.ID {
		return macro, errors.New("cancel playback before editing this macro")
	}
	macro.Name, macro.Category, macro.Color = strings.TrimSpace(name), strings.TrimSpace(category), normalizedMacroColor(color)
	err = runner.updateHostConfig(func(config *appconfig.Config) error {
		candidate := *config
		candidate.Macros = append([]appconfig.Macro(nil), config.Macros...)
		for index, item := range candidate.Macros {
			if item.ID != macro.ID {
				continue
			}
			candidate.Macros[index] = macro
			if err := candidate.Validate(); err != nil {
				return err
			}
			config.Macros = candidate.Macros
			return nil
		}
		return errors.New("macro disappeared before update")
	})
	if err == nil {
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.library", Lifecycle: "updated", Text: "macro metadata updated"})
	}
	return macro, err
}

// A complete applied mask is one scheduled command, not eight serial requests.
// The firmware owns output interlocks and off-before-on transitions.
func relayMaskSteps(at uint32, previous, mask byte, initial bool) []appconfig.MacroStep {
	if !initial && mask == previous {
		return nil
	}
	return []appconfig.MacroStep{{AtUS: at, Kind: "relay-mask", Value: uint16(mask)}}
}

// Called with recordMu held. MCU output timestamps preserve relay intervals
// even when USB delivery is delayed or several edges arrive together.
func (runner *MacroRunner) captureRelayEdge(evidence CommandEvidence) {
	if !evidence.Timed {
		runner.recording.LastError = "relay edge has no MCU timestamp"
		return
	}
	if runner.recording.BoardOwned {
		runner.recording.Steps++
		runner.runtime.PublishStructuredEvent(Event{Kind: "macro.recording", Lifecycle: "captured", Text: "board relay edge captured"})
		return
	}
	if !runner.recordRelayClock {
		runner.recordRelayOriginUS = evidence.DeviceMicros
		runner.recordRelayOriginAt = 0
		if runner.recordMacro.Mode == macroModeMCU {
			if !runner.recordHasBase {
				runner.recordBaseUS = evidence.DeviceMicros
				runner.recordHasBase = true
			}
			runner.recordRelayOriginUS = runner.recordBaseUS
		} else if !runner.recordBaseAt.IsZero() && evidence.ObservedAt.After(runner.recordBaseAt) {
			runner.recordRelayOriginAt = uint32(evidence.ObservedAt.Sub(runner.recordBaseAt) / time.Microsecond)
		}
		runner.recordRelayClock = true
	}
	elapsed := evidence.DeviceMicros - runner.recordRelayOriginUS
	if elapsed > 0x7fffffff {
		runner.recording.LastError = "recording exceeded the MCU signed timing window"
		return
	}
	at := runner.recordRelayOriginAt + elapsed
	if at < runner.recordRelayOriginAt {
		runner.recording.LastError = "recording timing overflow"
		return
	}
	if runner.recordBaseAt.IsZero() {
		runner.recordBaseAt = evidence.ObservedAt
	}
	steps := relayMaskSteps(at, runner.recordRelayMask, evidence.RelayMask, !runner.recordRelaySeen)
	if len(runner.recordMacro.Steps)+len(steps) > 65535 {
		runner.recording.LastError = "recording reached the step limit"
		return
	}
	runner.recordRelaySeen = true
	runner.recordRelayMask = evidence.RelayMask
	runner.recordMacro.Steps = append(runner.recordMacro.Steps, steps...)
	runner.recording.Steps = len(runner.recordMacro.Steps)
	if len(steps) > 0 {
		runner.publishRecordedStep(steps[len(steps)-1])
	}
}

// StartBoardRecording uses the firmware-owned circular RAM ring. The board
// continues capturing when the host disconnects; stopping imports a named copy.
func (runner *MacroRunner) StartBoardRecording(ctx context.Context, name, category, color string) (MacroRecordingState, error) {
	if !runner.runtime.Snapshot().Connected {
		return MacroRecordingState{}, errors.New("device is not connected")
	}
	state, err := runner.startRecording(name, category, color, macroModeMCU)
	if err != nil {
		return state, err
	}
	runner.recordMu.Lock()
	runner.recording.BoardOwned = true
	runner.recordMu.Unlock()
	_, err = runner.request(ctx, native.OpMacroStep, []byte{3, state.ID}, native.OpACK)
	if err != nil {
		runner.recordMu.Lock()
		runner.recording.BoardOwned = false
		runner.recordMu.Unlock()
		_, _ = runner.StopRecording(false)
		return MacroRecordingState{}, err
	}
	return runner.RecordingState(), nil
}

// ImportBoardRecording recovers retained board RAM after a host restart without
// issuing RECORD BEGIN (which would replace the retained take).
func (runner *MacroRunner) ImportBoardRecording(name, category, color string) (appconfig.Macro, error) {
	status, err := runner.queryBoard(context.Background())
	if err != nil {
		return appconfig.Macro{}, err
	}
	if status.State != native.MacroRecorded && status.State != native.MacroRecording {
		return appconfig.Macro{}, errors.New("board has no retained recording")
	}
	if _, err := runner.startRecording(name, category, color, macroModeMCU); err != nil {
		return appconfig.Macro{}, err
	}
	runner.recordMu.Lock()
	runner.recording.BoardOwned = true
	runner.recordMu.Unlock()
	return runner.StopRecording(true)
}

func (runner *MacroRunner) collectBoardRecording(ctx context.Context, save bool) error {
	if _, err := runner.request(ctx, native.OpMacroStep, []byte{4}, native.OpACK); err != nil {
		return err
	}
	if !save {
		return nil
	}
	status, err := runner.queryBoard(ctx)
	if err != nil {
		return err
	}
	if status.TotalSteps == 0 || status.TotalSteps > 25 {
		return fmt.Errorf("invalid board capture snapshot count %d", status.TotalSteps)
	}
	raw := make([]byte, 0, int(status.TotalSteps)*5)
	for len(raw) < int(status.TotalSteps)*5 {
		offset := len(raw)
		frame, err := runner.request(ctx, native.OpMacroStep, []byte{5, byte(offset), byte(offset >> 8)}, native.OpMacroStatus)
		if err != nil {
			return err
		}
		p := frame.Payload
		if len(p) <= 4 || p[0] != native.EventMacro || p[1] != 0x80 || int(binary.LittleEndian.Uint16(p[2:4])) != offset || len(p[4:])%5 != 0 || len(raw)+len(p[4:]) > int(status.TotalSteps)*5 {
			return errors.New("invalid board capture chunk")
		}
		raw = append(raw, p[4:]...)
	}
	steps, err := decodeRelayCapture(raw)
	if err != nil {
		return err
	}
	runner.recordMu.Lock()
	runner.recordMacro.Steps = steps
	runner.recording.Steps = len(steps)
	runner.recording.Overwritten = int(status.Underruns)
	runner.recordMu.Unlock()
	return nil
}

func decodeRelayCapture(raw []byte) ([]appconfig.MacroStep, error) {
	if len(raw) == 0 || len(raw)%5 != 0 {
		return nil, errors.New("invalid relay capture length")
	}
	var result []appconfig.MacroStep
	var previous byte
	var last uint32
	first := binary.LittleEndian.Uint32(raw[:4])
	for offset := 0; offset < len(raw); offset += 5 {
		original := binary.LittleEndian.Uint32(raw[offset : offset+4])
		at := original - first
		mask := raw[offset+4]
		if original > 0x7fffffff || original < first || at < last || at > 0x7fffffff {
			return nil, errors.New("invalid relay capture timing")
		}
		result = append(result, relayMaskSteps(at, previous, mask, offset == 0)...)
		previous, last = mask, at
	}
	return result, nil
}
