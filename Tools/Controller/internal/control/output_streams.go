package control

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

const (
	maxMelodyRepeats           = 20
	outputRequestTimeout       = 2 * time.Second
	outputCloseReleaseAttempts = 3
)

type outputCommander interface {
	Command(context.Context, byte, []byte) error
	PublishHostEvent(string, string)
}

type outputCapabilityReporter interface {
	Snapshot() Snapshot
}

type outputActivityReporter interface {
	setOutputActivity(string, bool)
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
	StripID               uint64  `json:"strip_id,omitempty"`
	StripName             string  `json:"strip_name,omitempty"`
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
	strip             *runningOutput
	stripMu           sync.Mutex
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
		if scheduler.strip != nil {
			scheduler.strip.cancel()
		}
	}
	scheduler.mu.Unlock()

	var err error
	for range outputCloseReleaseAttempts {
		hadOwner, releaseErr := scheduler.releaseRetainedStatusEffectUnderOperation(
			context.Background(), false,
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
	if scheduler.strip != nil {
		state.StripID = scheduler.strip.id
		state.StripName = scheduler.strip.name
	}
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

func (scheduler *OutputScheduler) retainedRunning(running *runningOutput) *retainedOutput {
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
		// The board owns its live profile. Cache this only as a host fallback.
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

// ClearStatusBase forgets host fallback state without changing board ownership.
func (scheduler *OutputScheduler) ClearStatusBase() {
	scheduler.mu.Lock()
	scheduler.statusBase = [4]byte{}
	scheduler.haveStatusBase = false
	scheduler.mu.Unlock()
}

// ReplaceStatusRGB transfers ownership to an explicitly requested steady
// preview. Existing ownership remains durable until the RGB command is ACKed.
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
	err := scheduler.target.Command(
		requestContext,
		native.OpStatusRGB,
		native.StatusRGBPayload(red, green, blue, brightness),
	)
	scheduler.statusWireMu.Unlock()
	if err != nil {
		return err
	}

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	running := scheduler.effect
	retained := scheduler.retainedEffect
	if running != nil {
		scheduler.effect = nil
		running.cancel()
		scheduler.reportActivity("effect", false)
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
			fmt.Sprintf("effect %q replaced by steady RGB (id=%d)", running.name, running.id),
		)
	} else if retained != nil {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf("effect %q replaced by steady RGB (id=%d)", retained.name, retained.id),
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
	reporter, ok := scheduler.target.(outputCapabilityReporter)
	if !ok || reporter.Snapshot().Hello.Capabilities&native.CapabilityStatusEffects == 0 {
		return StreamOperation{}, errors.New("connected firmware does not advertise status effects")
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

// completeStatusEffect commits terminal ownership without holding scheduler.mu
// across transport I/O. A finite native effect remains board-owned until an
// explicit replacement or release; canceled or failed descriptors are released.
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
				context.Background(), false,
			)
		}
		return
	}
	if !started {
		scheduler.mu.Unlock()
		return
	}
	payload := native.StatusRGBPayload(effect.Red, effect.Green, effect.Blue, effect.Brightness)
	if scheduler.haveStatusBase {
		payload = native.StatusRGBPayload(
			scheduler.statusBase[0], scheduler.statusBase[1],
			scheduler.statusBase[2], scheduler.statusBase[3],
		)
	}
	scheduler.mu.Unlock()

	requestContext, cancel := context.WithTimeout(context.Background(), outputRequestTimeout)
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

// ReleaseStatusEffect synchronously returns a retained board effect/preview to
// the board's native lifecycle. Failed ACKs remain represented for retry.
func (scheduler *OutputScheduler) ReleaseStatusEffect(ctx context.Context) (bool, error) {
	return scheduler.releaseStatusEffect(ctx, false)
}

// ReconcileStatusEffect forces one release at a reconnect/programming boundary,
// even when the process lost its local ownership record.
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
	return scheduler.releaseRetainedStatusEffectUnderOperation(ctx, force)
}

// OverrideStatusEffect cancels the animation and clears its lane immediately,
// preventing the canceled stream's steady-color cleanup from racing a
// caller's explicit RGB write.
func (scheduler *OutputScheduler) OverrideStatusEffect() bool {
	return scheduler.StopStatusEffect()
}

