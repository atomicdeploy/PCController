//go:build windows

package ui

import (
	"fmt"
	"github.com/lxn/walk"
	"github.com/lxn/win"
	"syscall"
)

// Measure captions and reserve only the rows the visible actions need. Retain
// the native grid/HWNDs while wrapping; no greedy flow can steal viewport space.
type controllerFormFooter struct {
	root                   *walk.Composite
	actions                *walk.ScrollView
	refresh, close         *walk.PushButton
	refreshCell, closeCell *walk.Composite
	layoutKey              string
}

func (f *controllerFormFooter) reflow(width int) {
	if f.root == nil || f.actions == nil || f.refresh == nil || f.close == nil || f.refreshCell == nil || f.closeCell == nil || f.root.IsDisposed() {
		return
	}
	var buttons []*walk.PushButton
	var allButtons []*walk.PushButton
	var widths []int
	var actionGrid *walk.Composite
	var collect func(walk.Container)
	collect = func(root walk.Container) {
		for i := 0; i < root.Children().Len(); i++ {
			child := root.Children().At(i)
			if button, ok := child.(*walk.PushButton); ok {
				allButtons = append(allButtons, button)
			}
			// Use this widget's visibility intent even before the modal owner is
			// shown. IsWindowVisible also tests parents and would collapse all
			// actions during the initial render.
			if button, ok := child.(*walk.PushButton); ok && win.GetWindowLong(button.Handle(), win.GWL_STYLE)&win.WS_VISIBLE != 0 {
				w := max(96, min(240, button.IntTo96DPI(button.SizeHint().Width)))
				if text, err := syscall.UTF16FromString(button.Text()); err == nil {
					dc := win.GetDC(button.Handle())
					if dc != 0 {
						font := win.SendMessage(button.Handle(), win.WM_GETFONT, 0, 0)
						old := win.SelectObject(dc, win.HGDIOBJ(font))
						var size win.SIZE
						if win.GetTextExtentPoint32(dc, &text[0], int32(len(text)-1), &size) {
							w = max(w, button.IntTo96DPI(int(size.CX))+48)
						}
						win.SelectObject(dc, old)
						win.ReleaseDC(button.Handle(), dc)
					}
				}
				w = min(240, w)
				_ = button.SetMinMaxSize(walk.Size{Width: w, Height: 32}, walk.Size{Width: w, Height: 32})
				buttons = append(buttons, button)
				widths = append(widths, w)
			}
			if nested, ok := child.(walk.Container); ok {
				if composite, ok := child.(*walk.Composite); ok {
					if _, ok := composite.Layout().(*walk.GridLayout); ok {
						actionGrid = composite
					}
				}
				collect(nested)
			}
		}
	}
	collect(f.actions)
	available := max(96, width-216)
	total := max(0, len(buttons)-1) * 8
	for _, w := range widths {
		total += w
	}
	above := total > available
	if above {
		available = max(96, width)
	}
	maximum := 96
	for _, w := range widths {
		maximum = max(maximum, w)
	}
	columns := max(1, len(buttons))
	if total > available {
		columns = max(1, min(len(buttons), (available+8)/(maximum+8)))
	}
	rows := max(1, (len(buttons)+columns-1)/columns)
	actionHeight := rows*34 + (rows-1)*8
	if len(buttons) == 0 {
		actionHeight = 0
	}
	key := fmt.Sprintf("%p/%d/%t/%v", actionGrid, columns, above, widths)
	for _, button := range allButtons {
		key += fmt.Sprintf("/%x", button.Handle())
	}
	// Continuous sizing normally keeps the same row/column arrangement. Let
	// native layouts adjust widths without repeatedly parking every HWND.
	if key == f.layoutKey {
		return
	}
	f.layoutKey = key
	if actionGrid != nil {
		layout := actionGrid.Layout().(*walk.GridLayout)
		for i, button := range allButtons {
			_ = layout.SetRange(button, walk.Rectangle{X: 0, Y: len(allButtons) + i + 2, Width: 1, Height: 1})
		}
		for i, button := range buttons {
			_ = layout.SetRange(button, walk.Rectangle{X: 1 + i%columns, Y: i / columns, Width: 1, Height: 1})
		}
		for i := 0; i < actionGrid.Children().Len(); i++ {
			if spacer, ok := actionGrid.Children().At(i).(*walk.Spacer); ok {
				_ = layout.SetRange(spacer, walk.Rectangle{X: 0, Y: 0, Width: 1, Height: rows})
			}
		}
		_ = actionGrid.SetMinMaxSize(walk.Size{Height: actionHeight}, walk.Size{Height: max(1, actionHeight)})
	}
	grid := f.root.Layout().(*walk.GridLayout)
	// Keep existing HWNDs/layout, including focused and disabled action state.
	_ = grid.SetRange(f.actions, walk.Rectangle{X: 0, Y: 3, Width: 1, Height: 1})
	_ = grid.SetRange(f.refreshCell, walk.Rectangle{X: 0, Y: 4, Width: 1, Height: 1})
	_ = grid.SetRange(f.closeCell, walk.Rectangle{X: 0, Y: 5, Width: 1, Height: 1})
	height := 34
	if above {
		_ = grid.SetRange(f.actions, walk.Rectangle{X: 0, Y: 0, Width: 3, Height: 1})
		_ = grid.SetRange(f.refreshCell, walk.Rectangle{X: 0, Y: 1, Width: 1, Height: 1})
		_ = grid.SetRange(f.closeCell, walk.Rectangle{X: 2, Y: 1, Width: 1, Height: 1})
		height += actionHeight + 8
	} else {
		_ = grid.SetRange(f.refreshCell, walk.Rectangle{X: 0, Y: 0, Width: 1, Height: 1})
		_ = grid.SetRange(f.actions, walk.Rectangle{X: 1, Y: 0, Width: 1, Height: 1})
		_ = grid.SetRange(f.closeCell, walk.Rectangle{X: 2, Y: 0, Width: 1, Height: 1})
	}
	_ = grid.SetColumnStretchFactor(1, 4)
	_ = f.actions.SetMinMaxSize(walk.Size{Height: actionHeight}, walk.Size{Height: max(1, actionHeight)})
	_ = f.root.SetMinMaxSize(walk.Size{Height: height}, walk.Size{Height: height})
}

