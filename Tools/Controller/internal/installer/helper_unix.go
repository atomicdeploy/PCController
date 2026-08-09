//go:build !windows

package installer

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func launchUninstallHelper(_ context.Context, helperPath, planPath string) error {
	command := exec.Command(helperPath, uninstallHelperCommand, planPath)
	command.Env = os.Environ()
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func parentProcessIdentity(pid int) (string, error) { return "pid:" + strconv.Itoa(pid), nil }

func waitForParentExit(ctx context.Context, pid int, expectedIdentity string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		identity, identityErr := parentProcessIdentity(pid)
		if identityErr != nil {
			return identityErr
		}
		if identity != expectedIdentity {
			return nil
		}
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
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

func scheduleHelperRemoval(path string) error { return os.Remove(path) }
