//go:build windows

package ui

import "testing"

func TestShellAppearanceMessagesCoverWindowsAndDWMChanges(t *testing.T) {
	t.Parallel()

	for _, message := range []uint32{
		wmSettingChange,
		wmSystemColorChange,
		wmThemeChanged,
		wmDWMCompositionChanged,
		wmDWMColorizationColorChanged,
	} {
		if !shellAppearanceMessage(message) {
			t.Errorf("appearance message %#x was ignored", message)
		}
	}
	for _, message := range []uint32{
		0,
		wmReapplyAppearance,
		wmReapplyWindowIcons,
		wmPowerBroadcast,
	} {
		if shellAppearanceMessage(message) {
			t.Errorf("unrelated message %#x triggered appearance refresh", message)
		}
	}
}
