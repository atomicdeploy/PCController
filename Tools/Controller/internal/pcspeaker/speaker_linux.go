//go:build linux

package pcspeaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// KDMKTONE programs both the PIT divisor and a kernel-owned duration. Unlike
// KIOCSOUND, the kernel stops this tone even if the controller is killed.
// It is intentionally kept local because x/sys/unix does not publish it.
const linuxKDMKTONE = 0x4B30

var linuxSpeakerMu sync.Mutex

type linuxSpeakerOperations struct {
	open  func(string, int, uint32) (int, error)
	ioctl func(int, uint, int) error
	close func(int) error
}

var nativeLinuxSpeakerOperations = linuxSpeakerOperations{
	open:  unix.Open,
	ioctl: unix.IoctlSetInt,
	close: unix.Close,
}

func driverDirectoryRequired() bool { return false }

func play(ctx context.Context, _ string, frequencyHz, durationMS uint32) error {
	return playLinuxConsoleSpeaker(
		ctx,
		[]string{"/dev/console", "/dev/tty0"},
		nativeLinuxSpeakerOperations,
		frequencyHz,
		durationMS,
	)
}

func playLinuxConsoleSpeaker(
	ctx context.Context,
	devices []string,
	operations linuxSpeakerOperations,
	frequencyHz, durationMS uint32,
) error {
	divisor, err := pitDivisor(frequencyHz)
	if err != nil {
		return err
	}
	if operations.open == nil || operations.ioctl == nil || operations.close == nil {
		return errors.New("Linux PC-speaker operations are incomplete")
	}

	linuxSpeakerMu.Lock()
	defer linuxSpeakerMu.Unlock()

	var attempts []error
	for _, device := range devices {
		fd, openErr := operations.open(device, unix.O_WRONLY|unix.O_CLOEXEC, 0)
		if openErr != nil {
			attempts = append(attempts, fmt.Errorf("open %s: %w", device, openErr))
			continue
		}
		argument := int((durationMS << 16) | uint32(divisor))
		startErr := operations.ioctl(fd, linuxKDMKTONE, argument)
		if startErr != nil {
			failure := fmt.Errorf("enable %s speaker: %w", device, startErr)
			if closeErr := operations.close(fd); closeErr != nil {
				failure = errors.Join(failure, fmt.Errorf("close %s: %w", device, closeErr))
			}
			attempts = append(attempts, failure)
			continue
		}

		timer := time.NewTimer(time.Duration(durationMS) * time.Millisecond)
		var result error
		select {
		case <-ctx.Done():
			result = ctx.Err()
		case <-timer.C:
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if stopErr := operations.ioctl(fd, linuxKDMKTONE, 0); stopErr != nil {
			result = errors.Join(result, fmt.Errorf("stop %s speaker: %w", device, stopErr))
		}
		if closeErr := operations.close(fd); closeErr != nil {
			result = errors.Join(result, fmt.Errorf("close %s: %w", device, closeErr))
		}
		return result
	}

	detail := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		detail = append(detail, attempt.Error())
	}
	return fmt.Errorf(
		"Linux PC-speaker backend is unavailable (load pcspkr and grant the service access to a console device): %s",
		strings.Join(detail, "; "),
	)
}
