//go:build linux

package runtimeinstall

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPrivilegedPackageValidationRejectsUserOwnedInput(t *testing.T) {
	packageDirectory, board := executablePackage(t, "1.0.0", "untrusted")
	if os.Geteuid() == 0 {
		if err := os.Chown(packageDirectory, 65534, -1); err != nil {
			t.Fatal(err)
		}
	}
	old := runtimeEffectiveUID
	runtimeEffectiveUID = func() int { return 0 }
	t.Cleanup(func() { runtimeEffectiveUID = old })
	if _, err := ValidatePackage(context.Background(), packageDirectory, board); err == nil ||
		(!strings.Contains(err.Error(), "owned by uid") && !strings.Contains(err.Error(), "group/world writable")) {
		t.Fatalf("privileged user-owned package error=%v", err)
	}
}

func TestBrowserExecutableHasDistinctBoundedSparseFileLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chrome")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := file.Truncate(maximumBinaryBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := validatePinnedFile(file, maximumBinaryBytes, false, true); err == nil {
		t.Fatal("general Controller/VirtualBoard limit accepted a browser-sized file")
	}
	if err := validatePinnedFile(file, maximumBrowserExecutableBytes, false, true); err != nil {
		t.Fatalf("browser limit rejected sparse file just above 256 MiB: %v", err)
	}
	if err := file.Truncate(maximumBrowserExecutableBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := validatePinnedFile(file, maximumBrowserExecutableBytes, false, true); err == nil {
		t.Fatal("browser limit accepted a file above its 512 MiB ceiling")
	}
}

func TestPinnedPackageCopySurvivesPathSwapAndRejectsSymlink(t *testing.T) {
	oldEffectiveUID := runtimeEffectiveUID
	runtimeEffectiveUID = func() int { return 1000 }
	t.Cleanup(func() { runtimeEffectiveUID = oldEffectiveUID })
	packageDirectory, board := executablePackage(t, "1.0.0", "original")
	validated, err := ValidatePackage(context.Background(), packageDirectory, board)
	if err != nil {
		t.Fatal(err)
	}
	defer validated.Close()
	originalController, err := os.ReadFile(filepath.Join(packageDirectory, "controller"))
	if err != nil {
		t.Fatal(err)
	}
	originalBoard, err := os.ReadFile(board)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(packageDirectory, "controller"), filepath.Join(packageDirectory, "controller.original")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(packageDirectory, "controller"), "replacement")
	if err := os.Rename(board, board+".original"); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, board, "replacement")
	destination := t.TempDir()
	controllerCopy := filepath.Join(destination, "controller")
	boardCopy := filepath.Join(destination, "virtual-board")
	if err := copyVerifiedExecutable(validated.controllerFile, validated.Controller, controllerCopy); err != nil {
		t.Fatal(err)
	}
	if err := copyVerifiedExecutable(validated.virtualBoardFile, validated.VirtualBoard, boardCopy); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(controllerCopy); string(got) != string(originalController) {
		t.Fatal("Controller copy followed swapped pathname")
	}
	if got, _ := os.ReadFile(boardCopy); string(got) != string(originalBoard) {
		t.Fatal("VirtualBoard copy followed swapped pathname")
	}

	symlinkPackage, symlinkBoard := executablePackage(t, "1.0.0", "symlink")
	realBoard := symlinkBoard + ".real"
	writeExecutable(t, realBoard, "board")
	if err := os.Remove(symlinkBoard); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realBoard, symlinkBoard); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidatePackage(context.Background(), symlinkPackage, symlinkBoard); err == nil {
		t.Fatal("symlinked VirtualBoard was accepted")
	}
}

