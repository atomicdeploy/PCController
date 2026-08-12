package control

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const (
	maxMelodyRepeats           = 20
	outputRequestTimeout       = 2 * time.Second
	minStatusStreamStep        = 50 * time.Millisecond
	statusBreatheStepCount     = 32
	outputCloseReleaseAttempts = 3
)

type outputCommander interface {
	Command(context.Context, byte, []byte) error
	PublishHostEvent(string, string)
}

type outputCapabilityReporter interface {
	Snapshot() Snapshot
}

type StreamOperation struct {
	ID   uint64
	Kind string
	Name string
	Done <-chan error
}

type OutputStreamState struct {
	MelodyID              uint64  `json:"melody_id,omitempty"`
	MelodyName            string  `json:"melody_name,omitempty"`
	EffectID              uint64  `json:"effect_id,omitempty"`
	EffectName            string  `json:"effect_name,omitempty"`
	EffectRetained        bool    `json:"effect_retained,omitempty"`
	EffectReleasePending  bool    `json:"effect_release_pending,omitempty"`
	EffectPendingID       uint64  `json:"effect_pending_id,omitempty"`
	EffectPendingName     string  `json:"effect_pending_name,omitempty"`
	StatusOwner           string  `json:"status_owner"`
	StatusOwnerGeneration uint64  `json:"status_owner_generation,omitempty"`
	StatusOwnerDevice     string  `json:"status_owner_device,omitempty"`
	StatusOwnerConnected  bool    `json:"status_owner_connected,omitempty"`
	StatusOwnerStale      bool    `json:"status_owner_stale,omitempty"`
	StatusBase            [4]byte `json:"status_base"`
	HaveStatusBase        bool    `json:"have_status_base"`
}

type runningOutput struct {
	id               uint64
	name             string
	cancel           context.CancelFunc
	done             chan error
	stopRequested    bool
	nativeAttempted  bool
	nativeAccepted   bool
	nativeGeneration uint64
	nativeDevice     string
}

type retainedOutput struct {
	id             uint64
	name           string
	releasePending bool
	owner          string
	generation     uint64
	device         string
}

// OutputScheduler streams high-level PC-side effects through existing native
// opcodes. Melody and status-LED lanes are independent; starting a new item on
// a lane cancels the previous one. Every command waits for its ACK and streams
// are rate-limited, so the MCU's small queues cannot be flooded.
type OutputScheduler struct {
	target outputCommander
	root   context.Context
	cancel context.CancelFunc

	mu                sync.Mutex
	statusWireMu      sync.Mutex
	statusOperationMu sync.Mutex
	nextID            uint64
	closed            bool
	melody            *runningOutput
	effect            *runningOutput
	retainedEffect    *retainedOutput
	statusBase        [4]byte
	haveStatusBase    bool
}

func NewOutputScheduler(target outputCommander) *OutputScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &OutputScheduler{target: target, root: ctx, cancel: cancel}
}

func (scheduler *OutputScheduler) Close() error {
	scheduler.statusOperationMu.Lock()
	defer scheduler.statusOperationMu.Unlock()

	scheduler.mu.Lock()
	if !scheduler.closed {
		scheduler.closed = true
		scheduler.cancel()
		if scheduler.melody != nil {
			scheduler.melody.cancel()
		}
		if scheduler.effect != nil {
			running := scheduler.effect
			running.stopRequested = true
			running.cancel()
			if running.nativeAccepted || running.nativeAttempted {
				scheduler.retainedEffect = scheduler.retainedRunning(running)
			}
			scheduler.effect = nil
		}
	}
	scheduler.mu.Unlock()

	var err error
	for attempt := 0; attempt < outputCloseReleaseAttempts; attempt++ {
		hadOwner, releaseErr := scheduler.releaseRetainedStatusEffectUnderOperation(
			context.Background(), false, true,
		)
		if !hadOwner || releaseErr == nil {
			return releaseErr
		}
		err = releaseErr
	}
	return err
}

