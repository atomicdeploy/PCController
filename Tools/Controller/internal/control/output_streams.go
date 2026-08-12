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
	maxMelodyRepeats       = 20
	outputRequestTimeout   = 2 * time.Second
	minStatusStreamStep    = 50 * time.Millisecond
	statusBreatheStepCount = 32
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
	MelodyID       uint64  `json:"melody_id,omitempty"`
	MelodyName     string  `json:"melody_name,omitempty"`
	EffectID       uint64  `json:"effect_id,omitempty"`
	EffectName     string  `json:"effect_name,omitempty"`
	StatusBase     [4]byte `json:"status_base"`
	HaveStatusBase bool    `json:"have_status_base"`
}

type runningOutput struct {
	id     uint64
	name   string
	cancel context.CancelFunc
	done   chan error
}

// OutputScheduler streams high-level PC-side effects through existing native
// opcodes. Melody and status-LED lanes are independent; starting a new item on
// a lane cancels the previous one. Every command waits for its ACK and streams
// are rate-limited, so the MCU's small queues cannot be flooded.
type OutputScheduler struct {
	target outputCommander
	root   context.Context
	cancel context.CancelFunc

	mu                   sync.Mutex
	nextID               uint64
	closed               bool
	melody               *runningOutput
	effect               *runningOutput
	statusBase           [4]byte
	haveStatusBase       bool
	statusBaseEffect     native.StatusEffectOptions
	haveStatusBaseEffect bool
}

func NewOutputScheduler(target outputCommander) *OutputScheduler {
	ctx, cancel := context.WithCancel(context.Background())
	return &OutputScheduler{target: target, root: ctx, cancel: cancel}
}

func (scheduler *OutputScheduler) Close() {
	scheduler.mu.Lock()
	if scheduler.closed {
		scheduler.mu.Unlock()
		return
	}
	scheduler.closed = true
	scheduler.cancel()
	if scheduler.melody != nil {
		scheduler.melody.cancel()
	}
	if scheduler.effect != nil {
		scheduler.effect.cancel()
	}
	scheduler.mu.Unlock()
}

func (scheduler *OutputScheduler) State() OutputStreamState {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	var state OutputStreamState
	if scheduler.melody != nil {
		state.MelodyID = scheduler.melody.id
		state.MelodyName = scheduler.melody.name
	}
	if scheduler.effect != nil {
		state.EffectID = scheduler.effect.id
		state.EffectName = scheduler.effect.name
	}
	state.StatusBase = scheduler.statusBase
	state.HaveStatusBase = scheduler.haveStatusBase
	return state
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
	scheduler.haveStatusBaseEffect = false
	if scheduler.effect != nil {
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

// SetStatusEffectBase updates the host policy base with one MCU-rendered
// descriptor. Explicit user/macro effects retain the overlay lane; when one is
// active, the descriptor is remembered and restored only after it finishes.
func (scheduler *OutputScheduler) SetStatusEffectBase(
	ctx context.Context,
	effect native.StatusEffectOptions,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload, err := native.StatusEffectPayload(effect)
	if err != nil {
		return err
	}
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	if scheduler.closed {
		return errors.New("output scheduler is closed")
	}
	scheduler.statusBase = [4]byte{
		effect.Red, effect.Green, effect.Blue, effect.Brightness,
	}
	scheduler.haveStatusBase = true
	scheduler.statusBaseEffect = effect
	scheduler.haveStatusBaseEffect = true
	if scheduler.effect != nil {
		return nil
	}
	requestContext, cancel := context.WithTimeout(ctx, outputRequestTimeout)
	defer cancel()
	return scheduler.target.Command(
		requestContext,
		native.OpStatusEffect,
		payload,
	)
}

func (scheduler *OutputScheduler) StatusEffectActive() bool {
	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()
	return scheduler.effect != nil
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
		if err := waitPreviousOutput(runContext, previousDone); err != nil {
			scheduler.finish("effect", running, err, nil)
			return
		}
		err := scheduler.streamStatusEffect(runContext, effect)
		restore := func() {
			requestContext, cancel := context.WithTimeout(
				context.Background(),
				outputRequestTimeout,
			)
			defer cancel()
			opcode := byte(native.OpStatusRGB)
			payload := native.StatusRGBPayload(
				effect.Red,
				effect.Green,
				effect.Blue,
				effect.Brightness,
			)
			if scheduler.haveStatusBase {
				payload = native.StatusRGBPayload(
					scheduler.statusBase[0],
					scheduler.statusBase[1],
					scheduler.statusBase[2],
					scheduler.statusBase[3],
				)
			}
			if scheduler.haveStatusBaseEffect {
				opcode = native.OpStatusEffect
				payload, _ = native.StatusEffectPayload(
					scheduler.statusBaseEffect,
				)
			}
			_ = scheduler.target.Command(
				requestContext,
				opcode,
				payload,
			)
		}
		scheduler.finish("effect", running, err, restore)
	}()
	return operation, nil
}

func (scheduler *OutputScheduler) StopMelody() bool {
	return scheduler.stop("melody")
}

func (scheduler *OutputScheduler) StopStatusEffect() bool {
	return scheduler.stop("effect")
}

// OverrideStatusEffect cancels the animation and clears its lane immediately,
// preventing the canceled stream's steady-color cleanup from racing a
// caller's explicit RGB write.
func (scheduler *OutputScheduler) OverrideStatusEffect() bool {
	scheduler.mu.Lock()
	running := scheduler.effect
	if running != nil {
		scheduler.effect = nil
		running.cancel()
	}
	scheduler.mu.Unlock()
	if running != nil {
		scheduler.target.PublishHostEvent(
			"output",
			fmt.Sprintf(
				"effect %q overridden by steady RGB (id=%d)",
				running.name,
				running.id,
			),
		)
		return true
	}
	return false
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
	defer scheduler.mu.Unlock()
	slot := scheduler.melody
	if kind == "effect" {
		slot = scheduler.effect
	}
	if slot == nil {
		return false
	}
	slot.cancel()
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
	// Never let an older canceled animation overwrite the first frame of its
	// replacement. On explicit stop or natural completion, leave a useful
	// steady base color instead of a dim breath/blank flash frame. The restore
	// and slot clear are serialized with replace/override so a new effect
	// cannot start in the small gap between those operations.
	if isCurrent && restore != nil {
		restore()
	}
	if isCurrent {
		*slot = nil
	}
	scheduler.mu.Unlock()
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
	effect appconfig.StatusLEDEffect,
) error {
	if reporter, ok := scheduler.target.(outputCapabilityReporter); ok &&
		reporter.Snapshot().Hello.Capabilities&native.CapabilityStatusEffects != 0 {
		options, duration, err := nativeStatusEffect(effect)
		if err != nil {
			return err
		}
		payload, err := native.StatusEffectPayload(options)
		if err != nil {
			return err
		}
		if err := scheduler.send(ctx, native.OpStatusEffect, payload); err != nil {
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