func TestControllerSmokeRequiresExactVersionAndSourceHash(t *testing.T) {
	manifest := HostManifest{}
	manifest.Identity.Version = "1.2.3"
	manifest.Identity.SourceSHA256 = strings.Repeat("a", 64)
	for _, output := range []string{
		"PCController 1.2.30 source-hash=" + strings.Repeat("a", 64),
		"PCController 1.2.3 source-hash=" + strings.Repeat("b", 64),
		"PCController prefix-1.2.3 source-hash=" + strings.Repeat("a", 64),
	} {
		if err := validateControllerSmoke(output, manifest); err == nil {
			t.Fatalf("accepted mismatched smoke output %q", output)
		}
	}
}

func TestTCPListenerParserAndBoundedRetry(t *testing.T) {
	content := []byte(strings.Join([]string{
		"  0: 0100007F:223D 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 4242 1 0000000000000000",
		"  1: 00000000:223D 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 4343 1 0000000000000000",
		"  2: 0102A8C0:223D 00000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 4444 1 0000000000000000",
		"  3: 00000000:223D 00000000:0000 01 00000000:00000000 00:00000000 00000000 1000 0 4545 1 0000000000000000",
	}, "\n"))
	inodes, err := parseTCPListenerInodes(content, "127.0.0.1:8765")
	if err != nil || !inodes["4242"] || !inodes["4343"] || len(inodes) != 2 {
		t.Fatalf("parsed listener inodes=%v err=%v", inodes, err)
	}
	content6 := []byte(strings.Join([]string{
		"  0: 00000000000000000000000000000000:223D 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 5252 1 0000000000000000",
		"  1: 00000000000000000000000001000000:223D 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000 1000 0 5353 1 0000000000000000",
	}, "\n"))
	ipv6Inodes, err := parseTCP6ListenerInodes(content6, "127.0.0.1:8765")
	if err != nil || !ipv6Inodes["5252"] || len(ipv6Inodes) != 1 {
		t.Fatalf("parsed IPv6 listener inodes=%v err=%v", ipv6Inodes, err)
	}
	old := runtimePIDOwnsTCPListener
	attempts := 0
	runtimePIDOwnsTCPListener = func(pid int, address string) error {
		attempts++
		if pid != 321 || address != "127.0.0.1:8765" {
			return errors.New("unexpected listener identity")
		}
		if attempts < 3 {
			return errors.New("not listening yet")
		}
		return nil
	}
	t.Cleanup(func() { runtimePIDOwnsTCPListener = old })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForPIDListener(ctx, 321, "127.0.0.1:8765"); err != nil || attempts != 3 {
		t.Fatalf("listener retry attempts=%d err=%v", attempts, err)
	}
}

func TestPIDOwnsWildcardTCPListenerOnlyForExactPID(t *testing.T) {
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := fmt.Sprintf("127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	if err := pidOwnsTCPListener(os.Getpid(), address); err != nil {
		t.Fatalf("current PID did not own wildcard listener %s: %v", address, err)
	}

	nonOwner := exec.Command("sleep", "30")
	if err := nonOwner.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = nonOwner.Process.Kill()
		_ = nonOwner.Wait()
	})
	if err := pidOwnsTCPListener(nonOwner.Process.Pid, address); err == nil {
		t.Fatalf("unrelated PID %d accepted as owner of %s", nonOwner.Process.Pid, address)
	}
}

func TestVerifyUserUnitBindsMainPIDToExecutable(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	run := func(arguments ...string) (string, error) {
		if len(arguments) == 5 && arguments[0] == "show" {
			return "active\n" + fmt.Sprint(os.Getpid()) + "\n", nil
		}
		return "", errors.New("unexpected systemctl argv")
	}
	pid, err := verifyUserUnit(context.Background(), run, "pccontroller-controller.service", executable)
	if err != nil || pid != os.Getpid() {
		t.Fatalf("verified MainPID=%d err=%v", pid, err)
	}
}

