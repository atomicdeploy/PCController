//go:build linux

package hostui

import (
	"context"
	"fmt"
)

const linuxGlobalHotkeyDetail = "global hotkeys are disabled on Linux because Wayland has no compositor-neutral, permission-scoped registration API; use WebUI shortcuts while the window is focused"

type linuxUnsupportedHotkeyRegistrar struct{}

func newPlatformHotkeyRegistrar() HotkeyRegistrar { return linuxUnsupportedHotkeyRegistrar{} }
func (linuxUnsupportedHotkeyRegistrar) Start(context.Context, []HotkeyBinding, func(HotkeyEvent)) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, linuxGlobalHotkeyDetail)
}
func (linuxUnsupportedHotkeyRegistrar) Stop() error { return nil }
func (linuxUnsupportedHotkeyRegistrar) Status() HotkeyStatus {
	return HotkeyStatus{Supported: false, LastError: linuxGlobalHotkeyDetail}
}
