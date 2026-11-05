//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestStripVT(t *testing.T) {
	raw := []byte(
		"\x1b[?1049h\x1b[38;2;1;2;3mPCController\x1b[0m\r\n" +
			"\x1b]0;private title\x07DEVICE CONTROL\x1b[?25l",
	)
	got := stripVT(raw)
	if !strings.Contains(got, "PCController\r\nDEVICE CONTROL") {
		t.Fatalf("stripVT() = %q", got)
	}
	if strings.Contains(got, "private title") || strings.ContainsRune(got, '\x1b') {
		t.Fatalf("stripVT retained a VT control: %q", got)
	}
}

func TestTranscriptBufferIsBounded(t *testing.T) {
	var buffer transcriptBuffer
	buffer.append(make([]byte, maxTranscriptBytes+128))
	got, truncated := buffer.snapshot()
	if !truncated {
		t.Fatal("large transcript was not marked truncated")
	}
	if len(got) != maxTranscriptBytes {
		t.Fatalf("bounded transcript length = %d, want %d", len(got), maxTranscriptBytes)
	}
}
