package main

import (
	"bytes"
	"testing"
)

func TestCanonicalContractSourceIgnoresLineEndingStyle(t *testing.T) {
	lf := []byte("first\nsecond\n")
	for _, source := range [][]byte{
		lf,
		[]byte("first\r\nsecond\r\n"),
		[]byte("first\rsecond\r"),
	} {
		if got := canonicalContractSource(source); !bytes.Equal(got, lf) {
			t.Fatalf("canonical source = %q, want %q", got, lf)
		}
	}
}
