package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

type StripEffectDescriptor struct {
	ID                string                 `json:"id"`
	Name              string                 `json:"name"`
	Category          string                 `json:"category,omitempty"`
	Description       string                 `json:"description"`
	Program           appconfig.StripProgram `json:"program"`
	Engine            string                 `json:"engine"`
	Editable          bool                   `json:"editable"`
	DefaultFPS        int                    `json:"default_fps"`
	DefaultDurationMS int                    `json:"default_duration_ms"`
	DefaultPixels     int                    `json:"default_pixels"`
	MinPixels         int                    `json:"min_pixels"`
	MaxPixels         int                    `json:"max_pixels"`
	MinFPS            int                    `json:"min_fps"`
	MaxFPS            int                    `json:"max_fps"`
}

type stripEffectDefinition = StripEffectDescriptor

func configuredStripEffectDescriptors(definitions []appconfig.StripEffect) []StripEffectDescriptor {
	result := make([]StripEffectDescriptor, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, StripEffectDescriptor{
			ID: definition.ID, Name: definition.Name, Category: definition.Category,
			Description: definition.Description, Program: definition.Program,
			Engine: "host-stream", Editable: true, DefaultFPS: definition.DefaultFPS,
			DefaultDurationMS: definition.DefaultDurationMS, DefaultPixels: definition.DefaultPixels,
			MinPixels: 1, MaxPixels: native.StripMaximumPixels, MinFPS: 1, MaxFPS: 30,
		})
	}
	return result
}

func StripEffectDescriptors() []StripEffectDescriptor {
	return configuredStripEffectDescriptors(appconfig.DefaultStripEffects())
}

func SupportedStripEffectDescriptors(connected bool, capabilities uint32) []StripEffectDescriptor {
	if !connected || capabilities&native.CapabilityAddressableLED == 0 {
		return nil
	}
	return StripEffectDescriptors()
}

func SupportedConfiguredStripEffectDescriptors(connected bool, capabilities uint32, definitions []appconfig.StripEffect) []StripEffectDescriptor {
	if !connected || capabilities&native.CapabilityAddressableLED == 0 {
		return nil
	}
	return configuredStripEffectDescriptors(definitions)
}

type stripEffectRenderer func(count int, elapsed time.Duration) []byte

const stripStreamConsecutiveTimeoutLimit = 3

func stripCommandError(err error) error {
	var remote *link.RemoteError
	if errors.As(err, &remote) {
		switch remote.Code {
		case native.ErrorBusy:
			return fmt.Errorf("board is busy; wait for startup, or stop/save the MCU macro and run 'macro buffer clear' to release strip memory: %w", err)
		case native.ErrorBadPayload:
			return fmt.Errorf("connected firmware rejected staged strip streaming; update the board firmware before using strip config, frame, rainbow, or effects: %w", err)
		}
	}
	return err
}

func (outputs *OutputScheduler) sendStrip(ctx context.Context, payload []byte) error {
	return stripCommandError(outputs.send(ctx, native.OpAddressableLED, payload))
}

