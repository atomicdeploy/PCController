//go:build windows

package artifacts

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"
)

type platformHelperLauncher struct{}

func (platformHelperLauncher) Launch(_ context.Context, helperPath, journalPath string) error {
	command := exec.Command(helperPath, selfUpdateHelperCommand, journalPath)
	command.Env = withoutSelfUpdateEnvironment(os.Environ())
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func platformHelperExtension() string { return ".helper.exe" }

func replaceExecutable(source, destination string) error {
	from, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				return nil
			}
			// Access denied proves that the PID still exists.
		} else {
			_ = windows.CloseHandle(handle)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("timed out waiting for the current host to exit")
		case <-ticker.C:
		}
	}
}
