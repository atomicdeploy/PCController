//go:build windows

package winfx

import (
	"errors"
	"testing"
)

func TestNormalizeRequestedBackdrop(t *testing.T) {
	tests := map[string]string{
		"":              BackdropAuto,
		" AUTO ":        BackdropAuto,
		"Mica":          BackdropMica,
		"ACRYLIC":       BackdropAcrylic,
		"blur":          BackdropBlur,
		" transparent ": BackdropTransparent,
		"none":          BackdropSolid,
		"disabled":      BackdropSolid,
	}
	for input, want := range tests {
		got, err := normalizeRequestedBackdrop(input)
		if err != nil {
			t.Fatalf("normalizeRequestedBackdrop(%q): %v", input, err)
		}
		if got != want {
			t.Errorf("normalizeRequestedBackdrop(%q) = %q, want %q", input, got, want)
		}
	}

	_, err := normalizeRequestedBackdrop("glassier")
	if !errors.Is(err, ErrUnknownBackdrop) {
		t.Fatalf("unknown backdrop error = %v", err)
	}
}

func TestRefreshSolidFallbackRequiresMaterialTeardown(t *testing.T) {
	if !fallbackRequiresBackdropClear(false) {
		t.Fatal("Refresh fallback would preserve the previous material stack")
	}
	if fallbackRequiresBackdropClear(true) {
		t.Fatal("Apply fallback would redundantly clear an already-reset stack")
	}
}

func TestFinalizeNativeCaptionReappliesThemeBeforeFramePaint(t *testing.T) {
	t.Parallel()

	var calls []string
	finalizeNativeCaption(
		func() { calls = append(calls, "dark-mode") },
		func() { calls = append(calls, "non-client-paint") },
	)

	if len(calls) != 2 ||
		calls[0] != "dark-mode" ||
		calls[1] != "non-client-paint" {
		t.Fatalf("native caption finalization order = %v", calls)
	}
}

func TestHighContrastForcesEveryDecorativeBackdropToSolid(t *testing.T) {
	t.Parallel()

	for _, requested := range []string{
		BackdropAuto,
		BackdropMica,
		BackdropAcrylic,
		BackdropBlur,
		BackdropTabbed,
		BackdropTransparent,
	} {
		if !highContrastRequiresSolid(requested, true) {
			t.Errorf("High Contrast retained %q backdrop", requested)
		}
	}
	if highContrastRequiresSolid(BackdropSolid, true) {
		t.Fatal("solid backdrop was unnecessarily treated as a fallback")
	}
	if highContrastRequiresSolid(BackdropTransparent, false) {
		t.Fatal("ordinary transparent mode was forced solid")
	}
}

func TestNativeCaptionRedrawIsSynchronousAndNonErasing(t *testing.T) {
	t.Parallel()

	flags := nativeCaptionRedrawFlags()
	required := uintptr(redrawInvalidate | redrawFrame | redrawUpdateNow)
	if flags&required != required {
		t.Fatalf("native caption redraw flags %#x omit required %#x", flags, required)
	}
	if flags&redrawNoErase == 0 {
		t.Fatalf("native caption redraw flags %#x permit client erase", flags)
	}
	if flags&redrawNoChildren == 0 {
		t.Fatalf("native caption redraw flags %#x redraw child windows", flags)
	}
}
