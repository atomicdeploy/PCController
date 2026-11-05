//go:build windows && !no_native_gui

package main

import (
	"pccontroller.local/controller/internal/control"
	"testing"
)

func TestDesktopMessagesRespectExplicitTargets(t *testing.T) {
	for _, target := range []string{"host", "all", "desktop", "gui", "native", "*"} {
		if !nativeDesktopReceivesMessage(control.Event{Kind: "message", Text: "Hello", Targets: []string{target}}) {
			t.Fatal(target)
		}
	}
	for _, target := range []string{"board", "lcd", "web", "unknown", ""} {
		if nativeDesktopReceivesMessage(control.Event{Kind: "message", Text: "Hello", Targets: []string{target}}) {
			t.Fatal(target)
		}
	}
}
