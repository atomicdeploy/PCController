//go:build !linux

package main

import (
	"errors"
	"io"
)

func runToolchainHostProvision(_ []string, _, _ io.Writer) error {
	return errors.New("controller toolchain provision-host is supported only on Linux; no host state was changed")
}