func TestVerifyUserUnitWaitsForStableExecutableAfterSystemdExecutor(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	oldRead := runtimeReadProcessExecutable
	oldTimeout := runtimeUserUnitReadyTimeout
	oldInterval := runtimeUserUnitPollInterval
	attempts := 0
	runtimeReadProcessExecutable = func(string) (string, error) {
		attempts++
		if attempts == 1 {
			return "/usr/lib/systemd/systemd-executor", nil
		}
		return expected, nil
	}
	runtimeUserUnitReadyTimeout = 100 * time.Millisecond
	runtimeUserUnitPollInterval = time.Millisecond
	t.Cleanup(func() {
		runtimeReadProcessExecutable = oldRead
		runtimeUserUnitReadyTimeout = oldTimeout
		runtimeUserUnitPollInterval = oldInterval
	})
	run := func(arguments ...string) (string, error) {
		return "active\n4242\n", nil
	}
	pid, err := verifyUserUnit(context.Background(), run, "pccontroller-virtual-board.service", executable)
	if err != nil || pid != 4242 || attempts != 3 {
		t.Fatalf("stable executor transition pid=%d attempts=%d err=%v", pid, attempts, err)
	}
}

func TestVerifyUserUnitRejectsSystemdExecutorThatNeverTransitions(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	oldRead := runtimeReadProcessExecutable
	oldTimeout := runtimeUserUnitReadyTimeout
	oldInterval := runtimeUserUnitPollInterval
	attempts := 0
	runtimeReadProcessExecutable = func(string) (string, error) {
		attempts++
		return "/usr/lib/systemd/systemd-executor", nil
	}
	runtimeUserUnitReadyTimeout = 20 * time.Millisecond
	runtimeUserUnitPollInterval = time.Millisecond
	t.Cleanup(func() {
		runtimeReadProcessExecutable = oldRead
		runtimeUserUnitReadyTimeout = oldTimeout
		runtimeUserUnitPollInterval = oldInterval
	})
	run := func(arguments ...string) (string, error) {
		return "active\n4242\n", nil
	}
	if _, err := verifyUserUnit(context.Background(), run, "pccontroller-virtual-board.service", executable); err == nil ||
		!strings.Contains(err.Error(), "systemd-executor") || attempts < 2 {
		t.Fatalf("never-transition attempts=%d error=%v", attempts, err)
	}
}

func TestRuntimeUnitProxyBypassAndReadinessArgv(t *testing.T) {
	unit := runtimeUnits(DefaultRoot, "/usr/bin/chromium", "/usr/bin/curl")["pccontroller-window.service"]
	for _, required := range []string{
		`ExecStartPre="/usr/bin/curl" --noproxy "*"`,
		`http://127.0.0.1:8787/healthz`,
		`toolchain runtime-window-ready --timeout 45s`,
		`--noerrdialogs --disable-session-crashed-bubble`,
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("window unit omitted %q:\n%s", required, unit)
		}
	}
	controller := runtimeUnits(DefaultRoot, "/usr/bin/chromium", "/usr/bin/curl")["pccontroller-controller.service"]
	if strings.Contains(controller, ` web --listen`) {
		t.Fatalf("controller unit must let the authenticated edge configuration own its listen address:\n%s", controller)
	}
	if !strings.Contains(controller, ` web --no-open --no-tray --port tcp://127.0.0.1:8765`) {
		t.Fatalf("controller unit omitted the config-owned web launch shape:\n%s", controller)
	}
}

func TestVirtualBoardUnitUsesSupportedEEPROMArgumentShape(t *testing.T) {
	unit := runtimeUnits(DefaultRoot, "/usr/bin/chromium", "/usr/bin/curl")["pccontroller-virtual-board.service"]
	want := `--eeprom "%h/.local/share/pccontroller/virtual-board/eeprom.bin"`
	if !strings.Contains(unit, want) || strings.Contains(unit, "--eeprom=") {
		t.Fatalf("VirtualBoard unit does not use the supported --eeprom FILE argv shape:\n%s", unit)
	}
}

