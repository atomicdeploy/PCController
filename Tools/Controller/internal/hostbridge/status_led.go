package hostbridge

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/transition"
)

const (
	statusLEDIdle        = "idle"
	statusLEDRunning     = "running"
	statusLEDBTConnected = "bluetooth-audio-connected"
	statusLEDBTSearching = "bluetooth-audio-searching"
	statusLEDBTOff       = "bluetooth-audio-off"
	statusLEDRF          = "rf-activity"
	statusLEDDoorOpened  = "door-opened"
	statusLEDDoorClosed  = "door-closed"
	statusLEDHot         = "hot"
	statusLEDDoorWarning = "running-door-open"
	statusLEDOffline     = "pc-offline"
)

type statusLEDFrame struct {
	red, green, blue, brightness byte
}

type statusLEDTarget interface {
	SetStatusRGBBase(context.Context, byte, byte, byte, byte) error
	SetStatusRGB(context.Context, byte, byte, byte, byte) error
	SetStatusLEDEffectBase(context.Context, controller.StatusEffectOptions) error
	SetStatusLEDEffect(context.Context, controller.StatusEffectOptions) error
}

// statusLEDArbiter owns only the host policy layer. Explicit streamed effects
// remain scheduler overlays, while firmware retains Boot/HOT/Fault/offline
// fallbacks if the PC or serial link is unavailable.
type statusLEDArbiter struct {
	ctx     context.Context
	target  statusLEDTarget
	onState func(string)
	onError func(error)
	wake    chan struct{}

	mu           sync.Mutex
	policy       appconfig.StatusLEDPolicy
	snapshot     controller.Snapshot
	rfUntil      time.Time
	macroActive  bool
	doorKnown    bool
	doorOpen     bool
	doorCueOpen  bool
	doorCueUntil time.Time
}

func newStatusLEDArbiter(
	ctx context.Context,
	target statusLEDTarget,
	onState func(string),
	onError func(error),
) *statusLEDArbiter {
	return &statusLEDArbiter{
		ctx: ctx, target: target, onState: onState, onError: onError,
		policy: appconfig.DefaultStatusLEDPolicy(), wake: make(chan struct{}, 1),
	}
}

// Observe replaces the latest snapshot and records short-lived door/RF cues
// and macro ownership without blocking the event bridge on serial output.
func (arbiter *statusLEDArbiter) Observe(
	policy appconfig.StatusLEDPolicy,
	snapshot controller.Snapshot,
	event controller.Event,
) {
	now := time.Now()
	arbiter.mu.Lock()
	arbiter.policy = policy
	arbiter.snapshot = snapshot
	if snapshot.Connected && snapshot.HaveStatus {
		changed := arbiter.doorKnown && arbiter.doorOpen != snapshot.Status.DoorOpen
		if changed || event.Kind == "door" {
			arbiter.doorCueOpen = snapshot.Status.DoorOpen
			arbiter.doorCueUntil = now.Add(
				time.Duration(policy.DoorCueHoldMS) * time.Millisecond,
			)
		}
		arbiter.doorKnown = true
		arbiter.doorOpen = snapshot.Status.DoorOpen
	} else {
		// Reconnect establishes a fresh baseline; it is not itself a door edge.
		arbiter.doorKnown = false
		arbiter.doorCueUntil = time.Time{}
	}
	if event.Kind == "rf.receive" {
		arbiter.rfUntil = now.Add(time.Duration(policy.RFHoldMS) * time.Millisecond)
	}
	if event.Kind == "macro" {
		switch strings.ToLower(strings.TrimSpace(event.Lifecycle)) {
		case "started", "running":
			arbiter.macroActive = true
		case "completed", "cancelled", "failed":
			arbiter.macroActive = false
		}
	}
	arbiter.mu.Unlock()
	select {
	case arbiter.wake <- struct{}{}:
	default:
	}
}