func (scheduler *OutputScheduler) State() OutputStreamState {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	var state OutputStreamState
	if scheduler.melody != nil {
		state.MelodyID = scheduler.melody.id
		state.MelodyName = scheduler.melody.name
	}
	if scheduler.effect != nil && !scheduler.effect.nativeAccepted &&
		supportsNativeStatusEffects(scheduler.target) {
		state.EffectPendingID = scheduler.effect.id
		state.EffectPendingName = scheduler.effect.name
		if scheduler.retainedEffect != nil {
			state.EffectID = scheduler.retainedEffect.id
			state.EffectName = scheduler.retainedEffect.name
			state.EffectRetained = true
			state.EffectReleasePending = scheduler.retainedEffect.releasePending
			state.StatusOwner = retainedStatusOwner(scheduler.retainedEffect)
		} else if supportsNativeStatusProfiles(scheduler.target) {
			state.StatusOwner = "native-lifecycle"
		} else {
			state.StatusOwner = "host-fallback"
		}
	} else if scheduler.effect != nil {
		state.EffectID = scheduler.effect.id
		state.EffectName = scheduler.effect.name
		if supportsNativeStatusEffects(scheduler.target) {
			state.StatusOwner = "board-effect"
		} else {
			state.StatusOwner = "host-fallback"
		}
	} else if scheduler.retainedEffect != nil {
		state.EffectID = scheduler.retainedEffect.id
		state.EffectName = scheduler.retainedEffect.name
		state.EffectRetained = true
		state.EffectReleasePending = scheduler.retainedEffect.releasePending
		state.StatusOwner = retainedStatusOwner(scheduler.retainedEffect)
	} else if supportsNativeStatusProfiles(scheduler.target) {
		state.StatusOwner = "native-lifecycle"
	} else if scheduler.haveStatusBase {
		state.StatusOwner = "host-static"
	} else {
		state.StatusOwner = "host-fallback"
	}
	state.StatusBase = scheduler.statusBase
	state.HaveStatusBase = scheduler.haveStatusBase
	if scheduler.retainedEffect != nil {
		current := scheduler.targetSnapshot()
		state.StatusOwnerGeneration = scheduler.retainedEffect.generation
		state.StatusOwnerDevice = scheduler.retainedEffect.device
		state.StatusOwnerConnected = current.Connected
		state.StatusOwnerStale = current.Connected &&
			scheduler.retainedEffect.generation != 0 &&
			current.ConnectionGeneration != scheduler.retainedEffect.generation
	}
	return state
}

func retainedStatusOwner(owner *retainedOutput) string {
	if owner != nil && owner.owner != "" {
		return owner.owner
	}
	return "board-effect"
}

func (scheduler *OutputScheduler) targetSnapshot() Snapshot {
	reporter, ok := scheduler.target.(outputCapabilityReporter)
	if !ok {
		return Snapshot{}
	}
	return reporter.Snapshot()
}

func (scheduler *OutputScheduler) retained(
	id uint64,
	name string,
	owner string,
) *retainedOutput {
	snapshot := scheduler.targetSnapshot()
	device := strings.TrimSpace(snapshot.Port.SerialNumber)
	if device == "" {
		device = strings.TrimSpace(snapshot.Port.InstanceID)
	}
	if device == "" {
		device = strings.TrimSpace(snapshot.Port.Name)
	}
	return &retainedOutput{
		id: id, name: name, owner: owner,
		generation: snapshot.ConnectionGeneration, device: device,
	}
}

func (scheduler *OutputScheduler) retainedRunning(
	running *runningOutput,
) *retainedOutput {
	return &retainedOutput{
		id: running.id, name: running.name, owner: "board-effect",
		generation: running.nativeGeneration, device: running.nativeDevice,
	}
}

