package traymenu

const (
	baseDPI              = int32(96)
	popupCornerRadiusDIP = int32(8)
)

type textMeasure func(string) int32

type dimensions struct {
	dpi             int32
	width           int32
	height          int32
	outerPadding    int32
	itemHeight      int32
	separatorHeight int32
	checkColumn     int32
	iconColumn      int32
	iconSize        int32
	checkX          int32
	iconX           int32
	textX           int32
	textRight       int32
	shortcutX       int32
	shortcutRight   int32
	cornerRadius    int32
	rightToLeft     bool
	rows            []row
}

type row struct {
	index  int
	top    int32
	bottom int32
}

func scale(value, dpi int32) int32 {
	if dpi <= 0 {
		dpi = baseDPI
	}
	result := (int64(value)*int64(dpi) + int64(baseDPI)/2) / int64(baseDPI)
	if result < 1 && value > 0 {
		return 1
	}
	return int32(result)
}

func buildDimensions(items []Item, dpi int32, measure textMeasure) dimensions {
	return buildDimensionsForDirection(items, dpi, measure, false)
}

func buildDimensionsForDirection(
	items []Item,
	dpi int32,
	measure textMeasure,
	rightToLeft bool,
) dimensions {
	if dpi <= 0 {
		dpi = baseDPI
	}
	d := dimensions{
		dpi:             dpi,
		outerPadding:    scale(6, dpi),
		itemHeight:      scale(38, dpi),
		separatorHeight: scale(9, dpi),
		iconSize:        scale(22, dpi),
		cornerRadius:    scale(popupCornerRadiusDIP, dpi),
		rightToLeft:     rightToLeft,
		rows:            make([]row, 0, len(items)),
	}

	var hasChecks, hasIcons bool
	var labelWidth, shortcutWidth int32
	for _, item := range items {
		hasChecks = hasChecks || item.Checked
		hasIcons = hasIcons || len(item.IconPNG) != 0 || item.IconHandle != 0
		if item.Separator {
			continue
		}
		if width := measure(item.Label); width > labelWidth {
			labelWidth = width
		}
		if width := measure(item.Shortcut); width > shortcutWidth {
			shortcutWidth = width
		}
	}

	if hasChecks {
		d.checkColumn = scale(22, dpi)
	}
	if hasIcons {
		d.iconColumn = scale(30, dpi)
	}
	leftPadding := scale(10, dpi)
	rightPadding := scale(12, dpi)
	d.checkX = d.outerPadding + leftPadding
	d.iconX = d.checkX + d.checkColumn
	columnGap := int32(0)
	if shortcutWidth > 0 {
		columnGap = scale(28, dpi)
		maxShortcut := scale(150, dpi)
		if shortcutWidth > maxShortcut {
			shortcutWidth = maxShortcut
		}
	}
	d.textX = d.outerPadding + leftPadding + d.checkColumn + d.iconColumn

	minLabel := scale(100, dpi)
	if labelWidth < minLabel {
		labelWidth = minLabel
	}
	d.width = d.textX + labelWidth + columnGap + shortcutWidth + rightPadding + d.outerPadding
	minWidth := scale(180, dpi)
	maxWidth := scale(520, dpi)
	if d.width < minWidth {
		d.width = minWidth
	}
	if d.width > maxWidth {
		d.width = maxWidth
	}
	contentRight := d.width - d.outerPadding - rightPadding
	d.textRight = contentRight
	d.shortcutX = contentRight
	d.shortcutRight = contentRight
	if shortcutWidth > 0 {
		d.shortcutX = contentRight - shortcutWidth
		d.textRight = d.shortcutX - scale(12, dpi)
		minTextRight := d.textX + scale(48, dpi)
		if d.textRight < minTextRight {
			d.textRight = minTextRight
			d.shortcutX = d.textRight + scale(12, dpi)
		}
	}
	if rightToLeft {
		checkWidth := scale(11, dpi)
		d.checkX = d.width - (d.checkX + checkWidth)
		d.iconX = d.width - (d.iconX + d.iconSize)
		textX, textRight := d.textX, d.textRight
		shortcutX, shortcutRight := d.shortcutX, d.shortcutRight
		d.textX = d.width - textRight
		d.textRight = d.width - textX
		d.shortcutX = d.width - shortcutRight
		d.shortcutRight = d.width - shortcutX
	}

	y := d.outerPadding
	for index, item := range items {
		height := d.itemHeight
		if item.Separator {
			height = d.separatorHeight
		}
		d.rows = append(d.rows, row{index: index, top: y, bottom: y + height})
		y += height
	}
	d.height = y + d.outerPadding
	return d
}

