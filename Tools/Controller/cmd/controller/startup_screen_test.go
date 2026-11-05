package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUIStartupScreenIsImmediateAndNeverWrittenToPipes(t *testing.T) {
	var output bytes.Buffer
	t.Cleanup(func() { finishStartupPresentation(&output) })
	writeTUIStartup(&output, false, "Controller")
	if output.Len() != 0 {
		t.Fatal("startup text leaked into redirected output")
	}
	writeTUIStartup(&output, true, "Controller")
	if !strings.Contains(output.String(), "Controller") || !strings.Contains(output.String(), "Starting terminal interface") {
		t.Fatal("missing startup presentation")
	}
	if strings.Contains(output.String(), "%") {
		t.Fatal("invented startup progress")
	}
	if !strings.Contains(output.String(), "\x1b[") || !strings.Contains(output.String(), "TERMINAL") {
		t.Fatal("unstyled startup splash")
	}
}

func TestStartupLoadingIsIndependentOfIntroAndConfiguration(t *testing.T) {
	for _, args := range [][]string{
		nil, {"tui"}, {"--intro", "auto", "tui"}, {"--intro=skip", "tui"},
		{"--intro", "always", "tui"}, {"--config", "does-not-exist.json", "tui"},
		{"web"}, {"web", "--intro", "skip", "--no-open", "--no-tray"},
		{"web", "--no-tui=false"},
	} {
		if !showStartupLoading(args, true, true) {
			t.Errorf("visible startup suppressed for %v", args)
		}
		if showStartupLoading(args, false, true) {
			t.Errorf("startup leaked to redirected output for %v", args)
		}
	}
	for _, args := range [][]string{
		{"version"}, {"config", "intro", "reset"}, {"web", "--no-tui"},
		{"web", "--no-tui=true"}, {"--config"}, {"firmware", "inspect"},
		{"tui", "--help"}, {"web", "-h"}, {"web", "export"},
		{"--intro", "always", "tui", "--simple"}, {"tui", "--simple=true"},
	} {
		if showStartupLoading(args, true, true) {
			t.Errorf("unexpected splash for %v", args)
		}
	}
	if showStartupLoading([]string{"web"}, true, false) {
		t.Fatal("noninteractive web host must not start a terminal splash")
	}
}

func TestStartupLoadingHandoffAndCleanup(t *testing.T) {
	var output bytes.Buffer
	t.Cleanup(func() { finishStartupPresentation(&output) })
	updateStartupPresentation(&output, "Ignored", "Not started")
	if output.Len() != 0 {
		t.Fatal("phase started its own splash")
	}
	writeTUIStartup(&output, true, "Controller")
	updateStartupPresentation(&output, "Renamed controller", "Loading history and runtime…")
	updateStartupPresentation(&output, "", "Starting host services…")
	if strings.Count(output.String(), "\x1b[?1049h") != 1 {
		t.Fatal("phases entered separate alternate screens")
	}
	if strings.Count(output.String(), "Renamed controller") != 2 {
		t.Fatal("resolved branding was not retained between phases")
	}
	if strings.Contains(output.String(), "\x1b[2J") {
		t.Fatal("startup clears to a blank frame before painting")
	}
	handoffStartupPresentation()
	size := output.Len()
	updateStartupPresentation(&output, "", "Must not overwrite TUI")
	if output.Len() != size {
		t.Fatal("startup renderer wrote after handoff")
	}
	finishStartupPresentation(&output)
	finishStartupPresentation(&output)
	if strings.Count(output.String(), "\x1b[?1049l") != 1 || startupScreenActive || startupScreenHandedOff {
		t.Fatal("screen cleanup was not balanced and idempotent")
	}
}
