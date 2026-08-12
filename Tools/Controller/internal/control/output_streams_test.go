package control

import (
	"context"
	"errors"
	"strings"
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
	mu           sync.Mutex
	commands     []recordedOutputCommand
	events       []string
	failAt       int
	failCommands map[int]error
	capabilities uint32
}

type blockingOutputTarget struct {
	recordingOutputTarget
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (target *blockingOutputTarget) Command(
	ctx context.Context,
	opcode byte,
	payload []byte,
) error {
	if err := target.recordingOutputTarget.Command(ctx, opcode, payload); err != nil {
		return err
	}
	target.once.Do(func() { close(target.entered) })
	<-target.release
	return nil
}

func (target *recordingOutputTarget) Snapshot() Snapshot {
	return Snapshot{Hello: native.Hello{Capabilities: target.capabilities}}
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
	defer target.mu.Unlock()
	target.commands = append(target.commands, recordedOutputCommand{
		at: time.Now(), opcode: opcode,
		payload: append([]byte(nil), payload...),
	})
	if err := target.failCommands[len(target.commands)]; err != nil {
		return err
	}
	if target.failAt != 0 && len(target.commands) >= target.failAt {
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

func (target *recordingOutputTarget) eventSnapshot() []string {
	target.mu.Lock()
	defer target.mu.Unlock()
	return append([]string(nil), target.events...)
}

func TestNativeStatusEffectReplacementSendsDescriptorDirectly(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	firstEffect := appconfig.StatusLEDEffect{
		Name: "first", Kind: "breathe", Red: 10, Green: 20, Blue: 200,
		Brightness: 180, MinBrightness: 20, PeriodMS: 640,
	}
	secondEffect := appconfig.StatusLEDEffect{
		Name: "second", Kind: "breathe", Red: 200, Green: 30, Blue: 10,
		Brightness: 220, MinBrightness: 40, PeriodMS: 1280,
	}
	first, err := scheduler.StartStatusEffect(context.Background(), firstEffect)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	second, err := scheduler.StartStatusEffect(context.Background(), secondEffect)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-first.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("replaced native effect did not finish")
	}
	deadline = time.Now().Add(time.Second)
	for len(target.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	commands := target.snapshot()
	if len(commands) != 2 {
		t.Fatalf("native replacement command count=%d, want 2", len(commands))
	}
	firstOptions, _, err := nativeStatusEffect(firstEffect)
	if err != nil {
		t.Fatal(err)
	}
	firstPayload, err := native.StatusEffectPayload(firstOptions)
	if err != nil {
		t.Fatal(err)
	}
	secondOptions, _, err := nativeStatusEffect(secondEffect)
	if err != nil {
		t.Fatal(err)
	}
	secondPayload, err := native.StatusEffectPayload(secondOptions)
	if err != nil {
		t.Fatal(err)
	}
	if commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[0].payload) != string(firstPayload) ||
		string(commands[1].payload) != string(secondPayload) ||
		string(commands[0].payload) == string(commands[1].payload) {
		t.Fatalf("native replacement was not descriptor A -> descriptor B: %#v", commands)
	}
	for _, command := range commands {
		if command.opcode == native.OpStatusRGB ||
			string(command.payload) == string(native.StatusEffectReleasePayload()) {
			t.Fatalf("native replacement inserted an owner-release gap: %#v", command)
		}
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("replacement effect was not active")
	}
	select {
	case err := <-second.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement effect did not stop")
	}
}

func TestStoppingPendingNativeReplacementReleasesPriorOwner(t *testing.T) {
	target := &blockingOutputTarget{
		recordingOutputTarget: recordingOutputTarget{
			capabilities: native.CapabilityStatusEffects,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	scheduler := NewOutputScheduler(target)
	first, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "first", Kind: "breathe", Red: 10, Green: 20, Blue: 30,
			Brightness: 180, MinBrightness: 20, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-target.entered:
	case <-time.After(time.Second):
		t.Fatal("first descriptor did not enter transport")
	}
	second, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "pending", Kind: "breathe", Red: 40, Green: 50, Blue: 60,
			Brightness: 200, MinBrightness: 30, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("pending replacement was not stoppable")
	}
	close(target.release)
	for name, done := range map[string]<-chan error{
		"first": first.Done, "second": second.Done,
	} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s effect: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s effect did not finish", name)
		}
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("pending replacement stop did not release prior owner: %#v", commands)
	}
	scheduler.Close()
	if commands = target.snapshot(); len(commands) != 2 {
		t.Fatalf("Close duplicated pending replacement release: %#v", commands)
	}
}