// SetStatusBase updates the state-owned RGB frame without interrupting an
// explicit user/macro effect. The newest base is restored atomically when the
// overlay finishes, so an animation cannot leave the indicator in stale color.
func (scheduler *OutputScheduler) SetStatusBase(
	ctx context.Context,
	red, green, blue, brightness byte,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return errors.New("output scheduler is closed")
	}
	scheduler.statusBase = [4]byte{red, green, blue, brightness}
	scheduler.haveStatusBase = true
	if scheduler.effect != nil || scheduler.retainedEffect != nil {
		return nil
	}
	if supportsNativeStatusProfiles(scheduler.target) {
		// Modern boards render their selected status profile locally. Keep the
		// host policy base for fallback/restoration without stealing ownership
		// by streaming STATUS_RGB on every arbiter tick.
		return nil
	}
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	return scheduler.target.Command(
		requestContext,
		native.OpStatusRGB,
		native.StatusRGBPayload(red, green, blue, brightness),
	)
}

// ClearStatusBase forgets host policy fallback state without sending a wire
// command or releasing an explicit board preview/effect owner.
func (scheduler *OutputScheduler) ClearStatusBase() {
	scheduler.mu.Lock()
	scheduler.statusBase = [4]byte{}
	scheduler.haveStatusBase = false
	scheduler.mu.Unlock()
}

// ReplaceStatusRGB atomically transfers status ownership to a steady host RGB
// frame. Active or retained native ownership remains represented until the RGB
// ACK succeeds; a failed replacement is therefore still releasable/retryable.
func (scheduler *OutputScheduler) ReplaceStatusRGB(
	ctx context.Context,
	red, green, blue, brightness byte,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	scheduler.statusOperationMu.Lock()
	defer scheduler.statusOperationMu.Unlock()
	scheduler.mu.Lock()
	if scheduler.closed {
		scheduler.mu.Unlock()
		return errors.New("output scheduler is closed")
	}
	scheduler.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	scheduler.statusWireMu.Lock()
	if err := scheduler.target.Command(
		requestContext,
		native.OpStatusRGB,
		native.StatusRGBPayload(red, green, blue, brightness),
	); err != nil {
		scheduler.statusWireMu.Unlock()
		return err
	}
	scheduler.statusWireMu.Unlock()
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	running := scheduler.effect
	retained := scheduler.retainedEffect
	if running != nil {
		scheduler.effect = nil
		running.cancel()
	}
	scheduler.retainedEffect = nil
	scheduler.statusBase = [4]byte{red, green, blue, brightness}
	scheduler.haveStatusBase = true
	if supportsNativeStatusEffects(scheduler.target) {
		scheduler.nextID++
		scheduler.retainedEffect = scheduler.retained(
			scheduler.nextID, "steady RGB", "board-preview",
		)
	}
	if running != nil {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf(
				"effect %q replaced by steady RGB (id=%d)",
				running.name,
				running.id,
			),
		)
	} else if retained != nil {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf(
				"effect %q replaced by steady RGB (id=%d)",
				retained.name,
				retained.id,
			),
		)
	}
	return nil
}

func (scheduler *OutputScheduler) StatusEffectActive() bool {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.effect != nil || scheduler.retainedEffect != nil
}

func (scheduler *OutputScheduler) StartMelody(
	ctx context.Context,
	melody appconfig.Melody,
	repeats int,
) (StreamOperation, error) {
	if err := ctx.Err(); err != nil {
		return StreamOperation{}, err
	}
	if err := appconfig.ValidateMelody(melody); err != nil {
		return StreamOperation{}, err
	}
	if repeats < 0 || repeats > maxMelodyRepeats {
		return StreamOperation{}, fmt.Errorf(
			"melody repeats must be 0 (until stopped) or 1..%d",
			maxMelodyRepeats,
		)
	}
	operation, runContext, running, previousDone, err :=
		scheduler.replace("melody", melody.Name)
	if err != nil {
		return StreamOperation{}, err
	}
	go func() {
		if err := waitPreviousOutput(runContext, previousDone); err != nil {
			scheduler.finish("melody", running, err, nil)
			return
		}
		err := scheduler.streamMelody(runContext, melody, repeats)
		scheduler.finish("melody", running, err, nil)
	}()
	return operation, nil
}

