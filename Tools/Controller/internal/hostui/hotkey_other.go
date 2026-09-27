//go:build !windows && !linux

package hostui

import "context"

type unsupportedHotkeyRegistrar struct{}

func newPlatformHotkeyRegistrar() HotkeyRegistrar { return unsupportedHotkeyRegistrar{} }
func (unsupportedHotkeyRegistrar) Start(context.Context, []HotkeyBinding, func(HotkeyEvent)) error {
	return ErrUnsupported
}
func (unsupportedHotkeyRegistrar) Stop() error { return nil }
func (unsupportedHotkeyRegistrar) Status() HotkeyStatus {
	return HotkeyStatus{Supported: false, LastError: ErrUnsupported.Error()}
}
