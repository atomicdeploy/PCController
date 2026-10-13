package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/startupmode"
)

func terminalOutput(output io.Writer) bool {
	file, ok := output.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}

func writeTUIStartup(output io.Writer, terminal bool, title string) {
	if !terminal {
		return
	}
	fmt.Fprintf(output, "\n  %s\n  Starting terminal interface...\n\n", title)
}

func prepareStartupPresentation(args []string, output io.Writer) {
	if startupmode.TUI(args) {
		writeTUIStartup(output, terminalOutput(output), productidentity.DefaultAppTitle())
		return
	}
	if startupmode.Command(args) == "web" {
		explicit := startupmode.BackgroundWeb(args)
		if !explicit && terminalOutput(output) && terminalOutput(os.Stdin) {
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