func TestRuntimeManifestRejectsDuplicateArtifactsAndTrailingJSON(t *testing.T) {
	manifest := RuntimeManifest{
		Format: RuntimeManifestFormat, ReleaseID: "release", GeneratedUTC: time.Now().UTC().Format(time.RFC3339),
		Target: "linux/" + runtime.GOARCH, TargetUser: "user", TargetUID: 1000, TargetHome: "/home/user",
		HostVersion: "1.2.3", HostSourceSHA256: strings.Repeat("a", 64), Browser: "/usr/bin/chromium",
		ExecutableSmokesPassed: true, UserDataPreservedOnRemove: true,
	}
	for path, role := range map[string]string{
		"bin/controller": "controller", "bin/virtual-board": "virtual-board", "host-manifest.json": "host-manifest",
		"systemd/user/pccontroller-virtual-board.service": "systemd-user-unit",
		"systemd/user/pccontroller-controller.service":    "systemd-user-unit",
		"systemd/user/pccontroller-window.service":        "systemd-user-unit",
	} {
		manifest.Files = append(manifest.Files, FileIdentity{Role: role, Path: path, Bytes: 1, SHA256: strings.Repeat("b", 64)})
	}
	if err := validateRuntimeManifest(manifest); err != nil {
		t.Fatal(err)
	}
	duplicate := manifest
	duplicate.Files = append(append([]FileIdentity(nil), manifest.Files...), manifest.Files[0])
	if err := validateRuntimeManifest(duplicate); err == nil {
		t.Fatal("duplicate runtime artifact accepted")
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "runtime-manifest.json")
	if err := os.WriteFile(path, append(encoded, []byte("\n{}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimeManifest(path); err == nil || !strings.Contains(err.Error(), "trailing JSON") {
		t.Fatalf("trailing manifest error=%v", err)
	}
}

func TestInstallStatusRollbackAndUninstallPreserveUserData(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("target-user integration test runs as a non-root account")
	}
	forceUserManagerAvailable(t)
	root := filepath.Join(t.TempDir(), "runtime")
	home := t.TempDir()
	forceCurrentUserHome(t, home)
	uid := uint32(os.Geteuid())
	runner := successfulRuntimeRunner
	oldMask := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(oldMask) })
	firstPackage, firstBoard := executablePackage(t, "1.0.0", "first")
	first, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uid, TargetHome: home,
		HostPackage: firstPackage, VirtualBoard: firstBoard, Browser: "/bin/sh",
		Apply: true, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.Installed || !first.UserUnitsReady || first.UserManager != "activated-ready" || !first.RuntimeReady {
		t.Fatalf("first=%+v", first)
	}
	firstRelease := filepath.Join(root, "releases", first.ReleaseID)
	for _, directory := range []string{root, filepath.Join(root, "releases"), filepath.Join(root, "bin"), firstRelease, filepath.Join(firstRelease, "bin"), filepath.Join(firstRelease, "systemd"), filepath.Join(firstRelease, "systemd", "user")} {
		info, err := os.Lstat(directory)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("managed traversal directory %s mode/error=%v/%v", directory, func() os.FileMode {
				if info == nil {
					return 0
				}
				return info.Mode().Perm()
			}(), err)
		}
	}
	for _, executable := range []string{filepath.Join(firstRelease, "bin", "controller"), filepath.Join(firstRelease, "bin", "virtual-board")} {
		info, err := os.Lstat(executable)
		if err != nil || info.Mode().Perm() != 0o755 {
			t.Fatalf("published executable %s is not 0755: mode/error=%v/%v", executable, func() os.FileMode {
				if info == nil {
					return 0
				}
				return info.Mode().Perm()
			}(), err)
		}
	}
	output, err := exec.Command(filepath.Join(root, "bin", "controller"), "version").CombinedOutput()
	if err != nil || !strings.Contains(string(output), "PCController 1.0.0") {
		t.Fatalf("target-readable stable Controller failed: %v: %s", err, output)
	}
	stateFile := filepath.Join(home, ".local", "share", "pccontroller", "virtual-board", "eeprom.bin")
	if err := os.WriteFile(stateFile, []byte("persistent"), 0o600); err != nil {
		t.Fatal(err)
	}
	secondPackage, secondBoard := executablePackage(t, "2.0.0", "second")
	second, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uid, TargetHome: home,
		HostPackage: secondPackage, VirtualBoard: secondBoard, Browser: "/bin/sh",
		Apply: true, Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ReleaseID == first.ReleaseID || second.PreviousReleaseID != first.ReleaseID {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	previousTarget, err := readManagedReleaseLink(root, "previous")
	if err != nil {
		t.Fatal(err)
	}
	previousManifestPath := filepath.Join(root, previousTarget, "runtime-manifest.json")
	originalPreviousManifest, err := os.ReadFile(previousManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var mismatched RuntimeManifest
	if err := json.Unmarshal(originalPreviousManifest, &mismatched); err != nil {
		t.Fatal(err)
	}
	mismatched.ReleaseID = "different-release-id"
	writeJSON(t, previousManifestPath, mismatched)
	if _, err := Rollback(context.Background(), OperationOptions{Root: root}); err == nil || !strings.Contains(err.Error(), "previous runtime pointer") {
		t.Fatalf("previous pointer/manifest mismatch error=%v", err)
	}
	if err := os.WriteFile(previousManifestPath, originalPreviousManifest, 0o644); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := Rollback(context.Background(), OperationOptions{Root: root, Apply: true, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack.ReleaseID != first.ReleaseID || rolledBack.PreviousReleaseID != second.ReleaseID {
		t.Fatalf("rollback=%+v", rolledBack)
	}
	status, err := Status(context.Background(), OperationOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !status.ArtifactsVerified || !status.StableLinksReady || !status.UserUnitsReady || status.ReleaseID != first.ReleaseID {
		t.Fatalf("status=%+v", status)
	}
	removed, err := Uninstall(context.Background(), OperationOptions{Root: root, Apply: true, Runner: runner})
	if err != nil {
		t.Fatal(err)
	}
	if removed.Installed {
		t.Fatalf("uninstall=%+v", removed)
	}
	if content, err := os.ReadFile(stateFile); err != nil || string(content) != "persistent" {
		t.Fatalf("user data changed: %q err=%v", content, err)
	}
}

func TestInstallActivationFailureRestoresCurrentRelease(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("target-user integration test runs as a non-root account")
	}
	forceUserManagerAvailable(t)
	root := filepath.Join(t.TempDir(), "runtime")
	home := t.TempDir()
	forceCurrentUserHome(t, home)
	uid := uint32(os.Geteuid())
	goodRunner := successfulRuntimeRunner
	firstPackage, firstBoard := executablePackage(t, "1.0.0", "first")
	first, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uid, TargetHome: home,
		HostPackage: firstPackage, VirtualBoard: firstBoard, Browser: "/bin/sh", Apply: true, Runner: goodRunner,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondPackage, secondBoard := executablePackage(t, "2.0.0", "second")
	failingRunner := func(_ context.Context, _ string, arguments []string, _ []string) (string, error) {
		if strings.Contains(strings.Join(arguments, " "), "restart pccontroller-controller.service") {
			return "synthetic restart failure", errors.New("exit status 1")
		}
		return successfulRuntimeRunner(context.Background(), "", arguments, nil)
	}
	if _, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uid, TargetHome: home,
		HostPackage: secondPackage, VirtualBoard: secondBoard, Browser: "/bin/sh", Apply: true, Runner: failingRunner,
	}); err == nil || !strings.Contains(err.Error(), "publication rolled back") || !strings.Contains(err.Error(), "reactivate previous release") {
		t.Fatalf("activation error=%v", err)
	}
	status, err := Status(context.Background(), OperationOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if status.ReleaseID != first.ReleaseID || !status.ArtifactsVerified {
		t.Fatalf("status after failed upgrade=%+v want=%s", status, first.ReleaseID)
	}
}

func TestFirstInstallPartialStartStopsBeforePublicationCleanup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("target-user integration test runs as a non-root account")
	}
	forceUserManagerAvailable(t)
	root := filepath.Join(t.TempDir(), "runtime")
	home := t.TempDir()
	forceCurrentUserHome(t, home)
	packageDirectory, board := executablePackage(t, "1.0.0", "first-cleanup")
	var calls []string
	failed := false
	runner := func(ctx context.Context, _ string, arguments []string, environment []string) (string, error) {
		joined := strings.Join(arguments, " ")
		calls = append(calls, joined)
		if !failed && strings.Contains(joined, "restart pccontroller-controller.service") {
			failed = true
			return "partial Controller start failure", errors.New("exit status 1")
		}
		return successfulRuntimeRunner(ctx, "", arguments, environment)
	}
	_, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uint32(os.Geteuid()), TargetHome: home,
		HostPackage: packageDirectory, VirtualBoard: board, Browser: "/bin/sh", Apply: true, Runner: runner,
	})
	if err == nil || !strings.Contains(err.Error(), "first-install services stopped and publication rolled back") {
		t.Fatalf("first-install cleanup error=%v", err)
	}
	stopSeen := false
	for _, call := range calls {
		if strings.Contains(call, "stop pccontroller-window.service pccontroller-controller.service pccontroller-virtual-board.service") {
			stopSeen = true
			break
		}
	}
	if !stopSeen {
		t.Fatalf("partial services were not stopped before cleanup: %v", calls)
	}
	if _, err := os.Lstat(filepath.Join(root, "current")); !os.IsNotExist(err) {
		t.Fatalf("failed first-install current pointer remained: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "releases"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed first-install release remained: entries=%v err=%v", entries, err)
	}
	unit := filepath.Join(home, ".config", "systemd", "user", runtimeUnitNames[0])
	if _, err := os.Lstat(unit); !os.IsNotExist(err) {
		t.Fatalf("failed first-install user link remained: %v", err)
	}
}

