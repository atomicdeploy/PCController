//go:build linux

package aptmirror

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"golang.org/x/sys/unix"
)

func TestGeneratedServiceRecreatesTrustedRuntimeDirectoryAfterBoot(t *testing.T) {
	config := DomesticFirstConfig("resolute", "amd64")
	service := string(SystemdService(config))
	directives := make(map[string]string)
	for _, line := range strings.Split(service, "\n") {
		name, value, ok := strings.Cut(line, "=")
		if ok {
			directives[name] = value
		}
	}

	runtimeName := directives["RuntimeDirectory"]
	if runtimeName == "" || filepath.IsAbs(runtimeName) || strings.Contains(runtimeName, "..") ||
		strings.IndexFunc(runtimeName, unicode.IsSpace) >= 0 {
		t.Fatalf("unsafe generated RuntimeDirectory=%q in:\n%s", runtimeName, service)
	}
	if got, want := filepath.ToSlash(filepath.Dir(config.Paths.Lock)), "/run/"+filepath.ToSlash(runtimeName); got != want {
		t.Fatalf("runtime directory %q does not create lock parent %q", runtimeName, got)
	}

	parsedMode, err := strconv.ParseUint(directives["RuntimeDirectoryMode"], 8, 32)
	if err != nil {
		t.Fatalf("invalid generated RuntimeDirectoryMode=%q: %v", directives["RuntimeDirectoryMode"], err)
	}
	if os.FileMode(parsedMode)&0o022 != 0 {
		t.Fatalf("generated runtime directory mode %04o permits group/world writes", parsedMode)
	}
	if directives["RuntimeDirectoryPreserve"] != "yes" {
		t.Fatalf("shared adoption/refresh lock directory must survive oneshot exit, got RuntimeDirectoryPreserve=%q", directives["RuntimeDirectoryPreserve"])
	}

	// Model the documented systemd fresh-start contract below a temporary
	// /run: the volatile directory does not exist after boot, systemd creates
	// it with the generated mode and service identity, and the refresh lock's
	// existing no-follow/ownership validation accepts it.
	fakeRun := filepath.Join(t.TempDir(), "run")
	runtimePath := filepath.Join(fakeRun, filepath.FromSlash(runtimeName))
	if _, err := os.Lstat(runtimePath); !os.IsNotExist(err) {
		t.Fatalf("fresh-boot fixture unexpectedly has %s: %v", runtimePath, err)
	}
	if err := os.MkdirAll(runtimePath, os.FileMode(parsedMode)); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(runtimePath, filepath.Base(config.Paths.Lock))
	unlock, err := acquireOperationLock(lockPath, "fresh-boot mirror refresh")
	if err != nil {
		t.Fatalf("systemd-created runtime directory failed lock trust validation: %v", err)
	}
	unlock()
}

func TestOperationLockUsesKernelLifetimeAndDoesNotConflictAcrossPaths(t *testing.T) {
	root := t.TempDir()
	adoptionPath := filepath.Join(root, "locks", "adoption.lock")
	refreshPath := filepath.Join(root, "locks", "refresh.lock")

	releaseAdoption, err := acquireOperationLock(adoptionPath, "APT mirror adoption")
	if err != nil {
		t.Fatal(err)
	}
	defer releaseAdoption()

	if _, err := acquireOperationLock(adoptionPath, "APT mirror adoption"); err == nil ||
		!strings.Contains(err.Error(), "adoption is already running") {
		t.Fatalf("second adoption lock err=%v", err)
	}

	// Install holds the adoption lock while its nested Refresh takes the
	// separate refresh lock. Prove those descriptors do not self-deadlock.
	releaseRefresh, err := acquireOperationLock(refreshPath, "APT mirror refresh")
	if err != nil {
		t.Fatalf("nested refresh lock conflicted with adoption lock: %v", err)
	}
	releaseRefresh()

	// flock is descriptor/process scoped: closing the owner releases it without
	// deleting a marker, which is the same stale-lock behavior after a crash.
	releaseAdoption()
	replacement, err := acquireOperationLock(adoptionPath, "APT mirror adoption")
	if err != nil {
		t.Fatalf("closed adoption descriptor left a stale lock: %v", err)
	}
	replacement()
}

func TestPackageManagerRecordLocksFailClosedWithOwnerPID(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		filepath.Join(root, "lock-frontend"),
		filepath.Join(root, "dpkg-lock"),
		filepath.Join(root, "lists-lock"),
		filepath.Join(root, "archives-lock"),
	}
	for _, path := range paths {
		if err := os.WriteFile(path, nil, 0o640); err != nil {
			t.Fatal(err)
		}
	}

	command := exec.Command(os.Args[0], "-test.run=^TestPackageManagerRecordLockHelper$")
	command.Env = append(os.Environ(),
		"PC_CONTROLLER_APT_LOCK_HELPER=1",
		"PC_CONTROLLER_APT_LOCK_PATH="+paths[1],
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatalf("record-lock helper did not become ready: %q", scanner.Text())
	}

	if release, err := acquirePackageManagerLocks(paths, uint32(os.Geteuid()), nil); err == nil {
		release()
		t.Fatal("acquired APT/dpkg locks while a fixture process owned one")
	} else if !strings.Contains(err.Error(), paths[1]) || !strings.Contains(err.Error(), fmt.Sprint(command.Process.Pid)) {
		t.Fatalf("busy lock error omitted path or owner pid: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	waited = true

	release, err := acquirePackageManagerLocks(paths, uint32(os.Geteuid()), nil)
	if err != nil {
		t.Fatalf("record locks stayed busy after owner exit: %v", err)
	}
	release()
	release() // release is intentionally idempotent for joined rollback paths.
}

func TestPackageManagerRecordLockHelper(t *testing.T) {
	if os.Getenv("PC_CONTROLLER_APT_LOCK_HELPER") != "1" {
		return
	}
	file, err := os.OpenFile(os.Getenv("PC_CONTROLLER_APT_LOCK_PATH"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err := unix.FcntlFlock(file.Fd(), unix.F_SETLK, &lock); err != nil {
		t.Fatal(err)
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}
