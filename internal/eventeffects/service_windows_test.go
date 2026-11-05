//go:build windows

package eventeffects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
)

func TestNewWindowsRunsConfiguredAutomationWithoutGUI(t *testing.T) {
	temp := t.TempDir()
	outputPath := filepath.Join(temp, "automation-result.txt")
	t.Setenv("RAYANLAMP_TEST_OUTPUT", outputPath)

	scriptPath := filepath.Join(temp, "state.cmd")
	script := "@echo off\r\n" +
		"> \"%RAYANLAMP_TEST_OUTPUT%\" (\r\n" +
		"  echo event=%RAYANLAMP_EVENT%\r\n" +
		"  echo state=%RAYANLAMP_STATE%\r\n" +
		"  echo previous=%RAYANLAMP_PREVIOUS_STATE%\r\n" +
		"  echo source=%RAYANLAMP_SOURCE%\r\n" +
		")\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{
		Automation: config.AutomationConfig{
			Enabled:          true,
			OnScript:         scriptPath,
			WorkingDirectory: temp,
			Timeout:          config.Duration(5 * time.Second),
			CaptureOutput:    true,
		},
	}
	events := make(chan model.Event, 2)
	service, err := NewWindows(cfg, events, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	events <- model.Event{
		Kind:          model.EventSnapshot,
		PreviousState: model.StateOff,
		Snapshot: model.Snapshot{
			State:      model.StateOn,
			LastSource: "websocket",
			DeviceURL:  "http://rayanlamp.local",
		},
	}

	deadline := time.Now().Add(3 * time.Second)
	expectedLines := []string{
		"event=state",
		"state=on",
		"previous=off",
		"source=websocket",
	}
	var lastOutput string
	for {
		data, readErr := os.ReadFile(outputPath)
		if readErr == nil {
			lastOutput = strings.ReplaceAll(string(data), "\r\n", "\n")
			complete := true
			for _, expected := range expectedLines {
				if !strings.Contains(lastOutput, expected) {
					complete = false
					break
				}
			}
			if complete {
				return
			}
		}
		if readErr != nil && !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		}
		if time.Now().After(deadline) {
			t.Fatalf(
				"timed out waiting for complete automation output; last output = %q",
				lastOutput,
			)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
