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

type recordedOutputCommand struct {
	at      time.Time
	opcode  byte
	payload []byte
}

type recordingOutputTarget struct {
	mu       sync.Mutex
	commands []recordedOutputCommand
	events   []string
	failAt   int
	ackDelay time.Duration
}

func (target *recordingOutputTarget) Command(
	ctx context.Context,
	opcode byte,
	payload []byte,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	target.mu.Lock()
	target.commands = append(target.commands, recordedOutputCommand{
		at: time.Now(), opcode: opcode,
		payload: append([]byte(nil), payload...),
	})
	commandCount := len(target.commands)
	delay := target.ackDelay
	target.mu.Unlock()
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	if target.failAt != 0 && commandCount >= target.failAt {
		return errors.New("USB disconnected")
	}
	return nil
}

func (target *recordingOutputTarget) PublishHostEvent(_, text string) {
	target.mu.Lock()
	target.events = append(target.events, text)
	target.mu.Unlock()
}

func (target *recordingOutputTarget) snapshot() []recordedOutputCommand {
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]recordedOutputCommand(nil), target.commands...)
}

func TestMelodyStreamingWaitsBetweenAcknowledgedNotes(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "test",
			Notes: []appconfig.MelodyNote{
				{FrequencyHz: 440, DurationMS: 20, GapMS: 5},
				{FrequencyHz: 660, DurationMS: 10},
			},
		},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("melody did not complete")
	}
	commands := target.snapshot()
	if len(commands) != 2 {
		t.Fatalf("commands=%d, want 2", len(commands))
	}
	if commands[0].opcode != native.OpBuzzer ||
		commands[1].opcode != native.OpBuzzer {
		t.Fatalf("unexpected opcodes: %#v", commands)
	}
	if spacing := commands[1].at.Sub(commands[0].at); spacing < 20*time.Millisecond {
		t.Fatalf("notes streamed too quickly: %v", spacing)
	}
}

func TestMelodyStreamingDoesNotAddAcknowledgementLatencyToEveryNote(t *testing.T) {
	target := &recordingOutputTarget{ackDelay: 40 * time.Millisecond}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "paced",
			Notes: []appconfig.MelodyNote{
				{FrequencyHz: 440, DurationMS: 60, GapMS: 10},
				{FrequencyHz: 660, DurationMS: 10},
			},
		},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("melody did not complete")
	}
	commands := target.snapshot()
	if len(commands) != 2 {
		t.Fatalf("commands=%d, want 2", len(commands))
	}
	spacing := commands[1].at.Sub(commands[0].at)
	if spacing < 60*time.Millisecond || spacing > 90*time.Millisecond {
		t.Fatalf("ACK latency changed the 70ms source cadence: %v", spacing)
	}
}

func TestReplacingMelodyCancelsOldStreamWithoutLeakingWaiter(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	first, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "long",
			Notes: []appconfig.MelodyNote{{
				FrequencyHz: 440, DurationMS: 500,
			}},
		},
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "short",
			Notes: []appconfig.MelodyNote{{
				FrequencyHz: 880, DurationMS: 1,
			}},
		},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	for name, done := range map[string]<-chan error{
		"first": first.Done, "second": second.Done,
	} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s operation: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s operation waiter leaked", name)
		}
	}
}

func TestMelodyZeroRepeatsUntilExplicitStop(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "attention",
			Notes: []appconfig.MelodyNote{{
				FrequencyHz: 880, DurationMS: 2, GapMS: 1,
			}},
		},
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if commands := len(target.snapshot()); commands < 3 {
		t.Fatalf("indefinite melody emitted only %d commands", commands)
	}
	if !scheduler.StopMelody() {
		t.Fatal("indefinite melody was not active")
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("indefinite melody did not stop")
	}
}

func TestBreatheEffectIsRateLimitedAndRestoresSteadyBase(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "test", Kind: "breathe",
			Red: 10, Green: 20, Blue: 30,
			Brightness: 100, MinBrightness: 10,
			PeriodMS: 640, DurationMS: 220,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("effect did not complete")
	}
	commands := target.snapshot()
	if len(commands) < 4 || len(commands) > 7 {
		t.Fatalf("rate-limited effect emitted %d commands", len(commands))
	}
	last := commands[len(commands)-1]
	if last.opcode != native.OpStatusRGB ||
		len(last.payload) != 4 ||
		last.payload[3] != 100 {
		t.Fatalf("final steady frame=% X", last.payload)
	}
	for index := 1; index < len(commands)-1; index++ {
		if spacing := commands[index].at.Sub(commands[index-1].at); spacing < 45*time.Millisecond {
			t.Fatalf("frames %d/%d are too close: %v", index-1, index, spacing)
		}
	}
}

func TestStatusEffectRestoresNewestPolicyBase(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	if err := scheduler.SetStatusBase(context.Background(), 1, 2, 3, 90); err != nil {
		t.Fatal(err)
	}
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "overlay", Kind: "breathe",
			Red: 90, Green: 20, Blue: 200,
			Brightness: 180, MinBrightness: 10,
			PeriodMS: 640, DurationMS: 220,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !scheduler.StatusEffectActive() {
		t.Fatal("effect lane was not marked active")
	}
	if err := scheduler.SetStatusBase(context.Background(), 7, 8, 9, 100); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("effect did not complete")
	}
	if scheduler.StatusEffectActive() {
		t.Fatal("effect lane remained active after completion")
	}
	commands := target.snapshot()
	last := commands[len(commands)-1]
	want := native.StatusRGBPayload(7, 8, 9, 100)
	if string(last.payload) != string(want) {
		t.Fatalf("latest policy base was not restored: got=% X want=% X", last.payload, want)
	}
}

func TestOutputStreamStopsCleanlyOnDisconnect(t *testing.T) {
	target := &recordingOutputTarget{failAt: 1}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartMelody(
		context.Background(),
		appconfig.Melody{
			Name: "disconnect",
			Notes: []appconfig.MelodyNote{{
				FrequencyHz: 440, DurationMS: 10,
			}},
		},
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err == nil || err.Error() != "USB disconnected" {
			t.Fatalf("stream error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect did not end stream")
	}
}

func TestSteadyRGBOverrideCannotBeUndoneByCanceledEffectCleanup(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "continuous", Kind: "breathe",
			Red: 1, Green: 2, Blue: 3,
			Brightness: 100, MinBrightness: 5,
			PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(target.snapshot()) == 0 {
		t.Fatal("effect did not emit its first frame")
	}
	if !scheduler.OverrideStatusEffect() {
		t.Fatal("effect was not canceled for override")
	}
	steady := native.StatusRGBPayload(9, 8, 7, 6)
	if err := target.Command(
		context.Background(),
		native.OpStatusRGB,
		steady,
	); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled effect did not terminate")
	}
	commands := target.snapshot()
	last := commands[len(commands)-1]
	if string(last.payload) != string(steady) {
		t.Fatalf("canceled cleanup overwrote steady RGB: % X", last.payload)
	}
}
