//go:build windows

package installer

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWaitForParentExitUsesSignaledStateWithRetainedHandle(t *testing.T) {
	command := exec.Command("cmd.exe", "/d", "/s", "/c", "exit 0")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pid := command.Process.Pid
	identity, err := parentProcessIdentity(pid)
	if err != nil {
		t.Fatal(err)
	}
	retained, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION,
		false,
		uint32(pid),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(retained)
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := waitForParentExit(context.Background(), pid, identity, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("signaled exited process took %s to detect", elapsed)
	}
}

func TestWaitForParentExitDoesNotFollowReusedPIDIdentity(t *testing.T) {
	command := exec.Command("cmd.exe", "/d", "/s", "/c", "ping -n 3 127.0.0.1 >nul")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	started := time.Now()
	if err := waitForParentExit(context.Background(), command.Process.Pid, "different-process-creation", time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("reused PID identity took %s to reject", elapsed)
	}
}
