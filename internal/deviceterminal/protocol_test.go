package deviceterminal

import (
	"bytes"
	"testing"
)

func TestTelnetDecoderHandlesSplitCommandsAndEscapedIAC(t *testing.T) {
	t.Parallel()

	var decoder telnetDecoder
	fragments := [][]byte{
		[]byte("before"),
		{telnetIAC},
		{telnetWILL},
		{1},
		[]byte("-middle-"),
		{telnetIAC, telnetIAC},
		[]byte("-after"),
		{telnetIAC, telnetSB, 24, 1, telnetIAC},
		{telnetIAC, 2, telnetIAC},
		{telnetSE},
		[]byte("-done"),
	}

	var output []byte
	for _, fragment := range fragments {
		output = append(output, decoder.Feed(fragment)...)
	}
	want := append([]byte("before-middle-"), telnetIAC)
	want = append(want, []byte("-after-done")...)
	if !bytes.Equal(output, want) {
		t.Fatalf("decoded bytes = %q, want %q", output, want)
	}
}

func TestTerminalFilterDropsSplitControlStringsAndPreservesCSI(t *testing.T) {
	t.Parallel()

	var filter terminalFilter
	fragments := [][]byte{
		[]byte("safe"),
		{0x1b},
		[]byte("]52;c;clipboard"),
		{0x07},
		[]byte("-"),
		{0x1b, 'P'},
		[]byte("malicious"),
		{0x1b},
		{'\\'},
		[]byte("-"),
		{0x9d},
		[]byte("title"),
		{0x9c},
		{0x1b},
		[]byte("[31mred"),
		{0x1b},
		[]byte("[0m"),
	}

	var output []byte
	for _, fragment := range fragments {
		output = append(output, filter.Feed(fragment)...)
	}
	want := []byte("safe--\x1b[31mred\x1b[0m")
	if !bytes.Equal(output, want) {
		t.Fatalf("filtered bytes = %q, want %q", output, want)
	}
	for _, forbidden := range [][]byte{
		[]byte("clipboard"),
		[]byte("malicious"),
		[]byte("title"),
	} {
		if bytes.Contains(output, forbidden) {
			t.Fatalf("filtered output retained control-string payload %q: %q", forbidden, output)
		}
	}
}

func TestTerminalFilterCANAndSUBAbortControlStrings(t *testing.T) {
	t.Parallel()

	var filter terminalFilter
	fragments := [][]byte{
		[]byte("before"),
		{0x1b, ']'},
		[]byte("unterminated OSC"),
		{0x18},
		[]byte("-after-can"),
		{0x1b, 'P'},
		[]byte("unterminated DCS"),
		{0x1b},
		{0x1a},
		[]byte("-after-sub"),
	}

	var output []byte
	for _, fragment := range fragments {
		output = append(output, filter.Feed(fragment)...)
	}
	want := []byte("before-after-can-after-sub")
	if !bytes.Equal(output, want) {
		t.Fatalf("filtered bytes = %q, want %q", output, want)
	}
}