// Retain native editors and their drafts across resize; only the layout changes.
type controllerFormRow struct {
	root, editor *walk.Composite
	label        *walk.Label
	multiline    bool
	initialized  bool
	stacked      bool
	labelWidth   int
}

func (r *controllerFormRow) reflow(width int) {
	if r.root == nil || r.label == nil || r.editor == nil || r.root.IsDisposed() {
		return
	}
	stacked := width < 600
	labelWidth := max(128, min(176, width/4))
	if r.initialized && r.stacked == stacked && r.labelWidth == labelWidth {
		return
	}
	if !r.initialized || r.stacked != stacked {
		layout, ok := r.root.Layout().(*walk.BoxLayout)
		if !ok {
			return
		}
		orientation := walk.Orientation(walk.Horizontal)
		if stacked {
			orientation = walk.Vertical
		}
		_ = layout.SetOrientation(orientation)
		_ = layout.SetMargins(walk.Margins{})
		_ = layout.SetSpacing(10)
		alignment := walk.AlignHNearVCenter
		if stacked || r.multiline {
			alignment = walk.AlignHNearVNear
		}
		_ = layout.SetAlignment(alignment)
		_ = layout.SetStretchFactor(r.editor, 4)
	}
	labelMin, labelMax := walk.Size{Width: labelWidth, Height: 32}, walk.Size{Width: labelWidth, Height: 32}
	height := 32
	if stacked {
		labelMin, labelMax = walk.Size{Height: 20}, walk.Size{Height: 24}
		height = 66
	}
	_ = r.label.SetMinMaxSize(labelMin, labelMax)
	if r.multiline {
		height = 0
	}
	_ = r.root.SetMinMaxSize(walk.Size{}, walk.Size{Height: height})
	r.initialized, r.stacked, r.labelWidth = true, stacked, labelWidth
}

func controllerFormGlyph(key string) string {
	switch key {
	case "connection", "network", "peers":
		return "\uE839"
	case "rf", "rf-bindings", "rf-transmit":
		return "\uE704"
	case "buzzer", "melodies":
		return "\uE767"
	case "strip", "status-lighting":
		return "\uE793"
	case "lcd", "display":
		return "\uE7F4"
	case "firmware", "updates", "firmware-tools":
		return "\uE896"
	case "events", "history":
		return "\uE9D9"
	case "sequence", "programs", "host-automations", "board-automations":
		return "\uE768"
	default:
		return "\uE713"
	}
}

func controllerFormWindowHeight(key string) int {
	switch key {
	case "connection":
		return 380
	case "display":
		return 460
	default:
		return 620
	}
}
