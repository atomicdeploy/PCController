package programmer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"pccontroller.local/controller/internal/link"
)

const (
	readbackResetTimeout = 2 * time.Second
	readbackResetPulse   = 120 * time.Millisecond
)

// readbackPreparer is deliberately optional for injected CommandRunner values.
// Offline fixture runners must never implicitly open a real serial device.
type readbackPreparer interface {
	PrepareReadback(context.Context, Options, io.Writer) error
}

type readbackResetSession interface {
	PulseDTR(context.Context, time.Duration) error
	Close() error
}

type readbackResetOpener func(context.Context, string, int) (readbackResetSession, error)

type productionCommandRunner struct {
	CommandRunner
	openReset readbackResetOpener
}

// NewCommandRunner runs programmer subprocesses synchronously and explicitly
// re-enters a local Urclock UART before independent write-readback verification.
// The caller must retain exclusive ownership of that port for the transaction.
func NewCommandRunner() CommandRunner {
	return &productionCommandRunner{
		CommandRunner: CommandRunnerFunc(Run),
		openReset: func(ctx context.Context, name string, baud int) (readbackResetSession, error) {
			return link.OpenContext(ctx, name, baud)
		},
	}
}

func (runner *productionCommandRunner) PrepareReadback(ctx context.Context, options Options, output io.Writer) error {
	if options.Method != MethodUrclock &&
		!(options.Method == MethodAvrdude && strings.EqualFold(options.Programmer, "urclock")) {
		return nil
	}
	port := strings.TrimSpace(options.Port)
	if port == "" {
		return errors.New("UART readback re-entry requires the transaction's serial port")
	}
	if link.IsNetworkEndpoint(port) {
		return fmt.Errorf("UART readback re-entry: %w", link.ErrControlLinesUnsupported)
	}
	resetContext, cancel := context.WithTimeout(ctx, readbackResetTimeout)
	defer cancel()
	if err := resetContext.Err(); err != nil {
		return err
	}
	baud := options.BaudRate
	if baud == 0 {
		baud = generatedBoardBaud
	}
	if output != nil {
		fmt.Fprintln(output, "UART readback re-entry: release/reset DTR, then close reset handle before independent verification.")
	}
	// link.OpenContext starts DTR/RTS inactive and does not require HELLO.
	// The pulse uses the existing recovery gesture (DTR only); no application
	// command, discovery, extra settling sleep or write retry belongs here.
	session, err := runner.openReset(resetContext, port, baud)
	if err != nil {
		return fmt.Errorf("open UART for readback re-entry: %w", err)
	}
	pulseErr := session.PulseDTR(resetContext, readbackResetPulse)
	closeErr := session.Close()
	if err := errors.Join(pulseErr, closeErr, resetContext.Err()); err != nil {
		return fmt.Errorf("reset/release UART for readback re-entry: %w", err)
	}
	return nil
}
