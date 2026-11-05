//go:build windows

package winfx

import "testing"

func TestValidateOpenURL(t *testing.T) {
	t.Parallel()
	valid := []string{
		"http://127.0.0.1:47831/app/",
		"http://localhost:47831/app/",
		"http://[::1]:47831/app/",
	}
	for _, input := range valid {
		if _, err := validateOpenURL(input); err != nil {
			t.Errorf("validateOpenURL(%q): %v", input, err)
		}
	}
	invalid := []string{
		"",
		"file:///C:/Windows/System32/calc.exe",
		"javascript:alert(1)",
		"http://user:secret@127.0.0.1/",
		"http://127.0.0.1/\n--bad",
	}
	for _, input := range invalid {
		if _, err := validateOpenURL(input); err == nil {
			t.Errorf("validateOpenURL(%q) succeeded", input)
		}
	}
}
