//go:build windows

package programmer

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsProgrammerProcessTree struct {
	mu  sync.Mutex
	job windows.Handle
}

func prepareProgrammerProcessTree(*exec.Cmd) (programmerProcessTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)),
		uint32(unsafe.Sizeof(limits)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &windowsProgrammerProcessTree{job: job}, nil
}

func (tree *windowsProgrammerProcessTree) Attach(process *os.Process) error {
	if process == nil {
		return os.ErrProcessDone
	}
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.job == 0 {
		return os.ErrProcessDone
	}
	var assignErr error
	err := process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(tree.job, windows.Handle(handle))
	})
	if err != nil {
		return err
	}
	return assignErr
}

func (tree *windowsProgrammerProcessTree) Terminate(process *os.Process) error {
	tree.mu.Lock()
	job := tree.job
	tree.mu.Unlock()
	if job != 0 {
		if err := windows.TerminateJobObject(job, 1); err == nil {
			return nil
		}
	}
	if process == nil {
		return os.ErrProcessDone
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func (tree *windowsProgrammerProcessTree) Close() error {
	tree.mu.Lock()
	defer tree.mu.Unlock()
	if tree.job == 0 {
		return nil
	}
	err := windows.CloseHandle(tree.job)
	tree.job = 0
	return err
}
