//go:build windows

package hostui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func temporaryShortcutStatus(t *testing.T) DesktopIntegrationStatus {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	start, err := shortcutPathInDirectory(filepath.Join(root, "Start Menu"), "Controller Tests")
	if err != nil {
		t.Fatal(err)
	}
	desktop, err := shortcutPathInDirectory(filepath.Join(root, "Redirected Desktop"), "Controller Tests")
	if err != nil {
		t.Fatal(err)
	}
	return DesktopIntegrationStatus{Supported: true, Executable: executable, Shortcut: start, DesktopShortcut: desktop}
}

func TestWindowsShortcutsCreateRepairAndVerifyBoth(t *testing.T) {
	status := temporaryShortcutStatus(t)
	const appID = "Tests.Controller.Desktop"
	ensure := func() {
		t.Helper()
		status.ShortcutReady, status.DesktopShortcutReady = false, false
		if err := ensureWindowsShortcuts(&status, appID, "Controller Tests"); err != nil {
			t.Fatal(err)
		}
		if !status.ShortcutReady || !status.DesktopShortcutReady {
			t.Fatalf("partial status: %+v", status)
		}
		for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
			link, err := inspectWindowsShortcut(path)
			if err != nil {
				t.Fatal(err)
			}
			if !sameWindowsPath(link.Target, status.Executable) || link.Arguments != "web" ||
				!sameWindowsPath(link.Icon, status.Executable) || link.IconIndex != 0 {
				t.Fatalf("link=%+v", link)
			}
			identity, err := shortcutAppUserModelID(path)
			if err != nil || identity != appID {
				t.Fatalf("identity=%q err=%v", identity, err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary link residue: %v err=%v", entries, err)
			}
		}
	}
	ensure()
	if err := os.Remove(status.DesktopShortcut); err != nil {
		t.Fatal(err)
	}
	ensure() // Matching-package installer/normal launch repairs a missing Desktop link.
	ensure() // Existing owned links also survive an idempotent repair.
	for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
		removed, preserved, err := removeOwnedShortcut(status.Executable, path, appID)
		if err != nil || !removed || preserved {
			t.Fatalf("remove=%t/%t err=%v", removed, preserved, err)
		}
		removed, preserved, err = removeOwnedShortcut(status.Executable, path, appID)
		if err != nil || removed || preserved {
			t.Fatalf("idempotent remove=%t/%t err=%v", removed, preserved, err)
		}
	}
}

func TestWindowsShortcutsPreserveUnownedAndReportPartial(t *testing.T) {
	for _, foreign := range []string{"target", "identity"} {
		t.Run(foreign, func(t *testing.T) {
			status := temporaryShortcutStatus(t)
			const appID = "Tests.Controller.Desktop"
			executable, identity := status.Executable, appID
			if foreign == "target" {
				executable = filepath.Join(t.TempDir(), "another-app.exe")
			} else {
				identity = "Tests.AnotherOwner"
			}
			if err := os.MkdirAll(filepath.Dir(status.DesktopShortcut), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := createWindowsShortcut(executable, status.DesktopShortcut, identity, "User link"); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(status.DesktopShortcut)
			if err != nil {
				t.Fatal(err)
			}
			err = ensureWindowsShortcuts(&status, appID, "Controller Tests")
			if err == nil || !status.ShortcutReady || status.DesktopShortcutReady ||
				strings.Join(status.Skipped, ",") != "desktop-shortcut-not-owned" {
				t.Fatalf("status=%+v err=%v", status, err)
			}
			removed, preserved, err := removeOwnedShortcut(status.Executable, status.DesktopShortcut, appID)
			if err != nil || removed || !preserved {
				t.Fatalf("foreign removal=%t/%t err=%v", removed, preserved, err)
			}
			after, err := os.ReadFile(status.DesktopShortcut)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("unowned link modified: %v", err)
			}
		})
	}
}

func TestWindowsShortcutsPreserveDirectoryAtLinkPath(t *testing.T) {
	status := temporaryShortcutStatus(t)
	if err := os.MkdirAll(status.DesktopShortcut, 0o755); err != nil {
		t.Fatal(err)
	}
	err := ensureWindowsShortcuts(&status, "Tests.Controller", "Controller Tests")
	if err == nil || status.DesktopShortcutReady {
		t.Fatalf("directory accepted: %+v err=%v", status, err)
	}
	info, err := os.Stat(status.DesktopShortcut)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory was replaced: %v", err)
	}
}

func TestWindowsShortcutPathPreservesKnownFolderDestination(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "Redirected", "Desktop")
	path, err := shortcutPathInDirectory(directory, `../Controller: Tests`)
	if err != nil || filepath.Dir(path) != directory || !strings.HasSuffix(path, ".lnk") {
		t.Fatalf("redirected/sanitized path=%q err=%v", path, err)
	}
	if _, err := shortcutPathInDirectory("", "Controller"); err == nil {
		t.Fatal("missing known folder accepted")
	}
}
