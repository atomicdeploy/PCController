//go:build !windows

package consolewindow

import "runtime"

func inspectPlatform() State { return State{} }
func hideBackgroundPlatform(_ bool) (Result, error) {
	return Result{Reason: "background classic-console management is Windows-only"}, nil
}

func applyPlatform(Settings) (Result, error) {
	return Result{Reason: "local console size/font management is unavailable on " + runtime.GOOS}, nil
}

func setTitlePlatform(string) (Result, error) {
	return Result{Reason: "local console title management is unavailable on " + runtime.GOOS}, nil
}
