//go:build linux

package portowner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxEnumeratorFindsProcessHoldingDevice(t *testing.T) {
	root := t.TempDir()
	deviceDirectory := filepath.Join(root, "dev")
	procRoot := filepath.Join(root, "proc")
	processDirectory := filepath.Join(procRoot, "123")
	if err := os.MkdirAll(filepath.Join(processDirectory, "fd"), 0o755); err != nil {
		t.Fatal(err)
	}
	device := filepath.Join(deviceDirectory, "ttyUSB0")
	if err := os.MkdirAll(deviceDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(device, []byte("device"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(device, filepath.Join(processDirectory, "fd", "4")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(processDirectory, "comm"), []byte("serial-console\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/opt/serial-console", filepath.Join(processDirectory, "exe")); err != nil {
		t.Fatal(err)
	}
	// Field 22 is the twentieth field after the closing command parenthesis.
	if err := os.WriteFile(filepath.Join(processDirectory, "stat"), []byte("123 (serial console) S 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 250\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procRoot, "stat"), []byte("cpu 1 2 3\nbtime 1000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner, found, err := findLinuxOwner(context.Background(), procRoot, device)
	if err != nil || !found {
		t.Fatalf("findLinuxOwner found=%t err=%v", found, err)
	}
	if owner.PID != 123 || owner.Name != "serial-console" || owner.Executable != "/opt/serial-console" || owner.ProcessStartTime == 0 {
		t.Fatalf("owner=%+v", owner)
	}
}

func TestLinuxActionsVerifyIdentityBeforeSignals(t *testing.T) {
	procRoot := t.TempDir()
	processDirectory := filepath.Join(procRoot, "321")
	if err := os.MkdirAll(processDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/opt/external-console", filepath.Join(processDirectory, "exe")); err != nil {
		t.Fatal(err)
	}
	var openedPID int
	var gotFD int
	var gotSignal unix.Signal
	closed := false
	actions := linuxActions{
		procRoot: procRoot,
		pidfdOpen: func(pid, flags int) (int, error) {
			if flags != 0 {
				t.Fatalf("pidfd flags=%d", flags)
			}
			openedPID = pid
			return 41, nil
		},
		pidfdSendSignal: func(fd int, signal unix.Signal, _ *unix.Siginfo, flags int) error {
			if flags != 0 {
				t.Fatalf("pidfd signal flags=%d", flags)
			}
			gotFD, gotSignal = fd, signal
			return nil
		},
		close: func(fd int) error {
			if fd != 41 {
				t.Fatalf("close fd=%d", fd)
			}
			closed = true
			return nil
		},
	}
	owner := Owner{PID: 321, Name: "external-console", Executable: "/opt/external-console"}
	if err := actions.Terminate(context.Background(), owner, "TERMINATE 321"); err != nil {
		t.Fatal(err)
	}
	if openedPID != 321 || gotFD != 41 || gotSignal != unix.SIGKILL || !closed {
		t.Fatalf("pidfd open pid=%d signal fd=%d value=%v closed=%t", openedPID, gotFD, gotSignal, closed)
	}
	owner.Executable = "/opt/replaced"
	if err := actions.RequestGracefulClose(context.Background(), owner); err == nil {
		t.Fatal("changed executable identity was not rejected")
	}
}

func TestLinuxActionsNeverFallBackToPIDSignal(t *testing.T) {
	owner := Owner{PID: 654, Name: "reused", Executable: "/opt/reused"}
	actions := linuxActions{
		procRoot: t.TempDir(),
		pidfdOpen: func(int, int) (int, error) {
			return -1, unix.ENOSYS
		},
		pidfdSendSignal: func(int, unix.Signal, *unix.Siginfo, int) error {
			t.Fatal("signal attempted without a pinned process")
			return nil
		},
	}
	if err := actions.RequestGracefulClose(context.Background(), owner); err == nil {
		t.Fatal("missing pidfd support was not reported")
	}
}

func TestLinuxSerialPathAcceptsDeviceAliasesOnly(t *testing.T) {
	for _, value := range []string{"ttyUSB0", "/dev/ttyACM0", "/dev/serial/by-id/controller"} {
		if _, err := linuxSerialPath(value); err != nil {
			t.Fatalf("linuxSerialPath(%q): %v", value, err)
		}
	}
	for _, value := range []string{"tcp://127.0.0.1:9000", "/tmp/ttyUSB0", "/dev/null"} {
		if _, err := linuxSerialPath(value); err == nil {
			t.Fatalf("linuxSerialPath(%q) unexpectedly succeeded", value)
		}
	}
}