func TestFirstInstallStopFailurePreservesPublishedFilesAndReportsCleanup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("target-user integration test runs as a non-root account")
	}
	forceUserManagerAvailable(t)
	root := filepath.Join(t.TempDir(), "runtime")
	home := t.TempDir()
	forceCurrentUserHome(t, home)
	packageDirectory, board := executablePackage(t, "1.0.0", "stop-failure")
	runner := func(ctx context.Context, _ string, arguments []string, environment []string) (string, error) {
		joined := strings.Join(arguments, " ")
		if strings.Contains(joined, "restart pccontroller-controller.service") {
			return "partial Controller start failure", errors.New("exit status 1")
		}
		if strings.Contains(joined, "stop pccontroller-window.service") {
			return "synthetic stop failure", errors.New("exit status 1")
		}
		return successfulRuntimeRunner(ctx, "", arguments, environment)
	}
	_, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uint32(os.Geteuid()), TargetHome: home,
		HostPackage: packageDirectory, VirtualBoard: board, Browser: "/bin/sh", Apply: true, Runner: runner,
	})
	if err == nil || !strings.Contains(err.Error(), "stop partially started first-install services before rollback") {
		t.Fatalf("stop cleanup error=%v", err)
	}
	current, readErr := os.Readlink(filepath.Join(root, "current"))
	if readErr != nil || current == "" {
		t.Fatalf("publication was deleted despite failed service stop: current=%q err=%v", current, readErr)
	}
	if _, statErr := os.Lstat(filepath.Join(root, current, "bin", "controller")); statErr != nil {
		t.Fatalf("running release bytes were removed after failed stop: %v", statErr)
	}
}