func (scheduler *OutputScheduler) StartStatusEffect(
	ctx context.Context,
	effect appconfig.StatusLEDEffect,
) (StreamOperation, error) {
	if err := ctx.Err(); err != nil {
		return StreamOperation{}, err
	}
	if err := appconfig.ValidateStatusLEDEffect(effect); err != nil {
		return StreamOperation{}, err
	}
	operation, runContext, running, previousDone, err :=
		scheduler.replace("effect", effect.Name)
	if err != nil {
		return StreamOperation{}, err
	}
	go func() {
		started := false
		err := waitPreviousOutput(runContext, previousDone)
		if err == nil {
			started = true
			err = scheduler.streamStatusEffect(runContext, running, effect)
		}
		scheduler.completeStatusEffect(running, effect, err, started)
		scheduler.finish("effect", running, err, nil)
	}()
	return operation, nil
}

// completeStatusEffect commits terminal ownership without holding the state
// mutex across transport I/O. Native completion retains the MCU's last frame;
// explicit stop or a possibly delivered failed descriptor is released.
func (scheduler *OutputScheduler) completeStatusEffect(
	running *runningOutput,
	effect appconfig.StatusLEDEffect,
	err error,
	started bool,
) {
	scheduler.statusOperationMu.Lock()
	defer scheduler.statusOperationMu.Unlock()

	scheduler.mu.Lock()
	if scheduler.effect != running {
		if running.nativeAccepted && scheduler.retainedEffect == nil {
			scheduler.retainedEffect = scheduler.retainedRunning(running)
		}
		scheduler.mu.Unlock()
		return
	}
	if running.nativeAttempted {
		if running.nativeAccepted || scheduler.retainedEffect == nil {
			scheduler.retainedEffect = scheduler.retainedRunning(running)
		}
		shouldRelease := running.stopRequested || err != nil
		scheduler.mu.Unlock()
		if shouldRelease {
			_, _ = scheduler.releaseRetainedStatusEffectUnderOperation(
				context.Background(), false, true,
			)
		}
		return
	}
	if !started {
		scheduler.mu.Unlock()
		return
	}
	payload := native.StatusRGBPayload(
		effect.Red, effect.Green, effect.Blue, effect.Brightness,
	)
	if scheduler.haveStatusBase {
		payload = native.StatusRGBPayload(
			scheduler.statusBase[0], scheduler.statusBase[1],
			scheduler.statusBase[2], scheduler.statusBase[3],
		)
	}
	scheduler.mu.Unlock()

	requestContext, cancel := context.WithTimeout(
		context.Background(), outputRequestTimeout,
	)
	defer cancel()
	scheduler.statusWireMu.Lock()
	_ = scheduler.target.Command(requestContext, native.OpStatusRGB, payload)
	scheduler.statusWireMu.Unlock()
}

func (scheduler *OutputScheduler) StopMelody() bool {
	return scheduler.stop("melody")
}

func (scheduler *OutputScheduler) StopStatusEffect() bool {
	return scheduler.stop("effect")
}

// ReleaseStatusEffect synchronously returns status ownership to firmware. The
// owner is retained until the release ACK succeeds so callers can safely retry.
// Native firmware receives one release even if local ownership is unknown,
// reconciling host state after reconnects or an earlier lost ACK.
func (scheduler *OutputScheduler) ReleaseStatusEffect(ctx context.Context) (bool, error) {
	return scheduler.releaseStatusEffect(ctx, false)
}

// ReconcileStatusEffect sends one explicit release even when no local owner is
// known. It is used at reconnect/programming boundaries to reconcile firmware.
func (scheduler *OutputScheduler) ReconcileStatusEffect(ctx context.Context) error {
	_, err := scheduler.releaseStatusEffect(ctx, true)
	return err
}

func (scheduler *OutputScheduler) releaseStatusEffect(
	ctx context.Context,
	force bool,
) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	scheduler.statusOperationMu.Lock()
	defer scheduler.statusOperationMu.Unlock()
	return scheduler.releaseRetainedStatusEffectUnderOperation(ctx, force, false)
}