func (scheduler *OutputScheduler) StopAll() {
	scheduler.StopMelody()
	scheduler.StopStatusEffect()
	scheduler.stop("strip")
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
	if scheduler.targetSnapshot().EmergencyStop.Active {
		return StreamOperation{}, nil, nil, nil, ErrEmergencyStopActive
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return StreamOperation{}, nil, nil, nil,
			errors.New("output scheduler is closed")
	}
	slot := &scheduler.melody
	if kind == "effect" {
		slot = &scheduler.effect
		// Keep the last acknowledged owner until its replacement descriptor is
		// ACKed. A failed replacement therefore leaves a releasable owner.
		if *slot != nil && (*slot).nativeAccepted && scheduler.retainedEffect == nil {
			scheduler.retainedEffect = scheduler.retainedRunning(*slot)
		}
	}
	if kind == "strip" {
		slot = &scheduler.strip
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
	scheduler.reportActivity(kind, true)
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
			released, _ := scheduler.ReleaseStatusEffect(context.Background())
			return released
		}
	}
	if kind == "strip" {
		slot = scheduler.strip
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
	if kind == "strip" {
		slot = &scheduler.strip
	}
	isCurrent := *slot == operation
	if isCurrent {
		*slot = nil
		scheduler.reportActivity(kind, false)
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

func (scheduler *OutputScheduler) reportActivity(kind string, active bool) {
	if reporter, ok := scheduler.target.(outputActivityReporter); ok {
		reporter.setOutputActivity(kind, active)
	}
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
	// Keep cadence on one monotonic timeline. Waiting a complete note interval
	// after every acknowledged command adds USB/bridge round-trip latency to
	// every note and audibly stretches melodies on the host and board alike.
	nextNoteAt := time.Now()
	for repeat := 0; repeats == 0 || repeat < repeats; repeat++ {
		for _, note := range melody.Notes {
			if err := scheduler.send(
				ctx,
				native.OpBuzzer,
				native.BuzzerPayload(note.FrequencyHz, note.DurationMS),
			); err != nil {
				return err
			}
			nextNoteAt = nextNoteAt.Add(
				time.Duration(note.DurationMS+note.GapMS) * time.Millisecond,
			)
			if err := waitOutput(ctx, time.Until(nextNoteAt)); err != nil {
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
	reporter, ok := scheduler.target.(outputCapabilityReporter)
	if !ok || reporter.Snapshot().Hello.Capabilities&native.CapabilityStatusEffects == 0 {
		return errors.New("connected firmware does not advertise status effects")
	}
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
	running.nativeAttempted = true
	scheduler.statusWireMu.Lock()
	scheduler.mu.Unlock()
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	err := scheduler.target.Command(requestContext, native.OpStatusEffect, payload)
	scheduler.statusWireMu.Unlock()
	if err != nil {
		return err
	}

	snapshot := scheduler.targetSnapshot()
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	running.nativeAccepted = true
	running.nativeGeneration = snapshot.ConnectionGeneration
	running.nativeDevice = strings.TrimSpace(snapshot.Port.SerialNumber)
	if running.nativeDevice == "" {
		running.nativeDevice = strings.TrimSpace(snapshot.Port.InstanceID)
	}
	if running.nativeDevice == "" {
		running.nativeDevice = strings.TrimSpace(snapshot.Port.Name)
	}
	if scheduler.effect != running {
		return context.Canceled
	}
	scheduler.retainedEffect = nil
	return nil
}

func supportsNativeStatusEffects(target outputCommander) bool {
	reporter, ok := target.(outputCapabilityReporter)
	return ok && reporter.Snapshot().Hello.Capabilities&native.CapabilityStatusEffects != 0
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
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	return scheduler.target.Command(
		requestContext,
		native.OpStatusEffect,
		native.StatusEffectReleasePayload(),
	)
}

// releaseRetainedStatusEffectUnderOperation expects statusOperationMu to be
// held. It keeps ownership durable until an ACK arrives so callers can retry.
func (scheduler *OutputScheduler) releaseRetainedStatusEffectUnderOperation(
	ctx context.Context,
	force bool,
) (bool, error) {
	scheduler.mu.Lock()
	owner := scheduler.retainedEffect
	if owner == nil && scheduler.effect != nil &&
		(scheduler.effect.nativeAttempted || scheduler.effect.nativeAccepted) {
		running := scheduler.effect
		running.stopRequested = true
		running.cancel()
		owner = scheduler.retainedRunning(running)
		scheduler.retainedEffect = owner
	}
	if owner == nil && !supportsNativeStatusEffects(scheduler.target) {
		scheduler.mu.Unlock()
		return false, nil
	}
	if owner == nil && !force {
		scheduler.mu.Unlock()
		return false, nil
	}
	if owner != nil {
		owner.releasePending = true
	}
	scheduler.mu.Unlock()

	if err := scheduler.releaseNativeStatusEffect(ctx); err != nil {
		if owner != nil {
			scheduler.target.PublishHostEvent(
				"error",
				fmt.Sprintf("effect %q release failed (id=%d): %v", owner.name, owner.id, err),
			)
		}
		return true, err
	}

	scheduler.mu.Lock()
	if owner != nil && scheduler.retainedEffect == owner {
		scheduler.retainedEffect = nil
	}
	scheduler.mu.Unlock()
	if owner != nil {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf("effect %q released (id=%d)", owner.name, owner.id),
		)
	}
	return true, nil
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
