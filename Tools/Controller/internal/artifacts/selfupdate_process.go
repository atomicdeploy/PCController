package artifacts

import (
	"context"
	"os"
	"os/exec"
)

// Lifecycle is the only distinction between a foreground host and SCM. File
// verification, replacement, health timeout, rollback and journals stay shared.
type updateProcess interface {
	PID() int
	Stop() error
	Wait() error
}

type updateServiceManager interface {
	Validate(name, executable string, pid int) error
	Stop(context.Context, string, string, int) error
	Start(context.Context, string, string) (updateProcess, error)
}

var serviceUpdates updateServiceManager = platformUpdateServices{}

type commandUpdateProcess struct{ command *exec.Cmd }

func (p commandUpdateProcess) PID() int    { return p.command.Process.Pid }
func (p commandUpdateProcess) Stop() error { return p.command.Process.Kill() }
func (p commandUpdateProcess) Wait() error { return p.command.Wait() }

func startUpdateProcess(ctx context.Context, journal selfUpdateJournal) (updateProcess, error) {
	if journal.WindowsService != "" {
		return serviceUpdates.Start(ctx, journal.WindowsService, journal.CurrentPath)
	}
	child := exec.Command(journal.CurrentPath, journal.Arguments...)
	child.Dir = journal.WorkingDirectory
	child.Env = selfUpdateEnvironment(os.Environ(), journal)
	inheritSelfUpdateIO(child)
	if err := platformStartReplacementProcess(child); err != nil {
		return nil, err
	}
	return commandUpdateProcess{child}, nil
}
