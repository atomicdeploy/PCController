package design

import "testing"

func TestParseColor(t *testing.T) {
	color, err := ParseColor("#24C8FF")
	if err != nil {
		t.Fatal(err)
	}
	if color.R != 0x24 || color.G != 0xC8 || color.B != 0xFF {
		t.Fatalf("color = %+v", color)
	}
	for _, invalid := range []string{"", "24C8FF", "#fff", "#GG0000"} {
		if _, err := ParseColor(invalid); err == nil {
			t.Errorf("ParseColor(%q) succeeded", invalid)
		}
	}
}
