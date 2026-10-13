package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestTUIStartupScreenIsImmediateAndNeverWrittenToPipes(t *testing.T) {
	var output bytes.Buffer
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
}
