package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/startupmode"
	"pccontroller.local/controller/internal/tui"
)

var startupScreenActive bool
var startupScreenTitle string
var startupScreenHandedOff bool

func terminalOutput(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}

func writeTUIStartup(output io.Writer, terminal bool, title string) {
	if !terminal {
		return
	}
	if !startupScreenActive {
		// Enter once. Later phases replace this frame in the same screen.
		fmt.Fprint(output, "\x1b[?1049h")
		startupScreenActive = true
		startupScreenHandedOff = false
	}
	updateStartupPresentation(output, title, "Starting terminal interface…")
}

func updateStartupPresentation(output io.Writer, title, phase string) {
	if !startupScreenActive || startupScreenHandedOff {
		return
	}
	if title != "" {
		startupScreenTitle = title
	}
	// Paint before clearing the remaining rows: never erase into a blank
	// console while a new frame is still being composed.
	fmt.Fprint(output, "\x1b[H", tui.StartupFrame(startupScreenTitle, phase, startupTerminalWidth(output)), "\x1b[J")
}

func startupTerminalWidth(output io.Writer) int {
	if file, ok := output.(*os.File); ok {
		if columns, _, err := term.GetSize(file.Fd()); err == nil && columns > 0 {
			return columns
		}
	}
	return 80
}

func handoffStartupPresentation() {
	// Bubble Tea owns all rendering from here, including reconnect/failover.
	startupScreenHandedOff = true
}

func finishStartupPresentation(output io.Writer) {
	if startupScreenActive {
		fmt.Fprint(output, "\x1b[?1049l")
		startupScreenActive = false
		startupScreenTitle = ""
		startupScreenHandedOff = false
	}
}

func prepareStartupPresentation(args []string, output io.Writer) {
	if mode := startupmode.Command(args); mode != "tui" && mode != "web" {
		return
	}
	// Loading visibility is not the first-run intro policy. Do not open the
	// config (or .env) before this first paint: slow disks/network profiles
	// must see the frame too. The resolved branding is applied in run().
	_, _, presentation, _ := extractGlobalArguments(args)
	title := productidentity.Title(presentation.AppName)
	if showStartupLoading(args, terminalOutput(output), terminalOutput(os.Stdin)) {
		enableStartupANSI(output)
		writeTUIStartup(output, true, title)
		return
	}
	if startupmode.Command(args) == "web" {
		explicit := startupmode.BackgroundWeb(args)
		_, _ = consolewindow.HideBackground(explicit)
		// The previous updater may still share its classic console until the
		// 10-second health commit. One bounded recheck handles that old launcher
		// without hiding a user's shared terminal or introducing idle polling.
		time.AfterFunc(12*time.Second, func() { _, _ = consolewindow.HideBackground(explicit) })
	}
}

func showStartupLoading(args []string, outputTerminal, inputTerminal bool) bool {
	if !outputTerminal {
		return false
	}
	remaining, _, _, err := extractGlobalArguments(args)
	if err != nil {
		return false
	}
	for _, arg := range remaining {
		if arg == "--help" || arg == "-h" || arg == "--simple" || arg == "--simple=true" {
			return false
		}
	}
	if len(remaining) > 1 && remaining[0] == "web" && remaining[1] == "export" {
		return false
	}
	switch startupmode.Command(args) {
	case "tui":
		return true
	case "web":
		return inputTerminal && !startupmode.BackgroundWeb(args)
	default:
		return false
	}
}
