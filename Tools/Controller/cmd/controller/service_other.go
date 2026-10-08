//go:build !windows

package main

import (
	"errors"
	"io"
)

func runPlatformServiceCommand(_ string, _ []string, _ string, _, _ io.Writer) error {
	return errors.New("controller service is available only on Windows")
}