func TestActivationRechecksBothListenersAndChromeMainExecutable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("target-user integration test runs as a non-root account")
	}
	forceUserManagerAvailable(t)
	root := filepath.Join(t.TempDir(), "runtime")
	home := t.TempDir()
	forceCurrentUserHome(t, home)
	oldUnit := runtimeVerifyUserUnit
	oldWait := runtimeWaitForPIDListener
	var windowExecutable string
	var listeners []string
	runtimeVerifyUserUnit = func(_ context.Context, _ userSystemctlRunner, unit, expected string) (int, error) {
		if unit == "pccontroller-window.service" {
			windowExecutable = expected
		}
		return 777, nil
	}
	runtimeWaitForPIDListener = func(_ context.Context, pid int, address string) error {
		if pid != 777 {
			return fmt.Errorf("listener checked against unexpected pid %d", pid)
		}
		listeners = append(listeners, address)
		return nil
	}
	t.Cleanup(func() {
		runtimeVerifyUserUnit = oldUnit
		runtimeWaitForPIDListener = oldWait
	})
	packageDirectory, board := executablePackage(t, "1.0.0", "identity")
	if _, err := Install(context.Background(), InstallOptions{
		Root: root, TargetUser: "test-user", TargetUID: uint32(os.Geteuid()), TargetHome: home,
		HostPackage: packageDirectory, VirtualBoard: board, Browser: "/bin/sh", Apply: true, Runner: successfulRuntimeRunner,
	}); err != nil {
		t.Fatal(err)
	}
	expectedBrowser, err := filepath.EvalSymlinks("/bin/sh")
	if err != nil {
		t.Fatal(err)
	}
	if windowExecutable != expectedBrowser {
		t.Fatalf("Chrome MainPID expected executable=%q want=%q", windowExecutable, expectedBrowser)
	}
	if strings.Join(listeners, ",") != "127.0.0.1:8765,127.0.0.1:8765,127.0.0.1:8787" {
		t.Fatalf("listener verification sequence=%v", listeners)
	}
}

