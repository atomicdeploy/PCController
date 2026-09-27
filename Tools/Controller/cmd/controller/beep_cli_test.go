package main

import (
	"bytes"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestParseBeepCommandUsesSharedBuzzerContract(t *testing.T) {
	connection, command, err := parseBeepCommand(
		[]string{"--port", "COM44", "--frequency", "440", "--duration", "125"},
		&bytes.Buffer{},
		appconfig.Defaults().Connection,
	)
	if err != nil {
		t.Fatal(err)
	}
	if command != "buzzer 440 125" || connection.port != "COM44" || !connection.overrides["port"] {
		t.Fatalf("connection=%+v command=%q", connection, command)
	}
}

func TestParseBeepCommandRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	for _, args := range [][]string{
		{"--frequency", "440"},
		{"--frequency", "19", "--duration", "1"},
		{"--frequency", "440", "--duration", "0"},
		{"--frequency", "0", "--duration", "1"},
		{"--frequency", "440", "--duration", "1", "extra"},
	} {
		_, _, err := parseBeepCommand(args, &bytes.Buffer{}, appconfig.Defaults().Connection)
		if err == nil {
			t.Fatalf("args %q unexpectedly accepted", args)
		}
	}
	_, command, err := parseBeepCommand([]string{"--frequency", "0", "--duration", "0"}, &bytes.Buffer{}, appconfig.Defaults().Connection)
	if err != nil || command != "buzzer 0 0" {
		t.Fatalf("stop command=%q err=%v", command, err)
	}
}
