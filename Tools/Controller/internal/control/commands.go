package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/shell"
)

const (
	settingsSetUsage = "settings set FLAGS LIGHT ON OFF DISPLAY STATUS " +
		"PWMBOOT STREAM [DEFAULT_PAGE SAVE_LAST " +
		"[STATUS_COLOR VOLTAGE_DECIMALS CURRENT_DECIMALS]]"
	settingsUsage = "settings | settings decimals VOLTAGE CURRENT | " +
		"settings color INDEX | settings motion always|closed|open|never | " +
		"settings motion-break 1|100 | " +
		"settings audio door|relay on|off | " + settingsSetUsage
)

type CommandOptions struct {
	ProjectPath      string
	FQBN             string
	Macros           func() []appconfig.Macro
	ArduinoCLI       string
	Avrdude          string
	AvrdudeConf      string
	Programmer       string
	HostConfig       func() appconfig.Config
	UpdateHostConfig func(func(*appconfig.Config) error) error
	Resolve          func() CommandOptions
	Outputs          *OutputScheduler
	ProgramRunner    programmer.CommandRunner
	ProgramExecute   func(context.Context, programmer.Options, io.Writer) error
	ProgramDataPaths programmer.HostDataPaths
}

func NewCommandEngine(runtime *Runtime, options CommandOptions) *shell.Engine {
	engine := shell.New(100)
	macroRunner := NewMacroRunner(
		runtime,
		options.Macros,
		options.HostConfig,
		options.UpdateHostConfig,
	)
	outputs := options.Outputs
	if outputs == nil {
		outputs = NewOutputScheduler(runtime)
	}
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
					"tLED=%.2f C tBT=%.2f C",
					float64(status.TLEDCenti)/100,
					float64(status.TBTCenti)/100,
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
			case "motion-break", "motion-dead-time":
				if len(args) != 2 {
					return "", fmt.Errorf("usage: settings motion-break 1|100")
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
				if err := storeSettings(ctx, runtime, settings); err != nil {
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
				if err := storeSettings(ctx, runtime, settings); err != nil {
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
					return "", fmt.Errorf("audio cue group must be door or relay")
				}
				if err := storeSettings(ctx, runtime, settings); err != nil {
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
				if err := storeSettings(ctx, runtime, settings); err != nil {
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
				if err := storeSettings(ctx, runtime, settings); err != nil {
					return "", err
				}
				return formatSettings(settings), nil

			case "set":
				if len(args) != 9 && len(args) != 11 && len(args) != 14 {
					return "", fmt.Errorf("usage: %s", settingsSetUsage)
				}
				current, err := querySettings(ctx, runtime)
				if err != nil {
					return "", err
				}
				settings, err := settingsFromSetArgs(current, args)
				if err != nil {
					return "", err
				}
				if err := storeSettings(ctx, runtime, settings); err != nil {
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
		Name: "pwm", Usage: "pwm get|off|mode MODE|set CHANNEL VALUE",
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
		Name: "buzzer", Usage: "buzzer FREQUENCY_HZ DURATION_MS", Summary: "play a buzzer tone",
		Run: func(ctx context.Context, args []string) (string, error) {
			values, err := exactUintArgs(args, 2, 16, "buzzer FREQUENCY_HZ DURATION_MS")
			if err != nil {
				return "", err
			}
			if values[1] == 0 {
				return "", errors.New("buzzer duration must be nonzero")
			}
			if values[0] != 0 && (values[0] < 20 || values[0] > 20000) {
				return "", errors.New(
					"buzzer frequency must be 0 or 20..20000 Hz",
				)
			}
			outputs.StopMelody()
			if err := command(ctx, runtime, native.OpBuzzer, native.BuzzerPayload(uint16(values[0]), uint16(values[1]))); err != nil {
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
		Name: "silent", Usage: "silent status|on|off",
		Summary: "query or persist the EEPROM-backed buzzer silent flag",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) != 1 {
				return "", fmt.Errorf("usage: silent status|on|off")
			}
			frame, err := request(ctx, runtime, native.OpGetSettings, nil, native.OpSettings)
			if err != nil {
				return "", err
			}
			settings, err := native.ParseSettings(frame.Payload)
			if err != nil {
				return "", err
			}
			switch strings.ToLower(args[0]) {
			case "status":
				return fmt.Sprintf(
					"silent=%t",
					settings.Flags&native.SettingsSilent != 0,
				), nil
			case "on":
				settings.Flags |= native.SettingsSilent
			case "off":
				settings.Flags &^= native.SettingsSilent
			default:
				return "", fmt.Errorf("usage: silent status|on|off")
			}
			payload, err := settings.Payload()
			if err != nil {
				return "", err
			}
			if err := command(ctx, runtime, native.OpSetSettings, payload); err != nil {
				return "", err
			}
			return fmt.Sprintf(
				"silent=%t saved to board EEPROM",
				settings.Flags&native.SettingsSilent != 0,
			), nil
		},
	})
	mustRegister(shell.Command{
		Name:    "display",
		Usage:   "display segments|lcd|both DURATION_MS [TEXT]",
		Summary: "show or clear bounded ASCII text on the board displays",
		Run: func(ctx context.Context, args []string) (string, error) {
			if len(args) < 2 {
				return "", fmt.Errorf("usage: display segments|lcd|both DURATION_MS [TEXT]")
			}
			targets := map[string]byte{
				"segments": native.DisplaySegments, "segment": native.DisplaySegments,
				"seg": native.DisplaySegments, "lcd": native.DisplayLCD,
				"both": native.DisplayBoth,
			}
			target, ok := targets[strings.ToLower(args[0])]
			if !ok {
				return "", fmt.Errorf("display target must be segments, lcd, or both")
			}
			duration, err := strconv.ParseUint(args[1], 0, 16)
			if err != nil {
				return "", fmt.Errorf("invalid display duration %q", args[1])
			}
			text := strings.Join(args[2:], " ")
			payload, err := native.DisplayTextPayload(target, uint16(duration), text)
			if err != nil {
				return "", err
			}
			if err := command(ctx, runtime, native.OpDisplayText, payload); err != nil {
				return "", err
			}
			if target == native.DisplayLCD || target == native.DisplayBoth {
				line1, line2 := text, ""
				if len(line1) > 16 {
					line2 = line1[16:]
					line1 = line1[:16]
				}
				if err := runtime.LCDPresenter().RenderPhysical(
					ctx,
					lcdLine(line1),
					lcdLine(line2),
				); err != nil {
					// The native display command remains successful when the
					// optional PC-owned backpack is absent or temporarily busy.
					runtime.PublishStructuredEvent(Event{
						Kind: "lcd.error", Lifecycle: "render", State: "degraded",
						Text: "direct LCD render: " + err.Error(),
					})
				}
			}
			if text == "" {
				return "display override cleared", nil
			}
			return fmt.Sprintf("display text accepted for %d ms", duration), nil
		},
	})
	mustRegister(shell.Command{
		Name: "macro", Usage: "macro list|show NAME_OR_ID|create ID NAME [CATEGORY [COLOR]]|delete NAME_OR_ID|record start NAME [CATEGORY [COLOR]]|record status|record save|record discard|play NAME_OR_ID|status|cancel [keep]",
		Summary: "manage and play MCU-timed multi-peripheral macros",
		Run: func(ctx context.Context, args []string) (string, error) {
			return macroCommand(ctx, macroRunner, args)
		},
	})
	mustRegister(shell.Command{
		Name: "os",
		Usage: "os status|policy|key KEY [HOLD_MS] | os virtual enable|disable|allow|deny [KEY] | " +
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
					"safe application reset acknowledged",
				); err != nil {
					return "", err
				}
				return "safe reset ACK received; application HELLO reauthenticated", nil
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
		Name:    "arduino",
		Usage:   "arduino update|compile SKETCH|core-info|burn-bootloader [PORT]",
		Summary: "update dependencies or use arduino-cli; flash via guarded program flash",
		Run: func(ctx context.Context, args []string) (string, error) {
			resolved := options
			if options.Resolve != nil {
				resolved = options.Resolve()
			}
			if len(args) == 1 && strings.EqualFold(args[0], "update") {
				var output bytes.Buffer
				report, updateErr := programmer.UpdateArduino(
					ctx,
					programmer.ArduinoUpdateOptions{
						ArduinoCLI:  resolved.ArduinoCLI,
						DirectRetry: true,
					},
					&output,
				)
				fmt.Fprintf(
					&output,
					"\nArduino update finished: %d steps.\n",
					len(report.Steps),
				)
				return strings.TrimSpace(output.String()), updateErr
			}
			programArgs, err := arduinoProgramArguments(args)
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
			resolved := options
			if options.Resolve != nil {
				resolved = options.Resolve()
			}
			return programCommand(ctx, runtime, resolved, programArgs)
		},
	})
	mustRegister(shell.Command{
		Name:    "program",
		Usage:   "program flash HEX [PORT] [--usbasp-troubleshooting] [--allow-incomplete-backup] | program OPERATION METHOD PATH [PORT]",
		Summary: "guarded backup-then-flash, or non-write programmer diagnostics",
		Run: func(ctx context.Context, args []string) (string, error) {
			resolved := options
			if options.Resolve != nil {
				resolved = options.Resolve()
			}
			return programCommand(ctx, runtime, resolved, args)
		},
	})
	return engine
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
		return "", errors.New("usage: os status|policy|key|virtual|power|power-policy|brightness|brightness-policy ...")
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
	return fmt.Sprintf(
		"monitor brightness %s=%d%% raw=%d/%d..%d display=%q",
		verb, result.Status.Percent, result.Status.RawCurrent,
		result.Status.RawMinimum, result.Status.RawMaximum, result.Status.Display,
	)
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
	for _, catalog := range [][]MenuPageInfo{protocolMenuPages, legacyMenuPages} {
		for _, page := range catalog {
			if normalizeMenuName(page.Label) == label {
				return page, true
			}
		}
	}
	return MenuPageInfo{}, false
}

func arduinoProgramArguments(args []string) ([]string, error) {
	const usage = "usage: arduino compile SKETCH | core-info | burn-bootloader [PORT]"
	if len(args) == 0 {
		return nil, fmt.Errorf("%s", usage)
	}
	switch strings.ToLower(args[0]) {
	case "compile":
		if len(args) != 2 {
			return nil, fmt.Errorf("%s", usage)
		}
		return []string{string(programmer.MethodCompile), args[1]}, nil
	case "upload":
		return nil, errors.New(
			"direct arduino upload is disabled; compile to Intel HEX, then use program flash HEX [PORT] so flash and EEPROM are backed up first",
		)
	case "core-info", "info":
		if len(args) != 1 {
			return nil, fmt.Errorf("%s", usage)
		}
		return []string{
			string(programmer.OperationCoreInfo),
			string(programmer.MethodArduino),
		}, nil
	case "burn", "burn-bootloader":
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
	payload, err := settings.Payload()
	if err != nil {
		return err
	}
	return command(ctx, runtime, native.OpSetSettings, payload)
}

func settingsFromSetArgs(
	current native.Settings,
	args []string,
) (native.Settings, error) {
	if (len(args) != 9 && len(args) != 11 && len(args) != 14) ||
		!strings.EqualFold(args[0], "set") {
		return native.Settings{}, fmt.Errorf("usage: %s", settingsSetUsage)
	}

	values := make([]uint64, 8)
	for index, value := range args[1:9] {
		bits := 8
		if index == 7 {
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

	// Begin with the device record, then replace only the fields present in
	// this command form. This makes both older positional forms safe after new
	// EEPROM flags are introduced.
	settings := current
	settings.Flags = byte(values[0])
	settings.LightMode = byte(values[1])
	settings.OnBrightness = byte(values[2])
	settings.OffBrightness = byte(values[3])
	settings.DisplayBrightness = byte(values[4])
	settings.StatusBrightness = byte(values[5])
	settings.PWMBootMode = byte(values[6])
	settings.StreamPeriodMS = uint16(values[7])

	if len(args) >= 11 {
		defaultPage, err := strconv.ParseUint(args[9], 0, 8)
		if err != nil {
			return native.Settings{}, fmt.Errorf(
				"invalid DEFAULT_PAGE value %q",
				args[9],
			)
		}
		saveLast, err := parseBool(args[10])
		if err != nil {
			return native.Settings{}, fmt.Errorf(
				"invalid SAVE_LAST value %q: %w",
				args[10],
				err,
			)
		}
		settings.DefaultPage = byte(defaultPage)
		settings.SetSaveLastPage(saveLast)
	}

	if len(args) == 14 {
		statusColor, err := parseBoundedByte(args[11], 7, "status color")
		if err != nil {
			return native.Settings{}, err
		}
		voltageDecimals, err := parseBoundedByte(
			args[12],
			2,
			"voltage decimals",
		)
		if err != nil {
			return native.Settings{}, err
		}
		currentDecimals, err := parseBoundedByte(
			args[13],
			2,
			"current decimals",
		)
		if err != nil {
			return native.Settings{}, err
		}
		if err := settings.SetStatusColor(statusColor); err != nil {
			return native.Settings{}, err
		}
		if err := settings.SetVoltageDecimals(voltageDecimals); err != nil {
			return native.Settings{}, err
		}
		if err := settings.SetCurrentDecimals(currentDecimals); err != nil {
			return native.Settings{}, err
		}
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
			"melody %q saved in PC host configuration with %d notes",
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
		return fmt.Sprintf("melody %q removed from PC host configuration", args[1]), nil
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
		if err != nil || value < 1 || value > maxMelodyRepeats {
			return "", fmt.Errorf(
				"melody repeats must be 1..%d",
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
	return fmt.Sprintf(
		"melody %q started (id=%d repeats=%d)",
		melody.Name,
		operation.ID,
		repeats,
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
		return statusEffectCommand(ctx, outputs, configProvider, args[1:])
	}
	if len(args) != 3 && len(args) != 4 {
		return "", fmt.Errorf(
			"usage: rgb R G B [BRIGHTNESS] | rgb effect list|play NAME|wait NAME|stop|status",
		)
	}
	values, err := uintArgs(args, 8)
	if err != nil {
		return "", err
	}
	brightness := uint64(255)
	if len(values) == 4 {
		brightness = values[3]
	}
	payload := native.StatusRGBPayload(
		byte(values[0]),
		byte(values[1]),
		byte(values[2]),
		byte(brightness),
	)
	outputs.OverrideStatusEffect()
	if err := command(ctx, runtime, native.OpStatusRGB, payload); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"status RGB=%d,%d,%d brightness=%d",
		values[0],
		values[1],
		values[2],
		brightness,
	), nil
}

func statusEffectCommand(
	ctx context.Context,
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
		return "all relays off", command(ctx, runtime, native.OpRelayAllOff, nil)
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
		return fmt.Sprintf("relay side %s %s", args[1], args[2]),
			command(ctx, runtime, native.OpRelaySide, payload)
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
		return fmt.Sprintf("relay R%d %s", number, onOff(active)),
			command(ctx, runtime, native.OpRelaySet, payload)
	}
	return "", fmt.Errorf("usage: relay N on|off|toggle | relay side left|right stop|up|down | relay off | relay test [MS]")
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
			return fmt.Sprintf("PWM mode=%d selected=%d values=%v", values.Mode, values.SelectedChannel, values.Values), nil
		case "off":
			return "all PWM channels off", command(ctx, runtime, native.OpPWMAllOff, nil)
		}
	}
	if len(args) == 2 && strings.EqualFold(args[0], "mode") {
		modes := map[string]byte{
			"off": native.PWMOff, "manual": native.PWMManual, "auto": native.PWMAuto,
			"0": native.PWMOff, "1": native.PWMManual, "2": native.PWMAuto,
		}
		mode, ok := modes[strings.ToLower(args[1])]
		if !ok {
			return "", fmt.Errorf("PWM mode must be off, manual, or auto")
		}
		return "PWM mode " + args[1], command(ctx, runtime, native.OpPWMMode, []byte{mode})
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
	return "", fmt.Errorf("usage: pwm get|off|mode MODE|set CHANNEL VALUE")
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
					"RF learning ended reason=%q captured=%d",
					state.Reason,
					state.Learned,
				), nil
			}
			duration := "indefinite"
			if !state.EndsAt.IsZero() {
				duration = time.Until(state.EndsAt).Round(time.Second).String()
			}
			return fmt.Sprintf(
				"RF learning active multi=%t remaining=%s captured=%d",
				state.Multiple,
				duration,
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
		if state.Indefinite {
			duration = "indefinite"
		}
		return fmt.Sprintf(
			"RF learning started multi=%t duration=%s; completion is reported as rf.learn.ended",
			state.Multiple,
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
		return description, command(ctx, runtime, native.OpRFMap, payload)
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
			"rf learn [SECONDS|DURATION|indefinite] [single|multi] | " +
			"rf status|cancel|list | rf remove ID|all | rf map ID ACTION ...",
	)
}

func parseRFLearnOptions(args []string) (RFLearnOptions, error) {
	options := RFLearnOptions{Timeout: 15 * time.Second}
	haveDuration := false
	for _, argument := range args {
		switch strings.ToLower(strings.TrimSpace(argument)) {
		case "multi", "multiple":
			options.Multiple = true
		case "single", "one":
			options.Multiple = false
		case "forever", "indefinite", "infinite":
			if haveDuration {
				return RFLearnOptions{}, fmt.Errorf("RF learn duration was specified more than once")
			}
			options.Indefinite = true
			options.Timeout = 0
			haveDuration = true
		default:
			if haveDuration {
				return RFLearnOptions{}, fmt.Errorf("RF learn duration was specified more than once")
			}
			duration, err := time.ParseDuration(argument)
			if err != nil {
				seconds, secondsErr := strconv.ParseUint(argument, 0, 32)
				if secondsErr != nil {
					return RFLearnOptions{}, fmt.Errorf("invalid RF learn duration %q", argument)
				}
				duration = time.Duration(seconds) * time.Second
			}
			if duration <= 0 || duration > 24*time.Hour {
				return RFLearnOptions{}, fmt.Errorf("RF learn duration must be positive and at most 24h")
			}
			options.Timeout = duration
			haveDuration = true
		}
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
	payload, err := native.RFMapPayload(id, kind, value, behavior)
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
		ArduinoCLI: options.ArduinoCLI,
		Avrdude:    options.Avrdude, AvrdudeConf: options.AvrdudeConf,
		Programmer: options.Programmer,
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
			// These arduino-cli operations do not take a sketch or memory file.
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
	programOptions.ApplicationDate = snapshot.Hello.BuildDate
	programOptions.ApplicationTime = snapshot.Hello.BuildTime
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
		runtime.ResumeAuto()
		reconnectContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			12*time.Second,
		)
		defer cancel()
		reconnectErr := runtime.EnsureConnected(reconnectContext)
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
	const usage = "usage: program flash HEX [PORT] [--usbasp-troubleshooting] [--allow-incomplete-backup]"
	if len(args) == 0 {
		return "", errors.New(usage)
	}
	firmwarePath := strings.TrimSpace(args[0])
	if firmwarePath == "" {
		return "", errors.New(usage)
	}
	method := programmer.MethodUrclock
	port := ""
	allowUSBasp := false
	allowIncomplete := false
	for _, argument := range args[1:] {
		switch strings.ToLower(strings.TrimSpace(argument)) {
		case "--usbasp-troubleshooting":
			method = programmer.MethodUSBasp
			allowUSBasp = true
		case "--allow-incomplete-backup":
			allowIncomplete = true
		default:
			if strings.HasPrefix(argument, "--") || port != "" {
				return "", fmt.Errorf("%s", usage)
			}
			port = argument
		}
	}
	if _, err := programmer.LoadIntelHex(firmwarePath); err != nil {
		return "", fmt.Errorf("inspect firmware before releasing UART: %w", err)
	}
	snapshot := runtime.Snapshot()
	if method == programmer.MethodUrclock {
		if port == "" {
			port = snapshot.Port.Name
		}
		if strings.TrimSpace(port) == "" {
			return "", errors.New("guarded Urclock flash requires a connected device or explicit port")
		}
	} else if port != "" {
		return "", errors.New("USBasp troubleshooting mode does not accept a serial port")
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
		ArduinoCLI: options.ArduinoCLI, Avrdude: options.Avrdude,
		AvrdudeConf:               options.AvrdudeConf,
		ApplicationHash:           snapshot.Hello.BuildHash,
		ApplicationDate:           snapshot.Hello.BuildDate,
		ApplicationTime:           snapshot.Hello.BuildTime,
		ApplicationIdentitySchema: snapshot.Hello.IdentitySchema,
	}
	writeOptions := backup
	writeOptions.Operation = programmer.OperationWriteFlash
	writeOptions.HexPath = firmwarePath
	serialWasOpen := snapshot.Connected
	if serialWasOpen {
		if err := runtime.Close(); err != nil {
			return "", fmt.Errorf("release application UART: %w", err)
		}
	}
	var output bytes.Buffer
	fmt.Fprintln(&output, "application UART released; guarded programmer transaction has exclusive ownership")
	fmt.Fprintf(&output, "pre-flash method=%s firmware=%s\n", method, firmwarePath)
	result, flashErr := programmer.AutomaticBackupThenFlash(
		ctx,
		programmer.AutomaticPreflashOptions{
			FirmwarePath: firmwarePath,
			Backup:       backup, DataPaths: dataPaths,
			AllowUSBaspTroubleshooting:  allowUSBasp,
			AllowFlashWithoutFullBackup: allowIncomplete,
		},
		runner,
		func(flashContext context.Context, path string, writer io.Writer) error {
			writeOptions.HexPath = path
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
	if serialWasOpen {
		runtime.ResumeAuto()
		reconnectContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 12*time.Second)
		reconnectErr := runtime.EnsureConnected(reconnectContext)
		cancel()
		if reconnectErr != nil {
			return strings.TrimSpace(output.String()), fmt.Errorf(
				"guarded flash result (%v); application HELLO reconnect failed: %w",
				flashErr, reconnectErr,
			)
		}
		connected := runtime.Snapshot()
		fmt.Fprintf(&output, "application mode restored and authenticated on %s: %s\n", connected.Port.Name, formatHello(connected.Hello))
	}
	if flashErr != nil {
		return strings.TrimSpace(output.String()), flashErr
	}
	return strings.TrimSpace(output.String()), nil
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
	return fmt.Sprintf(
		"uptime=%s supply=%.3fV bus=%.3fV current=%dmA power=%dmW tLED=%.2fC tBT=%.2fC\n"+
			"flags=0x%04X inputs=0x%02X keys=0x%02X relays=0x%02X menu=%d mode=%d door=%t bt=%d\n"+
			"PWM mode=%d channel=%d value=%d errors=%d LCD=0x%02X framing=%d crc=%d reset_cause=0x%02X reset_count=%d",
		(time.Duration(status.UptimeMS) * time.Millisecond).Round(time.Millisecond),
		float64(status.SupplyMV)/1000,
		float64(status.BusMV)/1000,
		status.CurrentMA,
		status.PowerMW,
		float64(status.TLEDCenti)/100,
		float64(status.TBTCenti)/100,
		status.Flags,
		status.RawInputs,
		status.ActiveKeys,
		status.ActiveRelays,
		status.MenuPage,
		status.ProgramMode,
		status.DoorOpen,
		status.BluetoothState,
		status.PWMMode,
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

func formatSettings(settings native.Settings) string {
	return fmt.Sprintf(
		"flags=0x%02X light=%d on=%d off=%d display=%d status=%d "+
			"PWMboot=%d stream=%dms default_page=%d save_last=%t "+
			"status_color=%d voltage_decimals=%d current_decimals=%d "+
			"motion_door=%s motion_break=%dms door_audio=%t relay_audio=%t extended=0x%02X",
		settings.Flags,
		settings.LightMode,
		settings.OnBrightness,
		settings.OffBrightness,
		settings.DisplayBrightness,
		settings.StatusBrightness,
		settings.PWMBootMode,
		settings.StreamPeriodMS,
		settings.DefaultPage,
		settings.SaveLastPage(),
		settings.StatusColor(),
		settings.VoltageDecimals(),
		settings.CurrentDecimals(),
		motionDoorPolicyName(settings.MotionDoorPolicy()),
		settings.MotionBreakMS(),
		settings.DoorAudioEnabled(),
		settings.RelayAudioEnabled(),
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
	if hello.IdentitySchema == native.IdentitySchema {
		stamp := hello.BuildStamp
		if stamp == "" {
			stamp = "unknown"
		}
		return fmt.Sprintf(
			"%s build=%08X timestamp=%s packed=0x%08X",
			base,
			hello.BuildHash,
			stamp,
			hello.BuildTimestamp,
		)
	}
	if hello.IdentitySchema == native.IdentitySchemaLegacy {
		return fmt.Sprintf(
			"%s build=%08X date=%s time=%s",
			base,
			hello.BuildHash,
			strings.TrimSpace(hello.BuildDate),
			strings.TrimSpace(hello.BuildTime),
		)
	}
	return fmt.Sprintf(
		"%s legacy-firmware=%d.%d.%d",
		base,
		hello.FirmwareMajor,
		hello.FirmwareMinor,
		hello.FirmwarePatch,
	)
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
	const usage = "macro list|show NAME_OR_ID|create ID NAME [CATEGORY [COLOR]]|delete NAME_OR_ID|record start NAME [CATEGORY [COLOR]]|record status|record save|record discard|play NAME_OR_ID|status|cancel [keep]"
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
			"macro id=%d name=%q category=%q color=%q label=%q steps=%d duration=%s encoded=%dB tolerance=%dus keep_on_cancel=%t",
			macro.ID, macro.Name, macro.Category, normalizedMacroColor(macro.Color),
			macro.Label, len(macro.Steps), time.Duration(compiled.durationUS)*time.Microsecond,
			len(compiled.stream), macro.TimingToleranceUS, macro.KeepOutputsOnCancel,
		)}
		for index, step := range macro.Steps {
			due, _ := macroStepDueUS(step)
			lines = append(lines, fmt.Sprintf(
				"%3d  +%-12s %-12s target=%d value=%d",
				index+1, time.Duration(due)*time.Microsecond, step.Kind, step.Target, step.Value,
			))
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
	case "delete", "remove":
		if len(args) != 2 {
			return "", fmt.Errorf("usage: macro delete NAME_OR_ID")
		}
		if err := runner.Delete(args[1]); err != nil {
			return "", err
		}
		return "macro deleted from PC configuration", nil
	case "record":
		if len(args) < 2 {
			return "", fmt.Errorf("usage: macro record start NAME [CATEGORY [COLOR]]|status|save|discard")
		}
		switch strings.ToLower(args[1]) {
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
			return fmt.Sprintf("recording macro %d/%s; acknowledged board commands will use exact MCU deltas", state.ID, state.Name), nil
		case "status":
			if len(args) != 2 {
				return "", fmt.Errorf("usage: macro record status")
			}
			state := runner.RecordingState()
			if !state.Active && state.Name == "" {
				return "no macro has been recorded in this session", nil
			}
			return fmt.Sprintf("macro recording active=%t id=%d name=%q category=%q color=%q steps=%d started=%s error=%q", state.Active, state.ID, state.Name, state.Category, state.Color, state.Steps, state.StartedAt.Format(time.RFC3339), state.LastError), nil
		case "save", "stop":
			if len(args) != 2 {
				return "", fmt.Errorf("usage: macro record save")
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
			return "", fmt.Errorf("usage: macro record start NAME [CATEGORY [COLOR]]|status|save|discard")
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
			"macro id=%d name=%q lifecycle=%s running=%t step=%d/%d buffer=%dB timing=%dus max=%dus violations=%d underruns=%d dispatch_errors=%d faithful=%t started=%s error=%q",
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
			state.Faithful,
			state.StartedAt.Format(time.RFC3339),
			state.LastError,
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
