//go:build windows

package artifacts

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

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
