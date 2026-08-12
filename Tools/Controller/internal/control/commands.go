package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostfacts"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/shell"
)

const (
	settingsSetUsage = "settings set FLAGS LIGHT ON OFF DISPLAY_OPEN " +
		"DISPLAY_CLOSED STATUS OUTPUT_PERSISTENCE STREAM DEFAULT_PAGE SAVE_LAST " +
		"STATUS_COLOR VOLTAGE_DECIMALS CURRENT_DECIMALS MOTION_EXIT_HOLD_SECONDS " +
		"MOTION_BREAK_MS RELAY_RESTORE_MASK"
	settingsUsage = "settings | settings decimals VOLTAGE CURRENT | " +
		"settings export-live | " +
		"settings color INDEX | settings motion always|closed|open|never | " +
		"settings motion-break 1..255 | " +
		"settings motion-exit-hold SECONDS | " +
		"settings audio door|relay on|off | " + settingsSetUsage
)

type CommandOptions struct {
	ProjectPath      string
	FQBN             string
	Macros           func() []appconfig.Macro
	ArduinoCLI       string
	ArduinoConfig    string
	Avrdude          string
	AvrdudeConf      string
	Programmer       string
	HostConfig       func() appconfig.Config
	HostFacts        hostfacts.Provider
	UpdateHostConfig func(func(*appconfig.Config) error) error
	Resolve          func() CommandOptions
	Outputs          *OutputScheduler
	ProgramRunner    programmer.CommandRunner
	ProgramExecute   func(context.Context, programmer.Options, io.Writer) error
	ProgramDataPaths programmer.HostDataPaths
	InitializeBoard  func(context.Context, *Runtime, []string, io.Writer) error
	BlankBoard       func(context.Context, *Runtime, []string, io.Writer) error
	USBaspDriver     func(context.Context, []string, io.Writer) error
}

func parseDisplayCommand(args []string) (DisplayRequest, error) {
	if len(args) < 1 {
		return DisplayRequest{}, errors.New("usage: display segments|lcd|both [options] [--] [TEXT]")
	}
	request := DisplayRequest{Target: args[0]}
	// Preserve the original compact form. For long segment text the historical
	// duration value was the step speed; for static/LCD text it was the hold.
	if len(args) >= 2 && !strings.HasPrefix(args[1], "--") {
		if legacy, err := strconv.ParseUint(args[1], 0, 16); err == nil {
			request.SpeedMS = int(legacy)
			request.DurationMS = int(legacy)
			request.Text = strings.Join(args[2:], " ")
			return request, nil
		}
	}
	for index := 1; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			request.Text = strings.Join(args[index+1:], " ")
			return request, nil
		}
		name, inline, hasInline := strings.Cut(argument, "=")
		nextValue := func() (string, error) {
			if hasInline {
				return inline, nil
			}
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			index++
			return args[index], nil
		}
		switch strings.ToLower(name) {
		case "--speed", "--speed-ms":
			value, err := nextValue()
			if err != nil {
				return DisplayRequest{}, err
			}
			request.SpeedMS, err = parseDisplayMilliseconds(value)
			if err != nil {
				return DisplayRequest{}, fmt.Errorf("invalid display speed %q: %w", value, err)
			}
		case "--duration", "--duration-ms", "--hold":
			value, err := nextValue()
			if err != nil {
				return DisplayRequest{}, err
			}
			request.DurationMS, err = parseDisplayMilliseconds(value)
			if err != nil {
				return DisplayRequest{}, fmt.Errorf("invalid display duration %q: %w", value, err)
			}
		case "--repeat":
			value, err := nextValue()
			if err != nil {
				return DisplayRequest{}, err
			}
			request.Repeat = DisplayRepeat(value)
		case "--interval", "--interval-ms", "--wait":
			value, err := nextValue()
			if err != nil {
				return DisplayRequest{}, err
			}
			request.IntervalMS, err = parseDisplayMilliseconds(value)
			if err != nil {
				return DisplayRequest{}, fmt.Errorf("invalid display interval %q: %w", value, err)
			}
		case "--scroll", "--marquee":
			if hasInline {
				return DisplayRequest{}, fmt.Errorf("%s does not take a value", name)
			}
			request.Scroll = true
		case "--no-scroll":
			if hasInline {
				return DisplayRequest{}, fmt.Errorf("%s does not take a value", name)
			}
			request.Scroll = false
		default:
			if strings.HasPrefix(argument, "--") {
				return DisplayRequest{}, fmt.Errorf("unknown display option %q", argument)
			}
			request.Text = strings.Join(args[index:], " ")
			return request, nil
		}
	}
	return request, nil
}