// fitDimensionsHeight preserves the complete styled menu on short or
// high-DPI work areas by reducing only vertical density. Icons, type, corner
// geometry, and horizontal metrics retain their requested device-pixel size.
// This is preferable to silently replacing the menu with TrackPopupMenu.
func fitDimensionsHeight(d dimensions, maxHeight int32) (dimensions, bool) {
	if d.height <= maxHeight {
		return d, true
	}
	if maxHeight <= d.outerPadding*2 {
		return d, false
	}

	separators := make([]bool, len(d.rows))
	var itemCount, separatorCount int32
	for index, row := range d.rows {
		if row.index < 0 {
			continue
		}
		if row.bottom-row.top == d.separatorHeight {
			separators[index] = true
			separatorCount++
		} else {
			itemCount++
		}
	}
	if itemCount == 0 {
		return d, false
	}

	minimumItemHeight := scale(26, d.dpi)
	if iconMinimum := d.iconSize + scale(4, d.dpi); iconMinimum > minimumItemHeight {
		minimumItemHeight = iconMinimum
	}
	minimumSeparatorHeight := scale(5, d.dpi)
	contentHeight := d.height - d.outerPadding*2
	availableContent := maxHeight - d.outerPadding*2
	if availableContent < itemCount*minimumItemHeight+
		separatorCount*minimumSeparatorHeight {
		return d, false
	}

	// Start with the same proportional density at every DPI, then trim at
	// most a few rounding pixels while respecting readable minima.
	itemHeight := int32(
		(int64(d.itemHeight)*int64(availableContent) +
			int64(contentHeight)/2) / int64(contentHeight),
	)
	if itemHeight < minimumItemHeight {
		itemHeight = minimumItemHeight
	}
	separatorHeight := int32(
		(int64(d.separatorHeight)*int64(availableContent) +
			int64(contentHeight)/2) / int64(contentHeight),
	)
	if separatorHeight < minimumSeparatorHeight {
		separatorHeight = minimumSeparatorHeight
	}
	heightFor := func() int32 {
		return d.outerPadding*2 +
			itemCount*itemHeight +
			separatorCount*separatorHeight
	}
	for heightFor() > maxHeight {
		switch {
		case itemHeight > minimumItemHeight:
			itemHeight--
		case separatorHeight > minimumSeparatorHeight:
			separatorHeight--
		default:
			return d, false
		}
	}

	d.itemHeight = itemHeight
	d.separatorHeight = separatorHeight
	y := d.outerPadding
	for index := range d.rows {
		height := d.itemHeight
		if separators[index] {
			height = d.separatorHeight
		}
		d.rows[index].top = y
		d.rows[index].bottom = y + height
		y += height
	}
	d.height = y + d.outerPadding
	return d, d.height <= maxHeight
}

func (d dimensions) rowAt(x, y int32) int {
	if x < d.outerPadding || x >= d.width-d.outerPadding {
		return -1
	}
	for _, row := range d.rows {
		if y >= row.top && y < row.bottom {
			return row.index
		}
	}
	return -1
}

func placePopup(anchor Rect, workArea Rect, width, height, margin int32, alignment Alignment) (x, y int32) {
	return placePopupForDirection(
		anchor,
		workArea,
		width,
		height,
		margin,
		alignment,
		false,
	)
}

func placePopupForDirection(
	anchor Rect,
	workArea Rect,
	width, height, margin int32,
	alignment Alignment,
	rightToLeft bool,
) (x, y int32) {
	if margin < 0 {
		margin = 0
	}
	leftLimit := workArea.Left + margin
	rightLimit := workArea.Right - margin
	topLimit := workArea.Top + margin
	bottomLimit := workArea.Bottom - margin

	alignRight := popupAlignsRight(anchor, workArea, alignment, rightToLeft)
	if alignRight {
		x = anchor.Right - width
	} else {
		x = anchor.Left
	}
	if x < leftLimit {
		x = leftLimit
	}
	if x+width > rightLimit {
		x = rightLimit - width
	}

	switch {
	case anchor.Bottom+height <= bottomLimit:
		y = anchor.Bottom
	case anchor.Top-height >= topLimit:
		y = anchor.Top - height
	default:
		y = anchor.Bottom
		if y+height > bottomLimit {
			y = bottomLimit - height
		}
		if y < topLimit {
			y = topLimit
		}
	}
	return x, y
}

func popupAlignsRight(
	anchor Rect,
	workArea Rect,
	alignment Alignment,
	rightToLeft bool,
) bool {
	if alignment == AlignAuto {
		anchorCenter := int64(anchor.Left) + int64(anchor.Right)
		workCenter := int64(workArea.Left) + int64(workArea.Right)
		return anchorCenter > workCenter
	}
	if rightToLeft {
		return alignment == AlignStart
	}
	return alignment == AlignEnd
}
