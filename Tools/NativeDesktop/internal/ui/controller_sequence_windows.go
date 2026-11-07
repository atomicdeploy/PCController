//go:build windows

package ui

import (
	"fmt"
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/image/vector"
	"image"
	"image/color"
	"math"
	"pccontroller.local/desktop/internal/winfx"
	"pccontroller.local/desktop/presentation"
	"strconv"
)

type controllerTimelineHit struct {
	key, lane string
	rect      walk.Rectangle
	at        uint32
	slot      int
}

func controllerTimelineLayout(cues []presentation.TimelineCue, width, height, dpi int) ([]controllerTimelineHit, uint64, int) {
	scale := float64(max(96, dpi)) / 96
	left := int(94 * scale)
	plot := max(1, width-left-int(12*scale))
	span := uint64(1000000)
	for _, cue := range cues {
		span = max(span, uint64(cue.AtUS)+uint64(cue.DurationMS)*1000)
	}
	span += span / 20
	lanes := map[string]int{}
	hits := make([]controllerTimelineHit, 0, len(cues))
	row := max(1, int(19*scale))
	top := int(24 * scale)
	for _, cue := range cues {
		slot, ok := lanes[cue.Lane]
		if !ok {
			slot = min(len(lanes), 4)
			lanes[cue.Lane] = slot
		}
		x := left + int(uint64(cue.AtUS)*uint64(plot)/span)
		bar := max(int(6*scale), int(uint64(cue.DurationMS)*1000*uint64(plot)/span))
		bar = min(bar, max(1, width-x-4))
		hits = append(hits, controllerTimelineHit{cue.Key, cue.Lane, walk.Rectangle{X: x, Y: top + slot*row, Width: bar, Height: max(4, row-int(5*scale))}, cue.AtUS, slot})
	}
	return hits, span, left
}

func timelineRoundedPath(v *vector.Rasterizer, r walk.Rectangle) {
	x, y, w, h := float32(r.X), float32(r.Y), float32(r.Width), float32(r.Height)
	radius := min(float32(4), min(w, h)/2)
	v.MoveTo(x+radius, y)
	v.LineTo(x+w-radius, y)
	v.QuadTo(x+w, y, x+w, y+radius)
	v.LineTo(x+w, y+h-radius)
	v.QuadTo(x+w, y+h, x+w-radius, y+h)
	v.LineTo(x+radius, y+h)
	v.QuadTo(x, y+h, x, y+h-radius)
	v.LineTo(x, y+radius)
	v.QuadTo(x, y, x+radius, y)
	v.ClosePath()
}

func (s *Shell) controllerTimelineWidget(cues []presentation.TimelineCue, selected func() string, selectKey func(string), at func() string, setAt func(string), assign **walk.CustomWidget) d.Widget {
	var hits []controllerTimelineHit
	var span uint64
	var left int
	dragging := false
	pressX := 0
	pressAt := uint32(0)
	paint := func(c *walk.Canvas, b walk.Rectangle) error {
		appearance, _ := winfx.ResolveAppearance(s.Config().UI.Theme)
		p := controllerPalette(appearance.Dark)
		brush, err := walk.NewSolidColorBrush(p.panel)
		if err != nil {
			return err
		}
		defer brush.Dispose()
		if err := c.FillRectanglePixels(brush, b); err != nil {
			return err
		}
		hits, span, left = controllerTimelineLayout(cues, b.Width, b.Height, c.DPI())
		selectedKey := selected()
		plot := max(1, b.Width-left-12)
		for i := range hits {
			if hits[i].key == selectedKey {
				if value, err := strconv.ParseUint(at(), 10, 32); err == nil {
					hits[i].rect.X = left + int(value*uint64(plot)/span)
				}
			}
		}
		ink := []walk.Color{p.accent, p.success, p.warning, p.danger, p.secondary, p.accentHover}
		target := image.NewRGBA(image.Rect(0, 0, b.Width, b.Height))
		for slot, paint := range ink {
			v := vector.NewRasterizer(b.Width, b.Height)
			for _, hit := range hits {
				group := hit.slot
				if hit.key == selectedKey {
					group = 5
				}
				if group == slot {
					timelineRoundedPath(v, hit.rect)
				}
			}
			v.Draw(target, target.Bounds(), image.NewUniform(color.RGBA{R: byte(paint), G: byte(paint >> 8), B: byte(paint >> 16), A: 255}), image.Point{})
		}
		bitmap, err := walk.NewBitmapFromImageForDPI(target, c.DPI())
		if err != nil {
			return err
		}
		defer bitmap.Dispose()
		if err := c.DrawImagePixels(bitmap, walk.Point{}); err != nil {
			return err
		}
		seen := map[int]bool{}
		for _, hit := range hits {
			if !seen[hit.slot] {
				seen[hit.slot] = true
				label := hit.lane
				if hit.slot == 4 {
					label = s.nativeText("Other")
				}
				_ = c.DrawTextPixels(label, s.captionFont, p.secondary, walk.Rectangle{X: 6, Y: hit.rect.Y, Width: left - 12, Height: hit.rect.Height}, walk.TextSingleLine|walk.TextVCenter|walk.TextEndEllipsis)
			}
		}
		axis := walk.Rectangle{X: left, Y: 0, Width: b.Width - left - 10, Height: max(18, int(20*float64(c.DPI())/96))}
		_ = c.DrawTextPixels("0 s", s.captionFont, p.secondary, axis, walk.TextSingleLine|walk.TextVCenter)
		return c.DrawTextPixels(fmt.Sprintf("%.3f s", float64(span)/1e6), s.captionFont, p.secondary, axis, walk.TextSingleLine|walk.TextVCenter|walk.TextRight)
	}
	return d.CustomWidget{AssignTo: assign, DoubleBuffering: true, InvalidatesOnResize: true, MinSize: d.Size{Height: 120}, MaxSize: d.Size{Height: 120}, PaintPixels: paint,
		OnMouseDown: func(x, y int, button walk.MouseButton) {
			if button != walk.LeftButton {
				return
			}
			point := walk.Point{X: x, Y: y}
			for i := len(hits) - 1; i >= 0; i-- {
				hit := hits[i]
				if pointInRect(point, hit.rect) {
					selectKey(hit.key)
					if hit.key != selected() {
						return
					}
					dragging = true
					pressX = x
					pressAt = hit.at
					if value, err := strconv.ParseUint(at(), 10, 32); err == nil {
						pressAt = uint32(value)
					}
					win.SetCapture((*assign).Handle())
					return
				}
			}
		},
		OnMouseMove: func(x, y int, button walk.MouseButton) {
			if !dragging {
				return
			}
			if controllerMotionCapture() != (*assign).Handle() {
				dragging = false
				return
			}
			width := max(1, (*assign).ClientBoundsPixels().Width-left-12)
			value := math.Max(0, math.Min(2147483647, float64(pressAt)+float64(x-pressX)*float64(span)/float64(width)))
			setAt(strconv.FormatUint(uint64(math.Round(value)), 10))
			(*assign).Invalidate()
		},
		OnMouseUp: func(_, _ int, button walk.MouseButton) {
			if button == walk.LeftButton {
				dragging = false
				if controllerMotionCapture() == (*assign).Handle() {
					win.ReleaseCapture()
				}
			}
		},
	}
}