// PrepareDisconnect makes a planned loss immediately visible. Unexpected USB
// loss is covered by the MCU's independently timed red offline fallback.
func (arbiter *statusLEDArbiter) PrepareDisconnect(ctx context.Context) error {
	arbiter.mu.Lock()
	visual := arbiter.policy.PCOffline
	arbiter.mu.Unlock()
	frame := statusLEDVisualFrame(visual, 0)
	return arbiter.target.SetStatusRGB(
		ctx, frame.red, frame.green, frame.blue, frame.brightness,
	)
}

func (arbiter *statusLEDArbiter) Run() {
	timer := time.NewTimer(0)
	defer timer.Stop()
	var state string
	var stateStarted, transitionStarted time.Time
	var current, transitionFrom, lastSent statusLEDFrame
	var haveCurrent, haveLastSent, suppressed bool
	var lastNativeEffect controller.StatusEffectOptions
	var haveLastNativeEffect bool
	var nextLegacyFrameAt time.Time
	var macroOverlayPreempted bool
	var lastErrorAt time.Time

	for {
		select {
		case <-arbiter.ctx.Done():
			return
		case <-arbiter.wake:
		case <-timer.C:
		}

		now := time.Now()
		policy, snapshot, rfUntil, doorCueUntil, doorCueOpen, macroActive :=
			arbiter.currentObservation()
		nextState, visual := selectStatusLEDState(
			policy, snapshot, rfUntil, doorCueUntil, doorCueOpen, now,
		)
		if !macroActive {
			macroOverlayPreempted = false
		}
		if nextState == statusLEDDoorWarning {
			macroOverlayPreempted = true
		}
		connected := policy.Enabled && snapshot.Connected && snapshot.HaveStatus
		stateChanged := nextState != state
		if stateChanged {
			state = nextState
			stateStarted = now
			transitionStarted = now
			transitionFrom = current
			if arbiter.onState != nil {
				arbiter.onState(state)
			}
		}

		if !connected {
			// Track the expected firmware fallback color so reconnect easing starts
			// from red instead of an unrelated pre-disconnect host frame.
			current = statusLEDVisualFrame(policy.PCOffline, 0)
			haveCurrent = true
			suppressed = true
			resetStatusLEDTimer(timer, policy.StepMS)
			continue
		}
		if macroActive && !macroOverlayPreempted {
			suppressed = true
			resetStatusLEDTimer(timer, policy.StepMS)
			continue
		}
		resuming := suppressed
		if suppressed {
			transitionFrom = current
			transitionStarted = now
			suppressed = false
		}

		nativeCapable := snapshot.Hello.Capabilities&
			controller.CapabilityStatusEffects != 0
		if nativeCapable {
			nextLegacyFrameAt = time.Time{}
			effect := statusLEDNativeEffect(visual)
			if stateChanged || resuming || !haveLastNativeEffect || effect != lastNativeEffect {
				requestContext, cancel := context.WithTimeout(
					arbiter.ctx, 500*time.Millisecond,
				)
				err := sendStatusLEDNativeEffect(
					requestContext, arbiter.target, state, effect,
				)
				cancel()
				if err == nil {
					lastNativeEffect = effect
					haveLastNativeEffect = true
				} else if arbiter.onError != nil && now.Sub(lastErrorAt) >= 5*time.Second {
					lastErrorAt = now
					arbiter.onError(err)
				}
			}
			current = statusLEDVisualFrame(visual, now.Sub(stateStarted))
			haveCurrent = true
			haveLastSent = false
			resetStatusLEDTimer(timer, policy.StepMS)
			continue
		}
		haveLastNativeEffect = false
		// Board feedback and telemetry events can wake the arbiter much faster
		// than the configured compatibility cadence. Do not let those wakes
		// form a STATUS_RGB -> event -> STATUS_RGB feedback loop.
		if !stateChanged && !resuming && !nextLegacyFrameAt.IsZero() &&
			now.Before(nextLegacyFrameAt) {
			continue
		}

		frame := statusLEDVisualFrame(visual, now.Sub(stateStarted))
		transitionMS := policy.TransitionMS
		if state == statusLEDDoorWarning || state == statusLEDOffline {
			transitionMS = 0
		}
		if haveCurrent && transitionMS > 0 {
			elapsed := now.Sub(transitionStarted)
			duration := time.Duration(transitionMS) * time.Millisecond
			frame = easeStatusLEDTransition(transitionFrom, frame, elapsed, duration)
		}
		current = frame
		haveCurrent = true
		if !haveLastSent || frame != lastSent {
			requestContext, cancel := context.WithTimeout(arbiter.ctx, 500*time.Millisecond)
			err := sendStatusLEDFrame(
				requestContext, arbiter.target, state, frame,
			)
			cancel()
			if err == nil {
				lastSent = frame
				haveLastSent = true
			} else if arbiter.onError != nil && now.Sub(lastErrorAt) >= 5*time.Second {
				lastErrorAt = now
				arbiter.onError(err)
			}
		}
		stepMS := policy.StepMS
		if stepMS < 20 {
			stepMS = 20
		}
		nextLegacyFrameAt = time.Now().Add(time.Duration(stepMS) * time.Millisecond)
		resetStatusLEDTimer(timer, policy.StepMS)
	}
}

