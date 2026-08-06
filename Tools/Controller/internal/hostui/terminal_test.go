package hostui

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalControlValidationAndEncoding(t *testing.T) {
	if _, err := ValidateTerminalTitle("Bench controller\x1b]2;bad"); err == nil {
		t.Fatal("control-bearing title was accepted")
	}
	if _, err := ValidateOSCPayload("9;4;1;50\x07"); err == nil {
		t.Fatal("terminated OSC payload was accepted")
	}
	if _, err := ValidateOSCPayload("title;not-numeric"); err == nil {
		t.Fatal("non-numeric OSC selector was accepted")
	}
	if _, err := ValidateOSCPayload("2;" + strings.Repeat("x", MaximumOSCPayloadBytes)); err == nil {
		t.Fatal("oversized OSC payload was accepted")
	}
	var output bytes.Buffer
	if err := WriteOSC(&output, "2;Bench controller"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "\x1b]2;Bench controller\x07"; got != want {
		t.Fatalf("OSC sequence=%q want=%q", got, want)
	}
}

func TestTerminalProgressAliasesProduceWindowsTerminalOSC(t *testing.T) {
	tests := map[string]string{
		"clear":         "9;4;0;0",
		"normal 42":     "9;4;1;42",
		"error;73":      "9;4;2;73",
		"indeterminate": "9;4;3;0",
		"warning,100":   "9;4;4;100",
	}
	for input, want := range tests {
		progress, err := ParseTerminalProgress(input)
		if err != nil {
			t.Fatalf("ParseTerminalProgress(%q): %v", input, err)
		}
		got, err := progress.OSCPayload()
		if err != nil || got != want {
			t.Fatalf("ParseTerminalProgress(%q) payload=%q err=%v want=%q", input, got, err, want)
		}
	}
	for _, invalid := range []string{"normal", "bad 10", "normal 101", "5 20"} {
		if _, err := ParseTerminalProgress(invalid); err == nil {
			t.Fatalf("invalid progress %q was accepted", invalid)
		}
	}
}