func TestRetainedOwnerSurvivesUntilReplacementDescriptorACK(t *testing.T) {
	target := &blockingOutputTarget{
		recordingOutputTarget: recordingOutputTarget{
			capabilities: native.CapabilityStatusEffects,
		},
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	scheduler := NewOutputScheduler(target)
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 41, name: "owner-a"}
	scheduler.mu.Unlock()

	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "owner-b", Kind: "breathe", Red: 40, Green: 50, Blue: 60,
			Brightness: 200, MinBrightness: 30, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-target.entered:
	case <-time.After(time.Second):
		t.Fatal("replacement descriptor did not enter transport")
	}
	scheduler.mu.Lock()
	retained := scheduler.retainedEffect
	scheduler.mu.Unlock()
	if retained == nil || retained.id != 41 || retained.name != "owner-a" {
		t.Fatalf("owner A was cleared before descriptor B ACK: %#v", retained)
	}
	state := scheduler.State()
	if state.EffectID != 41 || state.EffectName != "owner-a" ||
		state.EffectPendingID != operation.ID ||
		state.EffectPendingName != "owner-b" {
		t.Fatalf("in-flight B obscured actual owner A: %#v", state)
	}
	close(target.release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		scheduler.mu.Lock()
		retained = scheduler.retainedEffect
		scheduler.mu.Unlock()
		if retained == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if retained != nil {
		t.Fatalf("owner A remained after descriptor B ACK: %#v", retained)
	}
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		string(commands[0].payload) == string(native.StatusEffectReleasePayload()) {
		t.Fatalf("replacement ACK path inserted a release: %#v", commands)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("acknowledged replacement was not stoppable")
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("acknowledged replacement did not stop")
	}
	scheduler.Close()
}

func TestReplacementDescriptorFailurePreservesPriorRetainedOwner(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
		failCommands: map[int]error{1: errors.New("descriptor B ACK lost")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 51, name: "owner-a"}
	scheduler.mu.Unlock()

	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "owner-b", Kind: "breathe", Red: 40, Green: 50, Blue: 60,
			Brightness: 200, MinBrightness: 30, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err == nil || err.Error() != "descriptor B ACK lost" {
			t.Fatalf("replacement error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed replacement did not finish")
	}
	state := scheduler.State()
	if state.EffectID != 51 || state.EffectName != "owner-a" ||
		!state.EffectRetained || !state.EffectReleasePending {
		t.Fatalf("failed B did not preserve releasable owner A: %#v", state)
	}
	if commands := target.snapshot(); len(commands) != 1 {
		t.Fatalf("failed B unexpectedly released owner A: %#v", commands)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("preserved owner A was not releasable")
	}
	if scheduler.StopStatusEffect() {
		t.Fatal("owner A remained after acknowledged release")
	}
	commands := target.snapshot()
	if len(commands) != 2 ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("preserved owner release commands=%#v", commands)
	}
}

func TestStoppingNativeStatusEffectReleasesWithoutRGBFallback(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "stop", Kind: "breathe", Red: 30, Green: 40, Blue: 200,
			Brightness: 180, MinBrightness: 20, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("native effect was not active")
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("native effect did not stop")
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("native stop did not emit descriptor then release: %#v", commands)
	}
	for _, command := range commands {
		if command.opcode == native.OpStatusRGB {
			t.Fatalf("native stop inserted an RGB fallback: %#v", command)
		}
	}
}

func TestNativeStatusEffectACKFailureReleasesPossibleBoardOwner(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
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
			t.Fatalf("native descriptor error=%v, want USB disconnected", err)
		}
	case <-time.After(time.Second):
		t.Fatal("failed native descriptor did not finish")
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("failed descriptor did not receive one best-effort release: %#v", commands)
	}
	if scheduler.StatusEffectActive() || scheduler.State().EffectRetained {
		t.Fatalf("failed descriptor retained host ownership: %#v", scheduler.State())
	}
}

