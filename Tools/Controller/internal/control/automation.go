package control

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

const (
	defaultAutomationCooldown = 250 * time.Millisecond
	automationActionTimeout   = 30 * time.Second
	maxAutomationOutput       = 8 * 1024
)

// RunAutomations observes the retained event ring, so it never steals events
// from the TUI, monitor, IPC, or API channels. The provider is evaluated for
// every event, which makes a valid watched config replacement effective
// immediately without restarting the host.
func RunAutomations(
	ctx context.Context,
	runtime *Runtime,
	engine *shell.Engine,
	provider func() appconfig.Config,
) {
	if provider == nil {
		return
	}
	afterID := runtime.LatestEventID()
	lastTriggered := make(map[string]time.Time)
	for {
		event, err := runtime.WaitEvent(ctx, afterID, "")
		if err != nil {
			return
		}
		afterID = event.ID
		if strings.HasPrefix(strings.ToLower(event.Kind), "automation") {
			continue
		}
		config := provider()
		for _, automation := range config.Automations {
			if !automation.Enabled || !automationMatches(automation.Match, event) {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(automation.Name))
			cooldown := time.Duration(automation.CooldownMS) * time.Millisecond
			if cooldown == 0 {
				cooldown = defaultAutomationCooldown
			}
			now := time.Now()
			if previous := lastTriggered[name]; !previous.IsZero() &&
				now.Sub(previous) < cooldown {
				continue
			}
			lastTriggered[name] = now
			actionContext, cancel := context.WithTimeout(ctx, automationActionTimeout)
			err := executeAutomation(
				actionContext,
				runtime,
				engine,
				config,
				automation,
			)
			cancel()
			if err != nil {
				runtime.PublishHostEvent(
					"automation",
					fmt.Sprintf("%s failed: %v", automation.Name, err),
				)
			} else {
				runtime.PublishHostEvent(
					"automation",
					fmt.Sprintf("%s completed for event %d", automation.Name, event.ID),
				)
			}
		}
	}
}

func ExecuteAutomationByName(
	ctx context.Context,
	runtime *Runtime,
	engine *shell.Engine,
	config appconfig.Config,
	name string,
) error {
	for _, automation := range config.Automations {
		if strings.EqualFold(strings.TrimSpace(automation.Name), strings.TrimSpace(name)) {
			return executeAutomation(ctx, runtime, engine, config, automation)
		}
	}
	return fmt.Errorf("automation %q is not configured", name)
}

func AutomationList(config appconfig.Config) string {
	if len(config.Automations) == 0 {
		return "no host event automations configured"
	}
	automations := append([]appconfig.Automation(nil), config.Automations...)
	sort.SliceStable(automations, func(left, right int) bool {
		return strings.ToLower(automations[left].Name) <
			strings.ToLower(automations[right].Name)
	})
	lines := make([]string, 0, len(automations))
	for _, automation := range automations {
		state := "disabled"
		if automation.Enabled {
			state = "enabled"
		}
		lines = append(lines, fmt.Sprintf(
			"%s %s match=%s/%s actions=%d",
			automation.Name,
			state,
			automation.Match.Kind,
			automation.Match.Gesture,
			len(automation.Actions),
		))
	}
	return strings.Join(lines, "\n")
}

