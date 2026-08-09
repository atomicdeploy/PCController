//go:build !windows && !linux

package hostos

import (
	"context"
	"errors"
)

func platformKeyDown(uint16) error {
	return errors.New("virtual-key emission is currently implemented only on Windows")
}

func platformKeyUp(uint16) error {
	return errors.New("virtual-key emission is currently implemented only on Windows")
}

func platformPowerAction(context.Context, string) error {
	return errors.New("power actions are currently implemented only on Windows")
}

func platformMonitorBrightness(context.Context) (BrightnessStatus, error) {
	return BrightnessStatus{}, errors.New("monitor brightness is currently implemented on Windows through DDC/CI and laptop-panel WMI")
}

func platformSetMonitorBrightness(context.Context, int) (BrightnessStatus, error) {
	return BrightnessStatus{}, errors.New("monitor brightness is currently implemented on Windows through DDC/CI and laptop-panel WMI")
}

func platformUptimeMS() uint64 { return 0 }
