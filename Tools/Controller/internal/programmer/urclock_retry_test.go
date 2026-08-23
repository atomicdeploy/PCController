package programmer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUrclockBackupRetriesTransientWindowsPortReleaseFailure(t *testing.T) {
	attempts := 0
	waits := 0
	var output strings.Builder
	runner := CommandRunnerFunc(func(_ context.Context, _ Command, writer io.Writer) error {
		attempts++
		if attempts == 1 {
			_, _ = io.WriteString(writer, `avrdude: ser_open(): cannot open port "\\.\COM4": Access is denied.`)
			return errors.New("exit status 1")
		}
		return nil
	})

	err := runBackupCommandWithRetryPolicy(
		context.Background(),
		MethodUrclock,
		Command{Name: "avrdude"},
		&output,
		runner,
		[]time.Duration{time.Millisecond},
		func(context.Context, time.Duration) error { waits++; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || waits != 1 {
		t.Fatalf("attempts=%d waits=%d, want 2 attempts and one bounded wait", attempts, waits)
	}
	if !strings.Contains(output.String(), "attempt 2/2") {
		t.Fatalf("retry was not explained in output: %q", output.String())
	}
}

func TestUrclockBackupRetryIsBounded(t *testing.T) {
	attempts := 0
	runner := CommandRunnerFunc(func(_ context.Context, _ Command, writer io.Writer) error {
		attempts++
		_, _ = io.WriteString(writer, "cannot open port COM4: serial port busy")
		return errors.New("exit status 1")
	})

	err := runBackupCommandWithRetryPolicy(
		context.Background(),
		MethodUrclock,
		Command{Name: "avrdude"},
		io.Discard,
		runner,
		[]time.Duration{0, 0},
		func(context.Context, time.Duration) error { return nil },
	)
	if err == nil || attempts != 3 || !strings.Contains(err.Error(), "remained busy after 3 attempts") {
		t.Fatalf("err=%v attempts=%d, want failure after exactly three attempts", err, attempts)
	}
}

func TestGuardedBackupRetriesEEPROMReadAfterSuccessfulFlashRead(t *testing.T) {
	root := t.TempDir()
	base := newFakeAVRRunner(t)
	eepromAttempts := 0
	runner := CommandRunnerFunc(func(ctx context.Context, command Command, output io.Writer) error {
		if commandOutputPath(command, "-Ueeprom:r:") != "" {
			eepromAttempts++
			if eepromAttempts == 1 {
				_, _ = io.WriteString(output, `avrdude: ser_open(): can't open device "\\.\COM4": Access is denied.`)
				return errors.New("exit status 1")
			}
		}
		return base.Run(ctx, command, output)
	})

	directory, err := BackupWithRunner(
		context.Background(), fakeBackupOptions(root), io.Discard, runner,
	)
	if err != nil {
		t.Fatal(err)
	}
	if eepromAttempts != 2 {
		t.Fatalf("EEPROM attempts=%d, want one bounded retry", eepromAttempts)
	}
	content, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BackupManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Status != "complete" || len(manifest.Errors) != 0 {
		t.Fatalf("retry did not preserve complete backup gate: %#v", manifest)
	}
}

func TestBackupRetryDoesNotMaskNonTransientOrNonUrclockFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     Method
		diagnostic string
	}{
		{name: "protocol", method: MethodUrclock, diagnostic: "programmer is not responding"},
		{name: "unrelated permission", method: MethodUrclock, diagnostic: "cannot open output file: Access is denied"},
		{name: "USBasp", method: MethodUSBasp, diagnostic: "Access is denied"},
	} {
		t.Run(test.name, func(t *testing.T) {
			attempts := 0
			runner := CommandRunnerFunc(func(_ context.Context, _ Command, writer io.Writer) error {
				attempts++
				_, _ = io.WriteString(writer, test.diagnostic)
				return errors.New("exit status 1")
			})
			err := runBackupCommandWithRetryPolicy(
				context.Background(), test.method, Command{Name: "avrdude"}, io.Discard,
				runner, []time.Duration{0, 0}, func(context.Context, time.Duration) error { return nil },
			)
			if err == nil || attempts != 1 {
				t.Fatalf("err=%v attempts=%d, unsafe retry occurred", err, attempts)
			}
		})
	}
}

func TestUrclockBackupRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	runner := CommandRunnerFunc(func(_ context.Context, _ Command, writer io.Writer) error {
		attempts++
		_, _ = io.WriteString(writer, "cannot open port COM4: Access is denied")
		return errors.New("exit status 1")
	})

	err := runBackupCommandWithRetryPolicy(
		ctx, MethodUrclock, Command{Name: "avrdude"}, io.Discard, runner,
		[]time.Duration{time.Second}, waitForRetry,
	)
	if !errors.Is(err, context.Canceled) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want immediate cancellation", err, attempts)
	}
}
