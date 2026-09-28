package control

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
)

func stripStreamCommand(ctx context.Context, outputs *OutputScheduler, args []string) (string, error) {
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
		if err := outputs.send(ctx, native.OpAddressableLED, payload); err != nil {
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
		err = outputs.send(ctx, native.OpAddressableLED, payload)
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
		if err := outputs.send(ctx, native.OpAddressableLED, payload); err != nil {
			return err
		}
	}
	return nil
}

func (outputs *OutputScheduler) streamStripRainbow(ctx context.Context, count, fps int) error {
	ticker := time.NewTicker(time.Second / time.Duration(fps))
	defer ticker.Stop()
	started := time.Now()
	for {
		// Phase follows elapsed monotonic time; slow ACKs skip frames rather than queueing stale colors.
		phase := byte(time.Since(started).Milliseconds() / 16)
		if err := outputs.sendStripFrame(ctx, stripRainbowFrame(count, phase)); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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