func (scheduler *OutputScheduler) StopAll() {
	scheduler.StopMelody()
	scheduler.StopStatusEffect()
}

func (scheduler *OutputScheduler) replace(
	kind string,
	name string,
) (
	StreamOperation,
	context.Context,
	*runningOutput,
	<-chan error,
	error,
) {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return StreamOperation{}, nil, nil, nil,
			errors.New("output scheduler is closed")
	}
	slot := &scheduler.melody
	if kind == "effect" {
		slot = &scheduler.effect
		// Preserve the last acknowledged owner until descriptor B is itself
		// acknowledged. A successful B atomically replaces it without a release;
		// a failed B leaves a durable handle that can still release the board.
		if *slot != nil && (*slot).nativeAccepted &&
			scheduler.retainedEffect == nil {
			scheduler.retainedEffect = scheduler.retainedRunning(*slot)
		}
	}
	var previousDone <-chan error
	if *slot != nil {
		previousDone = (*slot).done
		(*slot).cancel()
	}
	scheduler.nextID++
	ctx, cancel := context.WithCancel(scheduler.root)
	done := make(chan error, 1)
	running := &runningOutput{
		id: scheduler.nextID, name: name, cancel: cancel, done: done,
	}
	*slot = running
	scheduler.target.PublishHostEvent(
		"output",
		fmt.Sprintf("%s %q started (id=%d)", kind, name, running.id),
	)
	return StreamOperation{
		ID: running.id, Kind: kind, Name: name, Done: done,
	}, ctx, running, previousDone, nil
}

func (scheduler *OutputScheduler) stop(kind string) bool {
	scheduler.mu.Lock()
	slot := scheduler.melody
	if kind == "effect" {
		slot = scheduler.effect
		if slot == nil && scheduler.retainedEffect != nil {
			scheduler.mu.Unlock()
			_, _ = scheduler.ReleaseStatusEffect(context.Background())
			return true
		}
	}
	if slot == nil {
		scheduler.mu.Unlock()
		return false
	}
	if slot.stopRequested {
		scheduler.mu.Unlock()
		return false
	}
	if kind == "effect" {
		slot.stopRequested = true
	}
	slot.cancel()
	scheduler.mu.Unlock()
	return true
}

func (scheduler *OutputScheduler) finish(
	kind string,
	operation *runningOutput,
	err error,
	restore func(),
) {
	scheduler.mu.Lock()
	slot := &scheduler.melody
	if kind == "effect" {
		slot = &scheduler.effect
	}
	isCurrent := *slot == operation
	if isCurrent {
		*slot = nil
	}
	scheduler.mu.Unlock()
	if isCurrent && restore != nil {
		restore()
	}
	if !isCurrent {
		operation.done <- normalizedStreamError(err)
		close(operation.done)
		return
	}
	if errors.Is(err, context.Canceled) {
		err = nil
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf(
				"%s %q stopped (id=%d)",
				kind,
				operation.name,
				operation.id,
			),
		)
	} else if err != nil {
		scheduler.target.PublishHostEvent(
			"error",
			fmt.Sprintf(
				"%s %q failed (id=%d): %v",
				kind,
				operation.name,
				operation.id,
				err,
			),
		)
	} else {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf(
				"%s %q completed (id=%d)",
				kind,
				operation.name,
				operation.id,
			),
		)
	}
	operation.done <- err
	close(operation.done)
}

