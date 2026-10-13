package control

import (
	"pccontroller.local/controller/internal/native"
	"time"
)

// ProgrammingSnapshot is separate from application-UART connectivity. LatchKnown
// identifies a verified settings observation retained only for a deliberate
// handoff of this device, never a guessed cleared latch or physical presence.
type ProgrammingSnapshot struct {
	Active                 bool                      `json:"active"`
	Phase                  string                    `json:"phase"`
	RestorationPending     bool                      `json:"restoration_pending"`
	IntentionalUARTRelease bool                      `json:"intentional_uart_release"`
	LatchKnown             bool                      `json:"latch_known"`
	ProgrammingLatch       bool                      `json:"programming_latch"`
	Device                 ProgrammingDeviceIdentity `json:"device"`
	ConnectionGeneration   uint64                    `json:"connection_generation"`
	Revision               uint64                    `json:"revision"`
	UpdatedAt              time.Time                 `json:"updated_at"`
}

func (runtime *Runtime) touchProgrammingLocked(previous ProgrammingSnapshot) {
	current := runtime.programming
	current.Revision, current.UpdatedAt = previous.Revision, previous.UpdatedAt
	if current == previous {
		return
	}
	runtime.programming.Revision = previous.Revision + 1
	runtime.programming.UpdatedAt = time.Now().UTC()
}

func (runtime *Runtime) observeProgrammingSettingsLocked(settings native.Settings) {
	previous := runtime.programming
	runtime.programming.LatchKnown = true
	runtime.programming.ProgrammingLatch = settings.Flags&native.SettingsProgrammingMode != 0
	runtime.programming.Device = programmingIdentity(runtime.port)
	runtime.programming.ConnectionGeneration = runtime.generation
	runtime.touchProgrammingLocked(previous)
}

func (runtime *Runtime) programmingPhaseLocked(phase string) ProgrammingSnapshot {
	previous := runtime.programming
	runtime.programming.Phase = phase
	runtime.programming.RestorationPending = phase != "complete"
	runtime.programming.Active = phase != "complete" && phase != "restore-failed-safe" && phase != "host-failed" && phase != "host-complete"
	if runtime.session != nil {
		runtime.programming.Device = programmingIdentity(runtime.port)
		runtime.programming.ConnectionGeneration = runtime.generation
	}
	if phase == "complete" {
		runtime.programming.IntentionalUARTRelease = false
	}
	runtime.touchProgrammingLocked(previous)
	return runtime.programming
}

func (runtime *Runtime) publishProgrammingState(state ProgrammingSnapshot) {
	runtime.PublishStructuredEvent(Event{
		Kind: "programming.lifecycle", Stream: EventStreamState, Lifecycle: "changed",
		Programming: &state, Text: "programming phase: " + state.Phase,
	})
}