func stripStreamCommand(ctx context.Context, outputs *OutputScheduler, args []string, configured ...[]appconfig.StripEffect) (string, error) {
	_ = configured
	if len(args) == 0 {
		return "", fmt.Errorf("usage: strip config COUNT | frame RGBHEX | rainbow [COUNT [FPS]] | stop | status")
	}
	switch strings.ToLower(args[0]) {
	case "status":
		if len(args) != 1 {
			break
		}
		state := outputs.State()
		data, err := json.Marshal(map[string]any{"running": state.StripID != 0, "id": state.StripID, "name": state.StripName, "maximum_pixels": native.StripMaximumPixels})
		return string(data), err
	case "stop":
		if len(args) != 1 {
			break
		}
		outputs.stop("strip")
		// Wait for the last in-flight frame before acknowledging stop.
		outputs.stripMu.Lock()
		outputs.stripMu.Unlock()
		return "strip streaming stopped", nil
	case "config":
		if len(args) != 2 {
			break
		}
		count, err := strconv.Atoi(args[1])
		if err != nil {
			return "", fmt.Errorf("strip count must be an integer")
		}
		payload, err := native.StripConfigurePayload(count)
		if err != nil {
			return "", err
		}
		outputs.stop("strip")
		outputs.stripMu.Lock()
		defer outputs.stripMu.Unlock()
		if err := outputs.sendStrip(ctx, payload); err != nil {
			return "", err
		}
		return fmt.Sprintf("strip configured: %d LEDs", count), nil
	case "frame":
		if len(args) != 2 {
			break
		}
		rgb, err := hex.DecodeString(strings.TrimPrefix(args[1], "#"))
		if err != nil {
			return "", fmt.Errorf("frame must be RGB hex triples: %w", err)
		}
		if _, err = native.StripFramePayloads(rgb); err != nil {
			return "", err
		}
		outputs.stop("strip")
		if err = outputs.sendStripFrame(ctx, rgb); err != nil {
			return "", err
		}
		return fmt.Sprintf("strip frame displayed: %d LEDs", len(rgb)/3), nil
	case "rainbow":
		if len(args) > 3 {
			break
		}
		count, fps := 100, 20
		var err error
		if len(args) > 1 {
			count, err = strconv.Atoi(args[1])
			if err != nil {
				return "", err
			}
		}
		if len(args) > 2 {
			fps, err = strconv.Atoi(args[2])
			if err != nil {
				return "", err
			}
		}
		payload, err := native.StripConfigurePayload(count)
		if err != nil {
			return "", err
		}
		if fps < 1 || fps > 30 {
			return "", fmt.Errorf("strip FPS must be 1..30")
		}
		// Validate configuration synchronously, so offline/old firmware never reports a started stream.
		outputs.stop("strip")
		outputs.stripMu.Lock()
		err = outputs.sendStrip(ctx, payload)
		outputs.stripMu.Unlock()
		if err != nil {
			return "", err
		}
		operation, runContext, running, previous, err := outputs.replace("strip", fmt.Sprintf("rainbow %d LEDs at %d FPS", count, fps))
		if err != nil {
			return "", err
		}
		go func() {
			err := waitPreviousOutput(runContext, previous)
			if err == nil {
				err = outputs.streamStripRainbow(runContext, count, fps)
			}
			outputs.finish("strip", running, err, nil)
		}()
		return fmt.Sprintf("strip rainbow started (id=%d)", operation.ID), nil
	}
	return "", fmt.Errorf("usage: strip config COUNT | frame RGBHEX | rainbow [COUNT [FPS]] | stop | status")
}

func playStripProgramCommand(ctx context.Context, outputs *OutputScheduler, id string, args []string, definitions []appconfig.StripEffect) (string, error) {
	if len(args) > 2 {
		return "", errors.New("usage: effect play ID [COUNT [FPS]]")
	}
	definition, program, ok := stripEffectByID(id, definitions)
	if !ok {
		return "", fmt.Errorf("effect %q is not configured", id)
	}
	count, fps := definition.DefaultPixels, definition.DefaultFPS
	var err error
	if len(args) > 0 {
		count, err = strconv.Atoi(args[0])
		if err != nil {
			return "", errors.New("LED count must be an integer")
		}
	}
	if len(args) > 1 {
		fps, err = strconv.Atoi(args[1])
		if err != nil {
			return "", errors.New("frame rate must be an integer")
		}
	}
	operation, err := outputs.startStripEffect(ctx, definition, program, count, fps)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("effect effect:%s started (id=%d)", definition.ID, operation.ID), nil
}

func stripEffectByID(id string, configured ...[]appconfig.StripEffect) (stripEffectDefinition, appconfig.StripProgram, bool) {
	definitions := appconfig.DefaultStripEffects()
	if len(configured) != 0 {
		definitions = configured[0]
	}
	lookupID := strings.ToLower(strings.TrimSpace(id))
	for _, candidate := range definitions {
		if !strings.EqualFold(strings.TrimSpace(candidate.ID), lookupID) {
			continue
		}
		descriptor := configuredStripEffectDescriptors([]appconfig.StripEffect{candidate})[0]
		return descriptor, candidate.Program, true
	}
	return stripEffectDefinition{}, appconfig.StripProgram{}, false
}

