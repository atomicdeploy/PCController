//go:build windows

package winfx

import "testing"

func TestNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if NoColor() {
		t.Fatal("empty NO_COLOR should not disable color")
	}
	t.Setenv("NO_COLOR", "1")
	if !NoColor() {
		t.Fatal("non-empty NO_COLOR should disable color")
	}
}
