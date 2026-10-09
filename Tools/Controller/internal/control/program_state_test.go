package control

import "testing"

func TestProgramStateLeaseDoesNotClearExplicitOwner(t *testing.T) {
	manager := NewProgramStateManager(nil)
	if _, err := manager.Set("consumer", ProgramRunning, "manual run"); err != nil {
		t.Fatal(err)
	}
	lease, state, err := manager.Acquire("macro:7", "playing demo")
	if err != nil || state.Mode != ProgramRunning || len(state.Owners) != 2 {
		t.Fatalf("acquire state=%#v err=%v", state, err)
	}
	lease.Release()
	state = manager.Snapshot()
	if state.Mode != ProgramRunning || len(state.Owners) != 1 || state.Owners[0].Name != "consumer" {
		t.Fatalf("macro release cleared explicit state: %#v", state)
	}
	if _, err := manager.Set("consumer", ProgramIdle, ""); err != nil {
		t.Fatal(err)
	}
	if state = manager.Snapshot(); state.Mode != ProgramIdle || len(state.Owners) != 0 {
		t.Fatalf("idle state=%#v", state)
	}
}

func TestProgramStateLeaseReleaseIsIdempotentAndRefcounted(t *testing.T) {
	manager := NewProgramStateManager(nil)
	one, _, _ := manager.Acquire("automation", "one")
	two, _, _ := manager.Acquire("automation", "two")
	one.Release()
	one.Release()
	state := manager.Snapshot()
	if state.Mode != ProgramRunning || len(state.Owners) != 1 || state.Owners[0].References != 1 {
		t.Fatalf("refcount after first release=%#v", state)
	}
	two.Release()
	if manager.Snapshot().Mode != ProgramIdle {
		t.Fatal("final release did not restore Idle")
	}
}
