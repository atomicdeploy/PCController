//go:build windows

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

type platformHelperLauncher struct{}

const windowsStillActive = 259

var setConsoleCtrlHandler = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetConsoleCtrlHandler")

// PrepareSelfUpdateHelperProcess prevents a Ctrl+C intended to finish the
// outgoing interactive host from terminating the transaction coordinator
// that is waiting to replace it. The replacement child is started with this
// flag temporarily cleared so normal Ctrl+C handling is not inherited.
func PrepareSelfUpdateHelperProcess() error { return setConsoleCtrlIgnored(true) }

func setConsoleCtrlIgnored(ignored bool) error {
	value := uintptr(0)
	if ignored {
		value = 1
	}
	result, _, callErr := setConsoleCtrlHandler.Call(0, value)
	if result != 0 {
		return nil
	}
	return fmt.Errorf("configure self-update helper console control handling: %w", callErr)
}

func platformStartReplacementProcess(command *exec.Cmd) error {
	if err := setConsoleCtrlIgnored(false); err != nil {
		return err
	}
	startErr := command.Start()
	restoreErr := setConsoleCtrlIgnored(true)
	if startErr != nil {
		return startErr
	}
	if restoreErr != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return restoreErr
	}
	return nil
}

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
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err == nil {
		return nil
	} else if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return err
	}

	// Windows may deny MOVEFILE_REPLACE_EXISTING for a recently executed PE
	// even after every mapping has closed. Preserve the destination under a
	// unique same-directory tombstone, publish into the now-empty canonical
	// path, and restore it if publishing fails. A genuinely live secondary
	// process still causes the first move to fail safely.
	tombstoneFile, err := os.CreateTemp(filepath.Dir(destination), ".controller-replace-*.old")
	if err != nil {
		return err
	}
	tombstone := tombstoneFile.Name()
	if err := tombstoneFile.Close(); err != nil {
		_ = os.Remove(tombstone)
		return err
	}
	if err := os.Remove(tombstone); err != nil {
		return err
	}
	tombstonePath, err := windows.UTF16PtrFromString(tombstone)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(to, tombstonePath, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		restoreErr := windows.MoveFileEx(tombstonePath, to, windows.MOVEFILE_WRITE_THROUGH)
		if restoreErr != nil {
			return errors.Join(err, fmt.Errorf("restore executable after failed publish: %w", restoreErr))
		}
		return err
	}
	_ = os.Remove(tombstone)
	return nil
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
			var exitCode uint32
			queryErr := windows.GetExitCodeProcess(handle, &exitCode)
			_ = windows.CloseHandle(handle)
			if queryErr != nil {
				return fmt.Errorf("query current host process state: %w", queryErr)
			}
			if exitCode != windowsStillActive {
				return nil
			}
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
