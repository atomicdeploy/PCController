//go:build windows

package artifacts

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type platformUpdateServices struct{}

// Open only our fixed service with the permissions the transaction needs. Never
// install, repair, reconfigure or elevate from a remotely supplied request.
func openUpdateService(name, executable string) (*mgr.Service, func(), error) {
	if name != "PCController" {
		return nil, nil, errors.New("unsupported self-update service")
	}
	managerHandle, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return nil, nil, err
	}
	manager := &mgr.Mgr{Handle: managerHandle}
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		manager.Disconnect()
		return nil, nil, err
	}
	handle, err := windows.OpenService(manager.Handle, namePtr,
		windows.SERVICE_QUERY_CONFIG|windows.SERVICE_QUERY_STATUS|windows.SERVICE_START|windows.SERVICE_STOP)
	if err != nil {
		manager.Disconnect()
		return nil, nil, err
	}
	service := &mgr.Service{Name: name, Handle: handle}
	close := func() { service.Close(); manager.Disconnect() }
	config, err := service.Config()
	if err != nil {
		close()
		return nil, nil, err
	}
	args, err := windows.DecomposeCommandLine(config.BinaryPathName)
	if err != nil || len(args) == 0 || !sameUpdateExecutable(args[0], executable) {
		close()
		return nil, nil, errors.New("SCM executable does not match the exact update owner")
	}
	return service, close, nil
}

func sameUpdateExecutable(left, right string) bool {
	left, err := filepath.Abs(left)
	if err != nil {
		return false
	}
	right, err = filepath.Abs(right)
	return err == nil && strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}

func (platformUpdateServices) Validate(name, executable string, pid int) error {
	service, close, err := openUpdateService(name, executable)
	if err != nil {
		return err
	}
	defer close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if pid <= 0 || status.State != svc.Running || status.ProcessId != uint32(pid) {
		return errors.New("SCM running PID does not match the exact update owner")
	}
	return nil
}

func (platformUpdateServices) Stop(ctx context.Context, name, executable string, pid int) error {
	service, close, err := openUpdateService(name, executable)
	if err != nil {
		return err
	}
	defer close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	if status.State == svc.Stopped {
		return nil
	}
	if pid <= 0 || status.ProcessId != uint32(pid) {
		return errors.New("refusing to stop a different service process")
	}
	if status.State != svc.StopPending {
		if _, err := service.Control(svc.Stop); err != nil {
			return err
		}
	}
	_, err = waitUpdateService(ctx, service, svc.Stopped, 30*time.Second)
	return err
}

func (platformUpdateServices) Start(ctx context.Context, name, executable string) (updateProcess, error) {
	service, close, err := openUpdateService(name, executable)
	if err != nil {
		return nil, err
	}
	defer close()
	status, err := service.Query()
	if err != nil {
		return nil, err
	}
	if status.State != svc.Stopped {
		return nil, errors.New("refusing to start over an existing service process")
	}
	if err := service.Start(); err != nil {
		return nil, err
	}
	status, err = waitUpdateService(ctx, service, svc.Running, 25*time.Second)
	if err != nil {
		// A failed startup must finish stopping before shared rollback touches
		// its executable. Do not force-kill an SCM-owned process.
		if current, queryErr := service.Query(); queryErr == nil && current.State != svc.Stopped {
			_, stopErr := service.Control(svc.Stop)
			_, waitErr := waitUpdateService(context.Background(), service, svc.Stopped, 30*time.Second)
			err = errors.Join(err, stopErr, waitErr)
		}
		return nil, err
	}
	if status.ProcessId == 0 {
		return nil, errors.New("SCM omitted replacement process identity")
	}
	return serviceUpdateProcess{name, executable, int(status.ProcessId)}, nil
}

func waitUpdateService(ctx context.Context, service *mgr.Service, state svc.State, timeout time.Duration) (svc.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		status, err := service.Query()
		if err != nil {
			return status, err
		}
		if status.State == state {
			return status, nil
		}
		if state == svc.Running && status.State == svc.Stopped {
			return status, fmt.Errorf("service exited during startup (code %d)", status.Win32ExitCode)
		}
		select {
		case <-ctx.Done():
			return status, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

type serviceUpdateProcess struct {
	name, executable string
	pid              int
}

func (p serviceUpdateProcess) PID() int { return p.pid }
func (p serviceUpdateProcess) Stop() error {
	return serviceUpdates.Stop(context.Background(), p.name, p.executable, p.pid)
}
func (p serviceUpdateProcess) Wait() error {
	for {
		service, close, err := openUpdateService(p.name, p.executable)
		if err != nil {
			return err
		}
		status, err := service.Query()
		close()
		if err != nil {
			return err
		}
		if status.State != svc.Running || status.ProcessId != uint32(p.pid) {
			return errors.New("replacement service stopped or changed process")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
