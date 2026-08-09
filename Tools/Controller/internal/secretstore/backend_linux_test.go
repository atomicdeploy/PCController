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
		run: func([]byte, string, ...string) ([]byte, error) { return nil, testExitError(1) },
	}
	if _, err := backend.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error=%v", err)
	}
	if err := backend.Delete("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete missing error=%v", err)
	}
}
