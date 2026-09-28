//go:build linux

package main

import (
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/hostui"
)

func TestLinuxSurfaceLauncherPrefersPtyxisWithoutShellText(t *testing.T) {
	spec := visibleSurfaceSpec{
		Surface: hostui.SurfaceTUI, Executable: "/opt/pccontroller/controller",
		Arguments: []string{"--config", "/home/asus/.config/PCController/config.json", "tui", "--ipc-addr", "127.0.0.1:8787"},
		Title:     "PCController", Directory: "/opt/pccontroller/runtime",
	}
	plans := linuxTerminalPlans(spec)
	if len(plans) != 4 || plans[0].executable != "ptyxis" || plans[0].backend != "ptyxis" {
		t.Fatalf("plans=%#v", plans)
	}
	separator := -1
	for index, argument := range plans[0].arguments {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 {
		t.Fatalf("Ptyxis plan has no argument boundary: %#v", plans[0].arguments)
	}
	want := append([]string{spec.Executable}, spec.Arguments...)
	if got := plans[0].arguments[separator+1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("child argv=%#v want=%#v", got, want)
	}
}

func TestLinuxSurfaceLauncherRequiresInheritedGraphicalSession(t *testing.T) {
	if haveInheritedGraphicalSession([]string{"PATH=/usr/bin", "XDG_RUNTIME_DIR=/run/user/1000"}) {
		t.Fatal("headless environment reported graphical")
	}
	if !haveInheritedGraphicalSession([]string{"WAYLAND_DISPLAY=wayland-0"}) ||
		!haveInheritedGraphicalSession([]string{"DISPLAY=:0"}) {
		t.Fatal("inherited display was not recognized")
	}
}
