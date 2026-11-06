//go:build windows

package ui

import "github.com/lxn/walk"

func trayToggleOnPress(controller bool, button walk.MouseButton) bool {
	return !controller && button == walk.LeftButton
}

func trayMenuOnRelease(controller bool, button walk.MouseButton) bool {
	return button == walk.RightButton || (controller && button == walk.LeftButton)
}