func statusLEDNativeEffect(
	visual appconfig.StatusLEDVisual,
) controller.StatusEffectOptions {
	kind := byte(controller.StatusEffectTransition)
	alternate := visual.AlternateColor
	periodMS := visual.PeriodMS
	switch strings.ToLower(strings.TrimSpace(visual.Effect)) {
	case "breathe":
		kind = controller.StatusEffectBreathe
	case "flash":
		kind = controller.StatusEffectFlash
	case "crossfade":
		kind = controller.StatusEffectCycle
	default:
		// The native opcode has no steady kind. A transition whose endpoints
		// match is a constant, locally rendered descriptor with no UART stream.
		alternate = visual.Color
		periodMS = int(controller.StatusEffectMinimumPeriodMS)
	}
	return controller.StatusEffectOptions{
		Kind: kind,
		Red:  visual.Color.Red, Green: visual.Color.Green, Blue: visual.Color.Blue,
		AlternateRed: alternate.Red, AlternateGreen: alternate.Green,
		AlternateBlue:     alternate.Blue,
		Brightness:        visual.Brightness,
		MinimumBrightness: visual.MinimumBrightness,
		PeriodMS:          uint16(periodMS),
	}
}

func sendStatusLEDNativeEffect(
	ctx context.Context,
	target statusLEDTarget,
	state string,
	effect controller.StatusEffectOptions,
) error {
	if state == statusLEDDoorWarning {
		return target.SetStatusLEDEffect(ctx, effect)
	}
	return target.SetStatusLEDEffectBase(ctx, effect)
}

func sendStatusLEDFrame(
	ctx context.Context,
	target statusLEDTarget,
	state string,
	frame statusLEDFrame,
) error {
	if state == statusLEDDoorWarning {
		// A Running+door-open safety warning must preempt an informational
		// streamed overlay instead of waiting for that effect or macro to end.
		return target.SetStatusRGB(
			ctx, frame.red, frame.green, frame.blue, frame.brightness,
		)
	}
	return target.SetStatusRGBBase(
		ctx, frame.red, frame.green, frame.blue, frame.brightness,
	)
}

func (arbiter *statusLEDArbiter) currentObservation() (
	appconfig.StatusLEDPolicy,
	controller.Snapshot,
	time.Time,
	time.Time,
	bool,
	bool,
) {
	arbiter.mu.Lock()
	defer arbiter.mu.Unlock()
	return arbiter.policy, arbiter.snapshot, arbiter.rfUntil,
		arbiter.doorCueUntil, arbiter.doorCueOpen, arbiter.macroActive
}

