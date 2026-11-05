package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"pccontroller.local/controller/internal/control"
)

func TestStartupFrameContinuesUntilTerminalSizeArrives(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	for _, width := range []int{0, 34, 80, 120} {
		model := NewWithOptions(runtime, nil, Options{StartupWidth: width, DisableWelcome: true})
		if model.ready {
			t.Fatal("model declared ready before terminal size arrived")
		}
		wantWidth := width
		if wantWidth == 0 {
			wantWidth = 80
		}
		if got, want := model.View(), StartupFrame(model.prefs.AppTitle, "Loading terminal controls…", wantWidth); got != want {
			t.Fatalf("width %d: first TUI paint did not retain shared loading frame", width)
		}
	}
}

func TestStartupFrameUsesSharedColorsAndFitsNarrowTerminals(t *testing.T) {
	for _, width := range []int{8, 20, 34, 60, 80, 120} {
		frame := StartupFrame("Workshop controller", "Starting terminal interface…", width)
		if !strings.Contains(frame, "\x1b[") {
			t.Fatal("startup frame lost color")
		}
		for _, line := range strings.Split(frame, "\n") {
			if ansi.StringWidth(line) > width {
				t.Fatalf("frame exceeds %d columns: %q", width, line)
			}
		}
	}
	frame := ansi.Strip(StartupFrame("Workshop controller", "Starting terminal interface…", 80))
	if !strings.Contains(frame, "─┤  PC   ├─") {
		t.Fatal("chip monogram escaped its fixed slot")
	}
	for _, caption := range []string{"Workshop controller", "TERMINAL", "Starting terminal interface"} {
		if !strings.Contains(frame, caption) {
			t.Fatalf("missing %q", caption)
		}
	}
	if strings.Contains(frame, "%") || strings.Contains(frame, "Connected") {
		t.Fatal("startup invents progress or connection")
	}
}

func TestStartupFrameSanitizesCaptionsAndDoesNotChangeGlobalProfile(t *testing.T) {
	profile := lipgloss.ColorProfile()
	frame := ansi.Strip(StartupFrame("\x1b[31mMy\ncontroller", "Loading\tterminal", 80))
	if !strings.Contains(frame, "My controller") || !strings.Contains(frame, "Loading terminal") {
		t.Fatal("unsafe caption retained")
	}
	if lipgloss.ColorProfile() != profile {
		t.Fatal("startup changed shared style")
	}
}
