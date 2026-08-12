//go:build !linux

package main

import (
	"errors"
	"io"

	"pccontroller.local/controller/internal/appconfig"
)

func runToolchainRuntimeWindowOpen([]string, io.Writer, io.Writer, *appconfig.Store) error {
	return errors.New("managed browser authentication is not available on this platform")
}

func openAuthenticatedBrowser(value string, _ *appconfig.Store) error {
	return openBrowser(value)
}
