//go:build !linux

package main

import (
	"errors"
	"io"

	"pccontroller.local/controller/internal/appconfig"
)

func runToolchainRuntime(_ string, _ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain runtime provisioning is supported only on Linux; no host state was changed")
}

func runToolchainRuntimeWindowReady(_ []string, _, _ io.Writer, _ *appconfig.Store) error {
	return errors.New("controller toolchain runtime readiness is supported only on Linux")
}
