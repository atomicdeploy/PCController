//go:build !windows || no_native_gui

package main

import (
	"errors"
	"io"
	"pccontroller.local/controller/internal/appconfig"
)

func runNativeGUI(_ []string, _, _ io.Writer, _ *appconfig.Store) error {
	return errors.New("native Windows GUI is unavailable in this build; use tui or web")
}
func startNativeConsoleBridge() (func(), error) { return func() {}, nil }
func prepareNativeConsoleStreams() error        { return nil }
