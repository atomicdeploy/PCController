//go:build linux

package pcspeaker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/unix"
)

// KIOCSOUND programs the Linux kernel PC-speaker driver. The divisor is the
// PIT channel-2 divisor, matching the firmware/Windows implementation. This
// path is intentionally direct: it does not shell out to beep, require an
// external daemon, or assume ALSA/PCM is installed.
const kioSound = 0x4B2F

var linuxSpeakerDevices = []string{"/dev/console", "/dev/tty0"}

func play(ctx context.Context, _ string, frequencyHz, durationMS uint32) error {
	divisor, err := pitDivisor(frequencyHz)
	if err != nil {
		return err
	}
	fd, device, err := openLinuxSpeaker()
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	stop := func() { _ = unix.IoctlSetInt(fd, kioSound, 0) }
	defer stop()
	if err := unix.IoctlSetInt(fd, kioSound, int(divisor)); err != nil {
		return fmt.Errorf("enable Linux PC speaker on %s: %w", device, err)
	}
	timer := time.NewTimer(time.Duration(durationMS) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func openLinuxSpeaker() (int, string, error) {
	var last error
	for _, device := range linuxSpeakerDevices {
		fd, err := unix.Open(device, unix.O_WRONLY|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, device, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("no Linux PC-speaker device found")
	}
	return -1, "", fmt.Errorf("open Linux PC speaker (%s): %w", linuxSpeakerDevices, last)
}

func probeNative(string) error {
	fd, _, err := openLinuxSpeaker()
	if err == nil {
		err = unix.Close(fd)
	}
	return err
}
