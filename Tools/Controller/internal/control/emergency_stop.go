package control

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

// EmergencyStopState is the host-authoritative, transport-neutral motion and
// effect interlock. Unlocking removes the command block but never resumes work
// that was cancelled when the latch engaged.
type EmergencyStopState struct {
	Active    bool      `json:"active"`
	Revision  uint64    `json:"revision"`
	Source    string    `json:"source,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	ChangedAt time.Time `json:"changed_at"`
}

var ErrEmergencyStopActive = errors.New("E-STOP is active; unlock it before starting effects or energizing motion/output channels")

func (runtime *Runtime) EmergencyStop() EmergencyStopState {
	runtime.emergencyStopStateMu.RLock()
	defer runtime.emergencyStopStateMu.RUnlock()
	return runtime.emergencyStopState
}

// SetEmergencyStop atomically engages or releases the host interlock. Engage
// first closes admission, then cancels every host/MCU effect source and finally
// reasserts all motion/output-off commands. Release only opens admission.
func (runtime *Runtime) SetEmergencyStop(ctx context.Context, active bool, source, reason string) (EmergencyStopState, error) {
	runtime.emergencyStopOperationMu.Lock()
	defer runtime.emergencyStopOperationMu.Unlock()

	source = strings.TrimSpace(source)
	if source == "" {
		source = "host"
	}
	reason = strings.TrimSpace(reason)
	previous := runtime.EmergencyStop()
	if previous.Active == active {
		return previous, nil
	}

	runtime.emergencyStop.Store(active)
	runtime.emergencyStopStateMu.Lock()
	next := EmergencyStopState{
		Active: active, Revision: previous.Revision + 1,
		Source: source, Reason: reason, ChangedAt: time.Now(),
	}
	runtime.emergencyStopState = next
	runtime.emergencyStopStateMu.Unlock()

	if !active {
		runtime.publishEmergencyStop(next, "released", nil)
		return next, nil
	}

	var stopErrors []error
	runtime.stopMediaTimeline()
	if runner := runtime.MacroRunner(); runner != nil {
		if runner.RecordingState().Active {
			if _, err := runner.StopRecording(false); err != nil {
				stopErrors = append(stopErrors, fmt.Errorf("discard active effect recording: %w", err))
			}
		}
		if err := runner.Cancel(ctx); err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("cancel effect playback: %w", err))
		}
	}
	if outputs := runtime.EnsureOutputScheduler(); outputs != nil {
		outputs.StopAll()
	}

	stopErrors = append(stopErrors, runtime.applyEmergencyStopTerminalState(ctx)...)
	stopErr := errors.Join(stopErrors...)
	runtime.publishEmergencyStop(next, "engaged", stopErr)
	return next, stopErr
}

// reassertEmergencyStop runs after an authenticated reconnect because an MCU
// macro can outlive a lost host transport. It does not create a new revision.
func (runtime *Runtime) reassertEmergencyStop() {
	if !runtime.emergencyStop.Load() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	err := errors.Join(runtime.applyEmergencyStopTerminalState(ctx)...)
	runtime.publishEmergencyStop(runtime.EmergencyStop(), "reasserted", err)
}

func (runtime *Runtime) applyEmergencyStopTerminalState(ctx context.Context) []error {
	var stopErrors []error
	// Repeat the board-safe terminal state after cancellation. This defeats a
	// final in-flight macro/stream frame without ever energizing an output.
	for _, stop := range []struct {
		name    string
		opcode  byte
		payload []byte
	}{
		{"cancel MCU effect playback", native.OpMacroCancel, native.MacroQueueCancelPayload(false)},
		{"stop relay test", native.OpRelayTest, []byte{0, 0}},
		{"stop left motion", native.OpRelaySide, []byte{0, 0}},
		{"stop right motion", native.OpRelaySide, []byte{1, 0}},
		{"release every relay", native.OpRelayAllOff, nil},
		{"release every PWM output", native.OpPWMAllOff, nil},
		{"clear addressable strip", native.OpAddressableLED, []byte{native.AddressableLEDFill, 0, 0, 0, 0xFF}},
	} {
		requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
		err := runtime.Command(requestContext, stop.opcode, stop.payload)
		cancel()
		if err != nil {
			stopErrors = append(stopErrors, fmt.Errorf("%s: %w", stop.name, err))
		}
	}
	return stopErrors
}

func (runtime *Runtime) publishEmergencyStop(state EmergencyStopState, lifecycle string, err error) {
	reason := state.Reason
	if err != nil {
		if reason != "" {
			reason += "; "
		}
		reason += err.Error()
	}
	runtime.PublishStructuredEvent(Event{
		Kind: "emergency_stop", Lifecycle: lifecycle,
		State: fmt.Sprintf("%t", state.Active), Source: state.Source,
		Reason: reason, Severity: map[bool]string{true: "critical", false: "info"}[state.Active],
		Text:     map[bool]string{true: "E-STOP engaged; effects and motion are locked", false: "E-STOP released; commands may be started normally"}[state.Active],
		Metadata: map[string]string{"revision": fmt.Sprintf("%d", state.Revision)},
	})
}

func emergencyStopAllows(opcode byte, payload []byte) bool {
	switch opcode {
	case native.OpRelayAllOff, native.OpPWMAllOff, native.OpGetStatus,
		native.OpPWMGet, native.OpMacroStatus, native.OpGetSettings,
		native.OpHello, native.OpTemperatureList, native.OpFrontPanelGet:
		return true
	case native.OpRelaySet:
		return len(payload) == 2 && payload[1] == 0
	case native.OpRelaySide:
		return len(payload) == 2 && payload[1] == 0
	case native.OpRelayTest:
		return len(payload) == 2 && binary.LittleEndian.Uint16(payload) == 0
	case native.OpPWMSet:
		return len(payload) == 3 && binary.LittleEndian.Uint16(payload[1:]) == 0
	case native.OpMacroCancel:
		return len(payload) == 1 && payload[0] == 0
	case native.OpMacroStep:
		return bytes.Equal(payload, native.MacroQueueQueryPayload())
	case native.OpProgramState:
		return len(payload) == 1 && payload[0] == native.ProgramStateIdle
	case native.OpMenuAction, native.OpRemoteKeyGesture:
		// Physical front-panel actions and remote-key gestures can be mapped to
		// motion or an effect by the active board profile.  Their meaning is not
		// knowable at this transport boundary, so fail closed while latched.
		return false
	case native.OpAddressableLED:
		return len(payload) == 5 && payload[0] == native.AddressableLEDFill && payload[1] == 0 && payload[2] == 0 && payload[3] == 0
	case native.OpStatusEffect:
		return bytes.Equal(payload, native.StatusEffectReleasePayload())
	default:
		return opcode != native.OpMacroStart
	}
}

func (runtime *Runtime) rejectEmergencyStopCommand(opcode byte, payload []byte) error {
	if !runtime.emergencyStop.Load() || emergencyStopAllows(opcode, payload) {
		return nil
	}
	return fmt.Errorf("%w (opcode %s/0x%02X)", ErrEmergencyStopActive, native.OpcodeName(opcode), opcode)
}