func (outputs *OutputScheduler) startStripEffect(ctx context.Context, definition stripEffectDefinition, program appconfig.StripProgram, count, fps int) (StreamOperation, error) {
	payload, err := native.StripConfigurePayload(count)
	if err != nil {
		return StreamOperation{}, err
	}
	if fps < 1 || fps > 30 {
		return StreamOperation{}, fmt.Errorf("strip FPS must be 1..30")
	}
	outputs.stop("strip")
	outputs.stripMu.Lock()
	err = outputs.sendStrip(ctx, payload)
	outputs.stripMu.Unlock()
	if err != nil {
		return StreamOperation{}, err
	}
	operation, runContext, running, previous, err := outputs.replace("strip", fmt.Sprintf("%s %d LEDs at %d FPS", definition.Name, count, fps))
	if err != nil {
		return StreamOperation{}, err
	}
	go func() {
		err := waitPreviousOutput(runContext, previous)
		if err == nil {
			err = outputs.streamStripProgram(runContext, count, fps, program)
		}
		outputs.finish("strip", running, err, nil)
	}()
	return operation, nil
}

func (outputs *OutputScheduler) streamStripProgram(ctx context.Context, count, fps int, program appconfig.StripProgram) error {
	return outputs.streamStripEffect(ctx, count, fps, func(count int, elapsed time.Duration) []byte {
		return renderStripProgram(program, count, elapsed)
	})
}

func (outputs *OutputScheduler) sendStripFrame(ctx context.Context, rgb []byte) error {
	frames, err := native.StripFramePayloads(rgb)
	if err != nil {
		return err
	}
	outputs.stripMu.Lock()
	defer outputs.stripMu.Unlock()
	for _, payload := range frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := outputs.sendStrip(ctx, payload); err != nil {
			return err
		}
	}
	return nil
}

func (outputs *OutputScheduler) streamStripRainbow(ctx context.Context, count, fps int) error {
	return outputs.streamStripEffect(ctx, count, fps, func(count int, elapsed time.Duration) []byte {
		return stripRainbowFrame(count, byte(elapsed.Milliseconds()/16))
	})
}

