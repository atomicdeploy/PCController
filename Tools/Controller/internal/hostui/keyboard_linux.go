//go:build linux

package hostui

import (
	"context"
	"fmt"
)

const linuxKeyboardControlDetail = "global keyboard capture is disabled on Linux because Wayland intentionally isolates input devices; use focused WebUI keyboard controls"

type linuxUnsupportedKeyboardRegistrar struct{}

func newPlatformKeyboardRegistrar() KeyboardRegistrar { return linuxUnsupportedKeyboardRegistrar{} }
func (linuxUnsupportedKeyboardRegistrar) Start(context.Context, []KeyboardBinding, func(KeyboardEvent) error) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, linuxKeyboardControlDetail)
}
func (linuxUnsupportedKeyboardRegistrar) ReleaseAll(string) error { return nil }
func (linuxUnsupportedKeyboardRegistrar) Stop(string) error       { return nil }
func (linuxUnsupportedKeyboardRegistrar) Status() KeyboardStatus {
	return KeyboardStatus{Supported: false, LastError: linuxKeyboardControlDetail}
}
