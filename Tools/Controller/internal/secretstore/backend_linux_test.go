//go:build linux

package secretstore

import (
	"errors"
	"reflect"
	"testing"
)

type testExitError int

func (value testExitError) Error() string { return "command failed" }
func (value testExitError) ExitCode() int { return int(value) }

func TestLinuxSecretBackendUsesStableLibsecretAttributes(t *testing.T) {
	type call struct {
		input     string
		arguments []string
	}
	var calls []call
	backend := &linuxSecretBackend{
		namespace: "DRSDavidSoft.PCController", toolPath: "/usr/bin/secret-tool",
		probe: func() bool { return true },
		run: func(input []byte, _ string, arguments ...string) ([]byte, error) {
			calls = append(calls, call{string(input), append([]string(nil), arguments...)})
			switch arguments[0] {
			case "store", "clear":
				return nil, nil
			case "lookup":
				return []byte("value-from-vault\n"), nil
			default:
				return nil, errors.New("unexpected command")
			}
		},
	}
	if err := backend.Set("ipc.remote", "secret-value"); err != nil {
		t.Fatal(err)
	}
	value, err := backend.Get("ipc.remote")
	if err != nil || value != "value-from-vault" {
		t.Fatalf("Get=%q err=%v", value, err)
	}
	if err := backend.Delete("ipc.remote"); err != nil {
		t.Fatal(err)
	}
	if calls[0].input != "secret-value\n" || !reflect.DeepEqual(calls[1].arguments, []string{
		"lookup", "application", "DRSDavidSoft.PCController", "key", "ipc.remote",
	}) {
		t.Fatalf("libsecret calls=%#v", calls)
	}
	status := backend.Status()
	if !status.Available || !status.Durable || status.Provider != "libsecret" {
		t.Fatalf("status=%+v", status)
	}
}

func TestLinuxSecretBackendMapsMissingValue(t *testing.T) {
	backend := &linuxSecretBackend{
		namespace: "test", toolPath: "secret-tool",
		probe: func() bool { return true },
		run:   func([]byte, string, ...string) ([]byte, error) { return nil, testExitError(1) },
	}
	if _, err := backend.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error=%v", err)
	}
	if err := backend.Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing error=%v", err)
	}
}

func TestLinuxSecretBackendRequiresUsableBusSecretService(t *testing.T) {
	originalLookPath, originalProbeRun := linuxSecretLookPath, linuxSecretProbeRun
	t.Cleanup(func() { linuxSecretLookPath, linuxSecretProbeRun = originalLookPath, originalProbeRun })
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	linuxSecretLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	probeOutput := []byte("NAME PID PROCESS USER CONNECTION UNIT SESSION DESCRIPTION\norg.freedesktop.DBus 1 dbus user :1.0 user@1000.service - -\n")
	linuxSecretProbeRun = func(environment []string, name string, arguments ...string) ([]byte, error) {
		if name != "/usr/bin/busctl" || len(environment) != 2 || len(arguments) != 3 {
			t.Fatalf("probe environment=%q name=%q arguments=%q", environment, name, arguments)
		}
		return probeOutput, nil
	}

	backend := newPlatformBackend("test")
	if status := backend.Status(); status.Available || status.Durable || status.Provider != "libsecret" {
		t.Fatalf("status without Secret Service=%+v", status)
	}
	if _, err := backend.Get("missing"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Get without Secret Service=%v", err)
	}

	probeOutput = append(probeOutput, []byte("org.freedesktop.secrets 42 gnome-keyring user :1.1 user@1000.service - -\n")...)
	if status := backend.Status(); !status.Available || !status.Durable || status.Provider != "libsecret" {
		t.Fatalf("status with Secret Service=%+v", status)
	}
}
