//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"

	"pccontroller.local/controller/internal/hostui"
)

type windowsVisibleSurfacePlatform struct{}

func newVisibleSurfacePlatform() visibleSurfacePlatform {
	return windowsVisibleSurfacePlatform{}
}

func (windowsVisibleSurfacePlatform) Start(
	ctx context.Context,
	spec visibleSurfaceSpec,
) visibleSurfaceStart {
	if err := ctx.Err(); err != nil {
		return visibleSurfaceStart{Reason: err.Error()}
	}
	var sessionID uint32
	if err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &sessionID); err != nil {
		return visibleSurfaceStart{Reason: "resolve current Windows session: " + err.Error()}
	}
	if sessionID == 0 || sessionID == 0xffffffff {
		return visibleSurfaceStart{Reason: "coordinator is not running in an interactive Windows user session"}
	}
	if spec.Surface == hostui.SurfaceWebUI {
		return startDetachedSurfaceCommand(
			"windows-url-handler",
			exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", spec.URL),
			spec,
		)
	}
	if terminal, err := exec.LookPath("wt.exe"); err == nil {
		arguments := []string{
			"-w", "new", "new-tab", "--title", spec.Title, "--", spec.Executable,
		}
		arguments = append(arguments, spec.Arguments...)
		result := startDetachedSurfaceCommand(
			"windows-terminal", exec.Command(terminal, arguments...), spec,
		)
		if result.Accepted {
			return result
		}
	}
	command := exec.Command(spec.Executable, spec.Arguments...)
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	return startDetachedSurfaceCommand("windows-console", command, spec)
}

func (platform windowsVisibleSurfacePlatform) Focus(
	ctx context.Context,
	spec visibleSurfaceSpec,
	_ hostui.AppInstance,
) visibleSurfaceStart {
	if spec.Surface == hostui.SurfaceWebUI {
		return platform.Start(ctx, spec)
	}
	return visibleSurfaceStart{
		Reason: "the live TUI is known, but safe Windows terminal focus is unavailable",
	}
}

func startDetachedSurfaceCommand(
	backend string,
	command *exec.Cmd,
	spec visibleSurfaceSpec,
) visibleSurfaceStart {
	if command == nil {
		return visibleSurfaceStart{Backend: backend, Reason: "surface process is unavailable"}
	}
	command.Dir = spec.Directory
	command.Env = append([]string(nil), spec.Environment...)
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	if err := command.Start(); err != nil {
		return visibleSurfaceStart{Backend: backend, Reason: err.Error()}
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return visibleSurfaceStart{Backend: backend, LauncherProcessID: pid, Reason: err.Error()}
	}
	return visibleSurfaceStart{Backend: backend, LauncherProcessID: pid, Accepted: true}
}
