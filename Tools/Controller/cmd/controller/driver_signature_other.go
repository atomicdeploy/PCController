//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

func verifyZadigExecutable(string) error {
	return errors.New("Windows Authenticode verification is unavailable on this platform")
}

func newDetachedCommand(path string) *exec.Cmd { return exec.Command(path) }
