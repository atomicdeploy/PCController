//go:build windows

package winfx

import "testing"

func TestDialogButtonSemanticIconsCoverNativeDialogs(t *testing.T) {
	tests := []struct {
		label string
		text  string
		icon  dialogButtonIcon
	}{
		{"&Save settings", "&Save settings", dialogButtonIconSave},
		{"Saving…", "Saving…", dialogButtonIconSave},
		{"&Cancel", "&Cancel", dialogButtonIconCancel},
		{"Close", "Close", dialogButtonIconCancel},
		{"C&lose", "C&lose", dialogButtonIconCancel},
		{"&Browse…", "&Browse…", dialogButtonIconBrowse},
		{"Browse &on…", "Browse &on…", dialogButtonIconBrowse},
		{"Browse o&ff…", "Browse o&ff…", dialogButtonIconBrowse},
		{"Browse &event…", "Browse &event…", dialogButtonIconBrowse},
		{"Browse fol&der…", "Browse fol&der…", dialogButtonIconBrowse},
		{"Test toast", "Test toast", dialogButtonIconNotification},
		{"&Test toast", "&Test toast", dialogButtonIconNotification},
		{"Test sound", "Test sound", dialogButtonIconSound},
		{"Test &audio", "Test &audio", dialogButtonIconSound},
		{"Copy token", "Copy token", dialogButtonIconCopy},
		{"Cop&y token", "Cop&y token", dialogButtonIconCopy},
		{"Copy output", "Copy output", dialogButtonIconCopy},
		{"&Copy output", "&Copy output", dialogButtonIconCopy},
		{"Regenerate…", "Regenerate…", dialogButtonIconRefresh},
		{"Re&generate…", "Re&generate…", dialogButtonIconRefresh},
		{"Refresh", "Refresh", dialogButtonIconRefresh},
		{"&Refresh", "&Refresh", dialogButtonIconRefresh},
		{"Save output…", "Save output…", dialogButtonIconSave},
		{"&Save output…", "&Save output…", dialogButtonIconSave},
		{"●  Record", "Record", dialogButtonIconRecord},
		{"■  Cancel", "Cancel", dialogButtonIconCancel},
		{"×  Clear", "Clear", dialogButtonIconClear},
		{"&Record", "&Record", dialogButtonIconRecord},
		{"Record &toggle", "Record &toggle", dialogButtonIconRecord},
		{"Record o&n", "Record o&n", dialogButtonIconRecord},
		{"Record of&f", "Record of&f", dialogButtonIconRecord},
		{"Record sho&w", "Record sho&w", dialogButtonIconRecord},
		{"C&lear", "C&lear", dialogButtonIconClear},
		{"Clear to&ggle", "Clear to&ggle", dialogButtonIconClear},
		{"Clea&r on", "Clea&r on", dialogButtonIconClear},
		{"Clear &off", "Clear &off", dialogButtonIconClear},
		{"C&lear show", "C&lear show", dialogButtonIconClear},
		{"Cancel r&ecording", "Cancel r&ecording", dialogButtonIconCancel},
		{"&Reveal", "&Reveal", dialogButtonIconReveal},
		{"Reveal &token", "Reveal &token", dialogButtonIconReveal},
	}
	for _, test := range tests {
		got := dialogButtonContentFor(test.label)
		if got.Text != test.text || got.Icon != test.icon {
			t.Fatalf(
				"content for %q = %+v, want text %q icon %#x",
				test.label,
				got,
				test.text,
				test.icon,
			)
		}
	}
}

func TestDialogButtonSemanticGlyphsUseDocumentedMDL2Codepoints(t *testing.T) {
	tests := []struct {
		name string
		got  dialogButtonIcon
		want dialogButtonIcon
	}{
		{"save", dialogButtonIconSave, 0xE74E},
		{"cancel", dialogButtonIconCancel, 0xE711},
		{"open file", dialogButtonIconBrowse, 0xE8E5},
		{"ringer", dialogButtonIconNotification, 0xEA8F},
		{"volume", dialogButtonIconSound, 0xE767},
		{"copy", dialogButtonIconCopy, 0xE8C8},
		{"refresh", dialogButtonIconRefresh, 0xE72C},
		{"record", dialogButtonIconRecord, 0xE7C8},
		{"delete", dialogButtonIconClear, 0xE74D},
		{"view", dialogButtonIconReveal, 0xE890},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Fatalf(
				"%s glyph = %#x, want %#x",
				test.name,
				test.got,
				test.want,
			)
		}
	}
	if len(dialogButtonSemanticGlyphs) != len(tests) {
		t.Fatalf(
			"font coverage probe has %d glyphs, want %d",
			len(dialogButtonSemanticGlyphs),
			len(tests),
		)
	}
	seen := make(map[dialogButtonIcon]bool, len(tests))
	for _, glyph := range dialogButtonSemanticGlyphs {
		if glyph == dialogButtonIconNone || seen[glyph] {
			t.Fatalf("font coverage probe contains invalid glyph %#x", glyph)
		}
		seen[glyph] = true
	}
}

