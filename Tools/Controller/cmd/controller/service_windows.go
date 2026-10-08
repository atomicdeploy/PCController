//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

func runPlatformServiceCommand(command string, args []string, configPath string, stdout, stderr io.Writer) error {
	switch command {
	case "install", "repair":
		options, err := parseServiceRuntimeOptions(command, args, configPath, stderr)
		if err != nil {
			return err
		}
		return reconcileWindowsService(command == "repair", options, stdout)
	case "run":
		options, err := parseServiceRuntimeOptions(command, args, configPath, stderr)
		if err != nil {
			return err
		}
		isService, err := svc.IsWindowsService()
		if err != nil {
			return fmt.Errorf("detect Windows service context: %w", err)
		}
		if !isService {
			return errors.New("service run is reserved for the Windows Service Control Manager")
		}
		return svc.Run(windowsServiceName, &controllerWindowsService{options: options})
	case "status":
		if len(args) != 0 {
			return errors.New("usage: controller service status")
		}
		return printWindowsServiceStatus(stdout)
	case "start", "stop", "restart", "remove":
		if len(args) != 0 {
			return fmt.Errorf("usage: controller service %s", command)
		}
		return controlWindowsService(command, stdout)
	default:
		return fmt.Errorf("unknown service command %q", command)
	}
}

