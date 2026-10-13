//go:build windows

package artifacts

import (
	"context"
	"os"
	"os/exec"
	"reflect"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"pccontroller.local/controller/internal/consolewindow"
)

func TestBackgroundReplacementFlagsPreserveTUIAndSharedTerminals(t *testing.T) {
	for _, test := range []struct {
		args                []string
		interactive, hidden bool
		soleOwner           bool
	}{
		{[]string{"web", "--no-open", "--no-tray"}, false, true, false},
		{[]string{"web", "--no-open", "--no-tray"}, true, true, true},
		{[]string{"web", "--no-open", "--no-tray"}, true, false, false},
		{[]string{"web"}, true, false, true},
		{nil, false, false, true},
		{[]string{"--config", "custom.json", "tui"}, false, false, true},
	} {
		command := exec.Command("controller.exe")
		command.SysProcAttr = &syscall.SysProcAttr{}
		configureReplacementConsole(command, test.args, consolewindow.State{InteractiveStreams: test.interactive, SoleOwner: test.soleOwner})
		if got := command.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW != 0; got != test.hidden {
			t.Fatalf("args=%q interactive=%v hidden=%v", test.args, test.interactive, got)
		}
	}
}

func TestWindowsReplacementLaunchClearsConsoleHandlerForCandidateAndRollback(t *testing.T) {
	oldSetConsoleCtrlIgnored := setReplacementConsoleCtrlIgnored
	oldStartCommand := startReplacementCommand
	t.Cleanup(func() {
		setReplacementConsoleCtrlIgnored = oldSetConsoleCtrlIgnored
		startReplacementCommand = oldStartCommand
	})

	events := make([]string, 0, 3)
	setReplacementConsoleCtrlIgnored = func(ignored bool) error {
		if ignored {
			events = append(events, "handler=ignored")
		} else {
			events = append(events, "handler=normal")
		}
		return nil
	}
	startReplacementCommand = func(command *exec.Cmd) error {
		events = append(events, "start")
		return command.Start()
	}

	assertLaunchOrder := func(label string, launch func() error) {
		t.Helper()
		events = events[:0]
		if err := launch(); err != nil {
			t.Fatalf("%s launch: %v", label, err)
		}
		want := []string{"handler=normal", "start", "handler=ignored"}
		if !reflect.DeepEqual(events, want) {
			t.Fatalf("%s handler ordering = %v, want %v", label, events, want)
		}
	}

	command := func() *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=^$")
	}
	assertLaunchOrder("candidate", func() error {
		child := command()
		if err := platformStartReplacementProcess(child); err != nil {
			return err
		}
		return child.Process.Release()
	})
	assertLaunchOrder("rollback", func() error {
		return launchRestoredHost(selfUpdateJournal{
			CurrentPath:      os.Args[0],
			Arguments:        []string{"-test.run=^$"},
			WorkingDirectory: t.TempDir(),
		})
	})
}

func TestWaitForProcessExitRecognizesTerminatedProcessWithRetainedHandle(t *testing.T) {
	command := exec.Command("cmd.exe", "/d", "/c", "exit", "/b", "0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	if err := waitForProcessExit(context.Background(), pid, time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("terminated process detection took %s", elapsed)
	}
}
