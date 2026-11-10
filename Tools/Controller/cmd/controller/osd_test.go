package main

import (
	"io"
	"testing"
)

func TestOSDCLIRejectsInvalidArgumentsBeforeAnyPresentation(t *testing.T) {
	for _, args := range [][]string{{}, {"wrong"}, {"show", "--duration", "0s", "--text", "x"}, {"show", "--duration", "3m", "--text", "x"}, {"show", "--text", "x", "--icon", "https://remote/image.png"}, {"show", "--text", "x", "--action", "Run=file:///unsafe"}, {"status", "--standalone"}, {"show", "--action", "not-an-action"}, {"show", "--text", ""}} {
		if err := runOSD(args, io.Discard, io.Discard); err == nil {
			t.Fatal(args)
		}
	}
}
