//go:build aix || js || plan9 || wasip1

package main

import (
	"errors"
	"os"
	"runtime"
)

type unsupportedHostInstanceLock struct{}

func platformHostInstanceUserKey() (string, error) {
	return os.UserConfigDir()
}

func platformTryHostInstanceLock(
	_ string,
	_ string,
) (platformHostInstanceLock, bool, error) {
	return nil, false, errors.New("per-user cross-process host locks are unavailable on " + runtime.GOOS)
}

func (*unsupportedHostInstanceLock) Close() error { return nil }