func parseDisplayMilliseconds(value string) (int, error) {
	if milliseconds, err := strconv.ParseUint(value, 0, 31); err == nil {
		return int(milliseconds), nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 || duration%time.Millisecond != 0 {
		return 0, errors.New("use milliseconds or a positive duration such as 220ms or 30s")
	}
	return int(duration / time.Millisecond), nil
}

func NewCommandEngine(runtime *Runtime, options CommandOptions) *shell.Engine {
	engine := shell.New(100)
	macroRunner := NewMacroRunner(
		runtime,
		options.Macros,
		options.HostConfig,
		options.UpdateHostConfig,
	)
	runtime.setMacroRunner(macroRunner)
	outputs := options.Outputs
	if outputs == nil {
		outputs = NewOutputScheduler(runtime)
	}
	// Keep the runtime-owned scheduler attached when a watched configuration
	// resolver refreshes only file-backed options. Programming capture/restore
	// must observe the same RGB/melody owner used by the live command engine.
	options.Outputs = outputs
	mustRegister := func(command shell.Command) {
		if err := engine.Register(command); err != nil {
			panic(err)
		}
	}
	mustRegister(shell.Command{
		Name: "clear", Usage: "clear", Summary: "clear the interactive console",
		Run: func(context.Context, []string) (string, error) {
			return "\x1b[2J\x1b[H", nil
		},
	})
	mustRegister(shell.Command{
		Name: "quit", Aliases: []string{"exit"}, Usage: "quit|exit",
		Summary: "close the interactive controller application",
		Run: func(context.Context, []string) (string, error) {
			return "", shell.ErrExit
		},
	})
	mustRegister(shell.Command{
		Name:    "config",
		Usage:   "config get ui.app_title | config set ui.app_title VALUE",
		Summary: "inspect or update supported watched host settings",
		Run: func(_ context.Context, args []string) (string, error) {
			return hostConfigCommand(options, args)
		},
	})
	mustRegister(shell.Command{
		Name: "peripherals", Usage: "peripherals", Summary: "list ordered host presentation descriptors",
		Run: func(_ context.Context, args []string) (string, error) {
			if len(args) != 0 {
				return "", errors.New("usage: peripherals")
			}
			if options.HostConfig == nil {
				return "", errors.New("host configuration is unavailable")
			}
			encoded, err := json.MarshalIndent(appconfig.ControlDescriptors(options.HostConfig().UI), "", "  ")
			if err != nil {
				return "", err
			}
			return string(encoded), nil
		},
	})
	mustRegister(shell.Command{
		Name: "program-state", Aliases: []string{"run-state"},
		Usage:   "program-state [running [REASON...]|idle] | program-state set OWNER idle|running [REASON...]",
		Summary: "inspect or set the host-owned Idle/Running application state",
		Run: func(_ context.Context, args []string) (string, error) {
			owner, mode, reason := "shell", ProgramMode(""), ""
			switch {
			case len(args) == 0 || (len(args) == 1 && strings.EqualFold(args[0], "status")):
				return formatProgramState(runtime.ProgramState()), nil
			case len(args) >= 3 && strings.EqualFold(args[0], "set"):
				owner, mode, reason = args[1], parseProgramMode(args[2]), strings.Join(args[3:], " ")
			case len(args) >= 1:
				mode, reason = parseProgramMode(args[0]), strings.Join(args[1:], " ")
			}
			if mode == "" {
				return "", fmt.Errorf("program state must be idle or running")
			}
			state, err := runtime.SetProgramState(owner, mode, reason)
			if err != nil {
				return "", err
			}
			return formatProgramState(state), nil
		},
	})

	mustRegister(shell.Command{
		Name: "ports", Usage: "ports", Summary: "list serial ports with VID/PID",
		Run: func(context.Context, []string) (string, error) {
			list, err := ports.List()
			if err != nil {
				return "", err
			}
			if len(list) == 0 {
				return "no serial ports found", nil
			}
			var lines []string
			for _, port := range list {
				lines = append(lines, port.Label())
			}
			return strings.Join(lines, "\n"), nil
		},
	})
	mustRegister(shell.Command{
		Name: "open", Usage: "open [PORT]", Summary: "open explicitly or auto-detect by HELLO",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) > 1 {
				return "", fmt.Errorf("usage: open [PORT]")
			}
			requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if len(args) == 1 {
				if err := runtime.Open(requestContext, args[0]); err != nil {
					return "", err
				}
			} else {
				runtime.ResumeAuto()
				if err := runtime.EnsureConnected(requestContext); err != nil {
					return "", err
				}
			}
			snapshot := runtime.Snapshot()
			return fmt.Sprintf("opened %s: %s", snapshot.Port.Name, formatHello(snapshot.Hello)), nil
		},
	})
	mustRegister(shell.Command{
		Name: "close", Usage: "close", Summary: "close and pause auto-reconnect",
		Run: func(context.Context, []string) (string, error) {
			return "serial port closed", runtime.Close()
		},
	})
	mustRegister(shell.Command{
		Name: "reconnect", Usage: "reconnect", Summary: "resume authenticated auto-reconnect",
		Run: func(ctx context.Context, _ []string) (string, error) {
			_ = runtime.Close()
			runtime.ResumeAuto()
			requestContext, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			if err := runtime.EnsureConnected(requestContext); err != nil {
				return "", err
			}
			return "reconnected " + runtime.Snapshot().Port.Name, nil
		},
	})
	mustRegister(shell.Command{
		Name: "hello", Usage: "hello", Summary: "query and verify device identity",
		Run: func(ctx context.Context, _ []string) (string, error) {
			frame, err := request(ctx, runtime, native.OpHello, nil, native.OpHelloResp)
			if err != nil {
				return "", err
			}
			hello, err := native.ParseHello(frame.Payload)
			if err != nil {
				return "", err
			}
			return formatHello(hello), nil
		},
	})
	mustRegister(shell.Command{
		Name: "status", Aliases: []string{"st"}, Usage: "status",
		Summary: "query complete measurements and I/O state",
		Run: func(ctx context.Context, _ []string) (string, error) {
			status, err := refresh(ctx, runtime)
			if err != nil {
				return "", err
			}
			return formatStatus(status), nil
		},
	})
	mustRegister(shell.Command{
		Name: "event", Usage: "event latest | event wait [KIND] [TIMEOUT]",
		Summary: "wait for an immediate door/key/RF/macro/device event",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 1 && strings.EqualFold(args[0], "latest") {
				return fmt.Sprintf("event_id=%d", runtime.LatestEventID()), nil
			}
			if len(args) < 1 || len(args) > 3 ||
				!strings.EqualFold(args[0], "wait") {
				return "", fmt.Errorf("usage: event latest | event wait [KIND] [TIMEOUT]")
			}
			kind := ""
			timeout := 30 * time.Second
			if len(args) == 2 {
				if parsed, err := time.ParseDuration(args[1]); err == nil {
					timeout = parsed
				} else {
					kind = args[1]
				}
			}
			if len(args) == 3 {
				kind = args[1]
				parsed, err := time.ParseDuration(args[2])
				if err != nil {
					return "", fmt.Errorf("invalid event timeout %q", args[2])
				}
				timeout = parsed
			}
			if timeout <= 0 || timeout > 24*time.Hour {
				return "", fmt.Errorf("event timeout must be positive and at most 24h")
			}
			after := runtime.LatestEventID()
			waitContext, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			event, err := runtime.WaitEvent(waitContext, after, kind)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"event_id=%d time=%s kind=%s lifecycle=%s state=%s port=%q reason=%q text=%q opcode=0x%02X payload=% X",
				event.ID,
				event.Time.Format(time.RFC3339Nano),
				event.Kind,
				event.Lifecycle,
				event.State,
				event.Port.Name,
				event.Reason,
				event.Text,
				event.Frame.Opcode,
				event.Frame.Payload,
			), nil
		},
	})
	mustRegister(metricCommand("voltage", "read supply and INA219 bus voltage", runtime,
		func(status native.Status) string {
			return fmt.Sprintf("supply=%.3f V bus=%.3f V", float64(status.SupplyMV)/1000, float64(status.BusMV)/1000)
		}))
	mustRegister(metricCommand("current", "read INA219 current", runtime,
		func(status native.Status) string {
			return fmt.Sprintf("current=%d mA", status.CurrentMA)
		}))
	mustRegister(shell.Command{
		Name: "temp", Aliases: []string{"temperature"},
		Usage:   "temp [list|scan]",
		Summary: "read tLED/tBT or list their DS18B20 ROM identities",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 0 {
				status, err := refresh(ctx, runtime)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf(
					"tLED=%s tBT=%s",
					formatLEDTemperature(status, 2),
					formatBTAudioTemperature(status, 2),
				), nil
			}
			if len(args) != 1 ||
				(!strings.EqualFold(args[0], "list") &&
					!strings.EqualFold(args[0], "scan")) {
				return "", fmt.Errorf("usage: temp [list|scan]")
			}
			payload := []byte(nil)
			if strings.EqualFold(args[0], "scan") {
				payload = []byte{1}
			}
			frame, err := request(ctx, runtime, native.OpTemperatureList, payload, native.OpTemperatures)
			if err != nil {
				return "", err
			}
			sensors, err := native.ParseTemperatures(frame.Payload)
			if err != nil {
				return "", err
			}
			return formatTemperatures(sensors), nil
		},
	})

	mustRegister(shell.Command{
		Name: "stream", Usage: "stream PERIOD_MS", Summary: "set STATUS stream period; zero disables",
		Run: func(ctx context.Context, args []string) (string, error) {
			value, err := oneUint(args, 16, "stream PERIOD_MS")
			if err != nil {
				return "", err
			}
			payload, err := native.StreamPeriodPayload(uint16(value))
			if err != nil {
				return "", err
			}
			if err := command(ctx, runtime, native.OpSetStream, payload); err != nil {
				return "", err
			}
			return fmt.Sprintf("stream period set to %d ms", value), nil
		},
	})
	mustRegister(shell.Command{
		Name: "message", Usage: "message TARGETS TYPE TEXT",
		Summary: "publish a bounded host notification event to native, web, TUI, or other targets",
		Run: func(_ context.Context, args []string) (string, error) {
			if len(args) < 3 {
				return "", errors.New("usage: message TARGETS TYPE TEXT")
			}
			targets := strings.Split(strings.ToLower(strings.TrimSpace(args[0])), ",")
			for _, target := range targets {
				if !messageTargetAllowed(strings.TrimSpace(target)) {
					return "", fmt.Errorf("unsupported message target %q", target)
				}
			}
			kind := strings.ToLower(strings.TrimSpace(args[1]))
			if kind == "" || len(kind) > 32 {
				return "", errors.New("message type must contain 1..32 characters")
			}
			text := strings.TrimSpace(strings.Join(args[2:], " "))
			if text == "" || len(text) > 4096 {
				return "", errors.New("message text must contain 1..4096 characters")
			}
			event := runtime.PublishStructuredEvent(Event{
				Kind: "message", Lifecycle: "completed", Source: "cli", Target: strings.Join(targets, ","),
				Targets: targets, MessageType: kind, Severity: "info", Delivery: "sync", Text: text,
			})
			return fmt.Sprintf("message id=%d targets=%s", event.ID, strings.Join(targets, ",")), nil
		},
	})
	mustRegister(shell.Command{
		Name: "settings", Usage: settingsUsage,
		Summary: "query or update the firmware-owned EEPROM settings",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 0 {
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil
			}

			action := strings.ToLower(args[0])
			switch action {
			case "export-live":
				if len(args) != 1 {
					return "", fmt.Errorf("usage: settings export-live")
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				encoded, err := encodeLiveSettingsExport(settings)
				if err != nil {
					return "", err
				}
				return encoded, nil

			case "motion-break", "motion-dead-time":
				if len(args) != 2 {
					return "", fmt.Errorf("usage: settings motion-break 1..255")
				}
				milliseconds, err := strconv.ParseUint(args[1], 10, 16)
				if err != nil {
					return "", fmt.Errorf("invalid motion break %q", args[1])
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				if err := settings.SetMotionBreakMS(uint16(milliseconds)); err != nil {
					return "", err
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "motion", "motion-policy":
				if len(args) != 2 {
					return "", fmt.Errorf(
						"usage: settings motion always|closed|open|never",
					)
				}
				policies := map[string]byte{
					"always":      native.MotionDoorAlways,
					"closed":      native.MotionDoorClosedOnly,
					"closed-only": native.MotionDoorClosedOnly,
					"open":        native.MotionDoorOpenOnly,
					"open-only":   native.MotionDoorOpenOnly,
					"never":       native.MotionDoorNever,
				}
				policy, ok := policies[strings.ToLower(args[1])]
				if !ok {
					return "", fmt.Errorf(
						"motion policy must be always, closed, open, or never",
					)
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				if err := settings.SetMotionDoorPolicy(policy); err != nil {
					return "", err
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "motion-exit-hold", "motion-hold":
				if len(args) != 2 {
					return "", fmt.Errorf(
						"usage: settings motion-exit-hold SECONDS (1..31)",
					)
				}
				holdSeconds, err := parseBoundedByte(
					args[1], native.SettingsMaximumMotionExitHoldSeconds,
					"motion exit hold",
				)
				if err != nil {
					return "", err
				}
				if holdSeconds == 0 {
					return "", errors.New("motion exit hold must be at least 1 second")
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				settings.MotionExitHoldSeconds = holdSeconds
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "audio", "cues":
				if len(args) != 3 {
					return "", fmt.Errorf(
						"usage: settings audio door|relay on|off",
					)
				}
				enabled, err := parseBool(args[2])
				if err != nil {
					return "", err
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				switch strings.ToLower(args[1]) {
				case "door":
					settings.SetDoorAudioEnabled(enabled)
				case "relay", "relays":
					settings.SetRelayAudioEnabled(enabled)
				default:
					return "", fmt.Errorf("buzzer cue group must be door or relay")
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "decimals":
				if len(args) != 3 {
					return "", fmt.Errorf(
						"usage: settings decimals VOLTAGE CURRENT (each 0..2)",
					)
				}
				voltage, err := parseBoundedByte(
					args[1],
					2,
					"voltage decimals",
				)
				if err != nil {
					return "", err
				}
				current, err := parseBoundedByte(
					args[2],
					2,
					"current decimals",
				)
				if err != nil {
					return "", err
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				if err := settings.SetVoltageDecimals(voltage); err != nil {
					return "", err
				}
				if err := settings.SetCurrentDecimals(current); err != nil {
					return "", err
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "color", "status-color":
				if len(args) != 2 {
					return "", fmt.Errorf("usage: settings color INDEX (0..7)")
				}
				color, err := parseBoundedByte(args[1], 7, "status color")
				if err != nil {
					return "", err
				}
				settings, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				if err := settings.SetStatusColor(color); err != nil {
					return "", err
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "set":
				if len(args) != 18 {
					return "", fmt.Errorf("usage: %s", settingsSetUsage)
				}
				settings, err := settingsFromSetArgs(args)
				if err != nil {
					return "", err
				}
				settings, err = storeSettingsLive(ctx, runtime, settings)
				if err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			default:
				return "", fmt.Errorf("usage: %s", settingsUsage)
			}
		},
	})
	mustRegister(shell.Command{
		Name: "menu", Usage: menuUsage,
		Summary: "navigate stable page IDs or inspect/configure persistent menu layout",
		Run: func(ctx context.Context, args []string) (string, error) {
			return runMenuCommand(ctx, runtime, args)
		},
	})
	mustRegister(shell.Command{
		Name:    "relay",
		Usage:   "relay N on|off|toggle | relay side left|right stop|up|down | relay off | relay test [MS]",
		Summary: "control safe relay outputs and side motion",
		Run: func(ctx context.Context, args []string) (string, error) {
			return relayCommand(ctx, runtime, options.HostConfig, args)
		},
	})
	mustRegister(shell.Command{
		Name: "pwm", Usage: "pwm get|off|set CHANNEL VALUE",
		Summary: "query/control logical PWM output values",
		Run: func(ctx context.Context, args []string) (string, error) {
			return pwmCommand(ctx, runtime, args)
		},
	})
	mustRegister(shell.Command{
		Name:    "rgb",
		Usage:   "rgb R G B [BRIGHTNESS] | rgb effect list|play|wait|stop|status ...",
		Summary: "set status RGB color or stream a configured flash/breathe effect",
		Run: func(ctx context.Context, args []string) (string, error) {
			return rgbCommand(ctx, runtime, outputs, options.HostConfig, args)
		},
	})
	mustRegister(shell.Command{
		Name:    "strip",
		Usage:   "strip pixel N R G B [BRIGHTNESS] | fill R G B [BRIGHTNESS] | clear",
		Summary: "control the 11-pixel WS2811/WS2812 status strip",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 1 && strings.EqualFold(args[0], "clear") {
				payload, _ := native.AddressableLEDPayload(
					native.AddressableLEDFill,
					0, 0, 0, 0,
				)
				if err := command(ctx, runtime, native.OpAddressableLED, payload); err != nil {
					return "", err
				}
				return "addressable LED strip cleared", nil
			}
			if len(args) < 4 || len(args) > 6 {
				return "", fmt.Errorf(
					"usage: strip pixel N R G B [BRIGHTNESS] | fill R G B [BRIGHTNESS] | clear",
				)
			}
			pixel := native.AddressableLEDFill
			valueOffset := 1
			switch strings.ToLower(args[0]) {
			case "pixel":
				if len(args) != 5 && len(args) != 6 {
					return "", fmt.Errorf(
						"usage: strip pixel N R G B [BRIGHTNESS]",
					)
				}
				parsed, err := strconv.ParseUint(args[1], 0, 8)
				if err != nil || parsed > uint64(native.AddressableLEDMaxPixel) {
					return "", fmt.Errorf(
						"strip pixel must be 0..%d",
						native.AddressableLEDMaxPixel,
					)
				}
				pixel = byte(parsed)
				valueOffset = 2
			case "fill":
				if len(args) != 4 && len(args) != 5 {
					return "", fmt.Errorf(
						"usage: strip fill R G B [BRIGHTNESS]",
					)
				}
			default:
				return "", fmt.Errorf(
					"usage: strip pixel N R G B [BRIGHTNESS] | fill R G B [BRIGHTNESS] | clear",
				)
			}
			values, err := uintArgs(args[valueOffset:], 8)
			if err != nil {
				return "", err
			}
			brightness := byte(255)
			if len(values) == 4 {
				brightness = byte(values[3])
			}
			payload, err := native.AddressableLEDPayload(
				pixel,
				byte(values[0]),
				byte(values[1]),
				byte(values[2]),
				brightness,
			)
			if err != nil {
				return "", err
			}
			if err := command(ctx, runtime, native.OpAddressableLED, payload); err != nil {
				return "", err
			}
			if pixel == native.AddressableLEDFill {
				return fmt.Sprintf(
					"addressable LED fill=%d,%d,%d brightness=%d",
					values[0], values[1], values[2], brightness,
				), nil
			}
			return fmt.Sprintf(
				"addressable LED pixel=%d color=%d,%d,%d brightness=%d",
				pixel, values[0], values[1], values[2], brightness,
			), nil
		},
	})
	mustRegister(shell.Command{
		Name: "buzzer", Aliases: []string{"beep"}, Usage: "buzzer|beep [FREQUENCY_HZ [DURATION_MS]] | buzzer 0 0 | buzzer status | buzzer path board|host|both|none", Summary: "play/stop a bounded tone or select board/PC buzzer routing",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) >= 1 && (strings.EqualFold(args[0], "status") || strings.EqualFold(args[0], "path")) {
				return buzzerRoutingCommand(ctx, runtime, options, args)
			}
			frequencyHz, durationMS, err := parseBuzzerToneArgs(args)
			if err != nil {
				return "", err
			}
			stopping := frequencyHz == 0 && durationMS == 0
			outputs.StopMelody()
			if stopping {
				if err := command(ctx, runtime, native.OpBuzzer, native.BuzzerPayload(0, 0)); err != nil {
					return "", err
				}
				return "buzzer stopped", nil
			}
			// A direct tone is an explicit action on every surface, but it must
			// never bypass the firmware-owned silent flag.  Query before sending
			// so CLI/RPC/Web/TUI can report a safe, observable suppression rather
			// than relying on an inaudible side effect on the board.
			settings, err := querySettings(ctx, runtime)
			if err != nil {
				return "", err
			}
			if settings.Flags&native.SettingsSilent != 0 {
				return "buzzer suppressed: board is silent", nil
			}
			if err := command(ctx, runtime, native.OpBuzzer, native.BuzzerPayload(frequencyHz, durationMS)); err != nil {
				return "", err
			}
			return "buzzer command accepted", nil
		},
	})
	mustRegister(shell.Command{
		Name:    "melody",
		Usage:   "melody list|create NAME NOTE...|delete NAME|play NAME [REPEATS]|wait NAME [REPEATS]|stop|status",
		Summary: "stream a reusable configured melody without filling the MCU queue",
		Run: func(ctx context.Context, args []string) (string, error) {
			return melodyCommandWithUpdate(
				ctx,
				outputs,
				options.HostConfig,
				options.UpdateHostConfig,
				args,
			)
		},
	})
	mustRegister(shell.Command{
		Name: "silent", Usage: "silent status|on|off | silent board|host|both status|on|off",
		Summary: "control independent board and host buzzer silence",
		Run: func(ctx context.Context, args []string) (string, error) {
			return silentCommand(ctx, runtime, options, args)
		},
	})
	mustRegister(shell.Command{
		Name:    "display",
		Usage:   "display segments|lcd|both [--speed 220ms] [--duration 5s] [--repeat once|loop|interval] [--interval 30s] [--scroll] [--] [TEXT]",
		Summary: "send arbitrary timed text; long segment messages marquee automatically",
		Run: func(ctx context.Context, args []string) (string, error) {
			request, err := parseDisplayCommand(args)
			if err != nil {
				return "", err
			}
			result, err := runtime.PresentDisplay(ctx, request)
			if err != nil {
				return "", err
			}
			if result.Text == "" {
				return "display override cleared", nil
			}
			return fmt.Sprintf(
				"display text accepted target=%s scroll=%t repeat=%s speed=%dms duration=%dms interval=%dms",
				result.Target, result.Scrolling, result.Repeat, result.SpeedMS,
				result.DurationMS, result.IntervalMS,
			), nil
		},
	})
	mustRegister(shell.Command{
		Name: "macro", Usage: "macro list|show NAME_OR_ID|create ID NAME [CATEGORY [COLOR]]|update NAME_OR_ID NEW_NAME [CATEGORY [COLOR]]|rename NAME_OR_ID NAME|category NAME_OR_ID CATEGORY|delete NAME_OR_ID|record start NAME [CATEGORY [COLOR]]|record board start ID|stop|clear [force]|status|record status|save|stop|discard|play NAME_OR_ID|status|monitor|cancel [keep]",
		Summary: "manage and play MCU-timed multi-peripheral macros",
		Run: func(ctx context.Context, args []string) (string, error) {
			return macroCommand(ctx, macroRunner, args)
		},
	})
	mustRegister(shell.Command{
		Name: "os",
		Usage: "os status|facts [PROFILE|list]|policy|key KEY [HOLD_MS] | os virtual enable|disable|allow|deny [KEY] | " +
			"os power ACTION CONFIRMATION | os power-policy enable|disable|allow|deny [ACTION] | " +
			"os brightness get|set VALUE | os brightness-policy enable|disable|range MIN MAX",
		Summary: "inspect the PC or invoke explicitly policy-gated OS actions",
		Run: func(ctx context.Context, args []string) (string, error) {
			return osCommand(ctx, runtime, options, args)
		},
	})
	mustRegister(shell.Command{
		Name: "automation", Usage: "automation list|run NAME",
		Summary: "list or manually run host JSON event-action rules",
		Run: func(ctx context.Context, args []string) (string, error) {
			if options.HostConfig == nil {
				return "", fmt.Errorf("host automations are unavailable")
			}
			config := options.HostConfig()
			if len(args) == 1 && strings.EqualFold(args[0], "list") {
				return AutomationList(config), nil
			}
			if len(args) == 2 && strings.EqualFold(args[0], "run") {
				if err := ExecuteAutomationByName(
					ctx,
					runtime,
					engine,
					config,
					args[1],
				); err != nil {
					return "", err
				}
				return "automation " + args[1] + " completed", nil
			}
			return "", fmt.Errorf("usage: automation list|run NAME")
		},
	})
	mustRegister(shell.Command{
		Name:    "rf",
		Usage:   "rf send ... | learn [SEC] | cancel | list | remove ID|all | map ID ACTION ...",
		Summary: "send, learn, list, remove, and map 433 MHz controls",
		Run: func(ctx context.Context, args []string) (string, error) {
			return rfCommand(ctx, runtime, options.HostConfig, args)
		},
	})
	mustRegister(shell.Command{
		Name: "i2c", Usage: "i2c scan|read|write|transfer|release|lcd", Summary: "scan or use the cooperative host-owned I2C bus",
		Run: func(ctx context.Context, args []string) (string, error) {
			return i2cCommand(ctx, runtime, args)
		},
	})
	mustRegister(shell.Command{
		Name: "reset", Usage: "reset lines|app|bootloader",
		Summary: "pulse DTR or request a device reset",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) != 1 {
				return "", fmt.Errorf("usage: reset lines|app|bootloader")
			}
			switch strings.ToLower(args[0]) {
			case "lines", "dtr", "rts":
				if err := runtime.PulseReset(ctx); err != nil {
					return "", err
				}
				reconnectContext, cancel := context.WithTimeout(ctx, 12*time.Second)
				defer cancel()
				if err := runtime.Reconnect(
					reconnectContext,
					"DTR reset pulse completed",
				); err != nil {
					return "", err
				}
				return "DTR reset complete; application HELLO reauthenticated", nil
			case "app":
				if err := command(
					ctx,
					runtime,
					native.OpReset,
					[]byte{native.ResetApplication},
				); err != nil {
					return "", err
				}
				reconnectContext, cancel := context.WithTimeout(ctx, 12*time.Second)
				defer cancel()
				if err := runtime.Reconnect(
					reconnectContext,
					"application reboot acknowledged",
				); err != nil {
					return "", err
				}
				return "reboot ACK received; application HELLO reauthenticated", nil
			case "boot", "bootloader":
				return "reset requested; use DTR/urclock for guaranteed bootloader entry",
					command(ctx, runtime, native.OpReset, []byte{native.ResetBootloader})
			default:
				return "", fmt.Errorf("usage: reset lines|app|bootloader")
			}
		},
	})
	mustRegister(shell.Command{
		Name: "query", Aliases: []string{"command"},
		Usage:   "query OPCODE RESPONSE_OPCODE [PAYLOAD_HEX]",
		Summary: "send a generic native request",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) < 2 || len(args) > 3 {
				return "", fmt.Errorf("usage: query OPCODE RESPONSE_OPCODE [PAYLOAD_HEX]")
			}
			opcode, err := parseByte(args[0])
			if err != nil {
				return "", err
			}
			responseOpcode, err := parseByte(args[1])
			if err != nil {
				return "", err
			}
			var payload []byte
			if len(args) == 3 {
				payload, err = decodeHex(args[2])
				if err != nil {
					return "", err
				}
			}
			frame, err := request(ctx, runtime, opcode, payload, responseOpcode)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%s seq=%d payload=% X", native.OpcodeName(frame.Opcode), frame.Seq, frame.Payload), nil
		},
	})
	mustRegister(shell.Command{
		Name:    "opcode",
		Usage:   "opcode OPCODE [PAYLOAD_HEX] [--expect RESPONSE_OPCODE]",
		Summary: "exchange an opaque versionless opcode (ACK expected by default)",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 0 {
				return "", fmt.Errorf("usage: opcode OPCODE [PAYLOAD_HEX] [--expect RESPONSE_OPCODE]")
			}
			opcode, err := parseByte(args[0])
			if err != nil || opcode == 0 {
				return "", fmt.Errorf("opcode must be 1..255")
			}
			expected := byte(native.OpACK)
			var payload []byte
			for index := 1; index < len(args); index++ {
				if strings.EqualFold(args[index], "--expect") {
					if index+1 >= len(args) {
						return "", errors.New("--expect requires a response opcode")
					}
					expected, err = parseByte(args[index+1])
					if err != nil || expected == 0 {
						return "", errors.New("response opcode must be 1..255")
					}
					index++
					continue
				}
				if payload != nil {
					return "", errors.New("only one hexadecimal payload is allowed")
				}
				payload, err = decodeHex(args[index])
				if err != nil {
					return "", err
				}
			}
			if len(payload) > native.MaxPayload {
				return "", native.ErrPayloadTooLong
			}
			frame, err := request(ctx, runtime, opcode, payload, expected)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"opcode=0x%02X name=%s seq=%d payload=% X",
				frame.Opcode, native.OpcodeName(frame.Opcode), frame.Seq, frame.Payload,
			), nil
		},
	})
	mustRegister(shell.Command{
		Name: "write", Usage: "write HEX_BYTES", Summary: "write raw serial bytes",
		Run: func(_ context.Context, args []string) (string, error) {
			if len(args) == 0 {
				return "", fmt.Errorf("usage: write HEX_BYTES")
			}
			data, err := decodeHex(strings.Join(args, ""))
			if err != nil {
				return "", err
			}
			if err := runtime.WriteRaw(data); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d raw bytes", len(data)), nil
		},
	})
	mustRegister(shell.Command{
		Name:    "board",
		Usage:   "board initialize [--name NAME] [...] | board blank --confirm NAME [...] | board name [get|set NAME|clear]",
		Summary: "provision, securely blank, or name a board",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) == 0 {
				return "", errors.New("usage: board initialize [--name NAME] [...] | board blank --confirm NAME [...] | board name [get|set NAME|clear]")
			}
			if strings.EqualFold(args[0], "initialize") {
				if options.InitializeBoard == nil {
					return "", errors.New("board initialization is unavailable")
				}
				var output bytes.Buffer
				err := options.InitializeBoard(ctx, runtime, args[1:], &output)
				return strings.TrimSpace(output.String()), err
			}
			if strings.EqualFold(args[0], "blank") {
				if options.BlankBoard == nil {
					return "", errors.New("board blanking is unavailable")
				}
				var output bytes.Buffer
				err := options.BlankBoard(ctx, runtime, args[1:], &output)
				return strings.TrimSpace(output.String()), err
			}
			if !strings.EqualFold(args[0], "name") {
				return "", errors.New("usage: board initialize [--name NAME] [...] | board blank --confirm NAME [...] | board name [get|set NAME|clear]")
			}
			if len(args) == 1 || (len(args) == 2 && strings.EqualFold(args[1], "get")) {
				value, err := runtime.BoardName(ctx)
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("name=%q persisted=%t", value.Name, value.Persisted), nil
			}
			name := ""
			switch {
			case len(args) == 2 && strings.EqualFold(args[1], "clear"):
			case len(args) == 3 && strings.EqualFold(args[1], "set"):
				name = args[2]
			default:
				return "", errors.New("usage: board name [get|set NAME|clear]")
			}
			value, err := runtime.SetBoardName(ctx, name)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("name=%q persisted=%t", value.Name, value.Persisted), nil
		},
	})
	mustRegister(shell.Command{
		Name:    "driver",
		Usage:   "driver usbasp status|ensure|zadig",
		Summary: "inspect or repair the Windows USBasp driver",
		Run: func(ctx context.Context, args []string) (string, error) {
			if options.USBaspDriver == nil {
				return "", errors.New("USBasp driver management is unavailable")
			}
			var output bytes.Buffer
			err := options.USBaspDriver(ctx, args, &output)
			return strings.TrimSpace(output.String()), err
		},
	})
	mustRegister(shell.Command{
		Name:    "toolchain",
		Usage:   "toolchain bootstrap|sync|profile|compile SKETCH|core-info|install-bootloader [PORT]",
		Summary: "bootstrap or synchronize the firmware build/programming toolchain",
		Run: func(ctx context.Context, args []string) (string, error) {
			resolved := resolveCommandOptions(options)
			if len(args) == 1 && strings.EqualFold(args[0], "sync") {
				var output bytes.Buffer
				report, updateErr := programmer.SyncToolchain(
					ctx,
					programmer.ToolchainSyncOptions{
						ToolchainCLI: resolved.ArduinoCLI,
						DirectRetry:  true,
					},
					&output,
				)
				fmt.Fprintf(
					&output,
					"\nToolchain sync finished: %d steps.\n",
					len(report.Steps),
				)
				return strings.TrimSpace(output.String()), updateErr
			}
			if len(args) == 1 && strings.EqualFold(args[0], "profile") {
				encoded, err := json.MarshalIndent(programmer.DefaultToolchainProfile(), "", "  ")
				return string(encoded), err
			}
			if len(args) >= 1 && strings.EqualFold(args[0], "bootstrap") {
				dryRun := false
				if len(args) == 2 && strings.EqualFold(args[1], "--dry-run") {
					dryRun = true
				} else if len(args) != 1 {
					return "", errors.New("usage: toolchain bootstrap [--dry-run]")
				}
				var output bytes.Buffer
				report, bootstrapErr := programmer.BootstrapToolchain(
					ctx,
					programmer.ToolchainBootstrapOptions{
						Profile:     programmer.DefaultToolchainProfile(),
						DirectRetry: true, DryRun: dryRun,
					},
					&output,
				)
				if bootstrapErr == nil && !dryRun && options.UpdateHostConfig != nil {
					if err := options.UpdateHostConfig(func(config *appconfig.Config) error {
						config.Programming.ToolchainCLI = report.CLIPath
						return nil
					}); err != nil {
						bootstrapErr = fmt.Errorf("save managed toolchain path: %w", err)
					}
				}
				encoded, _ := json.MarshalIndent(report, "", "  ")
				fmt.Fprintln(&output, string(encoded))
				return strings.TrimSpace(output.String()), bootstrapErr
			}
			programArgs, err := toolchainProgramArguments(args)
			if err != nil {
				return "", err
			}
			return programCommand(ctx, runtime, resolved, programArgs)
		},
	})
	mustRegister(shell.Command{
		Name:    "boot",
		Usage:   "boot probe|info|metadata|backup DIR|read FILE|write FILE|verify FILE|start [PORT]",
		Summary: "temporarily hand the UART to Urboot/Urclock, then reconnect the app",
		Run: func(ctx context.Context, args []string) (string, error) {
			programArgs, err := bootProgramArguments(args)
			if err != nil {
				return "", err
			}
			resolved := resolveCommandOptions(options)
			return programCommand(ctx, runtime, resolved, programArgs)
		},
	})
	mustRegister(shell.Command{
		Name:    "program",
		Usage:   "program flash HEX [PORT] [--method urclock|usbasp] [--allow-incomplete-backup] [--reinitialize-eeprom] | program OPERATION METHOD PATH [PORT]",
		Summary: "guarded backup-then-flash, or non-write programmer diagnostics",
		Run: func(ctx context.Context, args []string) (string, error) {
			resolved := resolveCommandOptions(options)
			return programCommand(ctx, runtime, resolved, args)
		},
	})
	return engine
}

func messageTargetAllowed(target string) bool {
	switch target {
	case "native", "web", "tui", "host", "client", "server", "bridge", "board", "lcd", "all":
		return true
	default:
		return false
	}
}

