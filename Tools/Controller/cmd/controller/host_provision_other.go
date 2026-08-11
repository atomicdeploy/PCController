//go:build !linux

package main

import (
	"errors"
	"io"
)

func runToolchainHostProvision(_ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain provision-host is supported only on Linux; no host state was changed")
}

func runToolchainMirrorRefresh(_ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain mirror-refresh is supported only on Linux; no host state was changed")
}

func runToolchainMirrorInstall(_ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain mirror-install is supported only on Linux; no host state was changed")
}

func runToolchainPrepareHostData(_ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain prepare-host-data is supported only on Linux; no host state was changed")
}
