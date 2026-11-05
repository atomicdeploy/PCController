//go:build windows && !no_native_gui

package main

import (
	"pccontroller.local/controller/internal/control"
	"strings"
)

func nativeDesktopReceivesMessage(event control.Event) bool {
	if event.Kind != "message" || strings.TrimSpace(event.Text) == "" {
		return false
	}
	for _, target := range event.Targets {
		switch strings.ToLower(strings.TrimSpace(target)) {
		case "host", "all", "desktop", "gui", "native", "*":
			return true
		}
	}
	return false
}
