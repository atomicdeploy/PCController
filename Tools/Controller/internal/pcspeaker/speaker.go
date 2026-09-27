// Package pcspeaker mirrors firmware buzzer notes on a motherboard speaker.
package pcspeaker

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const (
	BackendAuto     = "auto"
	BackendNative   = "native"
	BackendExternal = "external"
)

type BackendStatus struct {
	Backend    string `json:"backend"`
	Executable string `json:"executable,omitempty"`
}

const (
	MinFrequencyHz = 20
	MaxFrequencyHz = 20000
	MaxDurationMS  = 60000
)

// Play drives PIT channel 2 and the system-speaker gate through WinRing0.
func Play(ctx context.Context, driverDirectory string, frequencyHz, durationMS int) error {
	return PlayConfigured(ctx, driverDirectory, BackendAuto, "", frequencyHz, durationMS)
}

// PlayConfigured honors the persisted backend preference. Auto is the only
// mode that falls back; explicit native/external choices fail truthfully.
func PlayConfigured(ctx context.Context, driverDirectory, backend, executable string, frequencyHz, durationMS int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if frequencyHz < MinFrequencyHz || frequencyHz > MaxFrequencyHz {
		return fmt.Errorf("speaker frequency must be %d..%d Hz", MinFrequencyHz, MaxFrequencyHz)
	}
	if durationMS < 1 || durationMS > MaxDurationMS {
		return fmt.Errorf("speaker duration must be 1..%d ms", MaxDurationMS)
	}
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		backend = BackendAuto
	}
	if backend != BackendAuto && backend != BackendNative && backend != BackendExternal {
		return fmt.Errorf("unknown PC-speaker backend %q", backend)
	}
	var nativeErr error
	if backend != BackendExternal {
		if err := validateDriverDirectory(driverDirectory); err != nil {
			nativeErr = err
		} else {
			nativeErr = play(ctx, driverDirectory, uint32(frequencyHz), uint32(durationMS))
			if nativeErr == nil {
				return nil
			}
		}
		if backend == BackendNative {
			return nativeErr
		}
	}
	if fallbackErr := playExternalBeep(ctx, driverDirectory, executable, frequencyHz, durationMS); fallbackErr == nil {
		return nil
	} else {
		return errors.Join(nativeErr, fallbackErr)
	}
}

// ResolveBackend selects the startup backend without emitting a tone. Auto
// prefers an already-usable native device and otherwise resolves the configured
// executable or the platform's PATH beep command once.
func ResolveBackend(driverDirectory, backend, executable string) (BackendStatus, error) {
	backend = strings.ToLower(strings.TrimSpace(backend))
	if backend == "" {
		backend = BackendAuto
	}
	if backend != BackendExternal {
		if err := probeNative(driverDirectory); err == nil {
			return BackendStatus{Backend: BackendNative}, nil
		} else if backend == BackendNative {
			return BackendStatus{}, err
		}
	}
	path, err := findExternalBeep(driverDirectory, executable)
	if err != nil {
		return BackendStatus{}, err
	}
	return BackendStatus{Backend: BackendExternal, Executable: path}, nil
}

func IsHelperInvocation(args []string) bool {
	return len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "pc-speaker")
}

// RunHelperInvocation is intentionally config-independent and exists for
// operator diagnostics. Runtime buzzer mirroring calls Play directly; it never
// launches this helper or an SSH process.
func RunHelperInvocation(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("pc-speaker", flag.ContinueOnError)
	flags.SetOutput(stderr)
	driverDirectory := flags.String("driver-dir", "", "directory containing WinRing0x64.sys")
	frequencyHz := flags.Int("frequency", 0, "frequency in Hz")
	durationMS := flags.Int("duration", 0, "duration in milliseconds")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected pc-speaker arguments: %s", strconv.Quote(strings.Join(flags.Args(), " ")))
	}
	return Play(ctx, *driverDirectory, *frequencyHz, *durationMS)
}

func pitDivisor(frequencyHz uint32) (uint16, error) {
	if frequencyHz == 0 {
		return 0, errors.New("speaker frequency is zero")
	}
	divisor := uint32(1193182) / frequencyHz
	if divisor == 0 || divisor > 0xFFFF {
		return 0, fmt.Errorf("speaker frequency %d Hz has invalid PIT divisor %d", frequencyHz, divisor)
	}
	return uint16(divisor), nil
}
