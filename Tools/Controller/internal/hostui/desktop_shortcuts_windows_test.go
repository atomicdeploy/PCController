//go:build windows

package hostui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"pccontroller.local/controller/internal/productidentity"
)

func TestWindowsShortcutReplacementRetriesSharingViolationAndLeavesNoResidue(t *testing.T) {
	status := temporaryShortcutStatus(t)
	const appID = "Tests.Controller.Replace"
	if err := ensureWindowsShortcuts(&status, appID, "Controller Tests"); err != nil {
		t.Fatal(err)
	}
	original := moveShortcutFile
	defer func() { moveShortcutFile = original }()
	calls := 0
	moveShortcutFile = func(from, to *uint16, flags uint32) error {
		calls++
		if calls <= 2 {
			return windows.ERROR_SHARING_VIOLATION
		}
		return original(from, to, flags)
	}
	status.ShortcutReady, status.DesktopShortcutReady = false, false
	if err := ensureWindowsShortcuts(&status, appID, "Controller Tests"); err != nil {
		t.Fatal(err)
	}
	if calls < 4 { // two retries plus successful Start-menu and Desktop replacements.
		t.Fatalf("sharing-violation retry count=%d", calls)
	}
	for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 {
			t.Fatalf("temporary link residue after retry: %v err=%v", entries, err)
		}
	}
}

func TestWindowsShortcutReplacementFailureCleansExactTemporaryLink(t *testing.T) {
	status := temporaryShortcutStatus(t)
	const appID = "Tests.Controller.ReplaceFailure"
	if err := ensureWindowsShortcuts(&status, appID, "Controller Tests"); err != nil {
		t.Fatal(err)
	}
	original := replaceShortcutFile
	defer func() { replaceShortcutFile = original }()
	replaceShortcutFile = func(_, _ string) error { return windows.ERROR_ACCESS_DENIED }
	status.ShortcutReady, status.DesktopShortcutReady = false, false
	if err := ensureWindowsShortcuts(&status, appID, "Controller Tests"); err == nil || !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("replacement failure=%v", err)
	}
	for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
			t.Fatalf("temporary link residue after failure: %v err=%v", entries, err)
		}
	}
}

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
			if !sameWindowsPath(link.Target, status.Executable) || strings.TrimSpace(link.Arguments) != "" ||
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

func TestWindowsShortcutsMigrateOnlyValidatedPreviousSlot(t *testing.T) {
	status := temporaryShortcutStatus(t)
	previous := filepath.Join(t.TempDir(), "packages", "previous", "controller.exe")
	for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := createWindowsShortcut(previous, path, productidentity.StableAppID, "Controller Tests"); err != nil {
			t.Fatal(err)
		}
	}
	// Installer tests prove the validator's marker, active/previous-state and
	// full-package checks; these native tests prove both Shell links use it.
	checks := 0
	validated := func(executable, candidate string) (bool, error) {
		checks++
		return sameWindowsPath(executable, status.Executable) && sameWindowsPath(candidate, previous), nil
	}
	if err := ensureWindowsShortcutsWithPredecessor(&status, productidentity.StableAppID, "Controller Tests", validated); err != nil {
		t.Fatal(err)
	}
	if !status.ShortcutReady || !status.DesktopShortcutReady || checks != 4 {
		t.Fatalf("migration status=%+v checks=%d", status, checks)
	}
	for _, path := range []string{status.Shortcut, status.DesktopShortcut} {
		link, err := inspectWindowsShortcut(path)
		if err != nil || !sameWindowsPath(link.Target, status.Executable) || !sameWindowsPath(link.Icon, status.Executable) {
			t.Fatalf("old target/icon remains: %+v err=%v", link, err)
		}
	}
	if err := createWindowsShortcut(previous, status.DesktopShortcut, "Tests.ForeignIdentity", "User link"); err != nil {
		t.Fatal(err)
	}
	status.DesktopShortcutReady = false
	if err := ensureWindowsShortcutsWithPredecessor(&status, productidentity.StableAppID, "Controller Tests", validated); err == nil || status.DesktopShortcutReady {
		t.Fatalf("foreign AppID accepted via valid slot: %+v err=%v", status, err)
	}
}
