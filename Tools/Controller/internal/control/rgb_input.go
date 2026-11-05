package control

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/rgbinput"
	"pccontroller.local/controller/internal/shell"
)

const rgbInputUsage = "rgb-input start openrgb|tpm2net|adalight|adalight-openrgb|tpm2|ddp|artnet|sacn|wled|hyperion --pixels N [--listen IP:PORT --source IP | --serial PORT --baud 115200] [--universe 1 --channel 1] [--fps 60] [--timeout 5s] | status | stop"

func rgbInputCommand(runtime *Runtime, outputs *OutputScheduler) shell.Command {
	return shell.Command{Name: "rgb-input", Usage: rgbInputUsage, Summary: "receive external RGB frames on the host strip lane", Group: "Lighting", Run: func(ctx context.Context, args []string) (string, error) {
		if len(args) == 0 {
			return "", errors.New(rgbInputUsage)
		}
		switch args[0] {
		case "status":
			if len(args) != 1 {
				return "", errors.New(rgbInputUsage)
			}
			outputs.mu.Lock()
			receiver := outputs.rgbInput
			outputs.mu.Unlock()
			if receiver == nil {
				return `{"running":false}`, nil
			}
			data, err := json.Marshal(receiver.State())
			return string(data), err
		case "stop":
			if len(args) != 1 {
				return "", errors.New(rgbInputUsage)
			}
			outputs.mu.Lock()
			running := outputs.rgbInputRun
			if running != nil && outputs.strip == running {
				running.stopRequested = true
				running.cancel()
			}
			outputs.mu.Unlock()
			if running != nil {
				if err := waitPreviousOutput(ctx, running.done); err != nil {
					return "", err
				}
			}
			return "RGB input stopped", nil
		case "start":
			if len(args) < 2 {
				return "", errors.New(rgbInputUsage)
			}
			cfg := rgbinput.Config{Protocol: args[1]}
			flags := flag.NewFlagSet("rgb-input", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			flags.StringVar(&cfg.Listen, "listen", "", "UDP bind endpoint (protocol default on loopback)")
			flags.StringVar(&cfg.Serial, "serial", "", "dedicated virtual COM/PTY input, never the board port")
			flags.IntVar(&cfg.Baud, "baud", 115200, "serial input baud")
			flags.IntVar(&cfg.Universe, "universe", 1, "DMX universe (Art-Net 0..32767, sACN 1..63999)")
			flags.IntVar(&cfg.Channel, "channel", 1, "first DMX slot (1-based)")
			flags.StringVar(&cfg.Source, "source", "", "allowed sender IP")
			flags.IntVar(&cfg.Pixels, "pixels", 0, "exact physical LED count")
			flags.IntVar(&cfg.FPS, "fps", 60, "maximum output rate")
			flags.DurationVar(&cfg.Timeout, "timeout", 5*time.Second, "sender silence timeout")
			if err := flags.Parse(args[2:]); err != nil {
				return "", err
			}
			if flags.NArg() != 0 {
				return "", errors.New(rgbInputUsage)
			}
			if cfg.Serial == "" && cfg.Listen == "" {
				cfg.Listen = rgbinput.DefaultEndpoint(cfg.Protocol)
			}
			operation, err := outputs.startRGBInput(ctx, runtime, cfg)
			if err != nil {
				return "", err
			}
			endpoint := cfg.Listen
			if cfg.Serial != "" {
				endpoint = cfg.Serial
			}
			return fmt.Sprintf("RGB input listening on %s (%s, %d pixels, up to %d FPS, id=%d)", endpoint, cfg.Protocol, cfg.Pixels, cfg.FPS, operation.ID), nil
		default:
			return "", errors.New(rgbInputUsage)
		}
	}}
}

// The listener is an explicitly acquired strip owner. Network traffic cannot
// start it, replace another owner, reconnect a board, or execute host commands.
func (outputs *OutputScheduler) startRGBInput(ctx context.Context, runtime *Runtime, cfg rgbinput.Config) (StreamOperation, error) {
	if err := ctx.Err(); err != nil {
		return StreamOperation{}, err
	}
	snapshot := runtime.BoardSnapshot()
	if err := checkRGBInputBoard(snapshot, snapshot.ConnectionGeneration); err != nil {
		return StreamOperation{}, err
	}
	if cfg.Serial != "" && sameRGBSerialPort(cfg.Serial, snapshot.Port.Name) {
		return StreamOperation{}, errors.New("RGB input must use a separate virtual COM/PTY port, never the controller board port")
	}
	outputs.mu.Lock()
	occupied := outputs.closed || outputs.strip != nil
	outputs.mu.Unlock()
	if occupied {
		return StreamOperation{}, errors.New("strip lane is occupied or closing; explicitly stop its current owner first")
	}
	receiver, err := rgbinput.Open(cfg)
	if err != nil {
		return StreamOperation{}, err
	}
	if err = ctx.Err(); err == nil {
		err = checkRGBInputBoard(runtime.BoardSnapshot(), snapshot.ConnectionGeneration)
	}
	if err != nil {
		_ = receiver.Close()
		return StreamOperation{}, err
	}
	outputs.mu.Lock()
	if outputs.closed || outputs.strip != nil {
		outputs.mu.Unlock()
		_ = receiver.Close()
		return StreamOperation{}, errors.New("strip lane is occupied or closing; explicitly stop its current owner first")
	}
	runCtx, cancel := context.WithCancel(outputs.root)
	outputs.nextID++
	endpoint := cfg.Listen
	if cfg.Serial != "" {
		endpoint = cfg.Serial
	}
	running := &runningOutput{id: outputs.nextID, name: "RGB input · " + cfg.Protocol + " · " + endpoint, cancel: cancel, done: make(chan error, 1)}
	outputs.strip = running
	outputs.rgbInput = receiver
	outputs.rgbInputRun = running
	outputs.stripStats = StripStreamStats{Pixels: cfg.Pixels, RequestedFPS: cfg.FPS}
	outputs.reportActivity("strip", true)
	outputs.mu.Unlock()
	outputs.target.PublishHostEvent("output", fmt.Sprintf("%s started (id=%d)", running.name, running.id))
	go func() {
		configured := false
		check := func() error { return checkRGBInputBoard(runtime.BoardSnapshot(), snapshot.ConnectionGeneration) }
		err := receiver.Run(runCtx, check, func(ctx context.Context, rgb []byte) (frameErr error) {
			defer func() {
				outputs.mu.Lock()
				defer outputs.mu.Unlock()
				if outputs.strip != running {
					return
				}
				if frameErr != nil {
					outputs.stripStats.FramesFailed++
					outputs.stripStats.LastError = frameErr.Error()
				} else {
					outputs.stripStats.FramesAcked++
					outputs.stripStats.LastAckUnixMS = time.Now().UnixMilli()
					outputs.stripStats.LastError = ""
				}
			}()
			outputs.stripMu.Lock()
			defer outputs.stripMu.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			// One order snapshot and one authenticated board generation for the
			// complete configure/stage/commit transaction, including its ACKs.
			ctx = context.WithValue(ctx, stripOrderContextKey{}, runtime.stripOutputConfig())
			payloads, err := native.StripFramePayloads(rgb)
			if err != nil {
				return err
			}
			if !configured {
				configure, _ := native.StripConfigurePayload(cfg.Pixels)
				payloads = append([][]byte{configure}, payloads...)
			}
			for _, payload := range payloads {
				requestCtx, done := context.WithTimeout(ctx, outputRequestTimeout)
				_, err := runtime.requestAtGeneration(requestCtx, snapshot.ConnectionGeneration, native.OpAddressableLED, payload, native.OpACK)
				done()
				if err != nil {
					return stripCommandError(err)
				}
			}
			configured = true
			return nil
		})
		outputs.finish("strip", running, err, nil)
	}()
	return StreamOperation{ID: running.id, Kind: "strip", Name: running.name, Done: running.done}, nil
}

func sameRGBSerialPort(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if runtime.GOOS == "windows" {
		normalize := func(value string) string {
			return strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), `\\.\`), `\\?\`), ":")
		}
		return strings.EqualFold(normalize(a), normalize(b))
	}
	if resolved, err := filepath.EvalSymlinks(a); err == nil {
		a = resolved
	}
	if resolved, err := filepath.EvalSymlinks(b); err == nil {
		b = resolved
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func checkRGBInputBoard(state Snapshot, generation uint64) error {
	if !state.Connected || state.ConnectionGeneration != generation {
		return errors.New("RGB input board disconnected or changed; restart input explicitly")
	}
	if state.EmergencyStop.Active {
		return ErrEmergencyStopActive
	}
	if state.Programming.Active || state.Paused {
		return errors.New("RGB input is unavailable while hardware is paused or programming")
	}
	if state.Hello.Capabilities&native.CapabilityAddressableLED == 0 {
		return errors.New("connected firmware does not advertise addressable LED support")
	}
	return nil
}
