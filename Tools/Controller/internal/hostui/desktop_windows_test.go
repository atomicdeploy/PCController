//go:build windows

package hostui

import (
	"encoding/base64"
	"errors"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

type recordingRegistry map[string]string

func (registry recordingRegistry) Set(path, name, value string) error {
	registry[path+"|"+name] = value
	return nil
}

func TestDesktopRegistryUsesCurrentExecutableAndQuotedURIArgument(t *testing.T) {
	record := recordingRegistry{}
	executable := `C:\Program Files\PCController\controller.exe`
	if err := ensureProtocolRegistry(record, executable, "Test.PCController", "Test Controller"); err != nil {
		t.Fatal(err)
	}
	command := record[`Software\Classes\pccontroller\shell\open\command|`]
	if !strings.Contains(command, `"C:\Program Files\PCController\controller.exe"`) ||
		!strings.Contains(command, `uri "%1"`) {
		t.Fatalf("protocol command=%q", command)
	}
	if strings.Contains(command, "David") {
		t.Fatalf("protocol command unexpectedly hardcoded a user path: %q", command)
	}
}

type cleanupRegistry struct {
	values  map[string]string
	deleted []string
}

func (registry *cleanupRegistry) String(path, name string) (string, error) {
	value, ok := registry.values[path+"|"+name]
	if !ok {
		return "", os.ErrNotExist
	}
	return value, nil
}

func (registry *cleanupRegistry) DeleteTree(path string) error {
	registry.deleted = append(registry.deleted, path)
	return nil
}

func TestRemoveOwnedRegistryIntegrationDeletesOnlyExactExecutable(t *testing.T) {
	executable := `C:\Program Files\PCController\controller.exe`
	appID := "Test.PCController"
	registry := &cleanupRegistry{values: map[string]string{
		`Software\Classes\pccontroller\shell\open\command|`:         protocolCommand(executable),
		`Software\Classes\AppUserModelId\Test.PCController|IconUri`: executable,
	}}
	protocol, identity, skipped, err := removeOwnedRegistryIntegration(registry, executable, appID)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol || !identity || len(skipped) != 0 {
		t.Fatalf("removed=(protocol=%t identity=%t), skipped=%v", protocol, identity, skipped)
	}
	want := []string{`Software\Classes\pccontroller`, `Software\Classes\AppUserModelId\Test.PCController`}
	if len(registry.deleted) != len(want) {
		t.Fatalf("deleted=%v; want %v", registry.deleted, want)
	}
	for index := range want {
		if registry.deleted[index] != want[index] {
			t.Fatalf("deleted=%v; want %v", registry.deleted, want)
		}
	}
}

func TestRemoveOwnedRegistryIntegrationPreservesForeignRegistrations(t *testing.T) {
	executable := `C:\Program Files\PCController\controller.exe`
	registry := &cleanupRegistry{values: map[string]string{
		`Software\Classes\pccontroller\shell\open\command|`:         `"C:\Other\controller.exe" uri "%1"`,
		`Software\Classes\AppUserModelId\Test.PCController|IconUri`: `C:\Other\controller.exe`,
	}}
	protocol, identity, skipped, err := removeOwnedRegistryIntegration(registry, executable, "Test.PCController")
	if err != nil {
		t.Fatal(err)
	}
	if protocol || identity || len(registry.deleted) != 0 {
		t.Fatalf("foreign registration was removed: protocol=%t identity=%t deleted=%v", protocol, identity, registry.deleted)
	}
	want := []string{"protocol-registration-not-owned", "app-identity-registration-not-owned"}
	if strings.Join(skipped, "|") != strings.Join(want, "|") {
		t.Fatalf("skipped=%v; want %v", skipped, want)
	}
}

func TestRemoveOwnedRegistryIntegrationIsIdempotentWhenAbsent(t *testing.T) {
	registry := &cleanupRegistry{values: map[string]string{}}
	protocol, identity, skipped, err := removeOwnedRegistryIntegration(
		registry, `C:\Program Files\PCController\controller.exe`, "Test.PCController",
	)
	if err != nil || protocol || identity || len(skipped) != 0 || len(registry.deleted) != 0 {
		t.Fatalf("absent cleanup=(%t,%t,%v,%v,%v)", protocol, identity, skipped, registry.deleted, err)
	}
}

func TestShortcutRemovalScriptChecksTargetArgumentsAndReparsePoints(t *testing.T) {
	encoded := encodedShortcutRemovalScript(
		`C:\Program Files\PCController\controller.exe`,
		`X:\Fixture\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\PCController.lnk`,
	)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(data)%2 != 0 {
		t.Fatalf("encoded script has odd UTF-16LE byte count %d", len(data))
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = uint16(data[index*2]) | uint16(data[index*2+1])<<8
	}
	script := string(utf16.Decode(units))
	for _, guard := range []string{"ReparsePoint", "$link.TargetPath", "OrdinalIgnoreCase", ".Arguments", "-eq 'web'", "-eq 'tui'", "Remove-Item -LiteralPath"} {
		if !strings.Contains(script, guard) {
			t.Errorf("removal script does not contain ownership guard %q", guard)
		}
	}
	if strings.Contains(script, "Remove-Item -Recurse") {
		t.Fatal("removal script unexpectedly uses recursive deletion")
	}
}

func TestShortcutStartsBrowserFirstHost(t *testing.T) {
	encoded := encodedShortcutScript(
		`C:\Program Files\PCController\controller.exe`,
		`X:\Fixture\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\PCController.lnk`,
		"Test.PCController",
	)
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if len(data)%2 != 0 {
		t.Fatalf("encoded script has odd UTF-16LE byte count %d", len(data))
	}
	units := make([]uint16, len(data)/2)
	for index := range units {
		units[index] = uint16(data[index*2]) | uint16(data[index*2+1])<<8
	}
	script := string(utf16.Decode(units))
	if !strings.Contains(script, "$link.Arguments='web'") {
		t.Fatalf("shortcut does not start the Web host: %s", script)
	}
	if strings.Contains(script, "$link.Arguments='tui'") {
		t.Fatal("new shortcut still starts the TUI")
	}
}

func TestRemoveOwnedRegistryIntegrationReturnsInspectionErrors(t *testing.T) {
	registry := failingCleanupRegistry{err: errors.New("registry unavailable")}
	_, _, _, err := removeOwnedRegistryIntegration(registry, `C:\controller.exe`, "Test.PCController")
	if err == nil || !strings.Contains(err.Error(), "inspect protocol registration") || !strings.Contains(err.Error(), "inspect application identity") {
		t.Fatalf("cleanup error=%v", err)
	}
}

type failingCleanupRegistry struct{ err error }

func (registry failingCleanupRegistry) String(string, string) (string, error) {
	return "", registry.err
}

func (failingCleanupRegistry) DeleteTree(string) error { return nil }
