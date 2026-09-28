//go:build !windows

package programmer

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type unixProgrammerProcessTree struct{}

func prepareProgrammerProcessTree(command *exec.Cmd) (programmerProcessTree, error) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return unixProgrammerProcessTree{}, nil
}

func (unixProgrammerProcessTree) Attach(*os.Process) error { return nil }

func (unixProgrammerProcessTree) Terminate(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return process.Kill()
}

func (unixProgrammerProcessTree) Close() error { return nil }