func automationMatches(match appconfig.AutomationMatch, event Event) bool {
	kind := strings.ToLower(strings.TrimSpace(match.Kind))
	if kind != "*" && !eventKindMatches(kind, event.Kind) {
		return false
	}
	if match.Lifecycle != "" &&
		!strings.EqualFold(match.Lifecycle, event.Lifecycle) {
		return false
	}
	if match.State != "" && !strings.EqualFold(match.State, event.State) {
		return false
	}
	if match.Contains != "" &&
		!strings.Contains(
			strings.ToLower(event.Text),
			strings.ToLower(match.Contains),
		) {
		return false
	}
	var device native.DeviceEvent
	haveDevice := false
	if event.Frame.Opcode == native.OpEvent {
		parsed, err := native.ParseDeviceEvent(event.Frame.Payload)
		if err == nil {
			device = parsed
			haveDevice = true
		}
	}
	if match.Key != 0 &&
		(!haveDevice || device.Type != native.EventKey ||
			device.Key+1 != match.Key) {
		return false
	}
	if match.Gesture != "" {
		actual := event.Gesture
		if actual == "" && haveDevice && device.Type == native.EventKey {
			actual = NormalizeGesture(device.Gesture)
		}
		if !strings.EqualFold(actual, normalizeGestureName(match.Gesture)) {
			return false
		}
	}
	if match.Source != "" {
		actual := event.Source
		if actual == "" && haveDevice && device.Type == native.EventKey {
			actual = inputSourceName(device.Source)
		}
		if !strings.EqualFold(match.Source, actual) {
			return false
		}
	}
	if match.RFID != nil {
		matched := (event.HaveRFID && event.RFID == *match.RFID) ||
			(haveDevice && device.Type == native.EventRFLearned &&
				device.RFID == *match.RFID) ||
			(haveDevice && device.Type == native.EventRFReceived &&
				device.RFLearnedID == *match.RFID) ||
			(haveDevice && device.Type == native.EventKey &&
				device.Source == native.InputSourceRF &&
				device.SourceID == *match.RFID)
		if !matched {
			return false
		}
	}
	if match.RFCode != nil {
		actual := event.RFCode
		if actual == 0 && haveDevice && device.Type == native.EventRFReceived {
			actual = device.RFCode
		}
		if actual != *match.RFCode {
			return false
		}
	}
	if match.RFProtocol != 0 {
		actual := event.RFProtocol
		if actual == 0 && haveDevice && device.Type == native.EventRFReceived {
			actual = device.RFProtocol
		}
		if actual != match.RFProtocol {
			return false
		}
	}
	return true
}

func inputSourceName(source byte) string {
	return map[byte]string{
		native.InputSourcePhysical: "physical",
		native.InputSourceRF:       "rf",
		native.InputSourceHost:     "host",
	}[source]
}

func NormalizeGesture(value byte) string {
	return map[byte]string{
		0: "click",
		1: "double",
		2: "hold",
		3: "repeat",
		4: "release",
		5: "down",
		6: "up",
	}[value]
}