func selectStatusLEDState(
	policy appconfig.StatusLEDPolicy,
	snapshot controller.Snapshot,
	rfUntil, doorCueUntil time.Time,
	doorCueOpen bool,
	now time.Time,
) (string, appconfig.StatusLEDVisual) {
	if !snapshot.Connected || !snapshot.HaveStatus {
		return statusLEDOffline, policy.PCOffline
	}
	hot := snapshot.Status.TLEDCenti >= policy.HotThresholdCentiC ||
		snapshot.Status.TBTCenti >= policy.HotThresholdCentiC
	if hot {
		return statusLEDHot, policy.Hot
	}
	running := strings.EqualFold(string(snapshot.ProgramState.Mode), "Running")
	if running && snapshot.Status.DoorOpen {
		return statusLEDDoorWarning, policy.RunningDoorOpen
	}
	if now.Before(doorCueUntil) {
		if doorCueOpen {
			return statusLEDDoorOpened, policy.DoorOpened
		}
		return statusLEDDoorClosed, policy.DoorClosed
	}
	if now.Before(rfUntil) {
		return statusLEDRF, policy.RFActivity
	}
	if running {
		return statusLEDRunning, policy.Running
	}
	switch snapshot.Status.BluetoothState {
	case 0:
		return statusLEDBTOff, policy.BluetoothAudioOff
	case 1:
		return statusLEDBTConnected, policy.BluetoothAudioConnected
	case 2:
		return statusLEDBTSearching, policy.BluetoothAudioSearching
	default:
		return statusLEDIdle, policy.Idle
	}
}

func statusLEDVisualFrame(
	visual appconfig.StatusLEDVisual,
	elapsed time.Duration,
) statusLEDFrame {
	effect := strings.ToLower(strings.TrimSpace(visual.Effect))
	phase := 0.0
	if visual.PeriodMS > 0 {
		period := time.Duration(visual.PeriodMS) * time.Millisecond
		phase = math.Mod(float64(elapsed), float64(period)) / float64(period)
	}
	color := visual.Color
	brightness := visual.Brightness
	switch effect {
	case "flash":
		if phase >= 0.5 {
			if visual.AlternateColor != (appconfig.RGBColor{}) {
				color = visual.AlternateColor
			} else {
				brightness = visual.MinimumBrightness
			}
		}
	case "breathe":
		wave := 0.5 - 0.5*math.Cos(phase*2*math.Pi)
		brightness = interpolateByte(
			visual.MinimumBrightness, visual.Brightness, wave,
		)
		if visual.AlternateColor != (appconfig.RGBColor{}) {
			color = interpolateRGB(visual.Color, visual.AlternateColor, wave)
		}
	case "crossfade":
		wave := 0.5 - 0.5*math.Cos(phase*2*math.Pi)
		color = interpolateRGB(visual.Color, visual.AlternateColor, wave)
	}
	return statusLEDFrame{
		red: color.Red, green: color.Green, blue: color.Blue,
		brightness: brightness,
	}
}

func interpolateStatusLEDFrame(
	from, to statusLEDFrame,
	amount float64,
) statusLEDFrame {
	return statusLEDFrame{
		red:        transition.Uint8(from.red, to.red, amount),
		green:      transition.Uint8(from.green, to.green, amount),
		blue:       transition.Uint8(from.blue, to.blue, amount),
		brightness: transition.Uint8(from.brightness, to.brightness, amount),
	}
}

// easeStatusLEDTransition makes informational overlays enter and leave without
// replacing the underlying state abruptly. Critical door/offline states skip
// this helper in Run so their configured warning is immediate.
func easeStatusLEDTransition(
	from, to statusLEDFrame,
	elapsed, duration time.Duration,
) statusLEDFrame {
	if duration <= 0 || elapsed >= duration {
		return to
	}
	if elapsed <= 0 {
		return from
	}
	return interpolateStatusLEDFrame(
		from, to, transition.SmoothStep(float64(elapsed)/float64(duration)),
	)
}

func interpolateRGB(from, to appconfig.RGBColor, amount float64) appconfig.RGBColor {
	return appconfig.RGBColor{
		Red:   interpolateByte(from.Red, to.Red, amount),
		Green: interpolateByte(from.Green, to.Green, amount),
		Blue:  interpolateByte(from.Blue, to.Blue, amount),
	}
}

func interpolateByte(from, to byte, amount float64) byte {
	return transition.Uint8(from, to, amount)
}

func smoothStep(value float64) float64 {
	return transition.SmoothStep(value)
}

func resetStatusLEDTimer(timer *time.Timer, stepMS int) {
	if stepMS < 20 {
		stepMS = 20
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(time.Duration(stepMS) * time.Millisecond)
}
