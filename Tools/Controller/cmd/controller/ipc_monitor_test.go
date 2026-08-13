package main

import (
	"strings"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
)

func TestFormatMonitoredEventIncludesCorrelationAndText(t *testing.T) {
	event := controllerapi.Event{
		Time: time.Date(2026, time.August, 14, 12, 34, 56, 123000000, time.Local),
		Kind: "program.output", Text: "compiling Project/Firmware.cpp",
		Metadata: map[string]string{"operation_id": "firmware-build-123"},
	}
	got := formatMonitoredEvent(event)
	for _, expected := range []string{
		"12:34:56.123", "program.output", "[firmware-build-123]", "compiling Project/Firmware.cpp",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("formatted event %q missing %q", got, expected)
		}
	}
}