func TestDialogButtonGlyphLookupFailureIsDWORDSized(t *testing.T) {
	if !dialogButtonGlyphLookupFailed(uintptr(0xFFFFFFFF)) {
		t.Fatal("32-bit GDI_ERROR was not recognized")
	}
	if !dialogButtonGlyphLookupFailed(^uintptr(0)) {
		t.Fatal("pointer-width all-ones GDI_ERROR was not recognized")
	}
	if dialogButtonGlyphLookupFailed(0) {
		t.Fatal("successful glyph lookup was mistaken for GDI_ERROR")
	}
}

func TestDialogButtonMnemonicNormalizationPreservesPaintText(t *testing.T) {
	content := dialogButtonContentFor("  &Save && close  ")
	if content.Text != "&Save && close" {
		t.Fatalf("paint text = %q, want mnemonic markup preserved", content.Text)
	}
	if got := normalizeDialogButtonSemanticText(content.Text); got != "save & close" {
		t.Fatalf("semantic text = %q, want %q", got, "save & close")
	}
	if got := stripDialogButtonMnemonicMarkers("E&xit && Save"); got != "Exit & Save" {
		t.Fatalf("mnemonic stripping = %q, want %q", got, "Exit & Save")
	}
}

func TestDialogButtonUnknownLabelRemainsTextOnly(t *testing.T) {
	got := dialogButtonContentFor("&Do something")
	if got.Text != "&Do something" || got.Icon != dialogButtonIconNone {
		t.Fatalf("unknown content = %+v", got)
	}
}

func TestDialogButtonIconLayoutIsCenteredAndDPIAware(t *testing.T) {
	body := dialogButtonRect{Right: 140, Bottom: 36}
	icon, text, ok := dialogButtonContentLayout(body, 70, 96, true)
	if !ok {
		t.Fatal("roomy button rejected semantic icon")
	}
	if icon.Right-icon.Left != 14 || text.Left-icon.Right != 5 {
		t.Fatalf("96-DPI layout icon=%+v text=%+v", icon, text)
	}
	groupLeft := icon.Left
	groupRight := text.Right
	if leftGap, rightGap := groupLeft-body.Left, body.Right-groupRight; abs32(
		leftGap-rightGap,
	) > 1 {
		t.Fatalf("content is not centered: left %d right %d", leftGap, rightGap)
	}

	icon, text, ok = dialogButtonContentLayout(
		dialogButtonRect{Right: 280, Bottom: 72},
		140,
		192,
		true,
	)
	if !ok || icon.Right-icon.Left != 28 || text.Left-icon.Right != 10 {
		t.Fatalf("192-DPI layout icon=%+v text=%+v ok=%v", icon, text, ok)
	}
}

func abs32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func TestDialogButtonIconLayoutFallsBackWithoutCrushingText(t *testing.T) {
	body := dialogButtonRect{Right: 36, Bottom: 24}
	_, text, ok := dialogButtonContentLayout(body, 80, 96, true)
	if ok {
		t.Fatal("undersized button accepted an overlapping icon")
	}
	if text.Right <= text.Left {
		t.Fatalf("text fallback is invalid: %+v", text)
	}

	body = dialogButtonRect{Right: 100, Bottom: 30}
	_, text, ok = dialogButtonContentLayout(body, 80, 96, true)
	if ok {
		t.Fatal("button kept its icon by truncating a complete action label")
	}
	if text.Right-text.Left != 92 {
		t.Fatalf("medium-width text fallback = %+v, want full content area", text)
	}
}
