package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/mattn/go-isatty"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/startupmode"
	"pccontroller.local/controller/internal/tui"
)

var startupScreenActive bool

func terminalOutput(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}

func writeTUIStartup(output io.Writer, terminal bool, title string) {
	if !terminal {
		return
	}
	width := 80
	if file, ok := output.(*os.File); ok {
		if columns, _, err := term.GetSize(file.Fd()); err == nil {
			width = columns
		}
	}
	// Start the same alternate screen Bubble Tea takes over, so the early
	// frame is replaced in place instead of leaving a second splash behind.
	startupScreenActive = true
	fmt.Fprint(output, "\x1b[?1049h\x1b[H", tui.StartupFrame(title, "Starting terminal interface…", width))
}

func finishStartupPresentation(output io.Writer) {
	if startupScreenActive {
		fmt.Fprint(output, "\x1b[?1049l")
		startupScreenActive = false
	}
}

func prepareStartupPresentation(args []string, output io.Writer) {
	if mode := startupmode.Command(args); mode != "tui" && mode != "web" {
		return
	}
	_, configPath, presentation, parseErr := extractGlobalArguments(args)
	ui := appconfig.Defaults().UI
	if resolved, err := appconfig.ResolvePath(configPath); err == nil {
		if config, _, err := appconfig.Load(resolved); err == nil {
			ui = config.UI
		}
	}
	if presentation.IntroMode != "" {
		ui.IntroMode = presentation.IntroMode
	}
	if presentation.AppName != "" {
		ui.AppTitle = presentation.AppName
	}
	if startupmode.TUI(args) {
		if parseErr != nil || !ui.ShouldShowIntro() {
			return
		}
		enableStartupANSI(output)
		writeTUIStartup(output, terminalOutput(output), ui.AppTitle)
		return
	}
	if startupmode.Command(args) == "web" {
		explicit := startupmode.BackgroundWeb(args)
		if !explicit && terminalOutput(output) && terminalOutput(os.Stdin) {
			if parseErr != nil || !ui.ShouldShowIntro() {
				return
			}
			enableStartupANSI(output)
			writeTUIStartup(output, true, ui.AppTitle)
			return
		}
		_, _ = consolewindow.HideBackground(explicit)
		// The previous updater may still share its classic console until the
		// 10-second health commit. One bounded recheck handles that old launcher
		// without hiding a user's shared terminal or introducing idle polling.
		time.AfterFunc(12*time.Second, func() { _, _ = consolewindow.HideBackground(explicit) })
	}
}