func encodeLiveSettingsExport(settings native.Settings) (string, error) {
	encoded, err := json.MarshalIndent(struct {
		Format   string          `json:"format"`
		Source   string          `json:"source"`
		Settings native.Settings `json:"settings"`
	}{
		Format: "controller-mcu-settings/v1", Source: "live-opcode",
		Settings: settings,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func hostConfigCommand(options CommandOptions, args []string) (string, error) {
	const usage = "config get PATH | config set PATH VALUE; PATH is ui.app_title, ui.tagline, ui.appearance.*, ui.tui_console.*, or integrations.buzzer_mirror.{enabled,native_enabled,web_audio_enabled,driver_directory}"
	if len(args) < 2 {
		return "", errors.New(usage)
	}
	action := strings.ToLower(strings.TrimSpace(args[0]))
	path := strings.ToLower(strings.TrimSpace(args[1]))
	switch action {
	case "get":
		if len(args) != 2 {
			return "", errors.New(usage)
		}
		if options.HostConfig == nil {
			return "", errors.New("host configuration is unavailable")
		}
		config := options.HostConfig()
		ui := config.UI
		buzzer := config.Integrations.BuzzerMirror
		switch path {
		case "ui.app_title":
			return fmt.Sprintf("ui.app_title=%q", ui.AppTitle), nil
		case "ui.tagline":
			return fmt.Sprintf("ui.tagline=%q", ui.Tagline), nil
		case "ui.appearance.theme":
			return "ui.appearance.theme=" + ui.Appearance.Theme, nil
		case "ui.appearance.locale":
			return "ui.appearance.locale=" + ui.Appearance.Locale, nil
		case "ui.appearance.direction":
			return "ui.appearance.direction=" + ui.Appearance.Direction, nil
		case "ui.appearance.reduce_motion":
			return fmt.Sprintf("ui.appearance.reduce_motion=%t", ui.Appearance.ReduceMotion), nil
		case "ui.appearance.compact_numbers":
			return fmt.Sprintf("ui.appearance.compact_numbers=%t", ui.Appearance.CompactNumbers), nil
		case "ui.appearance.audio_muted":
			return fmt.Sprintf("ui.appearance.audio_muted=%t", ui.Appearance.AudioMuted), nil
		case "ui.appearance.audio_volume":
			return fmt.Sprintf("ui.appearance.audio_volume=%.0f%%", ui.Appearance.AudioVolume*100), nil
		case "ui.tui_console.enabled":
			return fmt.Sprintf("ui.tui_console.enabled=%t", ui.TUIConsole.Enabled), nil
		case "ui.tui_console.columns":
			return fmt.Sprintf("ui.tui_console.columns=%d", ui.TUIConsole.Columns), nil
		case "ui.tui_console.rows":
			return fmt.Sprintf("ui.tui_console.rows=%d", ui.TUIConsole.Rows), nil
		case "ui.tui_console.font_face":
			return fmt.Sprintf("ui.tui_console.font_face=%q", ui.TUIConsole.FontFace), nil
		case "ui.tui_console.font_size":
			return fmt.Sprintf("ui.tui_console.font_size=%d", ui.TUIConsole.FontSize), nil
		case "integrations.buzzer_mirror.enabled":
			return fmt.Sprintf("integrations.buzzer_mirror.enabled=%t", buzzer.Enabled), nil
		case "integrations.buzzer_mirror.native_enabled":
			return fmt.Sprintf("integrations.buzzer_mirror.native_enabled=%t", buzzer.NativeEnabled), nil
		case "integrations.buzzer_mirror.web_audio_enabled":
			return fmt.Sprintf("integrations.buzzer_mirror.web_audio_enabled=%t", buzzer.WebAudioEnabled), nil
		case "integrations.buzzer_mirror.driver_directory":
			return fmt.Sprintf("integrations.buzzer_mirror.driver_directory=%q", buzzer.DriverDirectory), nil
		default:
			return "", fmt.Errorf("unsupported host setting %q", args[1])
		}
	case "set":
		if len(args) < 3 {
			return "", errors.New(usage)
		}
		if options.HostConfig == nil {
			return "", errors.New("host configuration is unavailable")
		}
		if options.UpdateHostConfig == nil {
			return "", errors.New("host configuration is read-only")
		}
		raw := strings.TrimSpace(strings.Join(args[2:], " "))
		candidate := options.HostConfig()
		beforeUI := candidate.UI
		beforeBuzzer := candidate.Integrations.BuzzerMirror
		switch path {
		case "ui.app_title":
			if raw == "" {
				return "", errors.New("ui.app_title cannot be empty")
			}
			candidate.UI.AppTitle = raw
		case "ui.tagline":
			if raw == "" {
				return "", errors.New("ui.tagline cannot be empty")
			}
			candidate.UI.Tagline = raw
		case "ui.appearance.theme":
			candidate.UI.Appearance.Theme = raw
		case "ui.appearance.locale":
			candidate.UI.Appearance.Locale = raw
		case "ui.appearance.direction":
			candidate.UI.Appearance.Direction = raw
		case "ui.appearance.reduce_motion":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("ui.appearance.reduce_motion: %w", err)
			}
			candidate.UI.Appearance.ReduceMotion = value
		case "ui.appearance.compact_numbers":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("ui.appearance.compact_numbers: %w", err)
			}
			candidate.UI.Appearance.CompactNumbers = value
		case "ui.appearance.audio_muted":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("ui.appearance.audio_muted: %w", err)
			}
			candidate.UI.Appearance.AudioMuted = value
		case "ui.appearance.audio_volume":
			percent, err := strconv.ParseFloat(strings.TrimSuffix(raw, "%"), 64)
			if err != nil || percent < 0 || percent > 100 {
				return "", errors.New("ui.appearance.audio_volume must be 0..100 percent")
			}
			candidate.UI.Appearance.AudioVolume = percent / 100
		case "ui.tui_console.enabled":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("ui.tui_console.enabled: %w", err)
			}
			candidate.UI.TUIConsole.Enabled = value
		case "ui.tui_console.columns":
			value, err := strconv.Atoi(raw)
			if err != nil {
				return "", errors.New("ui.tui_console.columns must be 56..300")
			}
			candidate.UI.TUIConsole.Columns = value
		case "ui.tui_console.rows":
			value, err := strconv.Atoi(raw)
			if err != nil {
				return "", errors.New("ui.tui_console.rows must be 18..120")
			}
			candidate.UI.TUIConsole.Rows = value
		case "ui.tui_console.font_face":
			candidate.UI.TUIConsole.FontFace = raw
		case "ui.tui_console.font_size":
			value, err := strconv.Atoi(raw)
			if err != nil {
				return "", errors.New("ui.tui_console.font_size must be 5..72")
			}
			candidate.UI.TUIConsole.FontSize = value
		case "integrations.buzzer_mirror.enabled":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("integrations.buzzer_mirror.enabled: %w", err)
			}
			candidate.Integrations.BuzzerMirror.Enabled = value
		case "integrations.buzzer_mirror.native_enabled":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("integrations.buzzer_mirror.native_enabled: %w", err)
			}
			candidate.Integrations.BuzzerMirror.NativeEnabled = value
		case "integrations.buzzer_mirror.web_audio_enabled":
			value, err := parseHostConfigBool(raw)
			if err != nil {
				return "", fmt.Errorf("integrations.buzzer_mirror.web_audio_enabled: %w", err)
			}
			candidate.Integrations.BuzzerMirror.WebAudioEnabled = value
		case "integrations.buzzer_mirror.driver_directory":
			candidate.Integrations.BuzzerMirror.DriverDirectory = raw
		default:
			return "", fmt.Errorf("unsupported host setting %q", args[1])
		}
		candidate.UI.Appearance = appconfig.NormalizeAppearance(candidate.UI.Appearance)
		if err := candidate.Validate(); err != nil {
			return "", err
		}
		if reflect.DeepEqual(beforeUI, candidate.UI) &&
			reflect.DeepEqual(beforeBuzzer, candidate.Integrations.BuzzerMirror) {
			return path + " unchanged", nil
		}
		if err := options.UpdateHostConfig(func(config *appconfig.Config) error {
			config.UI = candidate.UI
			config.Integrations.BuzzerMirror = candidate.Integrations.BuzzerMirror
			return config.Validate()
		}); err != nil {
			return "", err
		}
		return path + " saved and hot-reload queued", nil
	default:
		return "", errors.New(usage)
	}
}

func parseHostConfigBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "on", "yes", "1":
		return true, nil
	case "false", "off", "no", "0":
		return false, nil
	default:
		return false, errors.New("value must be true/false or on/off")
	}
}

func resolveCommandOptions(base CommandOptions) CommandOptions {
	if base.Resolve == nil {
		return base
	}
	resolved := base.Resolve()
	if resolved.Outputs == nil {
		resolved.Outputs = base.Outputs
	}
	if resolved.HostConfig == nil {
		resolved.HostConfig = base.HostConfig
	}
	if resolved.UpdateHostConfig == nil {
		resolved.UpdateHostConfig = base.UpdateHostConfig
	}
	if resolved.ProgramRunner == nil {
		resolved.ProgramRunner = base.ProgramRunner
	}
	if resolved.ProgramExecute == nil {
		resolved.ProgramExecute = base.ProgramExecute
	}
	if strings.TrimSpace(resolved.ProgramDataPaths.DataDir) == "" {
		resolved.ProgramDataPaths = base.ProgramDataPaths
	}
	return resolved
}

func parseProgramMode(value string) ProgramMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "idle", "off", "stop", "stopped":
		return ProgramIdle
	case "running", "run", "on", "start":
		return ProgramRunning
	default:
		return ""
	}
}

func formatProgramState(state ProgramStateSnapshot) string {
	owners := make([]string, 0, len(state.Owners))
	for _, owner := range state.Owners {
		owners = append(owners, fmt.Sprintf("%s(x%d)", owner.Name, owner.References))
	}
	return fmt.Sprintf("program state=%s reason=%q owners=[%s] revision=%d",
		state.Mode, state.Reason, strings.Join(owners, ", "), state.Revision)
}

func osCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	if len(args) == 0 {
		return "", errors.New("usage: os status|facts|policy|key|virtual|power|power-policy|brightness|brightness-policy ...")
	}
	config := appconfig.Defaults()
	if options.HostConfig != nil {
		config = options.HostConfig()
	}
	audit := func(kind, text string) { runtime.PublishHostEvent(kind, text) }
	switch strings.ToLower(args[0]) {
	case "status", "info", "ip":
		if len(args) != 1 {
			return "", errors.New("usage: os status")
		}
		status, err := hostos.Status(ports.EnumerationSource())
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf(
			"host=%s os=%s/%s cpus=%d pid=%d uptime=%s serial=%s",
			status.Hostname, status.OperatingSystem, status.Architecture,
			status.LogicalCPUs, status.ProcessID,
			time.Duration(status.HostUptimeMS)*time.Millisecond,
			status.DiscoverySource,
		)}
		for _, address := range status.NetworkAddresses {
			if !address.Loopback {
				lines = append(lines, fmt.Sprintf("ip %-16s %s", address.Interface, address.Address))
			}
		}
		return strings.Join(lines, "\n"), nil
	case "facts":
		if len(args) > 2 {
			return "", errors.New("usage: os facts [system|computer|firmware|storage|serial|list]")
		}
		if len(args) == 2 && (strings.EqualFold(args[1], "list") || strings.EqualFold(args[1], "catalog")) {
			encoded, err := json.MarshalIndent(hostfacts.Catalog(), "", "  ")
			if err != nil {
				return "", err
			}
			return string(encoded), nil
		}
		profile := ""
		if len(args) == 2 {
			profile = args[1]
		}
		provider := options.HostFacts
		if provider == nil {
			provider = hostfacts.Default()
		}
		result, err := provider.Query(ctx, profile)
		if err != nil {
			audit("os.host-facts", "read unavailable: "+err.Error())
			return "", err
		}
		audit("os.host-facts", fmt.Sprintf("read profile=%s rows=%d truncated=%t", result.Profile, len(result.Rows), result.Truncated))
		return formatHostFacts(result), nil
	case "policy":
		if len(args) != 1 {
			return "", errors.New("usage: os policy")
		}
		return formatOSPolicy(config.OSActions), nil
	case "key", "vk", "virtual-key":
		if len(args) < 2 || len(args) > 3 {
			return "", errors.New("usage: os key KEY [HOLD_MS]")
		}
		request := hostos.VirtualKeyRequest{Key: args[1]}
		if len(args) == 3 {
			hold, err := strconv.Atoi(args[2])
			if err != nil {
				return "", fmt.Errorf("invalid hold_ms %q", args[2])
			}
			request.HoldMS = hold
		}
		result, err := hostos.DefaultExecutor.PressVirtualKey(ctx, config.OSActions.VirtualKeys, request)
		if err != nil {
			audit("os.virtual-key.audit", "CLI/API virtual key denied: "+err.Error())
			return "", err
		}
		audit("os.virtual-key.audit", "CLI/API "+result.Detail)
		return result.Detail, nil
	case "power":
		if len(args) != 3 {
			return "", errors.New("usage: os power ACTION CONFIRMATION")
		}
		result, err := hostos.DefaultExecutor.Power(ctx, config.OSActions.Power, hostos.PowerRequest{
			Action: args[1], Confirmation: args[2],
		})
		if err != nil {
			audit("os.power.audit", "CLI/API power action denied: "+err.Error())
			return "", err
		}
		audit("os.power.audit", "CLI/API "+result.Detail)
		return result.Detail, nil
	case "lock", "sleep", "hibernate", "shutdown", "restart", "logoff":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: os %s CONFIRMATION", args[0])
		}
		result, err := hostos.DefaultExecutor.Power(ctx, config.OSActions.Power, hostos.PowerRequest{
			Action: args[0], Confirmation: args[1],
		})
		if err != nil {
			audit("os.power.audit", "CLI/API power action denied: "+err.Error())
			return "", err
		}
		audit("os.power.audit", "CLI/API "+result.Detail)
		return result.Detail, nil
	case "virtual":
		return updateVirtualKeyPolicy(options, config, args[1:])
	case "power-policy":
		return updatePowerPolicy(options, config, args[1:])
	case "brightness":
		if len(args) == 2 && strings.EqualFold(args[1], "get") {
			result, err := hostos.DefaultExecutor.MonitorBrightness(ctx)
			if err != nil {
				audit("os.brightness", "brightness read unavailable: "+err.Error())
				return "", err
			}
			text := formatBrightness(result)
			audit("os.brightness", "brightness read: "+text)
			return text, nil
		}
		if len(args) == 3 && strings.EqualFold(args[1], "set") {
			percent, err := strconv.Atoi(args[2])
			if err != nil || percent < 0 || percent > 100 {
				return "", fmt.Errorf("monitor brightness %q must be an integer from 0 through 100", args[2])
			}
			result, err := hostos.DefaultExecutor.SetMonitorBrightness(ctx, config.OSActions.Brightness, percent)
			if err != nil {
				audit("os.brightness", fmt.Sprintf("brightness set %d denied/unavailable: %v", percent, err))
				return "", err
			}
			text := formatBrightness(result)
			audit("os.brightness", "brightness changed: "+text)
			return text, nil
		}
		return "", errors.New("usage: os brightness get|set VALUE")
	case "brightness-policy":
		return updateBrightnessPolicy(options, config, args[1:])
	default:
		return "", fmt.Errorf("unknown OS operation %q", args[0])
	}
}

func formatOSPolicy(policy hostos.Policy) string {
	return fmt.Sprintf(
		"virtual_keys enabled=%t allowed=%s interval=%dms hold=%dms\n"+
			"power enabled=%t allowed=%s confirm=%t automation=%t\n"+
			"brightness enabled=%t range=%d..%d",
		policy.VirtualKeys.Enabled, strings.Join(policy.VirtualKeys.Allowed, ","),
		policy.VirtualKeys.MinIntervalMS, policy.VirtualKeys.HoldMS,
		policy.Power.Enabled, strings.Join(policy.Power.Allowed, ","),
		policy.Power.RequireConfirmation, policy.Power.AllowAutomation,
		policy.Brightness.Enabled, policy.Brightness.MinPercent, policy.Brightness.MaxPercent,
	)
}

func formatBrightness(result hostos.BrightnessResult) string {
	verb := "read"
	if result.Changed {
		verb = "set"
	}
	backend := result.Status.Backend
	if backend == "" {
		backend = "platform"
	}
	kind := "external"
	if result.Status.Integrated {
		kind = "integrated"
	}
	return fmt.Sprintf(
		"monitor brightness %s=%d%% raw=%d/%d..%d display=%q backend=%s kind=%s",
		verb, result.Status.Percent, result.Status.RawCurrent,
		result.Status.RawMinimum, result.Status.RawMaximum, result.Status.Display,
		backend, kind,
	)
}

func formatHostFacts(result hostfacts.Result) string {
	lines := []string{fmt.Sprintf(
		"host facts profile=%s class=%s source=%s rows=%d truncated=%t duration=%dms collected=%s",
		result.Profile,
		result.Class,
		result.Source,
		len(result.Rows),
		result.Truncated,
		result.DurationMS,
		result.CollectedAt.UTC().Format(time.RFC3339),
	)}
	for index, row := range result.Rows {
		fields := make([]string, 0, len(result.Columns))
		for _, column := range result.Columns {
			value, exists := row[column]
			if !exists {
				continue
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				encoded = []byte("null")
			}
			fields = append(fields, column+"="+string(encoded))
		}
		lines = append(lines, fmt.Sprintf("row[%d] %s", index, strings.Join(fields, " ")))
	}
	return strings.Join(lines, "\n")
}

