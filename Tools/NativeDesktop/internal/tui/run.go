package tui

import (
	"context"
	"errors"
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"rayanlamp.local/app/internal/terminal"
)

// Run starts the interactive Bubble Tea program. The caller retains ownership
// of transport and must close it after Run returns.
func Run(
	ctx context.Context,
	transport Transport,
	options LaunchOptions,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if !terminal.IsTerminal(os.Stdin) ||
		!terminal.IsTerminal(os.Stdout) {
		return fmt.Errorf(
			"TUI requires an interactive terminal on stdin and stdout; " +
				"use `RayanLamp cli` for redirected or scripted output",
		)
	}
	// Keep Run safe for embedders that do not use cmd/RayanLamp's launcher.
	// Redirected streams are ignored by the platform implementation.
	_, _ = terminal.EnableVT100(os.Stdout, os.Stderr)
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()
	if quitSource, ok := transport.(QuitEventTransport); ok {
		if quitEvents := quitSource.QuitEvents(); quitEvents != nil {
			go func() {
				select {
				case <-quitEvents:
					cancel()
				case <-runContext.Done():
				}
			}()
		}
	}
	model, err := NewModel(runContext, transport, ModelOptions{
		Poll:          options.Poll,
		Timeout:       options.Transport.RequestTimeout,
		Theme:         options.Theme,
		AltScreen:     options.AltScreen,
		Mouse:         options.Mouse,
		InitialScreen: options.InitialScreen,
		OpenTerminal:  options.Terminal,
	})
	if err != nil {
		return err
	}
	programOptions := []tea.ProgramOption{tea.WithContext(runContext)}
	if !terminal.ColorEnabled(os.Stdout) {
		// Force ASCII when output is redirected or policy disables color so
		// styled diagnostics and device-provided SGR are stripped as well.
		programOptions = append(
			programOptions,
			tea.WithColorProfile(colorprofile.ASCII),
		)
	}
	_, err = tea.NewProgram(newRuntimeModel(model), programOptions...).Run()
	if errors.Is(err, tea.ErrInterrupted) ||
		errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
