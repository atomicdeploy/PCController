//go:build linux

package hostos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

var linuxHostCommand = func(ctx context.Context, name string, arguments ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, arguments...).CombinedOutput()
}

var linuxHostReadFile = os.ReadFile

func platformKeyDown(uint16) error {
	return errors.New("virtual-key emission is unavailable on Linux: Wayland deliberately prevents global synthetic input; use authenticated WebUI controls")
}

func platformKeyUp(uint16) error {
	return errors.New("virtual-key emission is unavailable on Linux: no key was injected")
}

func platformPowerAction(ctx context.Context, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := "systemctl"
	var arguments []string
	switch action {
	case "lock":
		name = "loginctl"
		arguments = []string{"lock-session"}
		if session := strings.TrimSpace(os.Getenv("XDG_SESSION_ID")); session != "" {
			arguments = append(arguments, session)
		}
	case "sleep":
		arguments = []string{"suspend"}
	case "hibernate":
		arguments = []string{"hibernate"}
	case "shutdown":
		arguments = []string{"--no-wall", "poweroff"}
	case "restart":
		arguments = []string{"--no-wall", "reboot"}
	case "logoff":
		session := strings.TrimSpace(os.Getenv("XDG_SESSION_ID"))
		if session == "" {
			return errors.New("Linux logoff requires XDG_SESSION_ID so another user's session cannot be terminated")
		}
		name, arguments = "loginctl", []string{"terminate-session", session}
	default:
		return fmt.Errorf("unsupported Linux power action %q", action)
	}
	output, err := linuxHostCommand(ctx, name, arguments...)
	if err != nil {
		return fmt.Errorf("%s %s: %w%s", name, action, err, commandOutputDetail(output))
	}
	return nil
}

func platformMonitorBrightness(ctx context.Context) (BrightnessStatus, error) {
	status, panelErr := linuxPanelBrightness(ctx)
	if panelErr == nil {
		return status, nil
	}
	status, ddcErr := linuxDDCBrightness(ctx)
	if ddcErr == nil {
		return status, nil
	}
	return BrightnessStatus{}, fmt.Errorf(
		"Linux monitor brightness is unavailable through brightnessctl and ddcutil: %w",
		errors.Join(panelErr, ddcErr),
	)
}

func linuxPanelBrightness(ctx context.Context) (BrightnessStatus, error) {
	output, err := linuxHostCommand(ctx, "brightnessctl", "--machine-readable", "info")
	if err != nil {
		return BrightnessStatus{}, fmt.Errorf("brightnessctl info: %w%s", err, commandOutputDetail(output))
	}
	return parseBrightnessctl(output)
}

func parseBrightnessctl(output []byte) (BrightnessStatus, error) {
	line := firstNonemptyLine(string(output))
	fields := strings.Split(line, ",")
	if len(fields) < 5 {
		return BrightnessStatus{}, fmt.Errorf("brightnessctl returned an unexpected machine-readable record %q", boundedText(line, 160))
	}
	current, currentErr := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 32)
	maximum, maximumErr := strconv.ParseUint(strings.TrimSpace(fields[4]), 10, 32)
	percent, percentErr := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(fields[3]), "%"))
	if currentErr != nil || maximumErr != nil || percentErr != nil || maximum == 0 || current > maximum || percent < 0 || percent > 100 {
		return BrightnessStatus{}, errors.New("brightnessctl returned invalid current, maximum, or percentage values")
	}
	return BrightnessStatus{
		Percent: percent, RawMinimum: 0, RawCurrent: uint32(current), RawMaximum: uint32(maximum),
		Display: strings.TrimSpace(fields[0]), Backend: "brightnessctl", Integrated: true,
	}, nil
}

func linuxDDCBrightness(ctx context.Context) (BrightnessStatus, error) {
	output, err := linuxHostCommand(ctx, "ddcutil", "getvcp", "10", "--brief")
	if err != nil {
		return BrightnessStatus{}, fmt.Errorf("ddcutil getvcp 10: %w%s", err, commandOutputDetail(output))
	}
	return parseDDCBrightness(output)
}

func parseDDCBrightness(output []byte) (BrightnessStatus, error) {
	fields := strings.Fields(firstNonemptyLine(string(output)))
	if len(fields) < 5 || !strings.EqualFold(fields[0], "VCP") || fields[1] != "10" || !strings.EqualFold(fields[2], "C") {
		return BrightnessStatus{}, fmt.Errorf("ddcutil returned an unexpected VCP record %q", boundedText(string(output), 160))
	}
	current, currentErr := strconv.ParseUint(fields[3], 10, 32)
	maximum, maximumErr := strconv.ParseUint(fields[4], 10, 32)
	if currentErr != nil || maximumErr != nil || maximum == 0 || current > maximum {
		return BrightnessStatus{}, errors.New("ddcutil returned invalid brightness values")
	}
	percent := int((current*100 + maximum/2) / maximum)
	return BrightnessStatus{
		Percent: percent, RawMinimum: 0, RawCurrent: uint32(current), RawMaximum: uint32(maximum),
		Display: "primary DDC/CI display", Backend: "ddcutil",
	}, nil
}

func platformSetMonitorBrightness(ctx context.Context, percent int) (BrightnessStatus, error) {
	if percent < 0 || percent > 100 {
		return BrightnessStatus{}, fmt.Errorf("monitor brightness %d is outside 0..100", percent)
	}
	if _, err := linuxPanelBrightness(ctx); err == nil {
		output, setErr := linuxHostCommand(ctx, "brightnessctl", "set", fmt.Sprintf("%d%%", percent))
		if setErr != nil {
			return BrightnessStatus{}, fmt.Errorf("brightnessctl set: %w%s", setErr, commandOutputDetail(output))
		}
		return linuxPanelBrightness(ctx)
	}
	status, err := linuxDDCBrightness(ctx)
	if err != nil {
		return BrightnessStatus{}, err
	}
	raw := uint32((uint64(status.RawMaximum)*uint64(percent) + 50) / 100)
	output, err := linuxHostCommand(ctx, "ddcutil", "setvcp", "10", strconv.FormatUint(uint64(raw), 10))
	if err != nil {
		return BrightnessStatus{}, fmt.Errorf("ddcutil setvcp 10: %w%s", err, commandOutputDetail(output))
	}
	return linuxDDCBrightness(ctx)
}

func platformUptimeMS() uint64 {
	content, err := linuxHostReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	value, err := parseLinuxUptimeMS(content)
	if err != nil {
		return 0
	}
	return value
}

func parseLinuxUptimeMS(content []byte) (uint64, error) {
	fields := strings.Fields(string(content))
	if len(fields) == 0 {
		return 0, errors.New("/proc/uptime is empty")
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || seconds < 0 || seconds > float64(^uint64(0))/1000 {
		return 0, errors.New("/proc/uptime contains an invalid duration")
	}
	return uint64(seconds * 1000), nil
}

func firstNonemptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

func commandOutputDetail(output []byte) string {
	if value := boundedText(string(output), 512); value != "" {
		return ": " + value
	}
	return ""
}

func boundedText(value string, maximum int) string {
	value = strings.Join(strings.Fields(strings.ToValidUTF8(value, "")), " ")
	if len(value) > maximum {
		value = value[:maximum] + "..."
	}
	return value
}