func updateVirtualKeyPolicy(
	options CommandOptions,
	config appconfig.Config,
	args []string,
) (string, error) {
	if options.HostConfig == nil || options.UpdateHostConfig == nil || len(args) < 1 || len(args) > 2 {
		return "", errors.New("usage: os virtual enable|disable|allow KEY|deny KEY")
	}
	action := strings.ToLower(args[0])
	if (action == "allow" || action == "deny") && len(args) != 2 {
		return "", errors.New("usage: os virtual allow|deny KEY")
	}
	if (action == "enable" || action == "disable") && len(args) != 1 {
		return "", errors.New("usage: os virtual enable|disable")
	}
	var resolved hostos.ResolvedVirtualKey
	var err error
	if len(args) == 2 {
		resolved, err = hostos.ResolveVirtualKey(args[1])
		if err != nil {
			return "", err
		}
	}
	err = options.UpdateHostConfig(func(value *appconfig.Config) error {
		switch action {
		case "enable":
			value.OSActions.VirtualKeys.Enabled = true
		case "disable":
			value.OSActions.VirtualKeys.Enabled = false
		case "allow":
			for _, existing := range value.OSActions.VirtualKeys.Allowed {
				candidate, _ := hostos.ResolveVirtualKey(existing)
				if candidate.Code == resolved.Code {
					return fmt.Errorf("virtual key %s is already allowed", resolved.Name)
				}
			}
			value.OSActions.VirtualKeys.Allowed = append(value.OSActions.VirtualKeys.Allowed, resolved.Name)
		case "deny":
			filtered := value.OSActions.VirtualKeys.Allowed[:0]
			for _, existing := range value.OSActions.VirtualKeys.Allowed {
				candidate, _ := hostos.ResolveVirtualKey(existing)
				if candidate.Code != resolved.Code {
					filtered = append(filtered, existing)
				}
			}
			value.OSActions.VirtualKeys.Allowed = filtered
		default:
			return fmt.Errorf("unknown virtual-key policy operation %q", action)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	config = options.HostConfig()
	return formatOSPolicy(config.OSActions), nil
}

func updatePowerPolicy(
	options CommandOptions,
	config appconfig.Config,
	args []string,
) (string, error) {
	if options.HostConfig == nil || options.UpdateHostConfig == nil || len(args) < 1 || len(args) > 2 {
		return "", errors.New("usage: os power-policy enable|disable|allow ACTION|deny ACTION")
	}
	action := strings.ToLower(args[0])
	if action == "enable" {
		if len(args) != 2 || args[1] != config.OSActions.Power.ConfirmationToken {
			return "", errors.New("enabling power actions requires the configured confirmation token")
		}
	} else if action == "disable" {
		if len(args) != 1 {
			return "", errors.New("usage: os power-policy disable")
		}
	} else if (action == "allow" || action == "deny") && len(args) != 2 {
		return "", errors.New("usage: os power-policy allow|deny ACTION")
	}
	var normalized string
	var err error
	if action == "allow" || action == "deny" {
		normalized, err = hostos.NormalizePowerAction(args[1])
		if err != nil {
			return "", err
		}
	}
	err = options.UpdateHostConfig(func(value *appconfig.Config) error {
		switch action {
		case "enable":
			value.OSActions.Power.Enabled = true
		case "disable":
			value.OSActions.Power.Enabled = false
		case "allow":
			for _, existing := range value.OSActions.Power.Allowed {
				candidate, _ := hostos.NormalizePowerAction(existing)
				if candidate == normalized {
					return fmt.Errorf("power action %s is already allowed", normalized)
				}
			}
			value.OSActions.Power.Allowed = append(value.OSActions.Power.Allowed, normalized)
		case "deny":
			filtered := value.OSActions.Power.Allowed[:0]
			for _, existing := range value.OSActions.Power.Allowed {
				candidate, _ := hostos.NormalizePowerAction(existing)
				if candidate != normalized {
					filtered = append(filtered, existing)
				}
			}
			value.OSActions.Power.Allowed = filtered
		default:
			return fmt.Errorf("unknown power-policy operation %q", action)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	config = options.HostConfig()
	return formatOSPolicy(config.OSActions), nil
}

func updateBrightnessPolicy(
	options CommandOptions,
	config appconfig.Config,
	args []string,
) (string, error) {
	if options.HostConfig == nil || options.UpdateHostConfig == nil || len(args) == 0 {
		return "", errors.New("usage: os brightness-policy enable|disable|range MIN MAX")
	}
	action := strings.ToLower(args[0])
	if (action == "enable" || action == "disable") && len(args) != 1 {
		return "", errors.New("usage: os brightness-policy enable|disable")
	}
	minimum, maximum := 0, 100
	if action == "range" {
		if len(args) != 3 {
			return "", errors.New("usage: os brightness-policy range MIN MAX")
		}
		var err error
		minimum, err = strconv.Atoi(args[1])
		if err != nil {
			return "", fmt.Errorf("invalid minimum brightness %q", args[1])
		}
		maximum, err = strconv.Atoi(args[2])
		if err != nil {
			return "", fmt.Errorf("invalid maximum brightness %q", args[2])
		}
	}
	if action != "enable" && action != "disable" && action != "range" {
		return "", fmt.Errorf("unknown brightness-policy operation %q", action)
	}
	err := options.UpdateHostConfig(func(value *appconfig.Config) error {
		switch action {
		case "enable":
			value.OSActions.Brightness.Enabled = true
		case "disable":
			value.OSActions.Brightness.Enabled = false
		case "range":
			value.OSActions.Brightness.MinPercent = minimum
			value.OSActions.Brightness.MaxPercent = maximum
		}
		return hostos.ValidatePolicy(value.OSActions)
	})
	if err != nil {
		return "", err
	}
	config = options.HostConfig()
	return formatOSPolicy(config.OSActions), nil
}

func formatMenuCatalog(catalog MenuCatalog) string {
	lines := []string{fmt.Sprintf(
		"firmware=%08X current=%d mode=%d source=%s layout_supported=%t persistent=%t mask=0x%04X",
		catalog.FirmwareHash,
		catalog.CurrentPage,
		catalog.ProgramMode,
		catalog.Source,
		catalog.Layout.Supported,
		catalog.Layout.Persistent,
		catalog.Layout.VisibleMask,
	)}
	lines = append(lines, "    RANK VIS ID KEY                 LABEL NAME")
	for rank, page := range catalog.Pages {
		marker := " "
		if page.ID == catalog.CurrentPage {
			marker = ">"
		}
		visible := "no "
		if catalog.Layout.Visible(page.ID) {
			visible = "yes"
		}
		lines = append(lines, fmt.Sprintf(
			"%s %4d %-3s %2d %-19s %-5q %s — %s",
			marker,
			rank,
			visible,
			page.ID,
			page.Key,
			page.Label,
			page.Name,
			page.Description,
		))
	}
	return strings.Join(lines, "\n")
}

func menuPageByID(pages []MenuPageInfo, id byte) (MenuPageInfo, error) {
	for _, page := range pages {
		if page.ID == id {
			return page, nil
		}
	}
	return MenuPageInfo{}, fmt.Errorf("menu page %d is not in the active catalog", id)
}

func queryLiveMenuCatalog(
	ctx context.Context,
	runtime *Runtime,
	status native.Status,
) (MenuCatalog, error) {
	var entries []native.MenuEntry
	cursor := byte(0)
	seen := make(map[byte]bool)
	for {
		if seen[cursor] {
			return MenuCatalog{}, fmt.Errorf("MENU_LIST cursor loop at %d", cursor)
		}
		seen[cursor] = true
		frame, err := request(
			ctx, runtime, native.OpMenuList, []byte{cursor}, native.OpMenuListResp,
		)
		if err != nil {
			return MenuCatalog{}, err
		}
		page, err := native.ParseMenuList(frame.Payload)
		if err != nil {
			return MenuCatalog{}, err
		}
		entries = append(entries, page.Entries...)
		if page.NextCursor == 0xFF {
			if len(entries) != int(page.Total) {
				return MenuCatalog{}, fmt.Errorf(
					"MENU_LIST returned %d of %d entries",
					len(entries), page.Total,
				)
			}
			break
		}
		cursor = page.NextCursor
	}
	pages := make([]MenuPageInfo, 0, len(entries))
	for _, entry := range entries {
		page, ok := describeLiveMenuEntry(entry)
		if !ok {
			label := strings.TrimSpace(entry.Label)
			page = MenuPageInfo{
				ID: entry.ID, Key: fmt.Sprintf("page-%d", entry.ID),
				Label: label, Name: label,
				Description: "Page reported by the connected firmware",
			}
		}
		page.ID = entry.ID
		page.Label = strings.TrimRight(entry.Label, " \x00")
		pages = append(pages, page)
	}
	snapshot := runtime.Snapshot()
	return MenuCatalog{
		Source: "board MENU_LIST (host descriptions)", LiveList: true,
		FirmwareHash: snapshot.Hello.BuildHash,
		CurrentPage:  status.MenuPage, ProgramMode: status.ProgramMode,
		Pages: pages,
	}, nil
}

func describeLiveMenuEntry(entry native.MenuEntry) (MenuPageInfo, bool) {
	label := normalizeMenuName(strings.TrimRight(entry.Label, " \x00"))
	for _, page := range protocolMenuPages {
		if normalizeMenuName(page.Label) == label {
			page.ID = entry.ID
			return page, true
		}
	}
	return MenuPageInfo{}, false
}

func toolchainProgramArguments(args []string) ([]string, error) {
	const usage = "usage: toolchain compile SKETCH | core-info | install-bootloader [PORT]"
	if len(args) == 0 {
		return nil, fmt.Errorf("%s", usage)
	}
	switch strings.ToLower(args[0]) {
	case "compile":
		if len(args) != 2 {
			return nil, fmt.Errorf("%s", usage)
		}
		return []string{string(programmer.MethodCompile), args[1]}, nil
	case "core-info", "info":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s", usage)
		}
		return []string{
			string(programmer.OperationCoreInfo),
			string(programmer.MethodArduino),
		}, nil
	case "install-bootloader":
		if len(args) > 2 {
			return nil, fmt.Errorf("%s", usage)
		}
		result := []string{
			string(programmer.OperationBurnBoot),
			string(programmer.MethodArduino),
		}
		return append(result, args[1:]...), nil
	default:
		return nil, fmt.Errorf("%s", usage)
	}
}

func bootProgramArguments(args []string) ([]string, error) {
	const usage = "usage: boot probe|info|metadata|backup DIR|read FILE|write FILE|verify FILE|start [PORT]"
	if len(args) == 0 {
		return nil, fmt.Errorf("%s", usage)
	}
	action := strings.ToLower(args[0])
	var operation programmer.Operation
	needsPath := false
	switch action {
	case "probe":
		operation = programmer.OperationProbe
	case "info", "metadata":
		operation = programmer.OperationMetadata
	case "read":
		operation = programmer.OperationReadFlash
		needsPath = true
	case "backup":
		operation = programmer.OperationBackup
		needsPath = true
	case "write":
		if len(args) < 2 || len(args) > 3 {
			return nil, fmt.Errorf("%s", usage)
		}
		result := []string{"flash", args[1]}
		return append(result, args[2:]...), nil
	case "verify":
		operation = programmer.OperationVerifyFlash
		needsPath = true
	case "start":
		operation = programmer.OperationStart
	default:
		return nil, fmt.Errorf("%s", usage)
	}
	minimum := 1
	if needsPath {
		minimum = 2
	}
	if len(args) < minimum || len(args) > minimum+1 {
		return nil, fmt.Errorf("%s", usage)
	}
	result := []string{string(operation), string(programmer.MethodUrclock)}
	if needsPath {
		result = append(result, args[1])
	}
	if len(args) == minimum+1 {
		result = append(result, args[minimum])
	}
	return result, nil
}

func metricCommand(
	name, summary string,
	runtime *Runtime,
	format func(native.Status) string,
) shell.Command {
	return shell.Command{
		Name: name, Usage: name, Summary: summary,
		Run: func(ctx context.Context, _ []string) (string, error) {
			status, err := refresh(ctx, runtime)
			if err != nil {
				return "", err
			}
			return format(status), nil
		},
	}
}

func buzzerRoutingCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	if len(args) == 0 {
		args = []string{"status"}
	}
	if len(args) != 1 && !(len(args) == 2 && strings.EqualFold(args[0], "path")) {
		return "", errors.New("usage: buzzer status | buzzer path board|host|both|none")
	}
	if options.HostConfig == nil {
		return "", errors.New("host buzzer configuration is unavailable")
	}
	settings, err := querySettings(ctx, runtime)
	if err != nil {
		return "", err
	}
	config := options.HostConfig()
	if len(args) == 1 {
		if !strings.EqualFold(args[0], "status") {
			return "", errors.New("usage: buzzer status | buzzer path board|host|both|none")
		}
		return formatBuzzerRouting(settings, config), nil
	}

	desiredPath := strings.ToLower(strings.TrimSpace(args[1]))
	var boardSilent, hostEnabled bool
	switch desiredPath {
	case "board":
		boardSilent, hostEnabled = false, false
	case "host":
		boardSilent, hostEnabled = true, true
	case "both":
		boardSilent, hostEnabled = false, true
	case "none":
		boardSilent, hostEnabled = true, false
	default:
		return "", errors.New("buzzer path must be board, host, both, or none")
	}

	beforeMirror := config.Integrations.BuzzerMirror
	hostChanged := beforeMirror.Enabled != hostEnabled
	if hostChanged {
		if options.UpdateHostConfig == nil {
			return "", errors.New("host buzzer configuration is read-only")
		}
		if err := options.UpdateHostConfig(func(value *appconfig.Config) error {
			value.Integrations.BuzzerMirror.Enabled = hostEnabled
			return value.Validate()
		}); err != nil {
			return "", fmt.Errorf("set host buzzer path: %w", err)
		}
	}

	boardChanged := settings.Flags&native.SettingsSilent != 0 != boardSilent
	if boardChanged {
		if boardSilent {
			settings.Flags |= native.SettingsSilent
		} else {
			settings.Flags &^= native.SettingsSilent
		}
		if err := storeSettings(ctx, runtime, settings); err != nil {
			if hostChanged {
				_ = options.UpdateHostConfig(func(value *appconfig.Config) error {
					value.Integrations.BuzzerMirror = beforeMirror
					return value.Validate()
				})
			}
			return "", fmt.Errorf("set board silent state: %w", err)
		}
	}

	verified, err := querySettings(ctx, runtime)
	if err != nil {
		return "", fmt.Errorf("verify board silent state: %w", err)
	}
	if (verified.Flags&native.SettingsSilent != 0) != boardSilent {
		return "", errors.New("board silent-state readback did not match the requested buzzer path")
	}
	return formatBuzzerRouting(verified, options.HostConfig()) + " applied live", nil
}

func formatBuzzerRouting(settings native.Settings, config appconfig.Config) string {
	boardSilent := settings.Flags&native.SettingsSilent != 0
	hostEnabled := config.Integrations.BuzzerMirror.Enabled
	path := "none"
	switch {
	case !boardSilent && hostEnabled:
		path = "both"
	case !boardSilent:
		path = "board"
	case hostEnabled:
		path = "host"
	}
	return fmt.Sprintf(
		"buzzer_path=%s board_silent=%t host_silent=%t native=%t web_audio=%t",
		path, boardSilent, !hostEnabled,
		hostEnabled && config.Integrations.BuzzerMirror.NativeEnabled,
		hostEnabled && config.Integrations.BuzzerMirror.WebAudioEnabled,
	)
}

func silentCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	const usage = "usage: silent status|on|off | silent board|host|both status|on|off"
	if len(args) == 2 {
		target := strings.ToLower(strings.TrimSpace(args[0]))
		action := strings.ToLower(strings.TrimSpace(args[1]))
		if action != "status" && action != "on" && action != "off" {
			return "", errors.New(usage)
		}
		switch target {
		case "board":
			args = []string{action}
		case "host":
			if options.HostConfig == nil {
				return "", errors.New("host buzzer configuration is unavailable")
			}
			config := options.HostConfig()
			if action == "status" {
				return fmt.Sprintf("host_silent=%t", !config.Integrations.BuzzerMirror.Enabled), nil
			}
			if options.UpdateHostConfig == nil {
				return "", errors.New("host buzzer configuration is read-only")
			}
			silent := action == "on"
			if err := options.UpdateHostConfig(func(value *appconfig.Config) error {
				value.Integrations.BuzzerMirror.Enabled = !silent
				return value.Validate()
			}); err != nil {
				return "", err
			}
			return fmt.Sprintf("host_silent=%t applied live", silent), nil
		case "both":
			if action == "status" {
				return buzzerRoutingCommand(ctx, runtime, options, []string{"status"})
			}
			path := "both"
			if action == "on" {
				path = "none"
			}
			return buzzerRoutingCommand(ctx, runtime, options, []string{"path", path})
		default:
			return "", errors.New(usage)
		}
	}
	if len(args) != 1 {
		return "", errors.New(usage)
	}
	settings, err := querySettings(ctx, runtime)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "status":
		return fmt.Sprintf("silent=%t board_silent=%t", settings.Flags&native.SettingsSilent != 0, settings.Flags&native.SettingsSilent != 0), nil
	case "on":
		settings.Flags |= native.SettingsSilent
	case "off":
		settings.Flags &^= native.SettingsSilent
	default:
		return "", errors.New(usage)
	}
	settings, err = storeSettingsLive(ctx, runtime, settings)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"silent=%t board_silent=%t applied_live=true persisted=%t",
		settings.Flags&native.SettingsSilent != 0,
		settings.Flags&native.SettingsSilent != 0,
		settings.Persisted,
	), nil
}

func refresh(ctx context.Context, runtime *Runtime) (native.Status, error) {
	requestContext, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	return runtime.RefreshStatus(requestContext)
}

func request(
	ctx context.Context,
	runtime *Runtime,
	opcode byte,
	payload []byte,
	response byte,
) (native.Frame, error) {
	requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return runtime.Request(requestContext, opcode, payload, response)
}

func command(ctx context.Context, runtime *Runtime, opcode byte, payload []byte) error {
	requestContext, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return runtime.Command(requestContext, opcode, payload)
}

func querySettings(ctx context.Context, runtime *Runtime) (native.Settings, error) {
	frame, err := request(
		ctx,
		runtime,
		native.OpGetSettings,
		nil,
		native.OpSettings,
	)
	if err != nil {
		return native.Settings{}, fmt.Errorf(
			"query current EEPROM settings: %w",
			err,
		)
	}
	settings, err := native.ParseSettings(frame.Payload)
	if err != nil {
		return native.Settings{}, fmt.Errorf("parse current EEPROM settings: %w", err)
	}
	return settings, nil
}

func storeSettings(
	ctx context.Context,
	runtime *Runtime,
	settings native.Settings,
) error {
	_, err := storeSettingsLive(ctx, runtime, settings)
	return err
}

// storeSettingsLive acknowledges the distinction between command acceptance
// and observable live state.  SET_SETTINGS has no response body, so every
// host-owned settings mutation performs one bounded GET_SETTINGS readback.
// The returned persistence bit is deliberately not guessed: callers display
// exactly what the board reports while its EEPROM commit completes.
func storeSettingsLive(
	ctx context.Context,
	runtime *Runtime,
	settings native.Settings,
) (native.Settings, error) {
	payload, err := settings.Payload()
	if err != nil {
		return native.Settings{}, err
	}
	if err := command(ctx, runtime, native.OpSetSettings, payload); err != nil {
		return native.Settings{}, err
	}
	live, err := querySettings(ctx, runtime)
	if err != nil {
		return native.Settings{}, fmt.Errorf("read live settings after accepted write: %w", err)
	}
	livePayload, err := live.Payload()
	if err != nil {
		return native.Settings{}, fmt.Errorf("encode live settings readback: %w", err)
	}
	if !bytes.Equal(payload, livePayload) {
		return native.Settings{}, errors.New("settings write was accepted but live readback differs")
	}
	return live, nil
}

func settingsFromSetArgs(args []string) (native.Settings, error) {
	if len(args) != 18 || !strings.EqualFold(args[0], "set") {
		return native.Settings{}, fmt.Errorf("usage: %s", settingsSetUsage)
	}

	values := make([]uint64, 9)
	for index, value := range args[1:10] {
		bits := 8
		if index == 8 {
			bits = 16
		}
		parsed, err := strconv.ParseUint(value, 0, bits)
		if err != nil {
			return native.Settings{}, fmt.Errorf(
				"invalid settings value %q",
				value,
			)
		}
		values[index] = parsed
	}

	defaultPage, err := parseBoundedByte(args[10], 13, "default page")
	if err != nil {
		return native.Settings{}, err
	}
	saveLast, err := parseBool(args[11])
	if err != nil {
		return native.Settings{}, fmt.Errorf("invalid SAVE_LAST value %q: %w", args[11], err)
	}
	statusColor, err := parseBoundedByte(args[12], 7, "status color")
	if err != nil {
		return native.Settings{}, err
	}
	voltageDecimals, err := parseBoundedByte(args[13], 2, "voltage decimals")
	if err != nil {
		return native.Settings{}, err
	}
	currentDecimals, err := parseBoundedByte(args[14], 2, "current decimals")
	if err != nil {
		return native.Settings{}, err
	}
	motionExitHoldSeconds, err := parseBoundedByte(
		args[15], native.SettingsMaximumMotionExitHoldSeconds,
		"motion exit hold",
	)
	if err != nil {
		return native.Settings{}, err
	}
	if motionExitHoldSeconds == 0 {
		return native.Settings{}, errors.New("motion exit hold must be at least 1 second")
	}
	motionBreakMS, err := parseBoundedByte(args[16], 0xFF, "motion break")
	if err != nil {
		return native.Settings{}, err
	}
	if motionBreakMS == 0 {
		return native.Settings{}, errors.New("motion break must be at least 1 ms")
	}
	relayRestoreMask, err := parseBoundedByte(args[17], 0xFF, "relay restore mask")
	if err != nil {
		return native.Settings{}, err
	}
	settings := native.Settings{
		Flags: byte(values[0]), LightMode: byte(values[1]),
		OnBrightness: byte(values[2]), OffBrightness: byte(values[3]),
		DisplayBrightness: byte(values[4]), DisplayClosedBrightness: byte(values[5]),
		StatusBrightness: byte(values[6]), OutputPersistence: byte(values[7]),
		StreamPeriodMS: uint16(values[8]), DefaultPage: defaultPage,
		MotionExitHoldSeconds: motionExitHoldSeconds,
		RelayRestoreMask:      relayRestoreMask,
		MotionBreakMSValue:    motionBreakMS,
	}
	settings.SetSaveLastPage(saveLast)
	if err := settings.SetStatusColor(statusColor); err != nil {
		return native.Settings{}, err
	}
	if err := settings.SetVoltageDecimals(voltageDecimals); err != nil {
		return native.Settings{}, err
	}
	if err := settings.SetCurrentDecimals(currentDecimals); err != nil {
		return native.Settings{}, err
	}
	return settings, nil
}

func parseBoundedByte(value string, maximum byte, name string) (byte, error) {
	parsed, err := strconv.ParseUint(value, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, value)
	}
	if parsed > uint64(maximum) {
		return 0, fmt.Errorf("%s %d is outside 0..%d", name, parsed, maximum)
	}
	return byte(parsed), nil
}

func melodyCommand(
	ctx context.Context,
	outputs *OutputScheduler,
	configProvider func() appconfig.Config,
	args []string,
) (string, error) {
	return melodyCommandWithUpdate(ctx, outputs, configProvider, nil, args)
}

