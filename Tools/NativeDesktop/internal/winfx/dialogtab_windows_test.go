//go:build windows

package winfx

import (
	"math"
	"testing"
	"unsafe"
)

func TestDialogTabPalettesHaveReadableSelectedContrast(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := dialogTabColors(dark, false)
		contrast := dialogTabContrastRatio(palette.Text, palette.Selected)
		if contrast < 4.5 {
			t.Fatalf(
				"dark=%v selected-tab contrast = %.2f, want at least 4.5",
				dark,
				contrast,
			)
		}
		if palette.Selected == palette.Text {
			t.Fatalf("dark=%v selected tab text and background are identical", dark)
		}
	}
}

func TestDialogTabMaterialPaletteHasContinuousBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := dialogTabColors(dark, true)
		if palette.Inactive != palette.Background {
			t.Fatalf(
				"dark=%v inactive = %#08x, background = %#08x",
				dark,
				palette.Inactive,
				palette.Background,
			)
		}
		if palette.Selected == palette.Background {
			t.Fatalf("dark=%v selected tab disappeared into material", dark)
		}
	}
}

func TestScaleDialogTabMetricIsDPIAware(t *testing.T) {
	tests := []struct {
		value int32
		dpi   int32
		want  int32
	}{
		{2, 96, 2},
		{2, 144, 3},
		{8, 144, 12},
		{7, 192, 14},
	}
	for _, test := range tests {
		if got := scaleDialogTabMetric(test.value, test.dpi); got != test.want {
			t.Fatalf(
				"scaleDialogTabMetric(%d, %d) = %d, want %d",
				test.value,
				test.dpi,
				got,
				test.want,
			)
		}
	}
}

func TestDialogTabNativeStructLayouts(t *testing.T) {
	paintSize := unsafe.Sizeof(dialogTabPaintStruct{})
	if paintSize != 64 && paintSize != 72 {
		t.Fatalf("PAINTSTRUCT-compatible size = %d, want 64 or 72", paintSize)
	}
	itemSize := unsafe.Sizeof(dialogTabItemW{})
	if itemSize != 28 && itemSize != 40 {
		t.Fatalf("TCITEMW-compatible size = %d, want 28 or 40", itemSize)
	}
	trackSize := unsafe.Sizeof(dialogTabTrackMouse{})
	if trackSize != 16 && trackSize != 24 {
		t.Fatalf("TRACKMOUSEEVENT-compatible size = %d, want 16 or 24", trackSize)
	}
}

func TestDialogTabMouseLeaveTrackingIsArmedOnlyOnce(t *testing.T) {
	const hwnd = uintptr(0x7A11)
	dialogTabMu.Lock()
	dialogTabThemes[hwnd] = dialogTabThemeState{Hot: -1}
	dialogTabMu.Unlock()
	t.Cleanup(func() {
		dialogTabMu.Lock()
		delete(dialogTabThemes, hwnd)
		dialogTabMu.Unlock()
	})

	if !beginDialogTabMouseTracking(hwnd) {
		t.Fatal("first mouse move did not arm leave tracking")
	}
	if beginDialogTabMouseTracking(hwnd) {
		t.Fatal("repeated mouse move re-armed leave tracking")
	}
	endDialogTabMouseTracking(hwnd)
	if !beginDialogTabMouseTracking(hwnd) {
		t.Fatal("mouse leave did not permit the next tracking cycle")
	}
}

func TestDialogTabRGBUsesCOLORREFOrder(t *testing.T) {
	if got := dialogTabRGB(0x11, 0x22, 0x33); got != 0x00332211 {
		t.Fatalf("dialogTabRGB = %#08x, want 0x00332211", got)
	}
}

