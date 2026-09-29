package programmer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

var ErrToolchainUnavailable = errors.New("programmer toolchain unavailable")

type PreflightError struct{ Err error }

func (err *PreflightError) Error() string {
	return "programmer toolchain preflight: " + err.Err.Error()
}
func (err *PreflightError) Unwrap() error        { return err.Err }
func (err *PreflightError) Is(target error) bool { return target == ErrToolchainUnavailable }

// PreflightProgrammer resolves and validates the exact tool pair without
// opening a programmer or changing the board. Keep the returned paths for all
// steps so a backup cannot rediscover a different installation mid-transaction.
func PreflightProgrammer(ctx context.Context, options Options) (resolvedOptions Options, failure error) {
	defer func() {
		if failure != nil && !errors.Is(failure, context.Canceled) && !errors.Is(failure, context.DeadlineExceeded) {
			failure = &PreflightError{Err: failure}
		}
	}()
	ReportProgress(ctx, Progress{Stage: "preflight", Percent: -1, Detail: "Checking programmer tools"})
	if err := ctx.Err(); err != nil {
		return options, err
	}
	executable, configuration, err := findAvrdudeWithContext(
		ctx, options.Avrdude, options.AvrdudeConf, options.ArduinoCLI, options.ArduinoConfig,
	)
	if err != nil {
		return options, err
	}
	resolved, err := exec.LookPath(executable)
	if err != nil {
		return options, fmt.Errorf("configured AVRDUDE executable %q is unavailable: %w", executable, err)
	}
	file, err := os.Open(configuration)
	if err != nil {
		return options, fmt.Errorf("configured AVRDUDE configuration %q is unavailable: %w", configuration, err)
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return options, statErr
	}
	if !info.Mode().IsRegular() {
		return options, fmt.Errorf("AVRDUDE configuration %q is not a regular file", configuration)
	}
	if closeErr != nil {
		return options, closeErr
	}
	if err := ctx.Err(); err != nil {
		return options, err
	}
	options.Avrdude, options.AvrdudeConf = resolved, configuration
	return options, nil
}