func melodyCommandWithUpdate(
	ctx context.Context,
	outputs *OutputScheduler,
	configProvider func() appconfig.Config,
	updateConfig func(func(*appconfig.Config) error) error,
	args []string,
) (string, error) {
	if len(args) >= 1 && strings.EqualFold(args[0], "create") {
		if len(args) < 3 {
			return "", fmt.Errorf(
				"usage: melody create NAME FREQ:DURATION_MS[:GAP_MS] ...",
			)
		}
		if updateConfig == nil {
			return "", errors.New("host configuration is read-only")
		}
		notes, err := parseMelodyNotes(args[2:])
		if err != nil {
			return "", err
		}
		melody := appconfig.Melody{Name: args[1], Notes: notes}
		if err := appconfig.ValidateMelody(melody); err != nil {
			return "", err
		}
		err = updateConfig(func(config *appconfig.Config) error {
			values := appconfig.EffectiveMelodies(*config)
			replaced := false
			for index := range values {
				if strings.EqualFold(values[index].Name, melody.Name) {
					values[index] = melody
					replaced = true
					break
				}
			}
			if !replaced {
				values = append(values, melody)
			}
			config.Melodies = values
			return nil
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"melody %q saved in HOST configuration with %d notes",
			melody.Name,
			len(melody.Notes),
		), nil
	}
	if len(args) == 2 &&
		(strings.EqualFold(args[0], "delete") ||
			strings.EqualFold(args[0], "remove")) {
		if updateConfig == nil {
			return "", errors.New("host configuration is read-only")
		}
		removed := false
		err := updateConfig(func(config *appconfig.Config) error {
			values := appconfig.EffectiveMelodies(*config)
			result := values[:0]
			for _, value := range values {
				if strings.EqualFold(value.Name, args[1]) {
					removed = true
					continue
				}
				result = append(result, value)
			}
			if !removed {
				return fmt.Errorf("configured melody %q was not found", args[1])
			}
			config.Melodies = append([]appconfig.Melody(nil), result...)
			return nil
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("melody %q removed from HOST configuration", args[1]), nil
	}
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "list":
			config := appconfig.Config{}
			if configProvider != nil {
				config = configProvider()
			}
			melodies := appconfig.EffectiveMelodies(config)
			lines := make([]string, 0, len(melodies))
			for _, melody := range melodies {
				var duration uint64
				for _, note := range melody.Notes {
					duration += uint64(note.DurationMS) + uint64(note.GapMS)
				}
				lines = append(lines, fmt.Sprintf(
					"%s notes=%d duration=%dms",
					melody.Name,
					len(melody.Notes),
					duration,
				))
			}
			return strings.Join(lines, "\n"), nil
		case "stop", "cancel":
			if outputs.StopMelody() {
				return "melody stop requested; the current note may finish", nil
			}
			return "no melody is playing", nil
		case "status":
			state := outputs.State()
			if state.MelodyID == 0 {
				return "melody idle", nil
			}
			return fmt.Sprintf(
				"melody playing id=%d name=%q",
				state.MelodyID,
				state.MelodyName,
			), nil
		}
	}
	if len(args) < 2 || len(args) > 3 ||
		(!strings.EqualFold(args[0], "play") &&
			!strings.EqualFold(args[0], "wait")) {
		return "", fmt.Errorf(
			"usage: melody list|create NAME NOTE...|delete NAME|play NAME [REPEATS]|wait NAME [REPEATS]|stop|status",
		)
	}
	config := appconfig.Config{}
	if configProvider != nil {
		config = configProvider()
	}
	melody, ok := findMelody(appconfig.EffectiveMelodies(config), args[1])
	if !ok {
		return "", fmt.Errorf("configured melody %q was not found", args[1])
	}
	repeats := 1
	if len(args) == 3 {
		value, err := strconv.ParseUint(args[2], 0, 8)
		if err != nil || value > maxMelodyRepeats {
			return "", fmt.Errorf(
				"melody repeats must be 0 (until stopped) or 1..%d",
				maxMelodyRepeats,
			)
		}
		repeats = int(value)
	}
	operation, err := outputs.StartMelody(ctx, melody, repeats)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(args[0], "wait") {
		select {
		case err := <-operation.Done:
			return fmt.Sprintf(
				"melody %q completed (id=%d)",
				melody.Name,
				operation.ID,
			), err
		case <-ctx.Done():
			outputs.StopMelody()
			return "", ctx.Err()
		}
	}
	repeatLabel := strconv.Itoa(repeats)
	if repeats == 0 {
		repeatLabel = "until-stopped"
	}
	return fmt.Sprintf(
		"melody %q started (id=%d repeats=%s)",
		melody.Name,
		operation.ID,
		repeatLabel,
	), nil
}

func parseMelodyNotes(values []string) ([]appconfig.MelodyNote, error) {
	var tokens []string
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			if token = strings.TrimSpace(token); token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	if len(tokens) == 0 || len(tokens) > appconfig.MaxMelodyNotes {
		return nil, fmt.Errorf(
			"melody requires 1..%d notes",
			appconfig.MaxMelodyNotes,
		)
	}
	notes := make([]appconfig.MelodyNote, 0, len(tokens))
	for _, token := range tokens {
		parts := strings.Split(token, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf(
				"invalid note %q; expected FREQ:DURATION_MS[:GAP_MS]",
				token,
			)
		}
		frequency, err := strconv.ParseUint(parts[0], 0, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid note frequency in %q", token)
		}
		duration, err := strconv.ParseUint(parts[1], 0, 16)
		if err != nil {
			return nil, fmt.Errorf("invalid note duration in %q", token)
		}
		gap := uint64(0)
		if len(parts) == 3 {
			gap, err = strconv.ParseUint(parts[2], 0, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid note gap in %q", token)
			}
		}
		notes = append(notes, appconfig.MelodyNote{
			FrequencyHz: uint16(frequency),
			DurationMS:  uint16(duration),
			GapMS:       uint16(gap),
		})
	}
	return notes, nil
}

func rgbCommand(
	ctx context.Context,
	runtime *Runtime,
	outputs *OutputScheduler,
	configProvider func() appconfig.Config,
	args []string,
) (string, error) {
	if len(args) != 0 && strings.EqualFold(args[0], "effect") {
		return statusEffectCommand(ctx, runtime, outputs, configProvider, args[1:])
	}
	if len(args) != 0 && strings.EqualFold(args[0], "profile") {
		return statusProfileCommand(ctx, runtime, args[1:])
	}
	if len(args) != 0 && strings.EqualFold(args[0], "color") {
		args = args[1:]
	}
	color, consumed, err := parseStatusColor(args)
	if err != nil {
		return "", fmt.Errorf(
			"usage: rgb [color] '#RRGGBB' [BRIGHTNESS] | rgb [color] R G B [BRIGHTNESS] | rgb effect ...: %w",
			err,
		)
	}
	if len(args) != consumed && len(args) != consumed+1 {
		return "", fmt.Errorf(
			"usage: rgb [color] '#RRGGBB' [BRIGHTNESS] | rgb [color] R G B [BRIGHTNESS] | rgb effect ...",
		)
	}
	brightness := uint64(255)
	if len(args) == consumed+1 {
		brightness, err = strconv.ParseUint(args[consumed], 10, 8)
		if err != nil {
			return "", fmt.Errorf("invalid brightness %q", args[consumed])
		}
	}
	payload := native.StatusRGBPayload(
		color.Red,
		color.Green,
		color.Blue,
		byte(brightness),
	)
	outputs.OverrideStatusEffect()
	if err := command(ctx, runtime, native.OpStatusRGB, payload); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"status RGB=%d,%d,%d brightness=%d",
		color.Red,
		color.Green,
		color.Blue,
		brightness,
	), nil
}

func statusEffectCommand(
	ctx context.Context,
	runtime *Runtime,
	outputs *OutputScheduler,
	configProvider func() appconfig.Config,
	args []string,
) (string, error) {
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "list":
			config := appconfig.Config{}
			if configProvider != nil {
				config = configProvider()
			}
			effects := appconfig.EffectiveStatusLEDEffects(config)
			lines := make([]string, 0, len(effects))
			for _, effect := range effects {
				duration := "until-stopped"
				if effect.DurationMS != 0 {
					duration = fmt.Sprintf("%dms", effect.DurationMS)
				}
				lines = append(lines, fmt.Sprintf(
					"%s kind=%s rgb=%d,%d,%d brightness=%d..%d period=%dms duration=%s",
					effect.Name,
					effect.Kind,
					effect.Red,
					effect.Green,
					effect.Blue,
					effect.MinBrightness,
					effect.Brightness,
					effect.PeriodMS,
					duration,
				))
			}
			return strings.Join(lines, "\n"), nil
		case "stop", "cancel":
			if outputs.StopStatusEffect() {
				return "status LED effect stop requested", nil
			}
			if runtime.Snapshot().Hello.Capabilities&native.CapabilityStatusEffects != 0 {
				if err := command(ctx, runtime, native.OpStatusEffect, native.StatusEffectReleasePayload()); err != nil {
					return "", err
				}
				return "status LED effect released to firmware ownership", nil
			}
			return "no status LED effect is playing", nil
		case "status":
			state := outputs.State()
			if state.EffectID == 0 {
				return "status LED effect idle", nil
			}
			return fmt.Sprintf(
				"status LED effect playing id=%d name=%q",
				state.EffectID,
				state.EffectName,
			), nil
		}
	}
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "breathe", "flash", "cycle", "transition":
			effect, err := parseAdHocStatusEffect(args)
			if err != nil {
				return "", err
			}
			operation, err := outputs.StartStatusEffect(ctx, effect)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"status LED %s started id=%d rgb=#%02X%02X%02X to=#%02X%02X%02X period=%dms repeats=%s",
				effect.Kind, operation.ID, effect.Red, effect.Green, effect.Blue,
				effect.AlternateRed, effect.AlternateGreen, effect.AlternateBlue,
				effect.PeriodMS, statusEffectRepeatLabel(effect.Repeats),
			), nil
		}
	}
	if len(args) != 2 ||
		(!strings.EqualFold(args[0], "play") &&
			!strings.EqualFold(args[0], "wait")) {
		return "", fmt.Errorf(
			"usage: rgb effect list|play NAME|wait NAME|stop|status",
		)
	}
	config := appconfig.Config{}
	if configProvider != nil {
		config = configProvider()
	}
	effect, ok := findStatusEffect(
		appconfig.EffectiveStatusLEDEffects(config),
		args[1],
	)
	if !ok {
		return "", fmt.Errorf("configured status LED effect %q was not found", args[1])
	}
	operation, err := outputs.StartStatusEffect(ctx, effect)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(args[0], "wait") {
		select {
		case err := <-operation.Done:
			return fmt.Sprintf(
				"status LED effect %q completed (id=%d)",
				effect.Name,
				operation.ID,
			), err
		case <-ctx.Done():
			outputs.StopStatusEffect()
			return "", ctx.Err()
		}
	}
	return fmt.Sprintf(
		"status LED effect %q started (id=%d)",
		effect.Name,
		operation.ID,
	), nil
}

func parseStatusColor(args []string) (appconfig.RGBColor, int, error) {
	if len(args) == 0 {
		return appconfig.RGBColor{}, 0, errors.New("color is required")
	}
	if strings.HasPrefix(args[0], "#") {
		value := strings.TrimPrefix(args[0], "#")
		if len(value) == 3 {
			value = strings.Repeat(value[0:1], 2) +
				strings.Repeat(value[1:2], 2) +
				strings.Repeat(value[2:3], 2)
		}
		if len(value) != 6 {
			return appconfig.RGBColor{}, 0, fmt.Errorf("hex color %q must be #RGB or #RRGGBB", args[0])
		}
		parsed, err := strconv.ParseUint(value, 16, 24)
		if err != nil {
			return appconfig.RGBColor{}, 0, fmt.Errorf("invalid hex color %q", args[0])
		}
		return appconfig.RGBColor{
			Red: byte(parsed >> 16), Green: byte(parsed >> 8), Blue: byte(parsed),
		}, 1, nil
	}
	if len(args) < 3 {
		return appconfig.RGBColor{}, 0, errors.New("decimal color requires R G B")
	}
	values := [3]byte{}
	for index := range values {
		parsed, err := strconv.ParseUint(args[index], 10, 8)
		if err != nil {
			return appconfig.RGBColor{}, 0,
				fmt.Errorf("invalid decimal color channel %q", args[index])
		}
		values[index] = byte(parsed)
	}
	return appconfig.RGBColor{Red: values[0], Green: values[1], Blue: values[2]}, 3, nil
}

func parseAdHocStatusEffect(args []string) (appconfig.StatusLEDEffect, error) {
	kind := strings.ToLower(args[0])
	effect := appconfig.StatusLEDEffect{
		Name: "manual-" + kind, Kind: kind, Brightness: 255, PeriodMS: 1000,
	}
	index := 1
	color, consumed, err := parseStatusColor(args[index:])
	if err != nil {
		return effect, fmt.Errorf("rgb effect %s: %w", kind, err)
	}
	effect.Red, effect.Green, effect.Blue = color.Red, color.Green, color.Blue
	index += consumed
	haveAlternate := false
	for index < len(args) {
		option := strings.ToLower(args[index])
		index++
		switch option {
		case "--to", "--alternate":
			color, consumed, err := parseStatusColor(args[index:])
			if err != nil {
				return effect, fmt.Errorf("%s: %w", option, err)
			}
			effect.AlternateRed, effect.AlternateGreen, effect.AlternateBlue =
				color.Red, color.Green, color.Blue
			index += consumed
			haveAlternate = true
		case "--period", "--speed":
			if index >= len(args) {
				return effect, fmt.Errorf("%s requires a duration", option)
			}
			duration, err := time.ParseDuration(args[index])
			if err != nil {
				milliseconds, numberErr := strconv.ParseUint(args[index], 10, 16)
				if numberErr != nil {
					return effect, fmt.Errorf("invalid effect period %q", args[index])
				}
				duration = time.Duration(milliseconds) * time.Millisecond
			}
			effect.PeriodMS = int(duration / time.Millisecond)
			index++
		case "--brightness", "--minimum", "--min", "--repeat", "--repeats":
			if index >= len(args) {
				return effect, fmt.Errorf("%s requires a value", option)
			}
			value := strings.ToLower(args[index])
			index++
			if option == "--repeat" || option == "--repeats" {
				switch value {
				case "loop", "forever", "infinite":
					effect.Repeats = 0
				case "once":
					effect.Repeats = 1
				default:
					parsed, err := strconv.ParseUint(value, 10, 8)
					if err != nil || parsed == 0 {
						return effect, fmt.Errorf("repeat must be once, loop, or 1..255")
					}
					effect.Repeats = byte(parsed)
				}
				continue
			}
			parsed, err := strconv.ParseUint(value, 10, 8)
			if err != nil {
				return effect, fmt.Errorf("%s must be 0..255", option)
			}
			if option == "--brightness" {
				effect.Brightness = byte(parsed)
			} else {
				effect.MinBrightness = byte(parsed)
			}
		default:
			return effect, fmt.Errorf("unknown status effect option %q", option)
		}
	}
	if (kind == "cycle" || kind == "transition") && !haveAlternate {
		return effect, fmt.Errorf("rgb effect %s requires --to COLOR", kind)
	}
	if err := appconfig.ValidateStatusLEDEffect(effect); err != nil {
		return effect, err
	}
	return effect, nil
}

func statusEffectRepeatLabel(repeats byte) string {
	if repeats == 0 {
		return "loop"
	}
	return strconv.Itoa(int(repeats))
}

var statusProfileNames = [...]string{
	"off", "boot", "ready", "learning", "hot", "fault", "custom",
	"bluetooth-connected", "bluetooth-off", "bluetooth-waiting", "running",
	"door-open", "door-closed", "bluetooth", "menu", "radio", "save",
	"discard", "reset",
}

func statusProfileCondition(value string) (byte, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for index, name := range statusProfileNames {
		if normalized == name {
			return byte(index), nil
		}
	}
	if normalized == "warning" {
		return 4, nil
	}
	parsed, err := strconv.ParseUint(normalized, 10, 8)
	if err == nil && parsed < uint64(len(statusProfileNames)) {
		return byte(parsed), nil
	}
	return 0, fmt.Errorf("unknown status profile condition %q", value)
}

func statusProfileCommand(ctx context.Context, runtime *Runtime, args []string) (string, error) {
	if len(args) == 1 && strings.EqualFold(args[0], "list") {
		lines := make([]string, 0, len(statusProfileNames))
		for condition, name := range statusProfileNames {
			profile, err := readStatusProfile(ctx, runtime, byte(condition))
			if err != nil {
				return "", err
			}
			lines = append(lines, formatStatusProfile(name, profile.Effect))
		}
		return strings.Join(lines, "\n"), nil
	}
	if len(args) == 2 && strings.EqualFold(args[0], "get") {
		condition, err := statusProfileCondition(args[1])
		if err != nil {
			return "", err
		}
		profile, err := readStatusProfile(ctx, runtime, condition)
		if err != nil {
			return "", err
		}
		return formatStatusProfile(statusProfileNames[condition], profile.Effect), nil
	}
	if len(args) >= 4 && strings.EqualFold(args[0], "set") {
		condition, err := statusProfileCondition(args[1])
		if err != nil {
			return "", err
		}
		var options native.StatusEffectOptions
		if strings.EqualFold(args[2], "color") || strings.EqualFold(args[2], "steady") {
			color, consumed, err := parseStatusColor(args[3:])
			if err != nil {
				return "", err
			}
			brightness := byte(255)
			if len(args) > 3+consumed {
				if len(args) != 4+consumed {
					return "", errors.New("steady profile accepts only an optional brightness")
				}
				parsed, err := strconv.ParseUint(args[3+consumed], 10, 8)
				if err != nil {
					return "", fmt.Errorf("invalid brightness %q", args[3+consumed])
				}
				brightness = byte(parsed)
			}
			options = native.StatusEffectOptions{
				Kind: native.StatusEffectNone, Red: color.Red, Green: color.Green,
				Blue: color.Blue, Brightness: brightness,
			}
		} else {
			effect, err := parseAdHocStatusEffect(args[2:])
			if err != nil {
				return "", err
			}
			options, _, err = nativeStatusEffect(effect)
			if err != nil {
				return "", err
			}
		}
		payload, err := native.StatusProfileSetPayload(condition, options)
		if err != nil {
			return "", err
		}
		if err := command(ctx, runtime, native.OpStatusProfileSet, payload); err != nil {
			return "", err
		}
		return "EEPROM status profile saved: " + formatStatusProfile(statusProfileNames[condition], options), nil
	}
	return "", errors.New("usage: rgb profile list|get CONDITION|set CONDITION color COLOR [BRIGHTNESS]|set CONDITION EFFECT COLOR [options]")
}

func readStatusProfile(ctx context.Context, runtime *Runtime, condition byte) (native.StatusProfile, error) {
	payload, err := native.StatusProfileGetPayload(condition)
	if err != nil {
		return native.StatusProfile{}, err
	}
	frame, err := runtime.Request(ctx, native.OpStatusProfileGet, payload, native.OpStatusProfile)
	if err != nil {
		return native.StatusProfile{}, err
	}
	return native.ParseStatusProfile(frame.Payload)
}

func formatStatusProfile(name string, effect native.StatusEffectOptions) string {
	kinds := [...]string{"color", "breathe", "flash", "cycle", "transition"}
	kind := "unknown"
	if int(effect.Kind) < len(kinds) {
		kind = kinds[effect.Kind]
	}
	return fmt.Sprintf(
		"%s effect=%s color=#%02X%02X%02X alternate=#%02X%02X%02X brightness=%d minimum=%d period=%dms repeats=%s",
		name, kind, effect.Red, effect.Green, effect.Blue,
		effect.AlternateRed, effect.AlternateGreen, effect.AlternateBlue,
		effect.Brightness, effect.MinimumBrightness, effect.PeriodMS,
		statusEffectRepeatLabel(effect.Repeats),
	)
}

func findMelody(
	values []appconfig.Melody,
	name string,
) (appconfig.Melody, bool) {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) {
			return value, true
		}
	}
	return appconfig.Melody{}, false
}

func findStatusEffect(
	values []appconfig.StatusLEDEffect,
	name string,
) (appconfig.StatusLEDEffect, bool) {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value.Name), strings.TrimSpace(name)) {
			return value, true
		}
	}
	return appconfig.StatusLEDEffect{}, false
}

func relayCommand(
	ctx context.Context,
	runtime *Runtime,
	configProvider func() appconfig.Config,
	args []string,
) (string, error) {
	if len(args) == 1 && strings.EqualFold(args[0], "off") {
		snapshot := runtime.Snapshot()
		changed := !snapshot.HaveStatus || snapshot.Status.ActiveRelays != 0
		return "all relays off", commandRelayWithCue(
			ctx, runtime, native.OpRelayAllOff, nil, false, changed,
		)
	}
	if len(args) >= 1 && strings.EqualFold(args[0], "test") {
		step := uint64(250)
		if len(args) == 2 {
			value, err := strconv.ParseUint(args[1], 0, 16)
			if err != nil {
				return "", fmt.Errorf("invalid relay test interval %q", args[1])
			}
			step = value
		} else if len(args) > 2 {
			return "", fmt.Errorf("usage: relay test [MS]")
		}
		payload, err := native.RelayTestPayload(uint16(step))
		if err != nil {
			return "", err
		}
		if step == 0 {
			return "relay test stopped",
				command(ctx, runtime, native.OpRelayTest, payload)
		}
		return fmt.Sprintf("relay test started at %d ms/step", step),
			command(ctx, runtime, native.OpRelayTest, payload)
	}
	if len(args) == 3 && strings.EqualFold(args[0], "side") {
		sides := map[string]byte{"left": 0, "right": 1, "0": 0, "1": 1}
		motions := map[string]byte{"stop": 0, "up": 1, "down": 2}
		side, ok := sides[strings.ToLower(args[1])]
		if !ok {
			return "", fmt.Errorf("side must be left or right")
		}
		motion, ok := motions[strings.ToLower(args[2])]
		if !ok {
			return "", fmt.Errorf("motion must be stop, up, or down")
		}
		payload, err := native.RelaySidePayload(side, motion)
		if err != nil {
			return "", err
		}
		if motion != 0 {
			if err := requireMotionAllowed(ctx, runtime, configProvider); err != nil {
				return "", err
			}
		}
		snapshot := runtime.Snapshot()
		changed := relaySideStateChanged(snapshot, side, motion)
		return fmt.Sprintf("relay side %s %s", args[1], args[2]),
			commandRelayWithCue(
				ctx, runtime, native.OpRelaySide, payload, motion != 0, changed,
			)
	}
	if len(args) == 2 {
		number, err := strconv.ParseUint(args[0], 0, 8)
		if err != nil || number < 1 || number > 8 {
			return "", fmt.Errorf("relay number must be 1..8")
		}
		var active bool
		alreadyCheckedDoor := false
		if strings.EqualFold(args[1], "toggle") {
			status, statusErr := refresh(ctx, runtime)
			if statusErr != nil {
				return "", fmt.Errorf("query relay state for toggle: %w", statusErr)
			}
			active = status.ActiveRelays&(1<<byte(number-1)) == 0
			if active && number <= 4 {
				if err := checkMotionDoorPolicy(status, motionDoorPolicy(configProvider)); err != nil {
					return "", err
				}
				alreadyCheckedDoor = true
			}
		} else {
			active, err = parseBool(args[1])
			if err != nil {
				return "", err
			}
		}
		payload, err := native.RelayPayload(byte(number-1), active)
		if err != nil {
			return "", err
		}
		if active && number <= 4 && !alreadyCheckedDoor {
			if err := requireMotionAllowed(ctx, runtime, configProvider); err != nil {
				return "", err
			}
		}
		snapshot := runtime.Snapshot()
		changed := !snapshot.HaveStatus ||
			(snapshot.Status.ActiveRelays&(1<<byte(number-1)) != 0) != active
		return fmt.Sprintf("relay R%d %s", number, onOff(active)),
			commandRelayWithCue(
				ctx, runtime, native.OpRelaySet, payload, active, changed,
			)
	}
	return "", fmt.Errorf("usage: relay N on|off|toggle | relay side left|right stop|up|down | relay off | relay test [MS]")
}

