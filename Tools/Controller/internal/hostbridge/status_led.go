package hostbridge

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
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
	ClearStatusRGBBase()
	OutputState() controller.OutputStreamState
	ReleaseStatusLEDEffect(context.Context) error
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
	wasEnabled   bool
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
		wasEnabled: true,
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
	if !policy.Enabled {
		arbiter.rfUntil = time.Time{}
		arbiter.macroActive = false
		arbiter.doorKnown = false
		arbiter.doorCueUntil = time.Time{}
	}
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

// PrepareDisconnect releases only an explicit native preview/effect owner.
// Native lifecycle boards receive no STATUS_RGB: firmware owns their planned
// and unexpected offline presentation. Legacy boards receive one deterministic
// offline fallback frame while the transport is still available.
func (arbiter *statusLEDArbiter) PrepareDisconnect(ctx context.Context) error {
	arbiter.mu.Lock()
	policy, snapshot := arbiter.policy, arbiter.snapshot
	arbiter.resetConnectionStateLocked()
	arbiter.mu.Unlock()
	arbiter.target.ClearStatusRGBBase()
	if nativeStatusLEDLifecycle(snapshot) {
		return arbiter.releaseExplicitNativeOwner(ctx)
	}
	frame := statusLEDVisualFrame(policy.PCOffline, 0)
	return arbiter.target.SetStatusRGB(
		ctx, frame.red, frame.green, frame.blue, frame.brightness,
	)
}

func (arbiter *statusLEDArbiter) resetConnectionStateLocked() {
	arbiter.snapshot = controller.Snapshot{}
	arbiter.rfUntil = time.Time{}
	arbiter.macroActive = false
	arbiter.doorKnown = false
	arbiter.doorOpen = false
	arbiter.doorCueOpen = false
	arbiter.doorCueUntil = time.Time{}
}

func (arbiter *statusLEDArbiter) releaseExplicitNativeOwner(ctx context.Context) error {
	switch arbiter.target.OutputState().StatusOwner {
	case "board-preview", "board-effect":
		return arbiter.target.ReleaseStatusLEDEffect(ctx)
	default:
		return nil
	}
}

func nativeStatusLEDLifecycle(snapshot controller.Snapshot) bool {
	want := uint32(native.CapabilityStatusEffects | native.CapabilityStatusProfiles)
	return snapshot.Connected && snapshot.Hello.Capabilities&want == want
}

func (arbiter *statusLEDArbiter) Run() {
	timer := time.NewTimer(0)
	defer timer.Stop()
	var state string
	var stateStarted, transitionStarted time.Time
	var current, transitionFrom, lastSent statusLEDFrame
	var haveCurrent, haveLastSent, suppressed, nativeOwned, wasConnected bool
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
		boardOwned := nativeStatusLEDLifecycle(snapshot)
		if snapshot.Connected != wasConnected {
			wasConnected = snapshot.Connected
			haveCurrent = false
			haveLastSent = false
			suppressed = true
			arbiter.target.ClearStatusRGBBase()
		}
		if boardOwned != nativeOwned {
			nativeOwned = boardOwned
			haveCurrent = false
			haveLastSent = false
			suppressed = true
			arbiter.target.ClearStatusRGBBase()
		}
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
		if nextState != state {
			state = nextState
			stateStarted = now
			transitionStarted = now
			transitionFrom = current
			if arbiter.onState != nil {
				arbiter.onState(state)
			}
		}

		if !policy.Enabled {
			if arbiter.wasEnabled {
				arbiter.target.ClearStatusRGBBase()
			}
			arbiter.wasEnabled = false
			haveCurrent = false
			haveLastSent = false
			suppressed = true
			resetStatusLEDTimer(timer, policy.StepMS)
			continue
		}
		arbiter.wasEnabled = true
		if boardOwned {
			// Effects+Profiles is the complete lifecycle contract. Firmware owns
			// normal, safety, door, and offline states; host policy never steals it.
			resetStatusLEDTimer(timer, policy.StepMS)
			continue
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
		if suppressed {
			transitionFrom = current
			transitionStarted = now
			suppressed = false
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
		resetStatusLEDTimer(timer, policy.StepMS)
	}
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
	phase := statusLEDEffectPhase(visual.PeriodMS, elapsed)
	color := visual.Color
	brightness := visual.Brightness
	switch effect {
	case "flash":
		if phase >= 128 {
			color = visual.AlternateColor
		}
	case "breathe":
		triangle := statusLEDTriangle(phase)
		brightness = visual.MinimumBrightness + statusLEDScale(
			visual.Brightness-visual.MinimumBrightness, triangle,
		)
	case "cycle", "crossfade":
		color = interpolateStatusLEDRGB(
			visual.Color, visual.AlternateColor, statusLEDTriangle(phase),
		)
	case "transition":
		color = interpolateStatusLEDRGB(visual.Color, visual.AlternateColor, phase)
	}
	return statusLEDFrame{
		red: color.Red, green: color.Green, blue: color.Blue,
		brightness: brightness,
	}
}

// statusLEDEffectPhase reproduces the AVR compositor's 64 discrete phases.
func statusLEDEffectPhase(periodMS int, elapsed time.Duration) byte {
	if periodMS <= 0 || elapsed <= 0 {
		return 0
	}
	period := time.Duration(periodMS) * time.Millisecond
	withinMS := int((elapsed % period) / time.Millisecond)
	step := 0
	for next := 1; next < 64; next++ {
		if withinMS < (next*periodMS)/64 {
			break
		}
		step = next
	}
	return byte(step * 4)
}

func statusLEDTriangle(phase byte) byte {
	if phase < 128 {
		return phase << 1
	}
	return (255 - phase) << 1
}

func statusLEDScale(value, level byte) byte {
	return byte((uint16(value) * (uint16(level) + 1)) >> 8)
}

func interpolateStatusLEDByte(from, to, phase byte) byte {
	if to >= from {
		return from + byte((uint16(to-from)*uint16(phase))/256)
	}
	return from - byte((uint16(from-to)*uint16(phase))/256)
}

func interpolateStatusLEDRGB(from, to appconfig.RGBColor, phase byte) appconfig.RGBColor {
	return appconfig.RGBColor{
		Red:   interpolateStatusLEDByte(from.Red, to.Red, phase),
		Green: interpolateStatusLEDByte(from.Green, to.Green, phase),
		Blue:  interpolateStatusLEDByte(from.Blue, to.Blue, phase),
	}
}

func interpolateStatusLEDFrame(
	from, to statusLEDFrame,
	amount float64,
) statusLEDFrame {
	return statusLEDFrame{
		red:        interpolateByte(from.red, to.red, amount),
		green:      interpolateByte(from.green, to.green, amount),
		blue:       interpolateByte(from.blue, to.blue, amount),
		brightness: interpolateByte(from.brightness, to.brightness, amount),
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
		from, to, smoothStep(float64(elapsed)/float64(duration)),
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
	if amount <= 0 {
		return from
	}
	if amount >= 1 {
		return to
	}
	return byte(math.Round(float64(from) + (float64(to)-float64(from))*amount))
}

func smoothStep(value float64) float64 {
	if value <= 0 {
		return 0
	}
	if value >= 1 {
		return 1
	}
	return value * value * (3 - 2*value)
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
