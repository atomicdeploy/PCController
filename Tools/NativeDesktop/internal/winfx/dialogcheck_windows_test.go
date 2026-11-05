//go:build windows

package winfx

import "testing"

func TestDialogCheckStylesPreserveOtherNativeButtons(t *testing.T) {
	for _, style := range []uint32{2, 3, 5, 6} {
		if !isDialogCheckStyle(style | 0x50000000) {
			t.Fatal(style)
		}
	}
	for _, style := range []uint32{0, 1, 4, 7, 9, 11} {
		if isDialogCheckStyle(style) {
			t.Fatal(style)
		}
	}
}
