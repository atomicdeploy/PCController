package traymenu

import "testing"

func TestScale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value int32
		dpi   int32
		want  int32
	}{
		{16, 96, 16},
		{16, 120, 20},
		{16, 144, 24},
		{16, 192, 32},
		{1, 48, 1},
	}
	for _, test := range tests {
		if got := scale(test.value, test.dpi); got != test.want {
			t.Errorf("scale(%d, %d) = %d, want %d", test.value, test.dpi, got, test.want)
		}
	}
}

func TestBuildDimensionsAndHitTesting(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "on", Label: "Turn on", IconPNG: []byte{1}, Checked: true},
		{Separator: true},
		{ID: "off", Label: "Turn off", Shortcut: "Ctrl+O"},
	}
	measure := func(value string) int32 { return int32(len(value) * 8) }
	d := buildDimensions(items, 144, measure)

	if d.dpi != 144 || d.width < scale(180, 144) {
		t.Fatalf("unexpected dimensions: %#v", d)
	}
	if d.checkColumn == 0 || d.iconColumn == 0 {
		t.Fatalf("check/icon columns not reserved: %#v", d)
	}
	if len(d.rows) != len(items) {
		t.Fatalf("rows = %d, want %d", len(d.rows), len(items))
	}
	if got := d.rows[1].bottom - d.rows[1].top; got != d.separatorHeight {
		t.Fatalf("separator height = %d, want %d", got, d.separatorHeight)
	}
	if got := d.rowAt(d.outerPadding+1, d.rows[0].top+1); got != 0 {
		t.Fatalf("rowAt first = %d", got)
	}
	if got := d.rowAt(0, d.rows[0].top+1); got != -1 {
		t.Fatalf("rowAt outside = %d", got)
	}
}

func TestBuildDimensionsClampsLongColumns(t *testing.T) {
	t.Parallel()
	items := []Item{{
		ID:       "long",
		Label:    "A very long label that should be ellipsized rather than escape the menu",
		Shortcut: "A remarkably long keyboard shortcut description",
		Checked:  true,
		IconPNG:  []byte{1},
	}}
	d := buildDimensions(items, 96, func(value string) int32 {
		return int32(len(value) * 20)
	})
	if d.width != scale(520, 96) {
		t.Fatalf("width = %d, want maximum %d", d.width, scale(520, 96))
	}
	if d.textX >= d.textRight || d.textRight >= d.shortcutX ||
		d.shortcutX >= d.width-d.outerPadding {
		t.Fatalf("invalid clamped columns: %#v", d)
	}
}

func TestBuildDimensionsMirrorsColumnsForRTL(t *testing.T) {
	t.Parallel()
	items := []Item{{
		ID:       "toggle",
		Label:    "تغییر وضعیت چراغ",
		Shortcut: "Ctrl+Alt+L",
		Checked:  true,
		IconPNG:  []byte{1},
	}}
	measure := func(value string) int32 { return int32(len([]rune(value)) * 9) }
	ltr := buildDimensionsForDirection(items, 144, measure, false)
	rtl := buildDimensionsForDirection(items, 144, measure, true)

	if ltr.width != rtl.width || ltr.height != rtl.height {
		t.Fatalf("direction changed menu size: LTR=%#v RTL=%#v", ltr, rtl)
	}
	if rtl.checkX != rtl.width-(ltr.checkX+scale(11, rtl.dpi)) {
		t.Fatalf("check x = %d, want mirrored LTR column", rtl.checkX)
	}
	if rtl.iconX != rtl.width-(ltr.iconX+ltr.iconSize) {
		t.Fatalf("icon x = %d, want mirrored LTR column", rtl.iconX)
	}
	if rtl.textX != rtl.width-ltr.textRight ||
		rtl.textRight != rtl.width-ltr.textX {
		t.Fatalf("text bounds were not mirrored: LTR=%#v RTL=%#v", ltr, rtl)
	}
	if rtl.shortcutX != rtl.width-ltr.shortcutRight ||
		rtl.shortcutRight != rtl.width-ltr.shortcutX {
		t.Fatalf("shortcut bounds were not mirrored: LTR=%#v RTL=%#v", ltr, rtl)
	}
	if rtl.shortcutRight > rtl.textX {
		t.Fatalf("RTL shortcut and label overlap: %#v", rtl)
	}
}

