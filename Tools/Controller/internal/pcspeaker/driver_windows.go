//go:build windows

package pcspeaker

import (
	"errors"
	"strings"
)

func validateDriverDirectory(driverDirectory string) error {
	if strings.TrimSpace(driverDirectory) == "" {
		return errors.New("WinRing0 driver directory is required")
	}
	return nil
}