func TestFiniteNativeTransitionKeepsMCUSettledEndpoint(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	effect := appconfig.StatusLEDEffect{
		Name: "settle", Kind: "transition", Red: 200, Green: 10, Blue: 0,
		AlternateRed: 0, AlternateGreen: 200, AlternateBlue: 30,
		Brightness: 180, MinBrightness: 0, PeriodMS: 640, Repeats: 1,
	}
	operation, err := scheduler.StartStatusEffect(context.Background(), effect)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finite native transition did not complete")
	}
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		len(commands[0].payload) != 12 ||
		commands[0].payload[0] != native.StatusEffectTransition ||
		commands[0].payload[4] != effect.AlternateRed ||
		commands[0].payload[5] != effect.AlternateGreen ||
		commands[0].payload[6] != effect.AlternateBlue {
		t.Fatalf("finite transition was followed by a host snap/release: %#v", commands)
	}
	state := scheduler.State()
	if !scheduler.StatusEffectActive() || !state.EffectRetained ||
		state.EffectID != operation.ID || state.EffectName != effect.Name {
		t.Fatalf("finite native owner was not retained: %#v", state)
	}
	if err := scheduler.SetStatusBase(context.Background(), 1, 2, 3, 40); err != nil {
		t.Fatal(err)
	}
	if commands = target.snapshot(); len(commands) != 1 {
		t.Fatalf("policy base overwrote retained native endpoint: %#v", commands)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("retained native owner could not be stopped")
	}
	if scheduler.StopStatusEffect() {
		t.Fatal("retained native owner released more than once")
	}
	commands = target.snapshot()
	if len(commands) != 2 || commands[1].opcode != native.OpStatusEffect ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("retained native stop did not emit one release: %#v", commands)
	}
	if scheduler.StatusEffectActive() || scheduler.State().EffectRetained {
		t.Fatalf("retained owner remained after stop: %#v", scheduler.State())
	}
}

func TestRetainedNativeEffectReplacementHasNoReleaseGap(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	firstEffect := appconfig.StatusLEDEffect{
		Name: "settled", Kind: "transition", Red: 150, Green: 10, Blue: 0,
		AlternateRed: 0, AlternateGreen: 90, AlternateBlue: 180,
		Brightness: 160, PeriodMS: 640, Repeats: 1,
	}
	first, err := scheduler.StartStatusEffect(context.Background(), firstEffect)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-first.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first native effect did not settle")
	}
	if !scheduler.State().EffectRetained {
		t.Fatal("first native effect was not retained")
	}
	secondEffect := appconfig.StatusLEDEffect{
		Name: "replacement", Kind: "breathe", Red: 30, Green: 80, Blue: 200,
		Brightness: 180, MinBrightness: 20, PeriodMS: 640,
	}
	second, err := scheduler.StartStatusEffect(context.Background(), secondEffect)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	commands := target.snapshot()
	if len(commands) != 2 || commands[0].opcode != native.OpStatusEffect ||
		commands[1].opcode != native.OpStatusEffect {
		t.Fatalf("retained replacement was not descriptor A -> B: %#v", commands)
	}
	for _, command := range commands {
		if command.opcode == native.OpStatusRGB ||
			string(command.payload) == string(native.StatusEffectReleasePayload()) {
			t.Fatalf("retained replacement inserted a release/RGB gap: %#v", commands)
		}
	}
	if scheduler.State().EffectRetained {
		t.Fatalf("running replacement reported retained: %#v", scheduler.State())
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("replacement native effect was not active")
	}
	select {
	case err := <-second.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("replacement native effect did not stop")
	}
}

func TestStopAllReleasesRetainedNativeOwnerExactlyOnce(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 7, name: "settled"}
	scheduler.mu.Unlock()

	scheduler.StopAll()
	scheduler.StopAll()
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		string(commands[0].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("StopAll release commands=%#v, want exactly one native release", commands)
	}
	if scheduler.StatusEffectActive() {
		t.Fatal("StopAll left retained native ownership active")
	}
	scheduler.Close()
	if commands = target.snapshot(); len(commands) != 1 {
		t.Fatalf("Close repeated StopAll release: %#v", commands)
	}
}

