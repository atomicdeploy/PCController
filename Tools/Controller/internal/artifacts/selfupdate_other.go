//go:build !windows

package artifacts

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
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

func platformHelperExtension() string { return ".helper" }

func replaceExecutable(source, destination string) error {
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return os.Chmod(destination, 0o755)
}

func waitForProcessExit(ctx context.Context, pid int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return nil
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
