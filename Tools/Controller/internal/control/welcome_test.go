package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

type welcomeTarget struct {
	recordingOutputTarget
	liveMu sync.Mutex
	live   Snapshot
}

func (t *welcomeTarget) Snapshot() Snapshot { t.liveMu.Lock(); defer t.liveMu.Unlock(); return t.live }
func (t *welcomeTarget) change(f func(*Snapshot)) {
	t.liveMu.Lock()
	defer t.liveMu.Unlock()
	f(&t.live)
}
func newWelcomeTarget() *welcomeTarget {
	return &welcomeTarget{live: Snapshot{Connected: true, HaveStatus: true, HaveSettings: true, ConnectionGeneration: 1,
		Hello: native.Hello{Capabilities: native.CapabilityBuzzerBusy | native.CapabilityAddressableLED}}}
}
func waitWelcomeCommands(t *testing.T, target *welcomeTarget, count int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for len(target.snapshot()) < count && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if len(target.snapshot()) < count {
		t.Fatal("welcome did not dispatch")
	}
}
func waitWelcomeEnd(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("welcome did not finish")
		return nil
	}
}

func TestWelcomeFiniteRainbowAndFanfare(t *testing.T) {
	target := newWelcomeTarget()
	s := NewOutputScheduler(target)
	defer s.Close()
	if err := s.RunWelcome(context.Background(), appconfig.DefaultWelcomeMelody()); err != nil {
		t.Fatal(err)
	}
	commands := target.snapshot()
	notes, commits := 0, 0
	for _, cmd := range commands {
		if cmd.opcode == native.OpBuzzer && (cmd.payload[0] != 0 || cmd.payload[1] != 0) {
			notes++
		}
		if cmd.opcode == native.OpAddressableLED && cmd.payload[0] == 0xfc {
			commits++
		}
		if cmd.opcode != native.OpBuzzer && cmd.opcode != native.OpAddressableLED {
			t.Fatalf("unexpected output opcode %x", cmd.opcode)
		}
	}
	if notes != 10 || commits < 5 {
		t.Fatalf("notes=%d rainbow frames=%d", notes, commits)
	}
	last := commands[len(commands)-1]
	if last.opcode != native.OpAddressableLED || last.payload[0] != native.AddressableLEDFill || last.payload[1] != 0 {
		t.Fatal("strip not cleared")
	}
	if state := s.State(); state.MelodyID != 0 || state.StripID != 0 {
		t.Fatal("welcome retained lanes")
	}
}

func TestWelcomeMuteCancellationAndMissingStrip(t *testing.T) {
	for _, silent := range []bool{false, true} {
		t.Run(map[bool]string{false: "audio-only", true: "silent-rainbow"}[silent], func(t *testing.T) {
			target := newWelcomeTarget()
			if silent {
				target.live.Settings.Flags = native.SettingsSilent
			} else {
				target.live.Hello.Capabilities = native.CapabilityBuzzerBusy
			}
			s := NewOutputScheduler(target)
			defer s.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
			defer cancel()
			err := s.RunWelcome(ctx, appconfig.DefaultWelcomeMelody())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			for _, cmd := range target.snapshot() {
				if silent && cmd.opcode == native.OpBuzzer {
					t.Fatal("silent board received audio")
				}
				if !silent && cmd.opcode == native.OpAddressableLED {
					t.Fatal("unsupported strip received commands")
				}
			}
		})
	}
}

func TestWelcomeSkipsBusyUnsafeAndUnknownBoards(t *testing.T) {
	for _, change := range []func(*Snapshot){
		func(s *Snapshot) { s.Connected = false }, func(s *Snapshot) { s.EmergencyStop.Active = true },
		func(s *Snapshot) { s.Programming.Active = true }, func(s *Snapshot) { s.Status.Hot = true },
		func(s *Snapshot) { s.Status.Flags = native.StatusBuzzerBusy }, func(s *Snapshot) { s.Hello.Capabilities = 0 },
		func(s *Snapshot) { s.ProgramState.Mode = ProgramRunning },
	} {
		target := newWelcomeTarget()
		change(&target.live)
		s := NewOutputScheduler(target)
		if err := s.RunWelcome(context.Background(), appconfig.DefaultWelcomeMelody()); err != ErrWelcomeSkipped {
			t.Fatal(err)
		}
		if len(target.snapshot()) != 0 {
			t.Fatal("unsafe startup wrote hardware")
		}
		s.Close()
	}
	target := newWelcomeTarget()
	s := NewOutputScheduler(target)
	defer s.Close()
	op, err := s.StartMelody(context.Background(), appconfig.DefaultWelcomeMelody(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RunWelcome(context.Background(), appconfig.DefaultWelcomeMelody()); err != ErrWelcomeSkipped {
		t.Fatal(err)
	}
	if s.State().MelodyID != op.ID {
		t.Fatal("welcome replaced user melody")
	}
}

func TestWelcomeReplacementAndReconnectCannotClearSuccessor(t *testing.T) {
	for _, reconnect := range []bool{false, true} {
		target := newWelcomeTarget()
		s := NewOutputScheduler(target)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.RunWelcome(ctx, appconfig.DefaultWelcomeMelody()) }()
		waitWelcomeCommands(t, target, 5)
		if reconnect {
			target.change(func(snapshot *Snapshot) { snapshot.ConnectionGeneration++ })
			cancel()
			_ = waitWelcomeEnd(t, done)
			for _, cmd := range target.snapshot() {
				if cmd.opcode == native.OpAddressableLED && cmd.payload[0] == native.AddressableLEDFill {
					t.Fatal("old welcome cleared new board")
				}
			}
		} else {
			if _, err := stripStreamCommand(context.Background(), s, []string{"frame", "123456"}); err != nil {
				t.Fatal(err)
			}
			_ = waitWelcomeEnd(t, done)
			commands := target.snapshot()
			for _, cmd := range commands {
				if cmd.opcode == native.OpAddressableLED && cmd.payload[0] == native.AddressableLEDFill {
					t.Fatal("old welcome cleared replacement frame")
				}
			}
		}
		cancel()
		s.Close()
	}
}

func TestWelcomeFailuresReleaseLanesWithoutClearingRetainedWorkspace(t *testing.T) {
	for _, failAt := range []int{1, 2, 3} {
		target := newWelcomeTarget()
		failure := errors.New("welcome command ACK failed")
		target.failCommands = map[int]error{failAt: failure}
		s := NewOutputScheduler(target)
		if err := s.RunWelcome(context.Background(), appconfig.DefaultWelcomeMelody()); !errors.Is(err, failure) {
			t.Fatalf("command %d: %v", failAt, err)
		}
		commands := target.snapshot()
		if failAt == 1 && len(commands) != 1 {
			t.Fatal("rejected strip configuration must not clear retained workspace or start notes")
		}
		if state := s.State(); state.MelodyID != 0 || state.StripID != 0 {
			t.Fatal("failure retained welcome lanes")
		}
		s.Close()
	}
	target := newWelcomeTarget()
	s := NewOutputScheduler(target)
	defer s.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RunWelcome(ctx, appconfig.DefaultWelcomeMelody()); err != context.Canceled || len(target.snapshot()) != 0 {
		t.Fatal("skipped queued welcome dispatched output")
	}
}
