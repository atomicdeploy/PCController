package control

import (
	"context"
	"errors"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
)

// ErrWelcomeSkipped is a normal handoff: startup must not steal active outputs.
var ErrWelcomeSkipped = errors.New("welcome outputs are already in use")

// RunWelcome reserves only idle melody/strip lanes. A user replacement on either
// lane cancels the whole show. It never changes relays, PWM, settings or EEPROM.
func (s *OutputScheduler) RunWelcome(parent context.Context, melody appconfig.Melody) (result error) {
	if err := parent.Err(); err != nil {
		return err
	}
	if err := appconfig.ValidateMelody(melody); err != nil {
		return err
	}
	initial := s.targetSnapshot()
	busy, known := native.BuzzerBusy(initial.Hello, initial.Status)
	if !initial.Connected || !initial.HaveStatus || initial.Paused || initial.EmergencyStop.Active ||
		initial.Programming.Active || initial.Programming.ProgrammingLatch || initial.Status.Hot ||
		initial.ProgramState.Mode == ProgramRunning || initial.Status.ProgramRunning || !known || busy {
		return ErrWelcomeSkipped
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()
	stopRoot := context.AfterFunc(s.root, cancel)
	defer stopRoot()
	stripAvailable := initial.Hello.Capabilities&native.CapabilityAddressableLED != 0
	s.mu.Lock()
	if s.closed || s.melody != nil || s.strip != nil {
		s.mu.Unlock()
		return ErrWelcomeSkipped
	}
	makeLane := func() *runningOutput {
		s.nextID++
		return &runningOutput{id: s.nextID, name: "Welcome", cancel: cancel, done: make(chan error, 1)}
	}
	music, strip := makeLane(), makeLane()
	s.melody = music
	s.reportActivity("melody", true)
	if stripAvailable {
		s.strip = strip
		s.reportActivity("strip", true)
	}
	s.mu.Unlock()
	stripStarted, toneStarted := false, false
	sameBoard := func() bool {
		live := s.targetSnapshot()
		return live.Connected && !live.Paused && live.ConnectionGeneration == initial.ConnectionGeneration &&
			!live.Programming.Active && !live.Programming.ProgrammingLatch && !live.EmergencyStop.Active
	}
	defer func() {
		// Fence cleanup by both lane identity and connection generation. Never
		// clear a successor's frame or stop its tone after Skip/replacement.
		s.stripMu.Lock()
		s.mu.Lock()
		ownMusic := s.melody == music
		ownStrip := s.strip == strip && !strip.stopRequested
		s.mu.Unlock()
		cleanup, done := context.WithTimeout(context.Background(), time.Second)
		if sameBoard() {
			if ownMusic && toneStarted {
				result = errors.Join(result, s.send(cleanup, native.OpBuzzer, native.BuzzerPayload(0, 0)))
			}
			if ownStrip && stripStarted {
				payload, _ := native.AddressableLEDPayload(native.AddressableLEDFill, 0, 0, 0, 0)
				result = errors.Join(result, s.sendStrip(cleanup, payload))
			}
		}
		done()
		s.stripMu.Unlock()
		s.finish("melody", music, result, nil)
		s.finish("strip", strip, result, nil)
	}()
	if stripAvailable {
		payload, _ := native.StripConfigurePayload(native.StripMaximumPixels)
		s.stripMu.Lock()
		err := s.sendStrip(ctx, payload)
		s.stripMu.Unlock()
		if err != nil {
			return err
		} // Busy retained recordings are never cleared.
		stripStarted = true
	}
	started, nextNote, nextFrame := time.Now(), time.Now(), time.Now()
	noteIndex := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sameBoard() {
			return context.Canceled
		}
		live := s.targetSnapshot()
		if live.Status.Hot || live.ProgramState.Mode == ProgramRunning || live.Status.ProgramRunning {
			return ErrWelcomeSkipped
		}
		now := time.Now()
		if noteIndex < len(melody.Notes) && !now.Before(nextNote) {
			note := melody.Notes[noteIndex]
			// Unknown settings are not permission to sound; firmware Silent remains
			// authoritative, and a mid-show mute suppresses subsequent notes.
			if live.HaveSettings && live.Settings.Flags&native.SettingsSilent == 0 {
				toneStarted = true
				if err := s.send(ctx, native.OpBuzzer, native.BuzzerPayload(note.FrequencyHz, note.DurationMS)); err != nil {
					return err
				}
			}
			nextNote = time.Now().Add(time.Duration(note.DurationMS+note.GapMS) * time.Millisecond)
			noteIndex++
		}
		elapsed := time.Since(started)
		if noteIndex == len(melody.Notes) && !time.Now().Before(nextNote) && elapsed >= 3200*time.Millisecond {
			return nil
		}
		if stripAvailable && !now.Before(nextFrame) {
			rgb := stripRainbowFrame(native.StripMaximumPixels, byte(elapsed.Milliseconds()/12))
			// Gentle fade-in and limited peak brightness; this is not a full-power
			// strip diagnostic. A slow serial link drops frames, never queues them.
			level := min(72, int(elapsed.Milliseconds())*72/500)
			if noteIndex == len(melody.Notes) {
				remaining := max(time.Until(nextNote), 3200*time.Millisecond-elapsed)
				level = min(level, max(0, int(remaining.Milliseconds())*72/450))
			}
			for i := range rgb {
				rgb[i] = byte(int(rgb[i]) * level / 255)
			}
			if err := s.sendStripFrame(ctx, rgb); err != nil {
				return err
			}
			nextFrame = time.Now().Add(80 * time.Millisecond)
		}
		if err := waitOutput(ctx, 15*time.Millisecond); err != nil {
			return err
		}
	}
}