func (outputs *OutputScheduler) streamStripEffect(ctx context.Context, count, fps int, renderer stripEffectRenderer) error {
	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()
	started := time.Now()
	consecutiveTimeouts := 0
	for {
		// Render from elapsed monotonic time; slow ACKs skip frames rather than queueing stale colors.
		if err := outputs.sendStripFrame(ctx, renderer(count, time.Since(started))); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			consecutiveTimeouts++
			if consecutiveTimeouts >= stripStreamConsecutiveTimeoutLimit {
				return fmt.Errorf(
					"strip stream timed out %d consecutive times: %w",
					consecutiveTimeouts,
					err,
				)
			}
			if consecutiveTimeouts == 1 {
				outputs.target.PublishHostEvent(
					"output",
					"strip stream dropped a timed-out frame; continuing from the current animation time",
				)
			}
		} else {
			if consecutiveTimeouts != 0 {
				outputs.target.PublishHostEvent(
					"output",
					"strip stream recovered after a transient frame timeout",
				)
			}
			consecutiveTimeouts = 0
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func renderStripProgram(program appconfig.StripProgram, count int, elapsed time.Duration) []byte {
	switch strings.ToLower(strings.TrimSpace(program.Primitive)) {
	case "alternating-zones":
		return stripAlternatingZonesFrame(program, count, elapsed)
	case "envelope":
		return stripEnvelopeFrame(program, count, elapsed)
	case "converging-points":
		return stripConvergingPointsFrame(program, count, elapsed)
	default:
		return make([]byte, count*3)
	}
}

func scaledStripColor(color appconfig.StripColor, intensity byte) (byte, byte, byte) {
	scale := func(value byte) byte { return byte(uint16(value) * uint16(intensity) / 255) }
	return scale(color.Red), scale(color.Green), scale(color.Blue)
}

func stripAlternatingZonesFrame(program appconfig.StripProgram, count int, elapsed time.Duration) []byte {
	frame := make([]byte, count*3)
	step := time.Duration(program.StepMS) * time.Millisecond
	steps := program.PeriodMS / program.StepMS
	if steps < 2 {
		steps = 2
	}
	phase := int(elapsed/step) % steps
	primaryDominant := phase < program.SwapAfterSteps
	flashOn := phase%2 == 0
	for pixel := 0; pixel < count; pixel++ {
		left := pixel < (count+1)/2
		primary := left == primaryDominant
		intensity := program.DimIntensity
		if flashOn {
			intensity = 255
		}
		color := program.Secondary
		if primary {
			color = program.Primary
		}
		frame[pixel*3], frame[pixel*3+1], frame[pixel*3+2] = scaledStripColor(color, intensity)
	}
	return frame
}

func stripEnvelopeFrame(program appconfig.StripProgram, count int, elapsed time.Duration) []byte {
	cycleMS := int((elapsed % (time.Duration(program.PeriodMS) * time.Millisecond)) / time.Millisecond)
	intensity := byte(0)
	for index, point := range program.Envelope {
		if cycleMS < point.AtMS {
			if index == 0 {
				break
			}
			previous := program.Envelope[index-1]
			span := point.AtMS - previous.AtMS
			progress := cycleMS - previous.AtMS
			value := int(previous.Intensity) + (int(point.Intensity)-int(previous.Intensity))*progress/span
			intensity = byte(value)
			break
		}
		intensity = point.Intensity
	}
	frame := make([]byte, count*3)
	r, g, b := scaledStripColor(program.Primary, intensity)
	for offset := 0; offset < len(frame); offset += 3 {
		frame[offset], frame[offset+1], frame[offset+2] = r, g, b
	}
	return frame
}

func stripConvergingPointsFrame(program appconfig.StripProgram, count int, elapsed time.Duration) []byte {
	frame := make([]byte, count*3)
	if count == 0 {
		return frame
	}
	half := (count - 1) / 2
	cycle := time.Duration(program.PeriodMS) * time.Millisecond
	progress := int((elapsed % cycle) * time.Duration(half+1) / cycle)
	leftHead, rightHead := progress, count-1-progress
	tail := program.TailPixels
	if tail == 0 {
		tail = count / 12
	}
	if tail < 3 {
		tail = 3
	}
	for pixel := 0; pixel < count; pixel++ {
		intensity := 0
		if pixel <= leftHead {
			distance := leftHead - pixel
			if distance <= tail {
				intensity = 255 * (tail + 1 - distance) / (tail + 1)
			}
		}
		if pixel >= rightHead {
			distance := pixel - rightHead
			candidate := 0
			if distance <= tail {
				candidate = 255 * (tail + 1 - distance) / (tail + 1)
			}
			if candidate > intensity {
				intensity = candidate
			}
		}
		frame[pixel*3], frame[pixel*3+1], frame[pixel*3+2] = scaledStripColor(program.Primary, byte(intensity))
	}
	return frame
}

func stripRainbowFrame(count int, phase byte) []byte {
	frame := make([]byte, count*3)
	for pixel := 0; pixel < count; pixel++ {
		wheel := byte(pixel*256/count) + phase
		var r, g, b byte
		switch {
		case wheel < 85:
			r, g = 255-wheel*3, wheel*3
		case wheel < 170:
			wheel -= 85
			g, b = 255-wheel*3, wheel*3
		default:
			wheel -= 170
			r, b = wheel*3, 255-wheel*3
		}
		frame[pixel*3], frame[pixel*3+1], frame[pixel*3+2] = r, g, b
	}
	return frame
}
