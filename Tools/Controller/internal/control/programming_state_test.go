package control

import (
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"testing"
)

func TestProgrammingLatchKnowledgeSurvivesOnlyIntentionalHandoff(t *testing.T) {
	runtime := &Runtime{port: ports.Info{Name: "fixture", SerialNumber: "board-a"}, generation: 7}
	runtime.observeProgrammingSettingsLocked(native.Settings{Flags: native.SettingsProgrammingMode})
	first := runtime.programming
	if !first.LatchKnown || !first.ProgrammingLatch || first.Revision == 0 || first.ConnectionGeneration != 7 {
		t.Fatalf("missing verified latch: %+v", first)
	}
	runtime.observeProgrammingSettingsLocked(native.Settings{Flags: native.SettingsProgrammingMode})
	if runtime.programming != first {
		t.Fatal("unchanged reads must not churn revision/timestamp")
	}
	runtime.programmingPhaseLocked("latched-safe")
	runtime.programming.IntentionalUARTRelease = true
	runtime.clearPeerStateLocked()
	if runtime.haveSettings || !runtime.programming.LatchKnown || !runtime.programming.ProgrammingLatch {
		t.Fatal("intentional UART handoff lost the verified latch")
	}
	runtime.programming.IntentionalUARTRelease = false
	runtime.clearPeerStateLocked()
	if runtime.programming.LatchKnown || !runtime.programming.ProgrammingLatch {
		t.Fatal("real loss must retain history but mark latch unknown")
	}
}

func TestProgrammingCompletionRequiresExplicitLifecycleCompletion(t *testing.T) {
	runtime := &Runtime{}
	runtime.programmingPhaseLocked("captured")
	if !runtime.programming.Active || !runtime.programming.RestorationPending || runtime.programming.LatchKnown {
		t.Fatal("preparation is not proof the latch was armed")
	}
	runtime.programmingPhaseLocked("latch-cleared")
	if !runtime.programming.Active || !runtime.programming.RestorationPending {
		t.Fatal("cleared latch is not completed restoration")
	}
	runtime.programmingPhaseLocked("restore-failed-safe")
	if runtime.programming.Active || !runtime.programming.RestorationPending {
		t.Fatal("failed restoration must remain recoverable, not active/complete")
	}
	revision := runtime.programming.Revision
	runtime.programmingPhaseLocked("complete")
	if runtime.programming.Active || runtime.programming.RestorationPending || runtime.programming.IntentionalUARTRelease || runtime.programming.Revision <= revision {
		t.Fatal("completion not monotonic")
	}
}