func executablePackage(t *testing.T, version, marker string) (string, string) {
	t.Helper()
	root := t.TempDir()
	controller := filepath.Join(root, "controller")
	board := filepath.Join(t.TempDir(), "virtual_board")
	writeExecutable(t, controller, "#!/bin/sh\necho 'PCController "+version+" source-hash="+strings.Repeat("a", 64)+"'\n# "+marker+"\n")
	writeExecutable(t, board, "#!/bin/sh\necho 'PCController Virtual Board --port PORT "+marker+"'\n")
	manifest := testHostManifest(t, controller, "linux", runtime.GOARCH)
	manifest.Identity.Version = version
	writeJSON(t, filepath.Join(root, "host-manifest.json"), manifest)
	return root, board
}

func forceUserManagerAvailable(t *testing.T) {
	t.Helper()
	old := runtimeUserManagerBusPresent
	runtimeUserManagerBusPresent = func(uint32) bool { return true }
	oldUnit := runtimeVerifyUserUnit
	oldListener := runtimeWaitForPIDListener
	oldHealth := runtimeWaitForHealth
	runtimeVerifyUserUnit = func(context.Context, userSystemctlRunner, string, string) (int, error) { return os.Getpid(), nil }
	runtimeWaitForPIDListener = func(context.Context, int, string) error { return nil }
	runtimeWaitForHealth = func(context.Context) error { return nil }
	t.Cleanup(func() {
		runtimeUserManagerBusPresent = old
		runtimeVerifyUserUnit = oldUnit
		runtimeWaitForPIDListener = oldListener
		runtimeWaitForHealth = oldHealth
	})
}

func forceCurrentUserHome(t *testing.T, home string) {
	t.Helper()
	old := runtimeCurrentUserHome
	runtimeCurrentUserHome = func() (string, error) { return home, nil }
	t.Cleanup(func() { runtimeCurrentUserHome = old })
}

func successfulRuntimeRunner(ctx context.Context, _ string, arguments []string, _ []string) (string, error) {
	joined := strings.Join(arguments, " ")
	if strings.Contains(joined, "is-active graphical-session.target") {
		return "active\n", nil
	}
	if strings.Contains(joined, "show-environment") {
		return "DISPLAY=:0\n", nil
	}
	for index, argument := range arguments {
		if (argument == "version" || argument == "--help") && index > 0 {
			command := exec.CommandContext(ctx, arguments[index-1], arguments[index:]...)
			output, err := command.CombinedOutput()
			return string(output), err
		}
	}
	return "", nil
}
