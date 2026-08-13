//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"pccontroller.local/controller/internal/hostui"
)

type linuxVisibleSurfacePlatform struct{}

type linuxTerminalPlan struct {
	backend    string
	executable string
	arguments  []string
}

func newVisibleSurfacePlatform() visibleSurfacePlatform {
	return linuxVisibleSurfacePlatform{}
}

func (linuxVisibleSurfacePlatform) Start(
	ctx context.Context,
	spec visibleSurfaceSpec,
) visibleSurfaceStart {
	if err := ctx.Err(); err != nil {
		return visibleSurfaceStart{Reason: err.Error()}
	}
	if os.Geteuid() == 0 {
		return visibleSurfaceStart{Reason: "visible Linux surfaces require a non-root graphical-session coordinator"}
	}
	if !haveInheritedGraphicalSession(spec.Environment) {
		return visibleSurfaceStart{Reason: "coordinator did not inherit DISPLAY or WAYLAND_DISPLAY from a graphical session"}
	}
	if spec.Surface == hostui.SurfaceWebUI {
		opener, err := exec.LookPath("xdg-open")
		if err != nil {
			return visibleSurfaceStart{Backend: "xdg-open", Reason: "xdg-open is unavailable"}
		}
		return startLinuxSurfaceCommand(
			"xdg-open", exec.Command(opener, spec.URL), spec,
		)
	}
	for _, plan := range linuxTerminalPlans(spec) {
		resolved, err := exec.LookPath(plan.executable)
		if err != nil {
			continue
		}
		result := startLinuxSurfaceCommand(
			plan.backend, exec.Command(resolved, plan.arguments...), spec,
		)
		if result.Accepted {
			return result
		}
	}
	return visibleSurfaceStart{Reason: "no supported graphical terminal is available (tried Ptyxis, GNOME Terminal, Konsole, and xterm)"}
}

func (platform linuxVisibleSurfacePlatform) Focus(
	ctx context.Context,
	spec visibleSurfaceSpec,
	_ hostui.AppInstance,
) visibleSurfaceStart {
	if spec.Surface == hostui.SurfaceWebUI {
		return platform.Start(ctx, spec)
	}
	return visibleSurfaceStart{
		Reason: "the live TUI is known, but compositor-safe terminal focus is unavailable",
	}
}

func linuxTerminalPlans(spec visibleSurfaceSpec) []linuxTerminalPlan {
	command := append([]string{spec.Executable}, spec.Arguments...)
	ptyxis := []string{
		"--new-window", "--title=" + spec.Title, "--working-directory=" + spec.Directory, "--",
	}
	ptyxis = append(ptyxis, command...)
	gnome := []string{
		"--window", "--title=" + spec.Title, "--working-directory=" + spec.Directory, "--",
	}
	gnome = append(gnome, command...)
	konsole := []string{"--separate", "--workdir", spec.Directory, "-p", "tabtitle=" + spec.Title, "-e"}
	konsole = append(konsole, command...)
	xterm := []string{"-T", spec.Title, "-e"}
	xterm = append(xterm, command...)
	return []linuxTerminalPlan{
		{backend: "ptyxis", executable: "ptyxis", arguments: ptyxis},
		{backend: "gnome-terminal", executable: "gnome-terminal", arguments: gnome},
		{backend: "konsole", executable: "konsole", arguments: konsole},
		{backend: "xterm", executable: "xterm", arguments: xterm},
	}
}

func haveInheritedGraphicalSession(environment []string) bool {
	for _, entry := range environment {
		name, value, found := strings.Cut(entry, "=")
		if found && value != "" && (name == "DISPLAY" || name == "WAYLAND_DISPLAY") {
			return true
		}
	}
	return false
}

func startLinuxSurfaceCommand(
	backend string,
	command *exec.Cmd,
	spec visibleSurfaceSpec,
) visibleSurfaceStart {
	command.Dir = spec.Directory
	command.Env = append([]string(nil), spec.Environment...)
	command.Stdin, command.Stdout, command.Stderr = nil, nil, nil
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return visibleSurfaceStart{Backend: backend, Reason: err.Error()}
	}
	pid := command.Process.Pid
	if err := command.Process.Release(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return visibleSurfaceStart{Backend: backend, LauncherProcessID: pid, Reason: err.Error()}
	}
	return visibleSurfaceStart{Backend: backend, LauncherProcessID: pid, Accepted: true}
}
