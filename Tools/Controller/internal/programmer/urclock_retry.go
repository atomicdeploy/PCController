package programmer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Windows can retain an Urclock AVRDUDE serial handle briefly after the
// process exits. The next guarded-backup read must remain safe and bounded: it
// is retried only when AVRDUDE explicitly reports a transient sharing/access
// failure, never for protocol, verification, or device-response failures.
var urclockPortReleaseRetryDelays = []time.Duration{
	250 * time.Millisecond,
	750 * time.Millisecond,
}

type retryWaitFunc func(context.Context, time.Duration) error

func runBackupCommandWithPortReleaseRetry(
	ctx context.Context,
	method Method,
	command Command,
	output io.Writer,
	runner CommandRunner,
) error {
	return runBackupCommandWithRetryPolicy(
		ctx,
		method,
		command,
		output,
		runner,
		urclockPortReleaseRetryDelays,
		waitForRetry,
	)
}

func runBackupCommandWithRetryPolicy(
	ctx context.Context,
	method Method,
	command Command,
	output io.Writer,
	runner CommandRunner,
	delays []time.Duration,
	wait retryWaitFunc,
) error {
	if runner == nil {
		return errors.New("backup command retry requires a command runner")
	}
	if output == nil {
		output = io.Discard
	}
	if wait == nil {
		wait = waitForRetry
	}

	for attempt := 0; ; attempt++ {
		var diagnostic bytes.Buffer
		runErr := runner.Run(ctx, command, io.MultiWriter(output, &diagnostic))
		if runErr == nil {
			return nil
		}
		transientPortRelease := isTransientUrclockPortReleaseFailure(
			runErr, diagnostic.String(),
		)
		if method != MethodUrclock || !transientPortRelease {
			return runErr
		}
		if attempt >= len(delays) {
			return fmt.Errorf(
				"Urclock serial port remained busy after %d attempts: %w",
				attempt+1,
				runErr,
			)
		}

		delay := delays[attempt]
		fmt.Fprintf(
			output,
			"Urclock serial handle is still being released; retrying this backup command in %s (attempt %d/%d).\n",
			delay,
			attempt+2,
			len(delays)+1,
		)
		if waitErr := wait(ctx, delay); waitErr != nil {
			return errors.Join(
				waitErr,
				fmt.Errorf("Urclock backup attempt %d: %w", attempt+1, runErr),
			)
		}
	}
}

func isTransientUrclockPortReleaseFailure(runErr error, diagnostic string) bool {
	text := strings.ToLower(diagnostic)
	if runErr != nil {
		text += "\n" + strings.ToLower(runErr.Error())
	}
	portOpenFailure := strings.Contains(text, "ser_open") ||
		strings.Contains(text, "cannot open port") ||
		strings.Contains(text, "can't open device") ||
		strings.Contains(text, "could not open port") ||
		strings.Contains(text, "serial port") ||
		strings.Contains(text, `\\.\com`)
	transientSharingFailure := strings.Contains(text, "access is denied") ||
		strings.Contains(text, "permission denied") ||
		strings.Contains(text, "sharing violation") ||
		strings.Contains(text, "serial port busy") ||
		strings.Contains(text, "resource busy") ||
		strings.Contains(text, "port is busy") ||
		strings.Contains(text, "being used by another process")
	return portOpenFailure && transientSharingFailure
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
