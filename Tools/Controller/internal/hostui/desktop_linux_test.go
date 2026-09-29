//go:build linux

package hostui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxDesktopIntegrationCreatesOwnedXDGEntry(t *testing.T) {
	originalExecutable, originalRun := linuxDesktopExecutable, linuxDesktopRun
	t.Cleanup(func() { linuxDesktopExecutable, linuxDesktopRun = originalExecutable, originalRun })
	dataHome, configHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	executable := filepath.Join(t.TempDir(), "controller")
	linuxDesktopExecutable = func() (string, error) { return executable, nil }
	var command []string
	linuxDesktopRun = func(name string, arguments ...string) ([]byte, error) {
		command = append([]string{name}, arguments...)
		return nil, nil
	}
	status, err := EnsureDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController", DisplayName: "Workshop Controller"})
	if err != nil || !status.Supported || !status.ShortcutReady || !status.ProtocolReady {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	content, err := os.ReadFile(status.Shortcut)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Name=Workshop Controller", " uri %u", "MimeType=x-scheme-handler/pccontroller;", executableDigest(executable)} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("desktop entry missing %q:\n%s", want, content)
		}
	}
	if strings.Join(command, " ") != "xdg-mime default test.pccontroller.desktop x-scheme-handler/pccontroller" {
		t.Fatalf("xdg command=%q", command)
	}

	mimeapps := filepath.Join(configHome, "mimeapps.list")
	longAssociation := "application/x-long=" + strings.Repeat("other.desktop;", 6000)
	if err := os.WriteFile(mimeapps, []byte("[Default Applications]\nx-scheme-handler/pccontroller=test.pccontroller.desktop;other.desktop;\n"+longAssociation+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanup, err := RemoveDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController", DisplayName: "Workshop Controller"})
	if err != nil || !cleanup.ShortcutRemoved || !cleanup.ProtocolRemoved || !cleanup.AppIdentityRemoved {
		t.Fatalf("cleanup=%+v err=%v", cleanup, err)
	}
	updated, err := os.ReadFile(mimeapps)
	if err != nil || strings.Contains(string(updated), "test.pccontroller.desktop") ||
		!strings.Contains(string(updated), "other.desktop") || !strings.Contains(string(updated), longAssociation) {
		t.Fatalf("updated mimeapps=%q err=%v", updated, err)
	}
}

func TestLinuxDesktopCleanupRemovesStaleMimeAssociationWithoutShortcut(t *testing.T) {
	originalExecutable := linuxDesktopExecutable
	t.Cleanup(func() { linuxDesktopExecutable = originalExecutable })
	dataHome, configHome := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	linuxDesktopExecutable = func() (string, error) { return "/opt/pccontroller/controller", nil }
	mimeapps := filepath.Join(configHome, "mimeapps.list")
	if err := os.WriteFile(mimeapps, []byte("[Default Applications]\nx-scheme-handler/pccontroller=test.pccontroller.desktop;\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := RemoveDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController"})
	if err != nil || status.ShortcutRemoved || !status.ProtocolRemoved {
		t.Fatalf("status=%+v err=%v", status, err)
	}
	content, readErr := os.ReadFile(mimeapps)
	if readErr != nil || strings.Contains(string(content), "test.pccontroller.desktop") {
		t.Fatalf("stale MIME association remains: %q err=%v", content, readErr)
	}
}

func TestLinuxDesktopCleanupDoesNotClaimProtocolRemovalOnFailure(t *testing.T) {
	originalExecutable, originalRun := linuxDesktopExecutable, linuxDesktopRun
	t.Cleanup(func() { linuxDesktopExecutable, linuxDesktopRun = originalExecutable, originalRun })
	dataHome := t.TempDir()
	invalidConfigHome := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(invalidConfigHome, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", invalidConfigHome)
	linuxDesktopExecutable = func() (string, error) { return "/opt/pccontroller/controller", nil }
	linuxDesktopRun = func(string, ...string) ([]byte, error) { return nil, nil }
	if _, err := EnsureDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController"}); err != nil {
		t.Fatal(err)
	}
	applicationsMimeapps := filepath.Join(dataHome, "applications", "mimeapps.list")
	if err := os.WriteFile(applicationsMimeapps, []byte("[Default Applications]\nx-scheme-handler/pccontroller=test.pccontroller.desktop;\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := RemoveDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController"})
	if err == nil || !status.ShortcutRemoved || status.ProtocolRemoved || status.LastError == "" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestLinuxDesktopIntegrationPreservesForeignEntry(t *testing.T) {
	originalExecutable, originalRun := linuxDesktopExecutable, linuxDesktopRun
	t.Cleanup(func() { linuxDesktopExecutable, linuxDesktopRun = originalExecutable, originalRun })
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	linuxDesktopExecutable = func() (string, error) { return "/opt/pccontroller/controller", nil }
	linuxDesktopRun = func(string, ...string) ([]byte, error) { return nil, nil }
	applications := filepath.Join(dataHome, "applications")
	if err := os.MkdirAll(applications, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(applications, "test.pccontroller.desktop")
	foreign := []byte("[Desktop Entry]\nExec=/opt/other\n")
	if err := os.WriteFile(path, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureDesktopIntegration(DesktopIntegrationOptions{AppID: "test.PCController"}); err == nil {
		t.Fatal("foreign desktop entry was overwritten")
	}
	content, _ := os.ReadFile(path)
	if string(content) != string(foreign) {
		t.Fatal("foreign desktop entry changed")
	}
}
