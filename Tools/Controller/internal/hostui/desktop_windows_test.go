//go:build windows

package hostui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestNativeShortcutRoundTripAndOwnership(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shortcut := filepath.Join(t.TempDir(), "Controller Tests.lnk")
	if err := createWindowsShortcut(executable, shortcut, "Tests.Controller", "Controller Tests"); err != nil {
		t.Fatal(err)
	}
	link, err := inspectWindowsShortcut(shortcut)
	if err != nil {
		t.Fatal(err)
	}
	if !sameWindowsPath(link.Target, executable) || link.Arguments != "web" {
		t.Fatalf("shortcut=%+v executable=%q", link, executable)
	}
	if !shortcutOwnedBy(executable, link) {
		t.Fatalf("shortcut should be owned: %+v", link)
	}
	appID, err := shortcutAppUserModelID(shortcut)
	if err != nil {
		t.Fatal(err)
	}
	if appID != "Tests.Controller" {
		t.Fatalf("shortcut AppUserModelID=%q", appID)
	}
	if shortcutOwnedBy(`C:\Other\controller.exe`, link) {
		t.Fatalf("foreign executable unexpectedly owns shortcut: %+v", link)
	}
}

func TestRemoveOwnedShortcutUsesNativeInspection(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shortcut := filepath.Join(t.TempDir(), "Controller Tests.lnk")
	if err := createWindowsShortcut(executable, shortcut, "Tests.Controller", "Controller Tests"); err != nil {
		t.Fatal(err)
	}
	removed, preserved, err := removeOwnedShortcut(executable, shortcut)
	if err != nil || !removed || preserved {
		t.Fatalf("remove=(removed=%t preserved=%t error=%v)", removed, preserved, err)
	}
	if _, err := os.Stat(shortcut); !os.IsNotExist(err) {
		t.Fatalf("shortcut remains after native removal: %v", err)
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