// commandRelayWithCue keeps every shell-backed surface on one policy. The cue
// is emitted once per accepted logical action, never once per electrical frame
// of a firmware motion reversal. Board silent/relay-audio settings remain the
// authoritative user controls.
func commandRelayWithCue(
	ctx context.Context,
	runtime *Runtime,
	opcode byte,
	payload []byte,
	activated, changed bool,
) error {
	if err := command(ctx, runtime, opcode, payload); err != nil {
		return err
	}
	if !changed {
		return nil
	}
	snapshot := runtime.Snapshot()
	if !snapshot.HaveSettings ||
		!snapshot.Settings.RelayAudioEnabled() ||
		snapshot.Settings.Flags&native.SettingsSilent != 0 {
		return nil
	}
	frequency := uint16(1250)
	if activated {
		frequency = 1900
	}
	if err := command(
		ctx, runtime, native.OpBuzzer, native.BuzzerPayload(frequency, 35),
	); err != nil {
		return fmt.Errorf("relay applied but its buzzer cue failed: %w", err)
	}
	return nil
}

func relaySideStateChanged(snapshot Snapshot, side, motion byte) bool {
	if !snapshot.HaveStatus {
		return true
	}
	shift := side * 2
	current := (snapshot.Status.ActiveRelays >> shift) & 0x03
	var desired byte
	switch motion {
	case 1:
		desired = 0x02
	case 2:
		desired = 0x03
	default:
		if snapshot.HaveSettings &&
			snapshot.Settings.OutputPersistence&native.OutputPersistDirectionOnly != 0 {
			desired = current & 0x01
		}
	}
	return current != desired
}

func requireMotionAllowed(
	ctx context.Context,
	runtime *Runtime,
	configProvider func() appconfig.Config,
) error {
	policy := motionDoorPolicy(configProvider)
	if policy == "always" {
		return nil
	}
	if policy == "never" {
		return errors.New("motion command rejected by PC safety policy (never)")
	}
	status, err := refresh(ctx, runtime)
	if err != nil {
		return fmt.Errorf(
			"motion command rejected because the door policy could not be verified: %w",
			err,
		)
	}
	return checkMotionDoorPolicy(status, policy)
}

func motionDoorPolicy(configProvider func() appconfig.Config) string {
	if configProvider == nil {
		return "always"
	}
	policy := strings.ToLower(strings.TrimSpace(
		configProvider().Safety.MotionDoorPolicy,
	))
	if policy == "" {
		return "always"
	}
	return policy
}

func checkMotionDoorPolicy(status native.Status, policy string) error {
	switch policy {
	case "always", "":
		return nil
	case "open":
		if status.DoorOpen {
			return nil
		}
		return errors.New("motion command rejected while enclosure door is closed")
	case "closed":
		if !status.DoorOpen {
			return nil
		}
		return errors.New("motion command rejected while enclosure door is open")
	case "never":
		return errors.New("motion command rejected by PC safety policy (never)")
	default:
		return fmt.Errorf("motion command rejected: unknown PC safety policy %q", policy)
	}
}

func pwmCommand(ctx context.Context, runtime *Runtime, args []string) (string, error) {
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "get":
			frame, err := request(ctx, runtime, native.OpPWMGet, nil, native.OpPWMValues)
			if err != nil {
				return "", err
			}
			values, err := native.ParsePWMValues(frame.Payload)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("PWM available=%t selected=%d values=%v", values.Available, values.SelectedChannel, values.Values), nil
		case "off":
			return "all PWM channels off", command(ctx, runtime, native.OpPWMAllOff, nil)
		}
	}
	if len(args) == 3 && strings.EqualFold(args[0], "set") {
		channel, err := parsePWMChannel(args[1])
		if err != nil {
			return "", err
		}
		value, err := strconv.ParseUint(args[2], 0, 16)
		if err != nil {
			return "", fmt.Errorf("invalid PWM value %q", args[2])
		}
		payload, err := native.PWMSetPayload(channel, uint16(value))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("PWM channel %d logical value %d", channel, value),
			command(ctx, runtime, native.OpPWMSet, payload)
	}
	return "", fmt.Errorf("usage: pwm get | pwm off | pwm set CHANNEL VALUE")
}

func parsePWMChannel(value string) (byte, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	named := map[string]byte{
		"enclosure": 11, "illumination": 11, "light": 11,
		"power": 12, "power-led": 12,
		"status-r": 13, "rgb-r": 13, "red": 13,
		"status-g": 14, "rgb-g": 14, "green": 14,
		"status-b": 15, "rgb-b": 15, "blue": 15,
	}
	if channel, ok := named[normalized]; ok {
		return channel, nil
	}
	for _, prefix := range []string{"user", "u"} {
		if strings.HasPrefix(normalized, prefix) {
			number, err := strconv.ParseUint(
				strings.TrimPrefix(normalized, prefix),
				10,
				8,
			)
			if err != nil || number < 1 || number > 11 {
				return 0, fmt.Errorf("PWM user channel must be user1..user11")
			}
			return byte(number - 1), nil
		}
	}
	number, err := strconv.ParseUint(normalized, 0, 8)
	if err != nil || number > 15 {
		return 0, fmt.Errorf(
			"PWM channel must be 0..15, user1..user11, enclosure, power, or status-r/g/b",
		)
	}
	return byte(number), nil
}

func rfCommand(
	ctx context.Context,
	runtime *Runtime,
	configProvider func() appconfig.Config,
	args []string,
) (string, error) {
	rfConfig := appconfig.DefaultRFConfig()
	if configProvider != nil {
		rfConfig = configProvider().RF
	}
	if len(args) == 1 {
		switch strings.ToLower(args[0]) {
		case "cancel":
			state := runtime.RFLearnState()
			if !state.Active {
				return "RF learning is not active", nil
			}
			return "RF learning cancellation requested",
				runtime.CancelRFLearning(ctx, "cancelled by user")
		case "status":
			state := runtime.RFLearnState()
			if !state.Active {
				return fmt.Sprintf(
					"RF learning ended mode=%s configured=%s reason=%q captured=%d",
					state.Mode,
					time.Duration(state.ConfiguredMS)*time.Millisecond,
					state.Reason,
					state.Learned,
				), nil
			}
			configured, remaining := "indefinite", "indefinite"
			if state.Mode == RFLearnTimer {
				configured = (time.Duration(state.ConfiguredMS) * time.Millisecond).String()
				remaining = (time.Duration(state.RemainingMS) * time.Millisecond).Round(time.Second).String()
			}
			return fmt.Sprintf(
				"RF learning active mode=%s multi-code=true configured=%s remaining=%s captured=%d",
				state.Mode,
				configured,
				remaining,
				state.Learned,
			), nil
		case "list":
			entries, err := listRFEntries(ctx, runtime)
			if err != nil {
				return "", err
			}
			return formatRFEntries(entries, rfConfig), nil
		}
	}
	if len(args) >= 1 && strings.EqualFold(args[0], "learn") {
		options, err := parseRFLearnOptions(args[1:])
		if err != nil {
			return "", err
		}
		state, err := runtime.StartRFLearning(ctx, options)
		if err != nil {
			return "", err
		}
		duration := options.Timeout.String()
		if state.Mode == RFLearnIndefinite {
			duration = "indefinite"
		}
		return fmt.Sprintf(
			"RF learning started mode=%s multi-code=true configured=%s; remaining time is reported by status and completion by rf.learn.ended",
			state.Mode,
			duration,
		), nil
	}
	if len(args) == 2 && strings.EqualFold(args[0], "remove") {
		if strings.EqualFold(args[1], "all") {
			return "all learned RF entries removed",
				command(ctx, runtime, native.OpRFLearnClear, nil)
		}
		id, err := strconv.ParseUint(args[1], 0, 8)
		if err != nil {
			return "", fmt.Errorf("invalid learned RF ID %q", args[1])
		}
		return fmt.Sprintf("learned RF entry %d removed", id),
			command(ctx, runtime, native.OpRFLearnRemove, []byte{byte(id)})
	}
	if len(args) >= 3 && strings.EqualFold(args[0], "map") {
		payload, description, err := rfMapArgs(args[1:])
		if err != nil {
			return "", err
		}
		return description, NewRFReplaceService(runtime).UpdateMapping(
			ctx, payload[0], payload[1], payload[2], payload[3],
		)
	}
	if len(args) >= 4 && len(args) <= 6 && strings.EqualFold(args[0], "send") {
		code, err := strconv.ParseUint(args[1], 0, 32)
		if err != nil {
			return "", fmt.Errorf("invalid RF code %q", args[1])
		}
		bits, err := strconv.ParseUint(args[2], 0, 8)
		if err != nil {
			return "", fmt.Errorf("invalid bit length %q", args[2])
		}
		protocol, err := strconv.ParseUint(args[3], 0, 8)
		if err != nil {
			return "", fmt.Errorf("invalid RF protocol %q", args[3])
		}
		pulse := uint64(0)
		if len(args) >= 5 {
			pulse, err = strconv.ParseUint(args[4], 0, 16)
			if err != nil {
				return "", fmt.Errorf("invalid pulse length %q", args[4])
			}
		}
		repeats := uint64(1)
		if len(args) == 6 {
			repeats, err = strconv.ParseUint(args[5], 0, 8)
			if err != nil || repeats == 0 || repeats > 20 {
				return "", fmt.Errorf("RF repeats must be 1..20")
			}
		}
		payload, err := native.RFTxPayload(uint32(code), byte(bits), byte(protocol), uint16(pulse))
		if err != nil {
			return "", err
		}
		if err := sendRF(ctx, runtime, payload, byte(repeats)); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"RF sent code=%s bits=%d protocol=%d repeats=%d",
			appconfig.FormatRFCode(uint32(code), rfConfig.DisplayRadix), bits, protocol, repeats,
		), nil
	}
	return "", fmt.Errorf(
		"usage: rf send CODE BITS PROTOCOL [PULSE_US] [REPEATS] | " +
			"rf learn [indefinite|timer [DURATION]] (timer aliases: single, one-shot) | " +
			"rf status|cancel|list | rf remove ID|all | rf map ID ACTION ...",
	)
}

func parseRFLearnOptions(args []string) (RFLearnOptions, error) {
	if len(args) == 0 {
		return RFLearnOptions{Mode: RFLearnIndefinite}, nil
	}
	if len(args) > 2 {
		return RFLearnOptions{}, fmt.Errorf("usage: rf learn [indefinite|timer [DURATION]]")
	}
	mode, err := ParseRFLearnMode(args[0])
	if err != nil {
		return RFLearnOptions{}, err
	}
	options := RFLearnOptions{Mode: mode}
	if mode == RFLearnIndefinite {
		if len(args) != 1 {
			return RFLearnOptions{}, fmt.Errorf("indefinite RF learning does not accept a duration")
		}
		return options, nil
	}
	options.Timeout = 15 * time.Second
	if len(args) == 2 {
		duration, parseErr := time.ParseDuration(args[1])
		if parseErr != nil {
			seconds, secondsErr := strconv.ParseUint(args[1], 0, 32)
			if secondsErr != nil {
				return RFLearnOptions{}, fmt.Errorf("invalid RF learn timer duration %q", args[1])
			}
			duration = time.Duration(seconds) * time.Second
		}
		maximum := time.Duration(native.MaxRFLearnSeconds) * time.Second
		if duration <= 0 || duration > maximum {
			return RFLearnOptions{}, fmt.Errorf("RF learn timer duration must be positive and at most %s", maximum)
		}
		options.Timeout = duration
	}
	return options, nil
}

func sendRF(
	ctx context.Context,
	runtime *Runtime,
	payload []byte,
	repeats byte,
) error {
	if repeats == 0 || repeats > 20 {
		return fmt.Errorf("RF repeats must be 1..20")
	}
	for repeat := byte(0); repeat < repeats; repeat++ {
		if err := command(ctx, runtime, native.OpRFTx, payload); err != nil {
			return fmt.Errorf("RF send %d/%d: %w", repeat+1, repeats, err)
		}
	}
	return nil
}

func listRFEntries(ctx context.Context, runtime *Runtime) ([]native.RFEntry, error) {
	cursor := byte(0)
	var entries []native.RFEntry
	for pageNumber := 0; pageNumber < 86; pageNumber++ {
		frame, err := request(ctx, runtime, native.OpRFLearnList, []byte{cursor}, native.OpRFEntries)
		if err != nil {
			return nil, err
		}
		page, err := native.ParseRFEntries(frame.Payload)
		if err != nil {
			return nil, err
		}
		entries = append(entries, page.Entries...)
		if page.NextCursor == 0xFF {
			sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
			return entries, nil
		}
		if page.NextCursor == cursor {
			return nil, fmt.Errorf("RF list cursor did not advance from %d", cursor)
		}
		cursor = page.NextCursor
	}
	return nil, fmt.Errorf("RF list exceeded pagination safety limit")
}

func formatRFEntries(entries []native.RFEntry, config appconfig.RFConfig) string {
	if len(entries) == 0 {
		return "no learned RF entries"
	}
	var lines []string
	lines = append(lines, "ID  CODE        BITS PROTO PULSE  NAME/CATEGORY  MAPPING")
	for _, entry := range entries {
		metadata, _ := config.MetadataFor(appconfig.RFCodeKey{
			Code: entry.Code, Bits: entry.Bits, Protocol: entry.Protocol,
		})
		label := "—"
		if metadata.Name != "" || metadata.Category != "" {
			label = strings.Trim(strings.TrimSpace(metadata.Name)+"/"+strings.TrimSpace(metadata.Category), "/")
		}
		lines = append(lines, fmt.Sprintf(
			"%-3d %-10s  %-4d %-5d %-5d  %-14s %s",
			entry.ID,
			appconfig.FormatRFCode(entry.Code, config.DisplayRadix),
			entry.Bits,
			entry.Protocol,
			entry.PulseUS,
			label,
			formatRFMapping(entry.ActionKind, entry.ActionValue, entry.Behavior),
		))
	}
	return strings.Join(lines, "\n")
}

func formatRFMapping(kind, value, behavior byte) string {
	kinds := []string{"none", "key", "menu", "relay", "side", "pwm"}
	behaviors := []string{"press", "toggle", "momentary", "up", "down", "stop"}
	kindName := fmt.Sprintf("kind-%d", kind)
	if int(kind) < len(kinds) {
		kindName = kinds[kind]
	}
	behaviorName := fmt.Sprintf("behavior-%d", behavior)
	if int(behavior) < len(behaviors) {
		behaviorName = behaviors[behavior]
	}
	if kind == native.RFActionNone {
		return "none"
	}
	displayValue := value
	if kind == native.RFActionKey || kind == native.RFActionRelay {
		displayValue++
	}
	return fmt.Sprintf("%s:%d/%s", kindName, displayValue, behaviorName)
}

func rfMapArgs(args []string) ([]byte, string, error) {
	if len(args) < 2 {
		return nil, "", fmt.Errorf(
			"usage: rf map ID none|key N|menu ACTION|relay N MODE|side SIDE MOTION|pwm CHANNEL MODE",
		)
	}
	id64, err := strconv.ParseUint(args[0], 0, 8)
	if err != nil {
		return nil, "", fmt.Errorf("invalid learned RF ID %q", args[0])
	}
	id := byte(id64)
	action := strings.ToLower(args[1])
	kind, value, behavior := native.RFActionNone, byte(0), native.RFBehaviorPress

	switch action {
	case "none", "unmap":
		if len(args) != 2 {
			return nil, "", fmt.Errorf("usage: rf map ID none")
		}
	case "key", "button":
		if len(args) < 3 || len(args) > 4 {
			return nil, "", fmt.Errorf("usage: rf map ID key 1|2|3|4 [press|toggle|momentary]")
		}
		number, parseErr := strconv.ParseUint(args[2], 0, 8)
		if parseErr != nil || number < 1 || number > 4 {
			return nil, "", fmt.Errorf("key number must be 1..4")
		}
		kind, value = native.RFActionKey, byte(number-1)
		if len(args) == 4 {
			behavior, err = parseRFBehavior(args[3], false)
		}
	case "menu":
		if len(args) != 3 {
			return nil, "", fmt.Errorf("usage: rf map ID menu prev|next|dec|inc")
		}
		actions := map[string]byte{
			"prev": native.MenuPrevious, "previous": native.MenuPrevious,
			"next": native.MenuNext, "dec": native.MenuDecrease,
			"decrease": native.MenuDecrease, "inc": native.MenuIncrease,
			"increase": native.MenuIncrease,
		}
		var ok bool
		value, ok = actions[strings.ToLower(args[2])]
		if !ok {
			return nil, "", fmt.Errorf("menu action must be prev, next, dec, or inc")
		}
		kind = native.RFActionMenu
	case "relay":
		if len(args) != 4 {
			return nil, "", fmt.Errorf("usage: rf map ID relay 5..8 toggle|momentary|press")
		}
		number, parseErr := strconv.ParseUint(args[2], 0, 8)
		if parseErr != nil || number < 5 || number > 8 {
			return nil, "", errors.New(
				"learned RF can map directly only to user relays R5..R8; " +
					"use the door-gated side A/B up/down/stop mapping for motion",
			)
		}
		kind, value = native.RFActionRelay, byte(number-1)
		behavior, err = parseRFBehavior(args[3], false)
	case "side":
		if len(args) != 4 {
			return nil, "", fmt.Errorf("usage: rf map ID side left|right up|down|stop")
		}
		sides := map[string]byte{"left": 0, "a": 0, "0": 0, "right": 1, "b": 1, "1": 1}
		var ok bool
		value, ok = sides[strings.ToLower(args[2])]
		if !ok {
			return nil, "", fmt.Errorf("side must be left/A or right/B")
		}
		kind = native.RFActionSide
		behavior, err = parseRFBehavior(args[3], true)
	case "pwm":
		if len(args) < 3 || len(args) > 4 {
			return nil, "", fmt.Errorf("usage: rf map ID pwm 0..10 [press|toggle|momentary]")
		}
		channel, parseErr := strconv.ParseUint(args[2], 0, 8)
		if parseErr != nil || channel > 10 {
			return nil, "", fmt.Errorf("PWM user channel must be 0..10")
		}
		kind, value = native.RFActionPWM, byte(channel)
		if len(args) == 4 {
			behavior, err = parseRFBehavior(args[3], false)
		}
	default:
		return nil, "", fmt.Errorf("unknown RF mapping action %q", action)
	}
	if err != nil {
		return nil, "", err
	}
	payload, err := native.RFMappingPayload(id, kind, value, behavior)
	if err != nil {
		return nil, "", err
	}
	return payload, fmt.Sprintf(
		"learned RF entry %d mapped to %s",
		id,
		formatRFMapping(kind, value, behavior),
	), nil
}

func parseRFBehavior(value string, allowMotion bool) (byte, error) {
	behaviors := map[string]byte{
		"press":     native.RFBehaviorPress,
		"toggle":    native.RFBehaviorToggle,
		"momentary": native.RFBehaviorMomentary,
		"push":      native.RFBehaviorMomentary,
	}
	if allowMotion {
		behaviors = map[string]byte{
			"up":   native.RFBehaviorUp,
			"down": native.RFBehaviorDown,
			"stop": native.RFBehaviorStop,
		}
	}
	behavior, ok := behaviors[strings.ToLower(value)]
	if !ok {
		if allowMotion {
			return 0, fmt.Errorf("motion must be up, down, or stop")
		}
		return 0, fmt.Errorf("behavior must be press, toggle, or momentary")
	}
	return behavior, nil
}

func programCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	if len(args) != 0 && strings.EqualFold(args[0], "flash") {
		return safeFlashCommand(ctx, runtime, options, args[1:])
	}
	if len(args) != 0 && strings.EqualFold(args[0], "recover") {
		return recoverProgrammingCommand(ctx, runtime, options, args[1:])
	}
	if len(args) < 2 || len(args) > 5 {
		return "", fmt.Errorf("usage: program flash HEX [PORT] [advanced flags] | program OPERATION METHOD PATH [PORT]")
	}
	operation := programmer.OperationWriteFlash
	methodIndex := 0
	if parsed := parseProgramOperation(args[0]); parsed != "" {
		operation = parsed
		methodIndex = 1
	}
	if len(args) <= methodIndex {
		return "", fmt.Errorf("programmer method is required")
	}
	method := programmer.Method(strings.ToLower(args[methodIndex]))
	programOptions := programmer.Options{
		Method: method, Operation: operation, FQBN: options.FQBN,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig,
		Avrdude: options.Avrdude, AvrdudeConf: options.AvrdudeConf,
		Programmer:     options.Programmer,
		USBaspAutoSlow: true,
	}
	pathIndex := methodIndex + 1
	needsPath := operation != programmer.OperationMetadata &&
		operation != programmer.OperationProbe &&
		operation != programmer.OperationStart &&
		operation != programmer.OperationCoreInfo &&
		operation != programmer.OperationBurnBoot
	if needsPath && len(args) <= pathIndex {
		return "", fmt.Errorf("%s requires an input or output path", operation)
	}
	switch method {
	case programmer.MethodCompile:
		if operation != programmer.OperationWriteFlash {
			return "", fmt.Errorf("%s does not support operation %s", method, operation)
		}
		programOptions.SketchPath = args[pathIndex]
		if programOptions.SketchPath == "." && options.ProjectPath != "" {
			programOptions.SketchPath = options.ProjectPath
		}
	case programmer.MethodArduino:
		switch operation {
		case programmer.OperationWriteFlash:
			programOptions.SketchPath = args[pathIndex]
			if programOptions.SketchPath == "." && options.ProjectPath != "" {
				programOptions.SketchPath = options.ProjectPath
			}
		case programmer.OperationCoreInfo, programmer.OperationBurnBoot:
			// These dependency-CLI operations do not take a project or memory file.
		default:
			return "", fmt.Errorf("%s does not support operation %s", method, operation)
		}
	case programmer.MethodUrclock, programmer.MethodUSBasp, programmer.MethodAvrdude:
		switch operation {
		case programmer.OperationReadFlash, programmer.OperationReadEEPROM,
			programmer.OperationBackup:
			programOptions.OutputPath = args[pathIndex]
		case programmer.OperationMetadata, programmer.OperationProbe,
			programmer.OperationStart:
			// No memory file.
		default:
			programOptions.HexPath = args[pathIndex]
		}
	default:
		return "", fmt.Errorf("unsupported programming method %q", method)
	}
	if operation == programmer.OperationWriteFlash && method != programmer.MethodCompile {
		return "", errors.New(
			"direct flash writes are disabled; use program flash HEX [PORT] so a complete flash, EEPROM, and metadata backup is verified first",
		)
	}
	nextIndex := pathIndex
	if needsPath {
		nextIndex++
	}
	if operation == programmer.OperationWriteEEPROM {
		if len(args) <= nextIndex ||
			!strings.EqualFold(args[nextIndex], "CONFIRM") {
			return "", fmt.Errorf(
				"EEPROM write requires literal CONFIRM before optional port",
			)
		}
		programOptions.ConfirmEEPROMWrite = true
		nextIndex++
	}
	if len(args) > nextIndex {
		programOptions.Port = args[nextIndex]
		nextIndex++
	} else if operation != programmer.OperationCoreInfo &&
		operation != programmer.OperationBurnBoot {
		programOptions.Port = runtime.Snapshot().Port.Name
	}
	if len(args) != nextIndex {
		return "", fmt.Errorf("too many program arguments")
	}
	snapshot := runtime.Snapshot()
	programOptions.ApplicationHash = snapshot.Hello.BuildHash
	programOptions.ApplicationIdentitySchema = snapshot.Hello.IdentitySchema
	programOptions.ApplicationPackedTimestamp = snapshot.Hello.BuildTimestamp
	if method == programmer.MethodCompile {
		var err error
		programOptions, _, err = programmer.PlanCompile(programOptions)
		if err != nil {
			return "", err
		}
	}
	if operation == programmer.OperationBackup {
		if err := programmer.ValidateBackup(programOptions); err != nil {
			return "", err
		}
	}
	commandDescription := fmt.Sprintf(
		"backup flash + EEPROM + metadata to a timestamped directory under %s",
		programOptions.OutputPath,
	)
	if operation != programmer.OperationBackup {
		commandLine, err := programmer.Build(programOptions)
		if err != nil {
			return "", err
		}
		commandDescription = commandLine.String()
	}

	deviceOperation := method != programmer.MethodCompile &&
		operation != programmer.OperationCoreInfo
	if deviceOperation {
		runtime.programmingMu.Lock()
		defer runtime.programmingMu.Unlock()
	}
	serialWasOpen := runtime.Snapshot().Connected
	if deviceOperation && serialWasOpen {
		if err := runtime.Close(); err != nil {
			return "", err
		}
	}

	var output bytes.Buffer
	if deviceOperation {
		fmt.Fprintln(&output, "application UART released; Urboot/AVRDUDE now has exclusive port ownership")
	}
	fmt.Fprintln(&output, commandDescription)
	programErr := programmer.Execute(ctx, programOptions, &output)
	if programErr == nil {
		fmt.Fprintln(&output, "programmer operation completed")
	} else {
		fmt.Fprintln(&output, "programmer operation failed:", programErr)
	}
	if deviceOperation && serialWasOpen {
		reconnectContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			12*time.Second,
		)
		defer cancel()
		reconnectErr := reconnectProgrammingDevice(reconnectContext, runtime, snapshot.Port)
		if reconnectErr != nil {
			return strings.TrimSpace(output.String()), fmt.Errorf(
				"programmer result (%v); application HELLO reconnect failed: %w",
				programErr,
				reconnectErr,
			)
		}
		snapshot := runtime.Snapshot()
		fmt.Fprintf(
			&output,
			"application mode restored and authenticated on %s: %s\n",
			snapshot.Port.Name,
			formatHello(snapshot.Hello),
		)
	}
	if programErr != nil {
		return strings.TrimSpace(output.String()), programErr
	}
	return strings.TrimSpace(output.String()), nil
}

func safeFlashCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	const usage = "usage: program flash HEX [PORT] [--method urclock|usbasp] [--allow-incomplete-backup] [--reinitialize-eeprom]"
	if len(args) == 0 {
		return "", errors.New(usage)
	}
	firmwarePath := strings.TrimSpace(args[0])
	if firmwarePath == "" {
		return "", errors.New(usage)
	}
	method := programmer.MethodUrclock
	port := ""
	allowIncomplete := false
	reinitializeEEPROM := false
	for index := 1; index < len(args); index++ {
		argument := strings.TrimSpace(args[index])
		lower := strings.ToLower(argument)
		switch {
		case lower == "--allow-incomplete-backup":
			allowIncomplete = true
		case lower == "--reinitialize-eeprom":
			reinitializeEEPROM = true
		case lower == "--method":
			if index+1 >= len(args) {
				return "", errors.New(usage)
			}
			index++
			method = programmer.Method(strings.ToLower(strings.TrimSpace(args[index])))
		case strings.HasPrefix(lower, "--method="):
			method = programmer.Method(strings.TrimPrefix(lower, "--method="))
		default:
			if strings.HasPrefix(argument, "--") || port != "" {
				return "", fmt.Errorf("%s", usage)
			}
			port = argument
		}
	}
	if method != programmer.MethodUrclock && method != programmer.MethodUSBasp {
		return "", fmt.Errorf("guarded flash method must be urclock or usbasp, got %q", method)
	}
	if reinitializeEEPROM && allowIncomplete {
		return "", errors.New("--reinitialize-eeprom requires a complete verified raw flash, EEPROM, and metadata backup; it cannot be combined with --allow-incomplete-backup")
	}
	if runtime == nil {
		return "", errors.New("guarded flash requires an application runtime")
	}
	runtime.programmingMu.Lock()
	defer runtime.programmingMu.Unlock()
	firmwareDocument, err := programmer.LoadIntelHex(firmwarePath)
	if err != nil {
		return "", fmt.Errorf("inspect firmware before releasing UART: %w", err)
	}
	snapshot := runtime.Snapshot()
	if reinitializeEEPROM && !snapshot.Connected {
		return "", errors.New("--reinitialize-eeprom requires an authenticated application connection so the post-backup Prog latch can be armed and the final settings can be verified")
	}
	if method == programmer.MethodUrclock {
		if port == "" {
			port = snapshot.Port.Name
		}
		if strings.TrimSpace(port) == "" {
			return "", errors.New("guarded Urclock flash requires a connected device or explicit port")
		}
		if snapshot.Connected {
			selector, err := ports.ParseSelector(port)
			if err != nil {
				return "", fmt.Errorf("parse guarded Urclock device selector: %w", err)
			}
			if len(ports.Candidates([]ports.Info{snapshot.Port}, selector)) != 1 {
				return "", fmt.Errorf(
					"guarded Urclock selector %q does not identify the authenticated device on %s",
					port,
					snapshot.Port.Name,
				)
			}
			port = snapshot.Port.Name
		}
	} else if port != "" {
		return "", errors.New("USBasp method does not accept a serial port")
	}
	dataPaths := options.ProgramDataPaths
	if strings.TrimSpace(dataPaths.DataDir) == "" {
		var err error
		dataPaths, err = programmer.DefaultHostDataPaths()
		if err != nil {
			return "", err
		}
	}
	if err := programmer.EnsureHostDataPaths(dataPaths); err != nil {
		return "", err
	}
	runner := options.ProgramRunner
	if runner == nil {
		runner = programmer.CommandRunnerFunc(programmer.Run)
	}
	execute := options.ProgramExecute
	if execute == nil {
		execute = programmer.Execute
	}
	backup := programmer.Options{
		Method: method, Port: port, Operation: programmer.OperationBackup,
		FQBN: options.FQBN, Programmer: options.Programmer,
		ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig, Avrdude: options.Avrdude,
		AvrdudeConf:               options.AvrdudeConf,
		USBaspAutoSlow:            true,
		ApplicationHash:           snapshot.Hello.BuildHash,
		ApplicationIdentitySchema: snapshot.Hello.IdentitySchema,
	}
	writeOptions := backup
	writeOptions.Operation = programmer.OperationWriteFlash
	writeOptions.HexPath = firmwarePath
	lifecycleOptions := ProgrammingLifecycleOptions{
		DataPaths:          dataPaths,
		Outputs:            options.Outputs,
		HostConfig:         options.HostConfig,
		ReinitializeEEPROM: reinitializeEEPROM,
	}
	serialWasOpen := snapshot.Connected
	var programmingSession *ProgrammingSession
	var prepareOutput bytes.Buffer
	if serialWasOpen {
		programmingSession, err = findRetryableProgrammingSession(
			dataPaths,
			programmingIdentity(snapshot.Port),
			firmwareDocument.SourceSHA256,
			reinitializeEEPROM,
		)
		if err != nil {
			return "", fmt.Errorf("inspect pending programming state: %w", err)
		}
		if programmingSession != nil {
			if err := reassertProgrammingSession(
				ctx,
				runtimeProgrammingDevice{runtime: runtime, options: lifecycleOptions},
				programmingSession,
				lifecycleOptions,
			); err != nil {
				return "", fmt.Errorf("resume failed programming transaction: %w", err)
			}
			fmt.Fprintf(
				&prepareOutput,
				"resuming verified-safe failed programming transaction for firmware SHA-256 %s\n",
				firmwareDocument.SourceSHA256,
			)
		} else {
			programmingSession, err = PrepareProgrammingSession(
				ctx,
				runtime,
				firmwarePath,
				lifecycleOptions,
				&prepareOutput,
			)
			if err != nil {
				return strings.TrimSpace(prepareOutput.String()), fmt.Errorf(
					"prepare application programming state: %w", err,
				)
			}
		}
		if err := runtime.Close(); err != nil {
			return strings.TrimSpace(prepareOutput.String()), fmt.Errorf(
				"release application UART (settings recovery marker retained): %w", err,
			)
		}
	}
	var output bytes.Buffer
	if serialWasOpen {
		output.Write(prepareOutput.Bytes())
		if output.Len() != 0 && !strings.HasSuffix(output.String(), "\n") {
			output.WriteByte('\n')
		}
	}
	fmt.Fprintln(&output, "application UART released; guarded programmer transaction has exclusive ownership")
	fmt.Fprintf(&output, "pre-flash method=%s firmware=%s\n", method, firmwarePath)
	var migratedEEPROMPath string
	defer func() {
		if migratedEEPROMPath != "" {
			_ = os.Remove(migratedEEPROMPath)
		}
	}()
	var afterBackup programmer.PostBackupOperation
	if serialWasOpen && programmingSession != nil && reinitializeEEPROM {
		afterBackup = func(
			backupContext context.Context,
			backupResult programmer.AutomaticPreflashResult,
			writer io.Writer,
		) error {
			reconnectContext, reconnectCancel := context.WithTimeout(
				context.WithoutCancel(backupContext), 12*time.Second,
			)
			reconnectErr := reconnectProgrammingDevice(
				reconnectContext, runtime, snapshot.Port,
			)
			reconnectCancel()
			if reconnectErr != nil {
				return fmt.Errorf("reconnect application after untouched raw backup: %w", reconnectErr)
			}
			armContext, armCancel := context.WithTimeout(
				context.WithoutCancel(backupContext), 8*time.Second,
			)
			armErr := ArmProgrammingSessionAfterBackup(
				armContext, runtime, programmingSession, lifecycleOptions, writer,
			)
			armCancel()
			closeErr := runtime.Close()
			if armErr != nil {
				return errors.Join(armErr, closeErr)
			}
			if closeErr != nil {
				return fmt.Errorf("release application UART after arming programming latch: %w", closeErr)
			}
			path, decoded, err := programmer.StageMigratedProgrammingEEPROM(
				backupResult.BackupManifest, dataPaths,
			)
			if err != nil {
				return fmt.Errorf("stage migrated Silent/Prog EEPROM for atomic application write: %w", err)
			}
			migratedEEPROMPath = path
			fmt.Fprintf(
				writer,
				"semantic EEPROM schema-%d migration staged for the same flash programmer session\n",
				decoded.Schema,
			)
			return nil
		}
	}
	result, flashErr := programmer.AutomaticBackupThenFlash(
		ctx,
		programmer.AutomaticPreflashOptions{
			FirmwarePath: firmwarePath,
			Backup:       backup, DataPaths: dataPaths,
			AllowFlashWithoutFullBackup: allowIncomplete,
			AfterBackup:                 afterBackup,
		},
		runner,
		func(flashContext context.Context, path string, writer io.Writer) error {
			writeOptions.HexPath = path
			if migratedEEPROMPath != "" {
				writeOptions.EEPROMHexPath = migratedEEPROMPath
				writeOptions.ConfirmEEPROMWrite = true
				fmt.Fprintln(writer, "atomic programmer transaction: flash first, migrated EEPROM second, one final target reset")
			}
			return execute(flashContext, writeOptions, writer)
		},
		&output,
	)
	fmt.Fprintf(&output, "firmware SHA-256: %s\n", result.FirmwareSHA256)
	if result.BackupReference != "" {
		fmt.Fprintf(&output, "verified backup: %s\n", result.BackupReference)
	}
	if result.BackupManifest != "" {
		fmt.Fprintf(&output, "backup manifest: %s\n", result.BackupManifest)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(&output, "WARNING:", warning)
	}
	if result.Flashed {
		fmt.Fprintln(&output, "guarded firmware flash completed")
	}
	verifiedProgram := flashErr == nil && result.Flashed
	if programmingSession != nil {
		if markerErr := MarkProgrammingSessionComplete(
			programmingSession, verifiedProgram,
		); markerErr != nil {
			flashErr = errors.Join(flashErr, fmt.Errorf(
				"persist host programming result (safety latch retained): %w", markerErr,
			))
			verifiedProgram = false
		}
	}
	var reconnectErr error
	var restoreErr error
	if serialWasOpen {
		reconnectContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 12*time.Second)
		reconnectErr = reconnectProgrammingDevice(reconnectContext, runtime, snapshot.Port)
		cancel()
		if reconnectErr == nil && verifiedProgram {
			restoreContext, restoreCancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
			restoreErr = RestoreProgrammingSession(
				restoreContext, runtime, programmingSession,
				lifecycleOptions, &output,
			)
			restoreCancel()
		} else if reconnectErr == nil && programmingSession != nil {
			fmt.Fprintln(&output,
				"programmer result was not verified successful; programming latch and recovery marker retained")
		} else {
			reconnectErr = fmt.Errorf(
				"application HELLO reconnect failed; settings recovery marker retained: %w",
				reconnectErr,
			)
		}
		if reconnectErr == nil {
			connected := runtime.Snapshot()
			fmt.Fprintf(&output, "application mode restored and authenticated on %s: %s\n", connected.Port.Name, formatHello(connected.Hello))
		}
	}
	if joined := errors.Join(flashErr, reconnectErr, restoreErr); joined != nil {
		return strings.TrimSpace(output.String()), joined
	}
	return strings.TrimSpace(output.String()), nil
}

// recoverProgrammingCommand proves an already-written image with a fresh
// Urboot readback, then completes the original durable restore transaction.
// It never writes flash and is owned by the same primary runtime as COM18.
func recoverProgrammingCommand(
	ctx context.Context,
	runtime *Runtime,
	options CommandOptions,
	args []string,
) (string, error) {
	const usage = "usage: program recover HEX [PORT]"
	if runtime == nil || len(args) < 1 || len(args) > 2 {
		return "", errors.New(usage)
	}
	runtime.programmingMu.Lock()
	defer runtime.programmingMu.Unlock()

	firmwarePath := strings.TrimSpace(args[0])
	if firmwarePath == "" {
		return "", errors.New(usage)
	}
	document, err := programmer.LoadIntelHex(firmwarePath)
	if err != nil {
		return "", fmt.Errorf("inspect programming recovery target: %w", err)
	}
	snapshot := runtime.Snapshot()
	if !snapshot.Connected || strings.TrimSpace(snapshot.Port.Name) == "" {
		return "", errors.New("programming recovery requires the authenticated application device")
	}
	if len(args) == 2 {
		selector, parseErr := ports.ParseSelector(args[1])
		if parseErr != nil {
			return "", fmt.Errorf("parse programming recovery selector: %w", parseErr)
		}
		if len(ports.Candidates([]ports.Info{snapshot.Port}, selector)) != 1 {
			return "", fmt.Errorf(
				"programming recovery selector %q does not identify the authenticated device on %s",
				args[1], snapshot.Port.Name,
			)
		}
	}
	dataPaths := options.ProgramDataPaths
	if strings.TrimSpace(dataPaths.DataDir) == "" {
		dataPaths, err = programmer.DefaultHostDataPaths()
		if err != nil {
			return "", err
		}
	}
	if err := programmer.EnsureHostDataPaths(dataPaths); err != nil {
		return "", err
	}
	session, err := findFailedProgrammingSession(
		dataPaths,
		programmingIdentity(snapshot.Port),
		document.SourceSHA256,
	)
	if err != nil {
		return "", fmt.Errorf("locate failed programming transaction: %w", err)
	}
	if session == nil {
		return "", errors.New("no failed programming transaction matches this device and firmware SHA-256")
	}
	lifecycleOptions := ProgrammingLifecycleOptions{
		DataPaths: dataPaths, Outputs: options.Outputs,
		HostConfig: options.HostConfig, ReinitializeEEPROM: session.ReinitializeEEPROM,
	}
	var output bytes.Buffer
	if err := reassertProgrammingSession(
		ctx,
		runtimeProgrammingDevice{runtime: runtime, options: lifecycleOptions},
		session,
		lifecycleOptions,
	); err != nil {
		return "", fmt.Errorf("reassert programming recovery safe state: %w", err)
	}
	fmt.Fprintf(
		&output,
		"fresh read-only recovery verification for firmware SHA-256 %s on %s\n",
		document.SourceSHA256,
		snapshot.Port.Name,
	)
	if err := runtime.Close(); err != nil {
		return strings.TrimSpace(output.String()), fmt.Errorf(
			"release application UART for recovery readback: %w", err,
		)
	}
	runner := options.ProgramRunner
	if runner == nil {
		runner = programmer.CommandRunnerFunc(programmer.Run)
	}
	verifyOptions := programmer.Options{
		Method: programmer.MethodUrclock, Port: snapshot.Port.Name,
		HexPath: firmwarePath, FQBN: options.FQBN,
		Programmer: options.Programmer, ArduinoCLI: options.ArduinoCLI, ArduinoConfig: options.ArduinoConfig,
		Avrdude: options.Avrdude, AvrdudeConf: options.AvrdudeConf,
		USBaspAutoSlow: true,
	}
	verifyErr := programmer.VerifyFlashReadbackWithRunner(
		ctx, verifyOptions, &output, runner,
	)
	verified := verifyErr == nil
	markerErr := MarkProgrammingSessionComplete(session, verified)
	if markerErr != nil {
		verified = false
		markerErr = fmt.Errorf("persist recovered programmer result: %w", markerErr)
	}

	reconnectContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 12*time.Second)
	reconnectErr := reconnectProgrammingDevice(reconnectContext, runtime, snapshot.Port)
	cancel()
	var restoreErr error
	if reconnectErr == nil && verified {
		restoreContext, restoreCancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		restoreErr = RestoreProgrammingSession(
			restoreContext, runtime, session, lifecycleOptions, &output,
		)
		restoreCancel()
	} else if reconnectErr == nil {
		fmt.Fprintln(&output,
			"fresh readback was not verified; safe outputs and recovery marker remain active")
	}
	if reconnectErr == nil {
		connected := runtime.Snapshot()
		fmt.Fprintf(
			&output,
			"application mode restored and authenticated on %s: %s\n",
			connected.Port.Name,
			formatHello(connected.Hello),
		)
	}
	if joined := errors.Join(verifyErr, markerErr, reconnectErr, restoreErr); joined != nil {
		return strings.TrimSpace(output.String()), joined
	}
	fmt.Fprintln(&output, "failed programming transaction recovered without rewriting flash")
	return strings.TrimSpace(output.String()), nil
}