func reconcileWindowsService(repair bool, options serviceRuntimeOptions, output io.Writer) error {
	if info, err := os.Stat(options.BinaryPath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("service binary must be an existing regular file: %s", options.BinaryPath)
	}
	if info, err := os.Stat(options.ConfigPath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("service config must be an existing regular file: %s", options.ConfigPath)
	}
	if err := os.MkdirAll(options.DataDir, 0o750); err != nil {
		return fmt.Errorf("create service data directory: %w", err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to Windows service manager (run elevated): %w", err)
	}
	defer manager.Disconnect()
	config := mgr.Config{
		DisplayName:      windowsServiceDisplayName,
		Description:      "Owns the PCController board session and authenticated loopback API for Pealayer.",
		StartType:        mgr.StartAutomatic,
		DelayedAutoStart: true,
		ErrorControl:     mgr.ErrorNormal,
		ServiceStartName: `NT SERVICE\PCController`,
		SidType:          windows.SERVICE_SID_TYPE_UNRESTRICTED,
	}
	service, openErr := manager.OpenService(windowsServiceName)
	created := false
	if openErr != nil {
		if repair {
			fmt.Fprintln(output, "PCController service was absent; creating it")
		}
		service, err = manager.CreateService(windowsServiceName, options.BinaryPath, config, serviceImageArguments(options)...)
		created = err == nil
	} else if !repair {
		service.Close()
		return errors.New("PCController service already exists; use 'controller service repair'")
	} else {
		config.BinaryPathName = windowsServiceCommandLine(options)
		err = service.UpdateConfig(config)
	}
	if err != nil {
		return fmt.Errorf("reconcile Windows service (run elevated): %w", err)
	}
	defer service.Close()
	if err := grantWindowsServicePaths(options); err != nil {
		if created {
			_ = service.Delete()
		}
		return err
	}
	recovery := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	if err := service.SetRecoveryActions(recovery, 24*60*60); err != nil {
		return fmt.Errorf("set service recovery actions: %w", err)
	}
	if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("include non-crash service failures in recovery: %w", err)
	}
	if options.Start {
		status, queryErr := service.Query()
		if queryErr != nil {
			return queryErr
		}
		if status.State == svc.Stopped {
			if err := service.Start(); err != nil {
				return fmt.Errorf("start PCController service: %w", err)
			}
			if _, err := waitForWindowsService(service, svc.Running, 25*time.Second); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(output, "PCController service reconciled: binary=%s config=%s data=%s delayed_auto_start=true\n", options.BinaryPath, options.ConfigPath, options.DataDir)
	return nil
}

func windowsServiceCommandLine(options serviceRuntimeOptions) string {
	parts := []string{windows.EscapeArg(options.BinaryPath)}
	for _, arg := range serviceImageArguments(options) {
		parts = append(parts, windows.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func grantWindowsServicePaths(options serviceRuntimeOptions) error {
	grants := []struct {
		path, permission string
	}{
		{options.ConfigPath, `NT SERVICE\PCController:(M)`},
		{filepath.Dir(options.ConfigPath), `NT SERVICE\PCController:(OI)(CI)(M)`},
		{options.DataDir, `NT SERVICE\PCController:(OI)(CI)(M)`},
		{filepath.Dir(options.BinaryPath), `NT SERVICE\PCController:(OI)(CI)(RX)`},
	}
	for _, grant := range grants {
		command := exec.Command("icacls.exe", grant.path, "/grant", grant.permission)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("grant service access to %s: %w: %s", grant.path, err, strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func printWindowsServiceStatus(output io.Writer) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("open PCController service: %w", err)
	}
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return err
	}
	config, err := service.Config()
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "name=%s state=%s pid=%d start=automatic delayed=%t account=%s binary=%s\n", windowsServiceName, windowsServiceState(status.State), status.ProcessId, config.DelayedAutoStart, config.ServiceStartName, config.BinaryPathName)
	return nil
}

func controlWindowsService(command string, output io.Writer) error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	service, err := manager.OpenService(windowsServiceName)
	if err != nil {
		return fmt.Errorf("open PCController service: %w", err)
	}
	defer service.Close()
	switch command {
	case "start":
		status, err := service.Query()
		if err == nil && status.State == svc.Stopped {
			err = service.Start()
		}
		if err == nil {
			status, err = waitForWindowsService(service, svc.Running, 25*time.Second)
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "PCController service %s (pid %d)\n", windowsServiceState(status.State), status.ProcessId)
	case "stop":
		status, err := stopWindowsService(service)
		if err != nil {
			return err
		}
		fmt.Fprintln(output, "PCController service", windowsServiceState(status.State))
	case "restart":
		if _, err := stopWindowsService(service); err != nil {
			return err
		}
		if err := service.Start(); err != nil {
			return err
		}
		status, err := waitForWindowsService(service, svc.Running, 25*time.Second)
		if err != nil {
			return err
		}
		fmt.Fprintf(output, "PCController service %s (pid %d)\n", windowsServiceState(status.State), status.ProcessId)
	case "remove":
		if _, err := stopWindowsService(service); err != nil {
			return err
		}
		if err := service.Delete(); err != nil {
			return err
		}
		fmt.Fprintln(output, "PCController service removed")
	}
	return nil
}

func stopWindowsService(service *mgr.Service) (svc.Status, error) {
	status, err := service.Query()
	if err != nil || status.State == svc.Stopped {
		return status, err
	}
	if status.State != svc.StopPending {
		if _, err = service.Control(svc.Stop); err != nil {
			return status, err
		}
	}
	return waitForWindowsService(service, svc.Stopped, 25*time.Second)
}

func waitForWindowsService(service *mgr.Service, target svc.State, timeout time.Duration) (svc.Status, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, err := service.Query()
		if err != nil {
			return status, err
		}
		if status.State == target {
			return status, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	status, _ := service.Query()
	return status, fmt.Errorf("PCController service did not reach %s within %s (current %s)", windowsServiceState(target), timeout, windowsServiceState(status.State))
}

func windowsServiceState(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start-pending"
	case svc.StopPending:
		return "stop-pending"
	case svc.Running:
		return "running"
	case svc.Paused:
		return "paused"
	default:
		return fmt.Sprintf("state-%d", state)
	}
}

type controllerWindowsService struct {
	options serviceRuntimeOptions
}

func (service *controllerWindowsService) Execute(_ []string, requests <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending, CheckPoint: 1, WaitHint: 15000}
	if err := os.MkdirAll(service.options.DataDir, 0o750); err != nil {
		return true, 1
	}
	logPath := filepath.Join(service.options.DataDir, "service.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
	if err != nil {
		return true, 2
	}
	defer logFile.Close()
	logOutput := io.MultiWriter(logFile)
	fmt.Fprintf(logOutput, "%s service starting version=%s source_hash=%s\n", time.Now().Format(time.RFC3339), version, sourceHash)
	if err := os.Setenv("PCCONTROLLER_DATA_DIR", service.options.DataDir); err != nil {
		fmt.Fprintln(logOutput, "set data directory:", err)
		return true, 3
	}
	_ = os.Setenv("PCCONTROLLER_WINDOWS_SERVICE", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		done <- runWindowsServiceWeb(ctx, service.options, logOutput, func() { close(ready) })
	}()
	accepted := svc.AcceptStop | svc.AcceptShutdown
	startupTimer := time.NewTimer(15 * time.Second)
	defer startupTimer.Stop()
	for {
		select {
		case <-ready:
			statuses <- svc.Status{State: svc.Running, Accepts: accepted}
			goto running
		case err := <-done:
			if err != nil {
				fmt.Fprintf(logOutput, "%s service startup failed: %v\n", time.Now().Format(time.RFC3339), err)
			}
			return true, 4
		case <-startupTimer.C:
			cancel()
			fmt.Fprintln(logOutput, "service startup did not establish the primary API within 15 seconds")
			return true, 8
		case request, ok := <-requests:
			if !ok {
				return true, 7
			}
			if request.Cmd == svc.Stop || request.Cmd == svc.Shutdown {
				statuses <- svc.Status{State: svc.StopPending, CheckPoint: 1, WaitHint: 20000}
				cancel()
				select {
				case <-done:
					return false, 0
				case <-time.After(20 * time.Second):
					return true, 6
				}
			}
			if request.Cmd == svc.Interrogate {
				statuses <- request.CurrentStatus
			}
		}
	}

running:
	for {
		select {
		case err := <-done:
			if err != nil {
				fmt.Fprintf(logOutput, "%s service failed: %v\n", time.Now().Format(time.RFC3339), err)
				return true, 4
			}
			fmt.Fprintf(logOutput, "%s service stopped\n", time.Now().Format(time.RFC3339))
			return false, 0
		case request, ok := <-requests:
			if !ok {
				cancel()
				return true, 7
			}
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending, CheckPoint: 1, WaitHint: 20000}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						fmt.Fprintln(logOutput, "graceful shutdown:", err)
						return true, 5
					}
					return false, 0
				case <-time.After(20 * time.Second):
					fmt.Fprintln(logOutput, "graceful shutdown exceeded 20 seconds")
					return true, 6
				}
			}
		}
	}
}

func runWindowsServiceWeb(ctx context.Context, options serviceRuntimeOptions, output io.Writer, ready func()) error {
	store, err := appconfig.Open(options.ConfigPath)
	if err != nil {
		return err
	}
	if err := applyMeasurementTimingEnvironment(store); err != nil {
		return err
	}
	runtimeConfig, err := store.Runtime()
	if err != nil {
		return err
	}
	configurePrimaryIPC(runtimeConfig)
	args := []string{"--no-open", "--no-tray"}
	if strings.TrimSpace(options.Listen) != "" {
		args = append(args, "--listen", options.Listen)
	}
	return runWebWithContext(ctx, ready, args, output, output, store, hostui.AppAction{})
}
