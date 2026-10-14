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
	fmt.Fprint(output, tui.StartupFrame(title, "Starting terminal interface…", width))
}

func prepareStartupPresentation(args []string, output io.Writer) {
	if startupmode.TUI(args) {
		enableStartupANSI(output)
		writeTUIStartup(output, terminalOutput(output), productidentity.DefaultAppTitle())
		return
	}
	if startupmode.Command(args) == "web" {
		explicit := startupmode.BackgroundWeb(args)
		if !explicit && terminalOutput(output) && terminalOutput(os.Stdin) {
			enableStartupANSI(output)
			writeTUIStartup(output, true, productidentity.DefaultAppTitle())
			return
		}
		_, _ = consolewindow.HideBackground(explicit)
		// The previous updater may still share its classic console until the
		// 10-second health commit. One bounded recheck handles that old launcher
		// without hiding a user's shared terminal or introducing idle polling.
		time.AfterFunc(12*time.Second, func() { _, _ = consolewindow.HideBackground(explicit) })
	}
}