func TestFitDimensionsHeightKeepsStyledMenuOnShortHighDPIMonitor(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "show", Label: "Lamp is off", IconPNG: []byte{1}},
		{Separator: true},
		{ID: "toggle", Label: "Toggle lamp", IconPNG: []byte{1}},
		{ID: "on", Label: "Turn on", IconPNG: []byte{1}},
		{ID: "off", Label: "Turn off", IconPNG: []byte{1}},
		{Separator: true},
		{ID: "view", Label: "Use dashboard view", IconPNG: []byte{1}},
		{ID: "settings", Label: "Settings", IconPNG: []byte{1}},
		{ID: "diagnostics", Label: "Diagnostics", IconPNG: []byte{1}},
		{ID: "web", Label: "Web dashboard", IconPNG: []byte{1}},
		{ID: "reconnect", Label: "Reconnect", IconPNG: []byte{1}},
		{Separator: true},
		{ID: "pin", Label: "Always on top", IconPNG: []byte{1}},
		{ID: "startup", Label: "Start with Windows", IconPNG: []byte{1}},
		{Separator: true},
		{ID: "quit", Label: "Exit Rayan Lamp", IconPNG: []byte{1}},
	}
	original := buildDimensions(items, 192, func(value string) int32 {
		return int32(len(value) * 16)
	})
	const availableHeight = int32(700)
	if original.height <= availableHeight {
		t.Fatalf("fixture height = %d, want oversized", original.height)
	}

	fitted, ok := fitDimensionsHeight(original, availableHeight)
	if !ok {
		t.Fatal("readable high-DPI menu was rejected instead of compacted")
	}
	if fitted.height > availableHeight {
		t.Fatalf("fitted height = %d, maximum %d", fitted.height, availableHeight)
	}
	if fitted.itemHeight < fitted.iconSize+scale(4, fitted.dpi) {
		t.Fatalf(
			"item height %d clips %dpx icon",
			fitted.itemHeight,
			fitted.iconSize,
		)
	}
	if len(fitted.rows) != len(items) {
		t.Fatalf("rows = %d, want %d", len(fitted.rows), len(items))
	}
	for index, row := range fitted.rows {
		if row.top >= row.bottom {
			t.Fatalf("row %d has invalid bounds %+v", index, row)
		}
		if index > 0 && fitted.rows[index-1].bottom != row.top {
			t.Fatalf("row %d is not contiguous", index)
		}
		if items[index].Separator {
			continue
		}
		if got := fitted.rowAt(
			fitted.outerPadding+1,
			row.top+(row.bottom-row.top)/2,
		); got != index {
			t.Fatalf("rowAt item %d = %d", index, got)
		}
	}
}

func TestFitDimensionsHeightRejectsUnreadableDensity(t *testing.T) {
	t.Parallel()
	items := []Item{
		{ID: "one", Label: "One", IconPNG: []byte{1}},
		{ID: "two", Label: "Two", IconPNG: []byte{1}},
	}
	original := buildDimensions(items, 192, func(value string) int32 {
		return int32(len(value) * 16)
	})
	fitted, ok := fitDimensionsHeight(original, scale(40, 192))
	if ok {
		t.Fatalf("unreadable fit accepted: %#v", fitted)
	}
	if fitted.height != original.height {
		t.Fatal("rejected fit mutated the original dimensions")
	}
}

func TestPlacePopup(t *testing.T) {
	t.Parallel()
	work := Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}

	x, y := placePopup(
		Rect{Left: 50, Top: 50, Right: 100, Bottom: 80},
		work,
		200,
		300,
		4,
		AlignStart,
	)
	if x != 50 || y != 80 {
		t.Fatalf("below/start = (%d,%d), want (50,80)", x, y)
	}

	x, y = placePopup(
		Rect{Left: 900, Top: 740, Right: 980, Bottom: 780},
		work,
		240,
		300,
		4,
		AlignEnd,
	)
	if x != 740 || y != 440 {
		t.Fatalf("above/end = (%d,%d), want (740,440)", x, y)
	}

	x, y = placePopup(
		Rect{Left: -100, Top: 790, Right: -50, Bottom: 810},
		work,
		300,
		900,
		4,
		AlignAuto,
	)
	if x != 4 || y != 4 {
		t.Fatalf("clamped oversized = (%d,%d), want (4,4)", x, y)
	}
}

func TestPlacePopupUsesLogicalStartAndEndForRTL(t *testing.T) {
	t.Parallel()
	work := Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	anchor := Rect{Left: 300, Top: 50, Right: 420, Bottom: 80}

	startX, _ := placePopupForDirection(
		anchor, work, 200, 300, 4, AlignStart, true,
	)
	endX, _ := placePopupForDirection(
		anchor, work, 200, 300, 4, AlignEnd, true,
	)
	if startX != 220 {
		t.Fatalf("RTL start x = %d, want right-edge alignment 220", startX)
	}
	if endX != 300 {
		t.Fatalf("RTL end x = %d, want left-edge alignment 300", endX)
	}
}

func TestPalettesAreDistinctAndReadable(t *testing.T) {
	t.Parallel()
	light := paletteFor(false)
	dark := paletteFor(true)
	if light.background == dark.background || light.text == dark.text {
		t.Fatal("light and dark palettes are not distinct")
	}
	if luminance(light.background) <= luminance(light.text) {
		t.Fatal("light palette text/background contrast direction is wrong")
	}
	if luminance(dark.background) >= luminance(dark.text) {
		t.Fatal("dark palette text/background contrast direction is wrong")
	}
}

func luminance(color rgb) int {
	return 299*int(color.r) + 587*int(color.g) + 114*int(color.b)
}
