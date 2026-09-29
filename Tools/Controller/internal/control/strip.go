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

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

type stripEffectDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DefaultFPS  int    `json:"default_fps"`
}

var stripEffectCatalog = []stripEffectDefinition{
	{ID: "police", Name: "Police", Description: "Alternating red and blue emergency-light sweep", DefaultFPS: 20},
	{ID: "white-thunder", Name: "White thunder", Description: "A sharp white lightning strike with secondary flashes and decay", DefaultFPS: 30},
	{ID: "converging-red", Name: "Converging red dots", Description: "Two fading red dots travel from both ends to the center", DefaultFPS: 30},
}

type stripEffectRenderer func(count int, elapsed time.Duration) []byte

func stripCommandError(err error) error {
	var remote *link.RemoteError
	if errors.As(err, &remote) && remote.Code == native.ErrorBusy {
		return fmt.Errorf("board is busy; wait for startup, or stop/save the MCU macro and run 'macro buffer clear' to release strip memory: %w", err)
	}
	return err
}

func (outputs *OutputScheduler) sendStrip(ctx context.Context, payload []byte) error {
	return stripCommandError(outputs.send(ctx, native.OpAddressableLED, payload))
}

func stripStreamCommand(ctx context.Context, outputs *OutputScheduler, args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("usage: strip config COUNT | frame RGBHEX | rainbow [COUNT [FPS]] | effect list | effect play NAME [COUNT [FPS]] | stop | status")
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
	case "effect":
		if len(args) == 2 && strings.EqualFold(args[1], "list") {
			data, err := json.Marshal(stripEffectCatalog)
			return string(data), err
		}
		if len(args) < 3 || len(args) > 5 || !strings.EqualFold(args[1], "play") {
			break
		}
		definition, renderer, ok := stripEffectByID(args[2])
		if !ok {
			return "", fmt.Errorf("unknown strip effect %q; run 'strip effect list'", args[2])
		}
		count, fps := 100, definition.DefaultFPS
		var err error
		if len(args) > 3 {
			count, err = strconv.Atoi(args[3])
			if err != nil {
				return "", fmt.Errorf("strip count must be an integer")
			}
		}
		if len(args) > 4 {
			fps, err = strconv.Atoi(args[4])
			if err != nil {
				return "", fmt.Errorf("strip FPS must be an integer")
			}
		}
		operation, err := outputs.startStripEffect(ctx, definition, renderer, count, fps)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("strip effect %s started (id=%d)", definition.ID, operation.ID), nil
	}
	return "", fmt.Errorf("usage: strip config COUNT | frame RGBHEX | rainbow [COUNT [FPS]] | effect list | effect play NAME [COUNT [FPS]] | stop | status")
}

func stripEffectByID(id string) (stripEffectDefinition, stripEffectRenderer, bool) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "police":
		return stripEffectCatalog[0], stripPoliceFrame, true
	case "white-thunder", "thunder", "lightning":
		return stripEffectCatalog[1], stripWhiteThunderFrame, true
	case "converging-red", "converge", "red-dots":
		return stripEffectCatalog[2], stripConvergingRedFrame, true
	default:
		return stripEffectDefinition{}, nil, false
	}
}

func (outputs *OutputScheduler) startStripEffect(ctx context.Context, definition stripEffectDefinition, renderer stripEffectRenderer, count, fps int) (StreamOperation, error) {
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
			err = outputs.streamStripEffect(runContext, count, fps, renderer)
		}
		outputs.finish("strip", running, err, nil)
	}()
	return operation, nil
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
	for {
		// Render from elapsed monotonic time; slow ACKs skip frames rather than queueing stale colors.
		if err := outputs.sendStripFrame(ctx, renderer(count, time.Since(started))); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func stripPoliceFrame(count int, elapsed time.Duration) []byte {
	frame := make([]byte, count*3)
	phase := int(elapsed/(100*time.Millisecond)) % 8
	redDominant := phase < 4
	flashOn := phase%2 == 0
	for pixel := 0; pixel < count; pixel++ {
		left := pixel < (count+1)/2
		red := left == redDominant
		intensity := byte(36)
		if flashOn {
			intensity = 255
		}
		if red {
			frame[pixel*3] = intensity
		} else {
			frame[pixel*3+2] = intensity
		}
	}
	return frame
}

func stripWhiteThunderFrame(count int, elapsed time.Duration) []byte {
	// The fixed strike envelope is deterministic, so video playback and tests can seek exactly.
	cycle := elapsed % (1600 * time.Millisecond)
	intensity := byte(0)
	switch {
	case cycle < 45*time.Millisecond:
		intensity = 255
	case cycle < 90*time.Millisecond:
		intensity = 24
	case cycle < 145*time.Millisecond:
		intensity = 220
	case cycle < 220*time.Millisecond:
		intensity = 52
	case cycle < 360*time.Millisecond:
		remaining := int((360*time.Millisecond - cycle) / time.Millisecond)
		intensity = byte(remaining * 150 / 140)
	}
	frame := make([]byte, count*3)
	for offset := 0; offset < len(frame); offset += 3 {
		frame[offset], frame[offset+1], frame[offset+2] = intensity, intensity, intensity
	}
	return frame
}

func stripConvergingRedFrame(count int, elapsed time.Duration) []byte {
	frame := make([]byte, count*3)
	if count == 0 {
		return frame
	}
	half := (count - 1) / 2
	cycle := 2 * time.Second
	progress := int((elapsed % cycle) * time.Duration(half+1) / cycle)
	leftHead, rightHead := progress, count-1-progress
	tail := count / 12
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
		frame[pixel*3] = byte(intensity)
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
