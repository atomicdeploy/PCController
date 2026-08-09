//go:build linux

package hostos

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestParseLinuxUptimeMS(t *testing.T) {
	got, err := parseLinuxUptimeMS([]byte("123.456 789.0\n"))
	if err != nil || got != 123456 {
		t.Fatalf("parseLinuxUptimeMS=%d err=%v", got, err)
	}
	if _, err := parseLinuxUptimeMS([]byte("invalid")); err == nil {
		t.Fatal("invalid uptime was accepted")
	}
}

func TestLinuxPowerActionsUseBoundedSystemInterfaces(t *testing.T) {
	original := linuxHostCommand
	t.Cleanup(func() { linuxHostCommand = original })
	t.Setenv("XDG_SESSION_ID", "session-7")
	type invocation struct {
		name      string
		arguments []string
	}
	var calls []invocation
	linuxHostCommand = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		calls = append(calls, invocation{name, append([]string(nil), arguments...)})
		return nil, nil
	}
	for _, action := range []string{"lock", "sleep", "hibernate", "shutdown", "restart", "logoff"} {
		if err := platformPowerAction(context.Background(), action); err != nil {
			t.Fatalf("platformPowerAction(%s): %v", action, err)
		}
	}
	want := []invocation{
		{"loginctl", []string{"lock-session", "session-7"}},
		{"systemctl", []string{"suspend"}},
		{"systemctl", []string{"hibernate"}},
		{"systemctl", []string{"--no-wall", "poweroff"}},
		{"systemctl", []string{"--no-wall", "reboot"}},
		{"loginctl", []string{"terminate-session", "session-7"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("power calls=%#v want=%#v", calls, want)
	}
}

func TestLinuxLockResolvesActivePhysicalSessionWithoutEnvironmentID(t *testing.T) {
	original := linuxHostCommand
	t.Cleanup(func() { linuxHostCommand = original })
	t.Setenv("XDG_SESSION_ID", "")
	var lockedSession string
	linuxHostCommand = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		if name != "loginctl" {
			return nil, errors.New("unexpected command")
		}
		switch arguments[0] {
		case "list-sessions":
			return []byte("4 0 root -\n5 1000 asus seat0\n"), nil
		case "show-session":
			if arguments[1] == "4" {
				return []byte("Active=yes\nRemote=yes\nType=tty\nState=active\nSeat=\nClass=user\nUser=0\nName=root\n"), nil
			}
			return []byte("Active=yes\nRemote=no\nType=wayland\nState=active\nSeat=seat0\nClass=user\nUser=1000\nName=asus\n"), nil
		case "lock-session":
			lockedSession = arguments[1]
			return nil, nil
		default:
			return nil, errors.New("unexpected loginctl action")
		}
	}

	if err := platformPowerAction(context.Background(), "lock"); err != nil {
		t.Fatal(err)
	}
	if lockedSession != "5" {
		t.Fatalf("locked session %q", lockedSession)
	}
}

func TestLinuxBrightnessPrefersPanelAndFallsBackToDDC(t *testing.T) {
	original := linuxHostCommand
	t.Cleanup(func() { linuxHostCommand = original })
	linuxHostCommand = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		if name == "brightnessctl" {
			return nil, errors.New("not installed")
		}
		if len(arguments) > 0 && arguments[0] == "getvcp" {
			return []byte("VCP 10 C 45 90\n"), nil
		}
		return nil, nil
	}
	status, err := platformMonitorBrightness(context.Background())
	if err != nil || status.Backend != "ddcutil" || status.Percent != 50 || status.RawMaximum != 90 {
		t.Fatalf("DDC brightness=%+v err=%v", status, err)
	}

	linuxHostCommand = func(_ context.Context, name string, arguments ...string) ([]byte, error) {
		if name == "brightnessctl" {
			return []byte("intel_backlight,backlight,40,40%,100\n"), nil
		}
		return nil, errors.New("unexpected ddcutil call")
	}
	status, err = platformMonitorBrightness(context.Background())
	if err != nil || status.Backend != "brightnessctl" || !status.Integrated || status.Percent != 40 {
		t.Fatalf("panel brightness=%+v err=%v", status, err)
	}
}
