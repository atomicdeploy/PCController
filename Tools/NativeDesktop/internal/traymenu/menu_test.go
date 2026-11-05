package traymenu

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateOptions(t *testing.T) {
	t.Parallel()

	valid := Options{
		Items: []Item{
			{ID: "toggle", Label: "Toggle lamp", Shortcut: "Ctrl+Alt+L", Checked: true},
			{Separator: true},
			{ID: "quit", Label: "Quit", Disabled: true},
		},
		Theme:     ThemeDark,
		Backdrop:  BackdropAcrylic,
		Alignment: AlignEnd,
	}
	if err := validateOptions(valid); err != nil {
		t.Fatalf("valid options: %v", err)
	}

	tests := []struct {
		name    string
		options Options
		target  error
	}{
		{name: "empty", options: Options{}, target: ErrNoItems},
		{
			name: "too many",
			options: Options{
				Items: make([]Item, maxItems+1),
			},
			target: ErrTooManyItems,
		},
		{
			name: "empty id",
			options: Options{
				Items: []Item{{Label: "Missing ID"}},
			},
			target: ErrInvalidItem,
		},
		{
			name: "duplicate id",
			options: Options{
				Items: []Item{
					{ID: "same", Label: "One"},
					{ID: "same", Label: "Two"},
				},
			},
			target: ErrDuplicateAction,
		},
		{
			name: "separator fields",
			options: Options{
				Items: []Item{{Separator: true, Label: "not empty"}},
			},
			target: ErrInvalidItem,
		},
		{
			name: "control",
			options: Options{
				Items: []Item{{ID: "bad", Label: "bad\nlabel"}},
			},
			target: ErrInvalidItem,
		},
		{
			name: "large png",
			options: Options{
				Items: []Item{{ID: "large", Label: "Large", IconPNG: make([]byte, maxPNGBytes+1)}},
			},
			target: ErrInvalidItem,
		},
		{
			name: "theme",
			options: Options{
				Items: []Item{{ID: "ok", Label: "OK"}},
				Theme: "sepia",
			},
			target: ErrInvalidItem,
		},
		{
			name: "backdrop",
			options: Options{
				Items:    []Item{{ID: "ok", Label: "OK"}},
				Backdrop: "fog",
			},
			target: ErrInvalidItem,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateOptions(test.options); !errors.Is(err, test.target) {
				t.Fatalf("validateOptions error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestValidateTextUnicodeAndLength(t *testing.T) {
	t.Parallel()
	if err := validateText("روشن کردن چراغ", false); err != nil {
		t.Fatalf("Persian label rejected: %v", err)
	}
	if err := validateText(strings.Repeat("x", maxTextRunes+1), false); err == nil {
		t.Fatal("overlong text accepted")
	}
	if err := validateText("", true); err != nil {
		t.Fatalf("optional empty text rejected: %v", err)
	}
}

func TestRectValidation(t *testing.T) {
	t.Parallel()
	if !(Rect{Left: 10, Top: 20, Right: 10, Bottom: 20}).validAnchor() {
		t.Fatal("point anchor should be valid")
	}
	if (Rect{Left: 11, Right: 10}).validAnchor() {
		t.Fatal("reversed anchor should be invalid")
	}
	if (Rect{Left: 1, Top: 1, Right: 1, Bottom: 2}).validArea() {
		t.Fatal("zero-width work area should be invalid")
	}
}

func TestCloneOptionsOwnsPNGData(t *testing.T) {
	t.Parallel()
	original := Options{Items: []Item{{
		ID:      "icon",
		Label:   "Icon",
		IconPNG: []byte{1, 2, 3},
	}}}
	cloned := cloneOptions(original)
	original.Items[0].Label = "Changed"
	original.Items[0].IconPNG[0] = 9
	if cloned.Items[0].Label != "Icon" || cloned.Items[0].IconPNG[0] != 1 {
		t.Fatalf("clone changed with source: %#v", cloned)
	}
}
