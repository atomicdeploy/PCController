//go:build windows

package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestRouteLaunch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		wantMode launchMode
		wantArgs []string
	}{
		{name: "default GUI", wantMode: launchGUI},
		{
			name:     "legacy command remains GUI handoff",
			input:    []string{"/on"},
			wantMode: launchGUI,
			wantArgs: []string{"/on"},
		},
		{
			name:     "TUI subcommand",
			input:    []string{"tui", "--mode", "standalone"},
			wantMode: launchTUI,
			wantArgs: []string{"--mode", "standalone"},
		},
		{
			name:     "TUI flag",
			input:    []string{"--TUI", "--no-mouse"},
			wantMode: launchTUI,
			wantArgs: []string{"--no-mouse"},
		},
		{
			name:     "CLI subcommand",
			input:    []string{"cli", "--status", "--json"},
			wantMode: launchCLI,
			wantArgs: []string{"--status", "--json"},
		},
		{
			name:     "CTL alias",
			input:    []string{"/ctl", "--off"},
			wantMode: launchCLI,
			wantArgs: []string{"--off"},
		},
		{
			name:     "headless dashboard server",
			input:    []string{"serve", "--no-open"},
			wantMode: launchServe,
			wantArgs: []string{"--no-open"},
		},
		{
			name:     "headless server alias",
			input:    []string{"--serve", "--listen", "localhost:49123"},
			wantMode: launchServe,
			wantArgs: []string{"--listen", "localhost:49123"},
		},
		{
			name:     "explicit GUI",
			input:    []string{"gui", "--background"},
			wantMode: launchGUI,
			wantArgs: []string{"--background"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			mode, args := routeLaunch(test.input)
			if mode != test.wantMode {
				t.Errorf("mode = %v, want %v", mode, test.wantMode)
			}
			if !reflect.DeepEqual(args, test.wantArgs) {
				t.Errorf("args = %#v, want %#v", args, test.wantArgs)
			}
		})
	}
}

func TestUnifiedUsageDocumentsAllModes(t *testing.T) {
	t.Parallel()

	usage := unifiedUsage()
	for _, required := range []string{
		"PCControllerDesktop.exe",
		"PCControllerDesktop.exe cli",
		"PCControllerDesktop.exe tui",
		"PCControllerDesktop.exe serve",
		"/on",
		"--mode standalone",
		"serve --no-open",
		"--register-shell",
		"--uri URI",
	} {
		if !strings.Contains(usage, required) {
			t.Errorf("unified usage does not contain %q", required)
		}
	}
	for _, retired := range []string{"desktopctl.exe", "desktoptui.exe"} {
		if strings.Contains(strings.ToLower(usage), retired) {
			t.Errorf("unified usage still advertises %q", retired)
		}
	}
}

func TestArgumentsAllowColorHonorsNoColorBeforeParsing(t *testing.T) {
	t.Parallel()

	if argumentsAllowColor([]string{"--on", "--NO-COLOR"}) {
		t.Fatal("--no-color should disable decorated parse and runtime errors")
	}
	if argumentsAllowColor([]string{"--no-color=true", "--bad-option"}) {
		t.Fatal("--no-color=true should disable decorated parse errors")
	}
	if !argumentsAllowColor([]string{"--no-color", "--no-color=false"}) {
		t.Fatal("the last explicit --no-color value should win")
	}
	if !argumentsAllowColor([]string{"--on", "--json"}) {
		t.Fatal("ordinary command arguments unexpectedly disabled color")
	}
}

func TestValidateTUITerminalRejectsRedirectedStreams(t *testing.T) {
	input, inputWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer inputWriter.Close()

	outputReader, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outputReader.Close()
	defer output.Close()

	err = validateTUITerminal(input, output)
	if err == nil {
		t.Fatal("redirected TUI streams unexpectedly passed terminal validation")
	}
	for _, required := range []string{
		"interactive terminal",
		"PCControllerDesktop cli",
	} {
		if !strings.Contains(err.Error(), required) {
			t.Fatalf("terminal validation error %q does not contain %q", err, required)
		}
	}
}
