//go:build !windows

package consolewindow

import "runtime"

func applyPlatform(Settings) (Result, error) {
	return Result{Reason: "local console size/font management is unavailable on " + runtime.GOOS}, nil
}

func setTitlePlatform(string) (Result, error) {
	return Result{Reason: "local console title management is unavailable on " + runtime.GOOS}, nil
}