func TestCloseReleasesRetainedNativeOwnerExactlyOnce(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 8, name: "terminal"}
	scheduler.mu.Unlock()

	scheduler.Close()
	scheduler.Close()
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		string(commands[0].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("Close release commands=%#v, want exactly one native release", commands)
	}
}

func TestFailedRetainedReleaseStaysPendingUntilRetryACK(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
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
	if state.EffectID != 61 || state.EffectName != "terminal" ||
		!state.EffectRetained || !state.EffectReleasePending {
		t.Fatalf("failed release did not remain pending: %#v", state)
	}
	events := target.eventSnapshot()
	foundFailure := false
	for _, event := range events {
		if strings.Contains(event, "release failed") {
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Fatalf("release failure event missing: %#v", events)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("pending release could not be retried")
	}
	if scheduler.StopStatusEffect() {
		t.Fatal("acknowledged release remained retryable")
	}
	commands := target.snapshot()
	if len(commands) != 2 {
		t.Fatalf("release attempts=%d, want failed attempt plus one retry", len(commands))
	}
	for _, command := range commands {
		if command.opcode != native.OpStatusEffect ||
			string(command.payload) != string(native.StatusEffectReleasePayload()) {
			t.Fatalf("unexpected release retry command: %#v", command)
		}
	}
	if scheduler.StatusEffectActive() {
		t.Fatal("owner remained after release retry ACK")
	}
}

func TestRetainedOwnerSurvivesFailedSteadyRGBReplacement(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
		failCommands: map[int]error{1: errors.New("RGB ACK lost")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 81, name: "terminal"}
	scheduler.mu.Unlock()

	err := scheduler.ReplaceStatusRGB(context.Background(), 1, 2, 3, 44)
	if err == nil || err.Error() != "RGB ACK lost" {
		t.Fatalf("ReplaceStatusRGB error=%v", err)
	}
	state := scheduler.State()
	if state.EffectID != 81 || state.EffectName != "terminal" ||
		!state.EffectRetained || state.StatusOwner != "board-effect" {
		t.Fatalf("failed RGB replacement lost owner: %#v", state)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("failed RGB replacement owner was not releasable")
	}
}

func TestRunningOwnerSurvivesFailedSteadyRGBReplacement(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
		failCommands: map[int]error{2: errors.New("RGB ACK lost")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "running", Kind: "breathe", Red: 10, Green: 20, Blue: 30,
			Brightness: 180, MinBrightness: 20, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err := scheduler.ReplaceStatusRGB(context.Background(), 4, 5, 6, 77); err == nil {
		t.Fatal("failed running RGB replacement returned nil")
	}
	state := scheduler.State()
	if state.EffectID != operation.ID || state.EffectName != "running" ||
		state.EffectRetained || state.StatusOwner != "board-effect" {
		t.Fatalf("failed running RGB replacement lost owner: %#v", state)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("running owner was not stoppable after failed RGB")
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("running owner did not stop")
	}
}

func TestSuccessfulSteadyRGBReplacementClearsOwnerWithoutRelease(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 91, name: "terminal"}
	scheduler.mu.Unlock()

	if err := scheduler.ReplaceStatusRGB(context.Background(), 7, 8, 9, 100); err != nil {
		t.Fatal(err)
	}
	state := scheduler.State()
	if state.EffectID == 0 || !state.EffectRetained ||
		state.StatusOwner != "board-preview" {
		t.Fatalf("successful RGB replacement did not track preview owner: %#v", state)
	}
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusRGB {
		t.Fatalf("successful RGB replacement commands=%#v", commands)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("acknowledged RGB preview was not releasable")
	}
	commands = target.snapshot()
	if len(commands) != 2 ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("RGB preview release commands=%#v", commands)
	}
}

func TestNativeProfileStatusBaseDoesNotStreamRGB(t *testing.T) {
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
		t.Fatalf("clear base changed native ownership/wire state: %#v", state)
	}
}

