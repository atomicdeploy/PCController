//go:build !windows && !linux

package hostui

import "context"

type unsupportedKeyboardRegistrar struct{}

func newPlatformKeyboardRegistrar() KeyboardRegistrar { return unsupportedKeyboardRegistrar{} }
func (unsupportedKeyboardRegistrar) Start(context.Context, []KeyboardBinding, func(KeyboardEvent) error) error {
	return ErrUnsupported
}
func (unsupportedKeyboardRegistrar) ReleaseAll(string) error { return nil }
func (unsupportedKeyboardRegistrar) Stop(string) error       { return nil }
func (unsupportedKeyboardRegistrar) Status() KeyboardStatus {
	return KeyboardStatus{Supported: false, LastError: ErrUnsupported.Error()}
}
