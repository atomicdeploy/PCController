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
	source  CommandSource
}

type recordingOutputTarget struct {
	mu              sync.Mutex
	commands        []recordedOutputCommand
	events          []string
	failAt          int
	failCommands    map[int]error
	ackDelay        time.Duration
	noStatusEffects bool
	capabilities    uint32
}

func (target *recordingOutputTarget) Snapshot() Snapshot {
	if target.noStatusEffects {
		return Snapshot{}
	}
	capabilities := target.capabilities
	if capabilities == 0 {
		capabilities = native.CapabilityStatusEffects
	}
	return Snapshot{Hello: native.Hello{Capabilities: capabilities}}
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
		source:  CommandSourceFromContext(ctx),
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
	if err := target.failCommands[commandCount]; err != nil {
		return err
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

func TestNativeProfileStatusBaseDoesNotStealBoardOwnership(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects | native.CapabilityStatusProfiles,
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	if err := scheduler.SetStatusBase(context.Background(), 10, 20, 30, 120); err != nil {
		t.Fatal(err)
	}
	if commands := target.snapshot(); len(commands) != 0 {
		t.Fatalf("native lifecycle base streamed STATUS_RGB: %#v", commands)
	}
	state := scheduler.State()
	if !state.HaveStatusBase || state.StatusOwner != "native-lifecycle" {
		t.Fatalf("native lifecycle owner state=%#v", state)
	}
	scheduler.ClearStatusBase()
	state = scheduler.State()
	if state.HaveStatusBase || state.StatusOwner != "native-lifecycle" ||
		len(target.snapshot()) != 0 {
		t.Fatalf("clearing fallback changed native ownership: %#v", state)
	}
}

func TestSuccessfulSteadyRGBReplacementTracksReleasablePreview(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 91, name: "terminal"}
	scheduler.mu.Unlock()

	if err := scheduler.ReplaceStatusRGB(context.Background(), 7, 8, 9, 100); err != nil {
		t.Fatal(err)
	}
	state := scheduler.State()
	if state.EffectID == 0 || !state.EffectRetained || state.StatusOwner != "board-preview" {
		t.Fatalf("successful RGB replacement did not track preview owner: %#v", state)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("acknowledged RGB preview was not releasable")
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusRGB ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("RGB preview lifecycle commands=%#v", commands)
	}
}

func TestFailedRetainedReleaseStaysPendingUntilRetryACK(t *testing.T) {
	target := &recordingOutputTarget{
		failCommands: map[int]error{1: errors.New("release ACK lost")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 61, name: "terminal"}
	scheduler.mu.Unlock()

	if !scheduler.StopStatusEffect() {
		t.Fatal("retained release was not attempted")
	}
	state := scheduler.State()
	if state.EffectID != 61 || !state.EffectRetained || !state.EffectReleasePending {
		t.Fatalf("failed release did not remain retryable: %#v", state)
	}
	if !scheduler.StopStatusEffect() || scheduler.StopStatusEffect() {
		t.Fatal("release retry did not clear exactly once")
	}
	commands := target.snapshot()
	if len(commands) != 2 {
		t.Fatalf("release attempts=%d, want failed attempt plus retry", len(commands))
	}
}

func TestNativeStatusEffectACKFailureReleasesPossibleOwner(t *testing.T) {
	target := &recordingOutputTarget{
		failCommands: map[int]error{1: errors.New("USB disconnected")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "lost-ack", Kind: "breathe", Red: 30, Green: 40, Blue: 200,
			Brightness: 180, MinBrightness: 20, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err == nil || err.Error() != "USB disconnected" {
			t.Fatalf("descriptor error=%v, want USB disconnected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed descriptor did not finish")
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("failed descriptor did not receive release: %#v", commands)
	}
	if scheduler.StatusEffectActive() {
		t.Fatalf("failed descriptor retained ownership: %#v", scheduler.State())
	}
}

func TestConcurrentStopsReleaseRetainedNativeOwnerExactlyOnce(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 9, name: "terminal"}
	scheduler.mu.Unlock()

	const callers = 16
	var wait sync.WaitGroup
	results := make(chan bool, callers)
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			results <- scheduler.StopStatusEffect()
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for stopped := range results {
		if stopped {
			successes++
		}
	}
	if successes != 1 || len(target.snapshot()) != 1 {
		t.Fatalf("successful stops=%d commands=%#v", successes, target.snapshot())
	}
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

func TestBreatheEffectUsesOneDescriptorAndRetainsBoardEndpoint(t *testing.T) {
	target := &recordingOutputTarget{}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "test", Kind: "breathe",
			Red: 10, Green: 20, Blue: 30,
			Brightness: 100, MinBrightness: 10,
			PeriodMS: 640, Repeats: 1,
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
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect {
		t.Fatalf("effect did not use exactly one native descriptor: %#v", commands)
	}
	state := scheduler.State()
	if !state.EffectRetained || state.EffectID != operation.ID {
		t.Fatalf("settled native endpoint was not retained: %#v", state)
	}
}

func TestStatusEffectRequiresAdvertisedFirmwareCapability(t *testing.T) {
	target := &recordingOutputTarget{noStatusEffects: true}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	_, err := scheduler.StartStatusEffect(context.Background(), appconfig.StatusLEDEffect{
		Name: "pulse", Kind: "flash", Brightness: 100, PeriodMS: 640, Repeats: 1,
	})
	if err == nil || err.Error() != "connected firmware does not advertise status effects" {
		t.Fatalf("missing capability error=%v", err)
	}
	if commands := target.snapshot(); len(commands) != 0 {
		t.Fatalf("unsupported effect sent commands: %#v", commands)
	}
}

func TestFiniteStatusEffectRetainsBoardEndpointUntilReleased(t *testing.T) {
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
			PeriodMS: 640, Repeats: 1,
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
	state := scheduler.State()
	if !scheduler.StatusEffectActive() || !state.EffectRetained ||
		state.EffectID != operation.ID || state.EffectName != "overlay" {
		t.Fatalf("finite board endpoint was not retained: %#v", state)
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[1].opcode != native.OpStatusEffect {
		t.Fatalf("finite effect emitted a host RGB snap: %#v", commands)
	}
	if commands[0].source != "" {
		t.Fatal("explicit base write was incorrectly classified as background")
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("retained finite endpoint could not be released")
	}
	commands = target.snapshot()
	if len(commands) != 3 || commands[2].opcode != native.OpStatusEffect ||
		string(commands[2].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("retained endpoint release commands=%#v", commands)
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
	steady := native.StatusRGBPayload(9, 8, 7, 6)
	if err := scheduler.ReplaceStatusRGB(context.Background(), 9, 8, 7, 6); err != nil {
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