func TestClearStatusBaseDoesNotReleaseExplicitPreview(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	if err := scheduler.ReplaceStatusRGB(context.Background(), 7, 8, 9, 100); err != nil {
		t.Fatal(err)
	}
	scheduler.ClearStatusBase()
	state := scheduler.State()
	if state.HaveStatusBase || !state.EffectRetained ||
		state.StatusOwner != "board-preview" {
		t.Fatalf("clear base released explicit preview: %#v", state)
	}
	if commands := target.snapshot(); len(commands) != 1 ||
		commands[0].opcode != native.OpStatusRGB {
		t.Fatalf("clear base emitted a wire command: %#v", commands)
	}
}

func TestReleaseStatusEffectWithoutOwnerIsNoOpUnlessReconciled(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	released, err := scheduler.ReleaseStatusEffect(context.Background())
	if err != nil || released || len(target.snapshot()) != 0 {
		t.Fatalf("ownerless non-forced release=(%t,%v) commands=%#v", released, err, target.snapshot())
	}
	if err := scheduler.ReconcileStatusEffect(context.Background()); err != nil {
		t.Fatal(err)
	}
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		string(commands[0].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("forced reconciliation commands=%#v", commands)
	}
	if scheduler.StatusEffectActive() {
		t.Fatal("successful reconciliation invented a stale owner")
	}
}

func TestActiveNativeStopReleaseFailureRetainsRetryableOwner(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
		failCommands: map[int]error{2: errors.New("release ACK lost")},
	}
	scheduler := NewOutputScheduler(target)
	defer scheduler.Close()
	operation, err := scheduler.StartStatusEffect(
		context.Background(),
		appconfig.StatusLEDEffect{
			Name: "active", Kind: "breathe", Red: 10, Green: 20, Blue: 30,
			Brightness: 180, MinBrightness: 20, PeriodMS: 640,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(target.snapshot()) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("active native effect was not stoppable")
	}
	select {
	case err := <-operation.Done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("active native effect did not stop")
	}
	state := scheduler.State()
	if state.EffectID != operation.ID || state.EffectName != "active" ||
		!state.EffectRetained || !state.EffectReleasePending {
		t.Fatalf("failed active release lost retry owner: %#v", state)
	}
	if !scheduler.StopStatusEffect() {
		t.Fatal("failed active release could not be retried")
	}
	if scheduler.StopStatusEffect() {
		t.Fatal("active owner remained after release retry ACK")
	}
	commands := target.snapshot()
	if len(commands) != 3 ||
		string(commands[1].payload) != string(native.StatusEffectReleasePayload()) ||
		string(commands[2].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("active release retry commands=%#v", commands)
	}
}

func TestCloseRetriesFailedRetainedRelease(t *testing.T) {
	target := &recordingOutputTarget{
		capabilities: native.CapabilityStatusEffects,
		failCommands: map[int]error{
			1: errors.New("release ACK lost"),
			2: errors.New("release ACK lost"),
			3: errors.New("release ACK lost"),
		},
	}
	scheduler := NewOutputScheduler(target)
	scheduler.mu.Lock()
	scheduler.retainedEffect = &retainedOutput{id: 71, name: "terminal"}
	scheduler.mu.Unlock()

	scheduler.Close()
	state := scheduler.State()
	if state.EffectID != 71 || !state.EffectRetained || !state.EffectReleasePending {
		t.Fatalf("Close lost failed release owner: %#v", state)
	}
	target.mu.Lock()
	target.failCommands = nil
	target.mu.Unlock()
	scheduler.Close()
	if scheduler.StatusEffectActive() {
		t.Fatal("second Close did not clear acknowledged release")
	}
	scheduler.Close()
	if commands := target.snapshot(); len(commands) != 4 {
		t.Fatalf("Close release attempts=%d, want three bounded failures plus retry", len(commands))
	}
}

func TestConcurrentStopsReleaseRetainedNativeOwnerExactlyOnce(t *testing.T) {
	target := &recordingOutputTarget{capabilities: native.CapabilityStatusEffects}
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
	if successes != 1 {
		t.Fatalf("successful concurrent stops=%d, want 1", successes)
	}
	commands := target.snapshot()
	if len(commands) != 1 || commands[0].opcode != native.OpStatusEffect ||
		string(commands[0].payload) != string(native.StatusEffectReleasePayload()) {
		t.Fatalf("concurrent stop commands=%#v, want exactly one release", commands)
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

func TestSteadyRGBReplacementCannotBeUndoneByCanceledEffectCleanup(t *testing.T) {
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