// reconnectProgrammingDevice keeps auto-discovery paused until the exact
// physical device released for programming has authenticated again.
func reconnectProgrammingDevice(
	ctx context.Context,
	runtime *Runtime,
	expected ports.Info,
) error {
	if runtime == nil {
		return errors.New("programming reconnect requires an application runtime")
	}
	if strings.TrimSpace(expected.Name) == "" {
		return errors.New("programming reconnect has no original port identity")
	}
	selector := programmingReconnectSelector(expected)
	if err := runtime.Open(ctx, selector); err != nil {
		return fmt.Errorf("reopen original device %s: %w", expected.Name, err)
	}
	connected := runtime.Snapshot()
	if !connected.Connected || !sameProgrammingDevice(
		programmingIdentity(expected),
		programmingIdentity(connected.Port),
	) {
		_ = runtime.Close()
		return fmt.Errorf(
			"authenticated device on %s does not match the original programming device",
			expected.Name,
		)
	}
	return nil
}

func programmingReconnectSelector(device ports.Info) string {
	if strings.TrimSpace(device.InstanceID) != "" {
		return "instance:" + device.InstanceID
	}
	if strings.TrimSpace(device.SerialNumber) != "" {
		return "serial:" + device.SerialNumber
	}
	return device.Name
}

func parseProgramOperation(value string) programmer.Operation {
	switch programmer.Operation(strings.ToLower(value)) {
	case programmer.OperationWriteFlash, programmer.OperationReadFlash,
		programmer.OperationVerifyFlash, programmer.OperationReadEEPROM,
		programmer.OperationWriteEEPROM, programmer.OperationMetadata,
		programmer.OperationProbe, programmer.OperationStart,
		programmer.OperationCoreInfo, programmer.OperationBurnBoot,
		programmer.OperationBackup:
		return programmer.Operation(strings.ToLower(value))
	default:
		return ""
	}
}

func formatStatus(status native.Status) string {
	ledTemperature := formatLEDTemperature(status, 2)
	btTemperature := formatBTAudioTemperature(status, 2)
	return fmt.Sprintf(
		"uptime=%s supply=%.3fV bus=%.3fV current=%dmA power=%dmW tLED=%s tBT=%s\n"+
			"flags=0x%04X running=%t host_offline=%t hot=%t inputs=0x%02X keys=0x%02X relays=0x%02X menu=%d mode=%d door=%t bt=%d\n"+
			"PWM available=%t channel=%d value=%d errors=%d LCD=0x%02X framing=%d crc=%d reset_cause=0x%02X reset_count=%d",
		(time.Duration(status.UptimeMS) * time.Millisecond).Round(time.Millisecond),
		float64(status.SupplyMV)/1000,
		float64(status.BusMV)/1000,
		status.CurrentMA,
		status.PowerMW,
		ledTemperature,
		btTemperature,
		status.Flags,
		status.ProgramRunning,
		status.HostOffline,
		status.Hot,
		status.RawInputs,
		status.ActiveKeys,
		status.ActiveRelays,
		status.MenuPage,
		status.ProgramMode,
		status.DoorOpen,
		status.BluetoothState,
		status.PWMAvailable,
		status.PWMChannel,
		status.PWMValue,
		status.PWMErrors,
		status.LCDAddress,
		status.FramingErrors,
		status.CRCErrors,
		status.ResetCause,
		status.ResetCount,
	)
}

func formatStatusTemperature(value int16, available bool, decimals int) string {
	if !available {
		return "unavailable"
	}
	return fmt.Sprintf("%.*fC", decimals, float64(value)/100)
}

func formatLEDTemperature(status native.Status, decimals int) string {
	value, available := status.LEDTemperature()
	return formatStatusTemperature(value, available, decimals)
}

func formatBTAudioTemperature(status native.Status, decimals int) string {
	value, available := status.BTAudioTemperature()
	return formatStatusTemperature(value, available, decimals)
}

const (
	defaultBuzzerFrequencyHz uint16 = 2000
	defaultBuzzerDurationMS  uint16 = 40
	minimumBuzzerFrequencyHz uint64 = 20
	maximumBuzzerFrequencyHz uint64 = 20000
	maximumBuzzerDurationMS  uint64 = 60000
)

func parseBuzzerToneArgs(args []string) (uint16, uint16, error) {
	if len(args) > 2 {
		return 0, 0, errors.New("usage: buzzer|beep [FREQUENCY_HZ [DURATION_MS]]")
	}
	if len(args) == 0 {
		return defaultBuzzerFrequencyHz, defaultBuzzerDurationMS, nil
	}
	frequency, err := strconv.ParseUint(args[0], 0, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid buzzer frequency %q", args[0])
	}
	duration := uint64(defaultBuzzerDurationMS)
	if len(args) == 2 {
		duration, err = strconv.ParseUint(args[1], 0, 16)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid buzzer duration %q", args[1])
		}
	}
	if frequency == 0 && duration == 0 && len(args) == 2 {
		return 0, 0, nil
	}
	if frequency == 0 {
		return 0, 0, errors.New("buzzer stop is exactly 0 0; timed pauses belong to melodies")
	}
	if frequency < minimumBuzzerFrequencyHz || frequency > maximumBuzzerFrequencyHz {
		return 0, 0, fmt.Errorf("buzzer frequency must be %d..%d Hz", minimumBuzzerFrequencyHz, maximumBuzzerFrequencyHz)
	}
	if duration == 0 || duration > maximumBuzzerDurationMS {
		return 0, 0, fmt.Errorf("buzzer duration must be 1..%d ms", maximumBuzzerDurationMS)
	}
	return uint16(frequency), uint16(duration), nil
}

func formatSettings(settings native.Settings) string {
	return fmt.Sprintf(
		"flags=0x%02X light=%d on=%d off=%d display_open=%d display_closed=%d status=%d "+
			"output_persistence=0x%02X relay_restore_mask=0x%02X stream=%dms default_page=%d save_last=%t "+
			"status_color=%d voltage_decimals=%d current_decimals=%d "+
			"motion_door=%s motion_break=%dms motion_exit_hold=%ds "+
			"door_audio=%t relay_audio=%t programming_latch=%t persisted=%t extended=0x%02X",
		settings.Flags,
		settings.LightMode,
		settings.OnBrightness,
		settings.OffBrightness,
		settings.DisplayBrightness,
		settings.DisplayClosedBrightness,
		settings.StatusBrightness,
		settings.OutputPersistence,
		settings.RelayRestoreMask,
		settings.StreamPeriodMS,
		settings.DefaultPage,
		settings.SaveLastPage(),
		settings.StatusColor(),
		settings.VoltageDecimals(),
		settings.CurrentDecimals(),
		motionDoorPolicyName(settings.MotionDoorPolicy()),
		settings.MotionBreakMS(),
		settings.MotionExitHoldSeconds,
		settings.DoorAudioEnabled(),
		settings.RelayAudioEnabled(),
		settings.Flags&native.SettingsProgrammingMode != 0,
		settings.Persisted,
		settings.ExtendedFlags,
	)
}

func motionDoorPolicyName(value byte) string {
	return map[byte]string{
		native.MotionDoorAlways:     "always",
		native.MotionDoorClosedOnly: "closed",
		native.MotionDoorOpenOnly:   "open",
		native.MotionDoorNever:      "never",
	}[value]
}

func formatHello(hello native.Hello) string {
	base := fmt.Sprintf(
		"%s kind=%d capabilities=0x%08X",
		hello.Name,
		hello.BoardKind,
		hello.Capabilities,
	)
	if hello.IdentitySchema == native.IdentitySchemaCompact {
		stamp := hello.BuildStamp
		if stamp == "" {
			stamp = "unknown"
		}
		return fmt.Sprintf(
			"%s build=%08X timestamp=%s packed=0x%08X profile=%s(%d) build_features=0x%02X",
			base,
			hello.BuildHash,
			stamp,
			hello.BuildTimestamp,
			native.FeatureProfileName(hello.FeatureProfile),
			hello.FeatureProfile,
			hello.BuildFeatures,
		)
	}
	return base + " build identity unavailable"
}

func formatTemperatures(sensors []native.TemperatureSensor) string {
	if len(sensors) == 0 {
		return "no DS18B20 sensors detected"
	}
	lines := []string{"ROLE  ROM                       TEMPERATURE"}
	for _, sensor := range sensors {
		role := fmt.Sprintf("role%d", sensor.Role)
		switch sensor.Role {
		case 0:
			role = "tLED"
		case 1:
			role = "tBT"
		}
		temperature := "--"
		if sensor.CelsiusCenti != -32768 {
			temperature = fmt.Sprintf("%.2f C", float64(sensor.CelsiusCenti)/100)
		}
		lines = append(lines, fmt.Sprintf(
			"%-5s %-25s %s",
			role,
			sensor.Identifier(),
			temperature,
		))
	}
	return strings.Join(lines, "\n")
}

func macroCommand(
	ctx context.Context,
	runner *MacroRunner,
	args []string,
) (string, error) {
	const usage = "macro list|show NAME_OR_ID|create ID NAME [CATEGORY [COLOR]]|update NAME_OR_ID NEW_NAME [CATEGORY [COLOR]]|rename NAME_OR_ID NAME|category NAME_OR_ID CATEGORY|delete NAME_OR_ID|record start NAME [CATEGORY [COLOR]]|record board start ID|stop|clear [force]|status|record status|save|stop|discard|play NAME_OR_ID|status|monitor|cancel [keep]"
	if len(args) < 1 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	switch strings.ToLower(args[0]) {
	case "list":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: macro list")
		}
		macros := runner.List()
		if len(macros) == 0 {
			return "no macros configured", nil
		}
		lines := []string{"ID  NAME                 CATEGORY       COLOR   STEPS  DURATION"}
		for _, macro := range macros {
			var duration time.Duration
			if len(macro.Steps) != 0 {
				if due, err := macroStepDueUS(macro.Steps[len(macro.Steps)-1]); err == nil {
					duration = time.Duration(due) * time.Microsecond
				}
			}
			lines = append(lines, fmt.Sprintf(
				"%-3d %-20s %-14s %-7s %-6d %s",
				macro.ID,
				macro.Name,
				macro.Category,
				normalizedMacroColor(macro.Color),
				len(macro.Steps),
				duration,
			))
		}
		return strings.Join(lines, "\n"), nil
	case "show", "inspect":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: macro show NAME_OR_ID")
		}
		macro, err := runner.find(args[1])
		if err != nil {
			return "", err
		}
		compiled, err := compileMacro(macro)
		if err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf(
			"macro id=%d name=%q category=%q color=%q label=%q steps=%d duration=%s encoded=%dB tolerance=%dus keep_on_cancel=%t recording_source=%q capture_dropped=%d capture_missing=%d",
			macro.ID, macro.Name, macro.Category, normalizedMacroColor(macro.Color),
			macro.Label, len(macro.Steps), time.Duration(compiled.durationUS)*time.Microsecond,
			len(compiled.stream), macro.TimingToleranceUS, macro.KeepOutputsOnCancel,
			macro.RecordingSource, macro.CaptureDroppedSteps, macro.CaptureMissingSteps,
		)}
		previousDue := uint32(0)
		for index, step := range macro.Steps {
			due, _ := macroStepDueUS(step)
			delta := due - previousDue
			lines = append(lines, fmt.Sprintf(
				"%3d  at_us=%-10d delta_us=%-10d (+%-12s) %-12s target=%d value=%d",
				index+1, due, delta, time.Duration(due)*time.Microsecond,
				step.Kind, step.Target, step.Value,
			))
			previousDue = due
		}
		return strings.Join(lines, "\n"), nil
	case "create":
		if len(args) < 3 || len(args) > 5 {
			return "", fmt.Errorf("usage: macro create ID NAME [CATEGORY [COLOR]]")
		}
		id, err := strconv.ParseUint(args[1], 0, 8)
		if err != nil {
			return "", fmt.Errorf("macro ID: %w", err)
		}
		category, color := "", ""
		if len(args) >= 4 {
			category = args[3]
		}
		if len(args) == 5 {
			color = args[4]
		}
		macro, err := runner.CreateDraft(byte(id), args[2], category, color)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("macro %d/%s draft created; add steps in the watched host config or record a new macro", macro.ID, macro.Name), nil
	case "update":
		if len(args) < 3 || len(args) > 5 {
			return "", fmt.Errorf("usage: macro update NAME_OR_ID NEW_NAME [CATEGORY [COLOR]]")
		}
		var category, color *string
		if len(args) >= 4 {
			category = &args[3]
		}
		if len(args) == 5 {
			color = &args[4]
		}
		macro, err := runner.UpdateMetadata(args[1], args[2], category, color)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("macro %d renamed to %q category=%q color=%q", macro.ID, macro.Name, macro.Category, macro.Color), nil
	case "delete", "remove":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: macro delete NAME_OR_ID")
		}
		if err := runner.Delete(args[1]); err != nil {
			return "", err
		}
		return "macro deleted from HOST configuration", nil
	case "rename":
		if len(args) != 3 {
			return "", fmt.Errorf("usage: macro rename NAME_OR_ID NAME")
		}
		macro, err := runner.Rename(args[1], args[2])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("macro %d renamed to %q in HOST configuration", macro.ID, macro.Name), nil
	case "category", "categorize":
		if len(args) != 3 {
			return "", fmt.Errorf("usage: macro category NAME_OR_ID CATEGORY")
		}
		macro, err := runner.SetCategory(args[1], args[2])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("macro %d/%s category set to %q in HOST configuration", macro.ID, macro.Name, macro.Category), nil
	case "record":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: macro record start NAME [CATEGORY [COLOR]]|board start ID|stop|clear [force]|status|save|stop|discard")
		}
		switch strings.ToLower(args[1]) {
		case "board":
			if len(args) < 3 {
				return "", errors.New("usage: macro record board start ID|stop|clear [force]|status")
			}
			switch strings.ToLower(args[2]) {
			case "start":
				if len(args) != 4 {
					return "", errors.New("usage: macro record board start ID")
				}
				id, parseErr := strconv.ParseUint(args[3], 0, 8)
				if parseErr != nil {
					return "", fmt.Errorf("macro capture ID: %w", parseErr)
				}
				state, startErr := runner.StartBoardCapture(ctx, byte(id))
				if startErr != nil {
					return "", startErr
				}
				return fmt.Sprintf("board macro capture %d started; front-panel, RF, and accepted ordinary actions use the retained MCU ring", state.BoardID), nil
			case "stop", "save":
				if len(args) != 3 {
					return "", errors.New("usage: macro record board stop")
				}
				state, stopErr := runner.StopBoardCapture(ctx)
				if stopErr != nil {
					return "", stopErr
				}
				return fmt.Sprintf("board macro capture %d sealed; retained pages are being fetched, deduplicated, saved, and export-acknowledged", state.BoardID), nil
			case "clear":
				if len(args) > 4 || (len(args) == 4 && !strings.EqualFold(args[3], "force")) {
					return "", errors.New("usage: macro record board clear [force]")
				}
				status, clearErr := runner.ClearBoardCapture(ctx, len(args) == 4)
				if clearErr != nil {
					return "", clearErr
				}
				return fmt.Sprintf("retained board macro capture cleared; state=%d fill=%d", status.State, status.Fill), nil
			case "status":
				if len(args) != 3 {
					return "", errors.New("usage: macro record board status")
				}
				state := runner.RecordingState()
				device := runner.State().Device
				return fmt.Sprintf("board macro recording active=%t owned=%t id=%d steps=%d dropped=%d device_state=%d fill=%d accepted=%d", state.Active, state.BoardOwned, state.BoardID, state.Steps, state.DroppedSteps, device.State, device.Fill, device.AcceptedSteps), nil
			default:
				return "", errors.New("usage: macro record board start ID|stop|clear [force]|status")
			}
		case "start":
			if len(args) < 3 || len(args) > 5 {
				return "", fmt.Errorf("usage: macro record start NAME [CATEGORY [COLOR]]")
			}
			category, color := "", ""
			if len(args) >= 4 {
				category = args[3]
			}
			if len(args) == 5 {
				color = args[4]
			}
			state, err := runner.StartRecording(args[2], category, color)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("recording macro %d/%s; host, front-panel, and RF actions use exact MCU deltas", state.ID, state.Name), nil
		case "status":
			if len(args) != 2 {
				return "", fmt.Errorf("usage: macro record status")
			}
			state := runner.RecordingState()
			if !state.Active && state.Name == "" {
				return "no macro has been recorded in this session", nil
			}
			return fmt.Sprintf("macro recording active=%t id=%d name=%q category=%q color=%q steps=%d host=%d panel=%d rf=%d last_at_us=%d last_delta_us=%d last_opcode=0x%02X last_source=%d started=%s error=%q", state.Active, state.ID, state.Name, state.Category, state.Color, state.Steps, state.HostSteps, state.PanelSteps, state.RFSteps, state.LastAtUS, state.LastDeltaUS, state.LastOpcode, state.LastSource, state.StartedAt.Format(time.RFC3339), state.LastError), nil
		case "save", "stop":
			if len(args) != 2 {
				return "", fmt.Errorf("usage: macro record save|stop")
			}
			macro, err := runner.StopRecording(true)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("macro %d/%s saved with %d exact MCU-timed steps", macro.ID, macro.Name, len(macro.Steps)), nil
		case "discard", "cancel":
			if len(args) != 2 {
				return "", fmt.Errorf("usage: macro record discard")
			}
			macro, err := runner.StopRecording(false)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("macro %d/%s recording discarded", macro.ID, macro.Name), nil
		default:
			return "", fmt.Errorf("usage: macro record start NAME [CATEGORY [COLOR]]|board start ID|stop|clear [force]|status|save|stop|discard")
		}
	case "play", "run", "start":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: macro play NAME_OR_ID")
		}
		state, err := runner.Start(ctx, args[1])
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"macro %d/%s buffered for MCU-timed playback with %d steps",
			state.ID,
			state.Name,
			state.StepCount,
		), nil
	case "status":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: macro status")
		}
		state := runner.State()
		if state.ID == 0 && state.Name == "" {
			return "no macro has run in this session", nil
		}
		return fmt.Sprintf(
			"macro id=%d name=%q lifecycle=%s running=%t step=%d/%d buffer=%dB timing=%dus max=%dus violations=%d underruns=%d dispatch_errors=%d dropped=%d faithful=%t started=%s error=%q",
			state.ID,
			state.Name,
			state.Lifecycle,
			state.Running,
			state.Step,
			state.StepCount,
			state.BufferFill,
			state.LastTimingDeltaUS,
			state.MaximumTimingErrorUS,
			state.TimingViolations,
			state.Underruns,
			state.DispatchErrors,
			state.DroppedSteps,
			state.Faithful,
			state.StartedAt.Format(time.RFC3339),
			state.LastError,
		), nil
	case "monitor":
		if len(args) != 1 {
			return "", fmt.Errorf("usage: macro monitor")
		}
		state := runner.State()
		recording := runner.RecordingState()
		return fmt.Sprintf(
			"macro monitor playback=%s running=%t id=%d name=%q step=%d/%d buffer=%dB underruns=%d faithful=%t recording=%t record_id=%d record_name=%q record_steps=%d",
			state.Lifecycle, state.Running, state.ID, state.Name, state.Step, state.StepCount,
			state.BufferFill, state.Underruns, state.Faithful, recording.Active, recording.ID,
			recording.Name, recording.Steps,
		), nil
	case "cancel", "stop":
		if len(args) > 2 || (len(args) == 2 && !strings.EqualFold(args[1], "keep")) {
			return "", fmt.Errorf("usage: macro cancel [keep]")
		}
		requestContext, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		keep := len(args) == 2
		if err := runner.CancelWithPolicy(requestContext, keep); err != nil {
			return "", err
		}
		if keep {
			return "macro cancelled; current outputs deliberately preserved", nil
		}
		return "macro cancelled safely; relays and user PWM outputs switched off", nil
	default:
		return "", fmt.Errorf("usage: %s", usage)
	}
}

func oneUint(args []string, bits int, usage string) (uint64, error) {
	values, err := exactUintArgs(args, 1, bits, usage)
	if err != nil {
		return 0, err
	}
	return values[0], nil
}

func exactUintArgs(args []string, count, bits int, usage string) ([]uint64, error) {
	if len(args) != count {
		return nil, fmt.Errorf("usage: %s", usage)
	}
	return uintArgs(args, bits)
}

func uintArgs(args []string, bits int) ([]uint64, error) {
	values := make([]uint64, len(args))
	for index, argument := range args {
		value, err := strconv.ParseUint(argument, 0, bits)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q", argument)
		}
		values[index] = value
	}
	return values, nil
}

func parseByte(value string) (byte, error) {
	parsed, err := strconv.ParseUint(value, 0, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid byte %q", value)
	}
	return byte(parsed), nil
}

func decodeHex(value string) ([]byte, error) {
	replacer := strings.NewReplacer(" ", "", ":", "", "-", "", "_", "")
	value = replacer.Replace(value)
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	if len(value)%2 != 0 {
		return nil, fmt.Errorf("hex input must contain complete bytes")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	return decoded, nil
}

func parseBool(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "1", "on", "true", "active":
		return true, nil
	case "0", "off", "false", "inactive":
		return false, nil
	default:
		return false, fmt.Errorf("state must be on or off")
	}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}
