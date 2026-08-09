//go:build windows

package installer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

func launchUninstallHelper(_ context.Context, helperPath, planPath string) error {
	command := exec.Command(helperPath, uninstallHelperCommand, planPath)
	command.Env = os.Environ()
	command.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func waitForParentExit(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("open installed host process %d: %w", pid, err)
		}
		state, waitErr := windows.WaitForSingleObject(handle, 0)
		closeErr := windows.CloseHandle(handle)
		if waitErr != nil {
			return fmt.Errorf("query installed host process %d: %w", pid, waitErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close installed host process %d: %w", pid, closeErr)
		}
		switch state {
		case windows.WAIT_OBJECT_0:
			return nil
		case uint32(windows.WAIT_TIMEOUT):
			// The process object can remain open after exit; its signaled state,
			// not handle availability, is the native liveness contract.
		default:
			return fmt.Errorf("wait for installed host process %d returned 0x%08X", pid, state)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for the installed host to exit")
		case <-ticker.C:
		}
	}
}

func scheduleHelperRemoval(path string) error {
	value, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(value, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
}