func normalizedStreamError(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (scheduler *OutputScheduler) streamMelody(
	ctx context.Context,
	melody appconfig.Melody,
	repeats int,
) error {
	for repeat := 0; repeats == 0 || repeat < repeats; repeat++ {
		for _, note := range melody.Notes {
			if err := scheduler.send(
				ctx,
				native.OpBuzzer,
				native.BuzzerPayload(note.FrequencyHz, note.DurationMS),
			); err != nil {
				return err
			}
			if err := waitOutput(
				ctx,
				time.Duration(note.DurationMS+note.GapMS)*time.Millisecond,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func (scheduler *OutputScheduler) streamStatusEffect(
	ctx context.Context,
	running *runningOutput,
	effect appconfig.StatusLEDEffect,
) error {
	if supportsNativeStatusEffects(scheduler.target) {
		options, duration, err := nativeStatusEffect(effect)
		if err != nil {
			return err
		}
		payload, err := native.StatusEffectPayload(options)
		if err != nil {
			return err
		}
		if err := scheduler.sendStatusDescriptor(ctx, running, payload); err != nil {
			return err
		}
		if duration == 0 {
			<-ctx.Done()
			return ctx.Err()
		}
		return waitOutput(ctx, duration)
	}

	// Compatibility path for older firmware. Current boards advertise the
	// compact effect opcode and receive only one descriptor per animation.
	started := time.Now()
	period := time.Duration(effect.PeriodMS) * time.Millisecond
	var step time.Duration
	switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
	case "flash":
		step = period / 2
	case "breathe":
		step = period / statusBreatheStepCount
		if step < minStatusStreamStep {
			step = minStatusStreamStep
		}
	default:
		return fmt.Errorf("unknown status effect kind %q", effect.Kind)
	}
	if step < minStatusStreamStep {
		step = minStatusStreamStep
	}

	var priorBrightness = -1
	for {
		elapsed := time.Since(started)
		if effect.DurationMS > 0 &&
			elapsed >= time.Duration(effect.DurationMS)*time.Millisecond {
			return nil
		}
		phase := math.Mod(float64(elapsed), float64(period)) / float64(period)
		brightness := effect.MinBrightness
		switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
		case "flash":
			if phase < 0.5 {
				brightness = effect.Brightness
			}
		case "breathe":
			// Raised cosine starts at the minimum and has no discontinuity
			// when the cycle wraps.
			level := 0.5 - 0.5*math.Cos(phase*2*math.Pi)
			span := float64(effect.Brightness - effect.MinBrightness)
			brightness = effect.MinBrightness + byte(math.Round(level*span))
		}
		if int(brightness) != priorBrightness {
			if err := scheduler.send(
				ctx,
				native.OpStatusRGB,
				native.StatusRGBPayload(
					effect.Red,
					effect.Green,
					effect.Blue,
					brightness,
				),
			); err != nil {
				return err
			}
			priorBrightness = int(brightness)
		}
		if err := waitOutput(ctx, step); err != nil {
			return err
		}
	}
}

func (scheduler *OutputScheduler) sendStatusDescriptor(
	ctx context.Context,
	running *runningOutput,
	payload []byte,
) error {
	scheduler.statusOperationMu.Lock()
	defer scheduler.statusOperationMu.Unlock()
	scheduler.mu.Lock()
	if scheduler.effect != running {
		scheduler.mu.Unlock()
		return context.Canceled
	}
	if err := ctx.Err(); err != nil {
		scheduler.mu.Unlock()
		return err
	}
	scheduler.statusWireMu.Lock()
	defer scheduler.statusWireMu.Unlock()
	scheduler.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	if err := scheduler.target.Command(
		requestContext,
		native.OpStatusEffect,
		payload,
	); err != nil {
		return err
	}
	running.nativeAccepted = true
	snapshot := scheduler.targetSnapshot()
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.effect != running {
		return context.Canceled
	}
	running.nativeAccepted = true
	running.nativeGeneration = snapshot.ConnectionGeneration
	running.nativeDevice = strings.TrimSpace(snapshot.Port.SerialNumber)
	if running.nativeDevice == "" {
		running.nativeDevice = strings.TrimSpace(snapshot.Port.InstanceID)
	}
	if running.nativeDevice == "" {
		running.nativeDevice = strings.TrimSpace(snapshot.Port.Name)
	}
	scheduler.retainedEffect = nil
	return nil
}

func supportsNativeStatusEffects(target outputCommander) bool {
	reporter, ok := target.(outputCapabilityReporter)
	return ok && reporter.Snapshot().Hello.Capabilities&
		native.CapabilityStatusEffects != 0
}

func supportsNativeStatusProfiles(target outputCommander) bool {
	reporter, ok := target.(outputCapabilityReporter)
	if !ok {
		return false
	}
	capabilities := reporter.Snapshot().Hello.Capabilities
	return capabilities&native.CapabilityStatusEffects != 0 &&
		capabilities&native.CapabilityStatusProfiles != 0
}

func (scheduler *OutputScheduler) releaseNativeStatusEffect(ctx context.Context) error {
	scheduler.statusWireMu.Lock()
	defer scheduler.statusWireMu.Unlock()
	requestContext, cancel := context.WithTimeout(
		ctx,
		outputRequestTimeout,
	)
	defer cancel()
	return scheduler.target.Command(
		requestContext,
		native.OpStatusEffect,
		native.StatusEffectReleasePayload(),
	)
}

// releaseRetainedStatusEffectLocked keeps the owner durable until the release
// ACK arrives. Callers hold scheduler.mu, serializing release with replacement.
func (scheduler *OutputScheduler) releaseRetainedStatusEffectLocked(
	ctx context.Context,
) error {
	owner := scheduler.retainedEffect
	if owner == nil {
		return nil
	}
	owner.releasePending = true
	if err := scheduler.releaseNativeStatusEffect(ctx); err != nil {
		scheduler.target.PublishHostEvent(
			"error",
			fmt.Sprintf(
				"effect %q release failed (id=%d): %v",
				owner.name,
				owner.id,
				err,
			),
		)
		return err
	}
	scheduler.retainedEffect = nil
	scheduler.target.PublishHostEvent(
		"output",
		fmt.Sprintf("effect %q released (id=%d)", owner.name, owner.id),
	)
	return nil
}

func nativeStatusEffect(effect appconfig.StatusLEDEffect) (
	native.StatusEffectOptions,
	time.Duration,
	error,
) {
	kind := byte(0)
	switch strings.ToLower(strings.TrimSpace(effect.Kind)) {
	case "breathe":
		kind = native.StatusEffectBreathe
	case "flash":
		kind = native.StatusEffectFlash
	case "cycle":
		kind = native.StatusEffectCycle
	case "transition":
		kind = native.StatusEffectTransition
	default:
		return native.StatusEffectOptions{}, 0,
			fmt.Errorf("unknown status effect kind %q", effect.Kind)
	}
	repeats := effect.Repeats
	duration := time.Duration(0)
	if repeats != 0 {
		duration = time.Duration(effect.PeriodMS) * time.Millisecond *
			time.Duration(repeats)
	} else if effect.DurationMS > 0 {
		cycles := (effect.DurationMS + effect.PeriodMS - 1) / effect.PeriodMS
		if cycles > 255 {
			return native.StatusEffectOptions{}, 0,
				fmt.Errorf("status effect duration needs %d cycles; maximum is 255", cycles)
		}
		repeats = byte(cycles)
		// Wait for the same whole-cycle duration encoded for the MCU. Restoring
		// the base color at the requested partial duration would truncate its
		// last procedurally generated cycle.
		duration = time.Duration(cycles*effect.PeriodMS) * time.Millisecond
	}
	return native.StatusEffectOptions{
		Kind: kind,
		Red:  effect.Red, Green: effect.Green, Blue: effect.Blue,
		AlternateRed:      effect.AlternateRed,
		AlternateGreen:    effect.AlternateGreen,
		AlternateBlue:     effect.AlternateBlue,
		Brightness:        effect.Brightness,
		MinimumBrightness: effect.MinBrightness,
		PeriodMS:          uint16(effect.PeriodMS), Repeats: repeats,
	}, duration, nil
}

func (scheduler *OutputScheduler) send(
	ctx context.Context,
	opcode byte,
	payload []byte,
) error {
	if opcode == native.OpStatusEffect {
		scheduler.statusWireMu.Lock()
		defer scheduler.statusWireMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	return scheduler.target.Command(requestContext, opcode, payload)
}

func waitOutput(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitPreviousOutput(
	ctx context.Context,
	previousDone <-chan error,
) error {
	if previousDone == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-previousDone:
		return nil
	}
}
