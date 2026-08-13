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
	MinFrequencyHz = 20
	MaxFrequencyHz = 20000
	MaxDurationMS  = 60000
)

// Play drives PIT channel 2 and the system-speaker gate through WinRing0.
func Play(ctx context.Context, driverDirectory string, frequencyHz, durationMS int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := validateDriverDirectory(driverDirectory); err != nil {
		return err
	}
	if frequencyHz < MinFrequencyHz || frequencyHz > MaxFrequencyHz {
		return fmt.Errorf("speaker frequency must be %d..%d Hz", MinFrequencyHz, MaxFrequencyHz)
	}
	if durationMS < 1 || durationMS > MaxDurationMS {
		return fmt.Errorf("speaker duration must be 1..%d ms", MaxDurationMS)
	}
	return play(ctx, driverDirectory, uint32(frequencyHz), uint32(durationMS))
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