func TestDialogTabGlyphsCoverEverySettingsPage(t *testing.T) {
	want := map[string]uint16{
		"Lamp":          0xEA80,
		"Appearance":    0xE790,
		"Notifications": 0xEA8F,
		"Hotkeys":       0xE765,
		"Automation":    0xE945,
		"Integration":   0xE703,
	}
	seen := make(map[uint16]string, len(want))
	for title, expected := range want {
		glyph, ok := dialogTabGlyph(title)
		if !ok {
			t.Fatalf("%q has no settings-tab glyph", title)
		}
		if glyph != expected {
			t.Fatalf("%q glyph = %#04x, want %#04x", title, glyph, expected)
		}
		if previous, duplicate := seen[glyph]; duplicate {
			t.Fatalf("%q and %q share glyph %#04x", previous, title, glyph)
		}
		seen[glyph] = title
	}
	if _, ok := dialogTabGlyph("Unknown"); ok {
		t.Fatal("unknown page unexpectedly received a glyph")
	}
	if glyph, ok := dialogTabGlyph("  HOTKEYS  "); !ok || glyph != want["Hotkeys"] {
		t.Fatalf(
			"normalized Hotkeys glyph = %#04x, ok=%v; want %#04x, true",
			glyph,
			ok,
			want["Hotkeys"],
		)
	}
}

func TestDialogTabIconTextLayoutIsDPIAwareAndNonOverlapping(t *testing.T) {
	item := dialogTabRect{Left: 10, Top: 4, Right: 142, Bottom: 38}
	for _, dpi := range []int32{96, 144, 192} {
		icon, text, ok := dialogTabIconTextLayout(item, 74, dpi)
		if !ok {
			t.Fatalf("dpi=%d layout failed", dpi)
		}
		if icon.Left < item.Left || icon.Top < item.Top ||
			icon.Right > item.Right || icon.Bottom > item.Bottom {
			t.Fatalf("dpi=%d icon %#v escaped item %#v", dpi, icon, item)
		}
		if text.Left < icon.Right || text.Right > item.Right ||
			text.Bottom > item.Bottom {
			t.Fatalf(
				"dpi=%d text %#v overlaps icon %#v or escapes item %#v",
				dpi,
				text,
				icon,
				item,
			)
		}
		wantSize := scaleDialogTabMetric(16, dpi)
		if icon.Right-icon.Left != wantSize || icon.Bottom-icon.Top != wantSize {
			t.Fatalf(
				"dpi=%d icon size = %dx%d, want %d",
				dpi,
				icon.Right-icon.Left,
				icon.Bottom-icon.Top,
				wantSize,
			)
		}
	}

	if _, _, ok := dialogTabIconTextLayout(
		dialogTabRect{Right: 12, Bottom: 12},
		20,
		192,
	); ok {
		t.Fatal("undersized tab accepted an overlapping icon layout")
	}
}

func TestPackDialogTabPaddingUsesWin32LowAndHighWords(t *testing.T) {
	packed := packDialogTabPadding(
		dialogTabIconHorizontalPadding,
		dialogTabVerticalPadding,
	)
	if horizontal := uint16(packed); horizontal != dialogTabIconHorizontalPadding {
		t.Fatalf(
			"horizontal padding = %d, want %d",
			horizontal,
			dialogTabIconHorizontalPadding,
		)
	}
	if vertical := uint16(packed >> 16); vertical != dialogTabVerticalPadding {
		t.Fatalf(
			"vertical padding = %d, want %d",
			vertical,
			dialogTabVerticalPadding,
		)
	}
}

func dialogTabContrastRatio(foreground, background uint32) float64 {
	lighter := dialogTabRelativeLuminance(foreground)
	darker := dialogTabRelativeLuminance(background)
	if lighter < darker {
		lighter, darker = darker, lighter
	}
	return (lighter + 0.05) / (darker + 0.05)
}

func dialogTabRelativeLuminance(color uint32) float64 {
	channel := func(value byte) float64 {
		normalized := float64(value) / 255
		if normalized <= 0.04045 {
			return normalized / 12.92
		}
		return math.Pow((normalized+0.055)/1.055, 2.4)
	}
	red := channel(byte(color))
	green := channel(byte(color >> 8))
	blue := channel(byte(color >> 16))
	return 0.2126*red + 0.7152*green + 0.0722*blue
}
