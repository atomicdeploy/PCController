package appconfig

import "testing"

func TestCustomEffectColorValidation(t *testing.T) {
	for _, color := range []string{"", "violet", "green", "#A142F4", "#f59e0b"} {
		if !ValidEffectColor(color) {
			t.Fatalf("valid color rejected: %q", color)
		}
	}
	for _, color := range []string{"orange", "#FFF", "#12345678", "#ZZZZZZ", "red; play", "rgb(1,2,3)"} {
		if ValidEffectColor(color) {
			t.Fatalf("malformed color accepted: %q", color)
		}
	}
}