func normalizeGestureName(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "double-click":
		return "double"
	case "hold-start":
		return "hold"
	case "hold-repeat":
		return "repeat"
	case "hold-release":
		return "release"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func executeAutomation(
	ctx context.Context,
	runtime *Runtime,
	engine *shell.Engine,
	config appconfig.Config,
	automation appconfig.Automation,
) error {
	for index, action := range automation.Actions {
		switch strings.ToLower(strings.TrimSpace(action.Type)) {
		case "board":
			output, err := engine.Execute(ctx, action.Command)
			if err != nil {
				return fmt.Errorf("action %d board command: %w", index, err)
			}
			if output != "" {
				runtime.PublishHostEvent(
					"automation",
					fmt.Sprintf("%s board: %s", automation.Name, output),
				)
			}
		case "macro":
			// Automations use the exact same named macro command as TUI, CLI,
			// JSON-RPC, and WebSocket clients. This keeps queue timing, motion
			// guards, cancellation, and lifecycle events authoritative in one
			// playback engine.
			output, err := engine.Execute(
				ctx,
				shell.Join([]string{"macro", "play", action.Macro}),
			)
			if err != nil {
				return fmt.Errorf("action %d macro %q: %w", index, action.Macro, err)
			}
			if output != "" {
				runtime.PublishHostEvent(
					"automation",
					fmt.Sprintf("%s macro: %s", automation.Name, output),
				)
			}
		case "rf":
			if action.RF == nil {
				return fmt.Errorf("action %d RF payload is missing", index)
			}
			payload, err := native.RFTxPayload(
				action.RF.Code,
				action.RF.Bits,
				action.RF.Protocol,
				action.RF.PulseUS,
			)
			if err != nil {
				return fmt.Errorf("action %d RF payload: %w", index, err)
			}
			repeats := action.RF.Repeats
			if repeats == 0 {
				repeats = 1
			}
			if err := sendRF(ctx, runtime, payload, repeats); err != nil {
				return fmt.Errorf("action %d RF transmit: %w", index, err)
			}
		case "host", "script":
			executable, arguments, err := automationHostCommand(config, action)
			if err != nil {
				return fmt.Errorf("action %d: %w", index, err)
			}
			process := exec.CommandContext(ctx, executable, arguments...)
			var output boundedBuffer
			process.Stdout = &output
			process.Stderr = &output
			if err := process.Run(); err != nil {
				return fmt.Errorf(
					"action %d host command failed: %w (%s)",
					index,
					err,
					strings.TrimSpace(output.String()),
				)
			}
			if text := strings.TrimSpace(output.String()); text != "" {
				runtime.PublishHostEvent(
					"automation",
					fmt.Sprintf("%s host: %s", automation.Name, text),
				)
			}
		case "emit":
			runtime.PublishHostEvent(
				"automation."+strings.ToLower(strings.TrimSpace(action.Event)),
				fmt.Sprintf("automation %s emitted %s", automation.Name, action.Event),
			)
		case "virtual-key", "virtual_key", "vk":
			result, err := hostos.DefaultExecutor.PressVirtualKey(
				ctx,
				config.OSActions.VirtualKeys,
				hostos.VirtualKeyRequest{Key: action.VirtualKey, HoldMS: action.HoldMS},
			)
			if err != nil {
				runtime.PublishHostEvent(
					"os.virtual-key.audit",
					fmt.Sprintf("automation %s denied: %v", automation.Name, err),
				)
				return fmt.Errorf("action %d virtual key: %w", index, err)
			}
			runtime.PublishHostEvent(
				"os.virtual-key.audit",
				fmt.Sprintf("automation %s: %s", automation.Name, result.Detail),
			)
		case "power":
			result, err := hostos.DefaultExecutor.Power(
				ctx,
				config.OSActions.Power,
				hostos.PowerRequest{
					Action: action.Power, Confirmation: action.Confirm, Automation: true,
				},
			)
			if err != nil {
				runtime.PublishHostEvent(
					"os.power.audit",
					fmt.Sprintf("automation %s denied: %v", automation.Name, err),
				)
				return fmt.Errorf("action %d power: %w", index, err)
			}
			runtime.PublishHostEvent(
				"os.power.audit",
				fmt.Sprintf("automation %s: %s", automation.Name, result.Detail),
			)
		default:
			return fmt.Errorf("action %d type %q is unknown", index, action.Type)
		}
	}
	return nil
}

func automationHostCommand(
	config appconfig.Config,
	action appconfig.AutomationAction,
) (string, []string, error) {
	executable := strings.TrimSpace(action.Executable)
	arguments := append([]string(nil), action.Args...)
	if strings.EqualFold(action.Type, "script") {
		path, ok := config.Scripts[action.Script]
		if !ok || strings.TrimSpace(path) == "" {
			return "", nil, fmt.Errorf("script %q is not configured", action.Script)
		}
		executable = path
	}
	if executable == "" {
		return "", nil, fmt.Errorf("host executable is empty")
	}
	if strings.EqualFold(action.Type, "script") {
		switch strings.ToLower(filepath.Ext(executable)) {
		case ".ps1":
			powerShell, err := findHostExecutable("pwsh", "powershell")
			if err != nil {
				return "", nil, err
			}
			return powerShell,
				append([]string{"-NoProfile", "-NonInteractive", "-File", executable}, arguments...),
				nil
		case ".cmd", ".bat":
			if runtime.GOOS != "windows" {
				return "", nil, fmt.Errorf("%s scripts require Windows", filepath.Ext(executable))
			}
			return "cmd.exe", append([]string{"/d", "/c", executable}, arguments...), nil
		case ".sh":
			shellPath, err := findHostExecutable("bash", "sh")
			if err != nil {
				return "", nil, err
			}
			return shellPath, append([]string{executable}, arguments...), nil
		}
	}
	return executable, arguments, nil
}

func findHostExecutable(names ...string) (string, error) {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("none of %s is available on PATH", strings.Join(names, ", "))
}

type boundedBuffer struct {
	buffer bytes.Buffer
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	original := len(value)
	remaining := maxAutomationOutput - buffer.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = buffer.buffer.Write(value)
	}
	return original, nil
}

func (buffer *boundedBuffer) String() string {
	return buffer.buffer.String()
}
