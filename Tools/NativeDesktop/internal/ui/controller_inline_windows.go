//go:build windows

package ui

import (
	"context"
	"strings"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"rayanlamp.local/app/internal/dialogs"
)

type controllerInlineEdit struct {
	key, revision, originalName, originalIcon, icon string
	popup                                           *walk.Dialog
	input                                           *walk.LineEdit
	theme                                           *dialogs.ThemeSession
	boundsHandler                                   int
}

func (s *Shell) beginControllerInline(key string) {
	if !s.canEditControllerChannel() {
		return
	}
	if s.controllerInline != nil {
		s.controllerInline.input.SetFocus()
		return
	}
	var hit *controllerChannelHit
	for i := range s.controllerChannelHits {
		if s.controllerChannelHits[i].key == key {
			hit = &s.controllerChannelHits[i]
			break
		}
	}
	if hit == nil {
		return
	}
	popup, err := walk.NewDialogWithFixedSize(s.mw)
	if err != nil {
		s.showError("Rename channel", err)
		return
	}
	popup.SetPersistent(false)
	popup.SetCenterInOwnerWhenRun(false)
	layout := walk.NewHBoxLayout()
	layout.SetMargins(walk.Margins{})
	layout.SetSpacing(0)
	popup.SetLayout(layout)
	// An owned native edit overlay remains visible above both the DComp and
	// UpdateLayeredWindow surfaces. It is not modal and never reflows the card.
	popupStyle := uint32(win.GetWindowLong(popup.Handle(), win.GWL_STYLE))&^uint32(win.WS_CAPTION|win.WS_SYSMENU|win.WS_THICKFRAME) | win.WS_POPUP
	win.SetWindowLong(popup.Handle(), win.GWL_STYLE, int32(popupStyle))
	win.SetWindowLong(popup.Handle(), win.GWL_EXSTYLE, int32(win.WS_EX_TOOLWINDOW|win.WS_EX_CONTROLPARENT))
	win.SetWindowPos(popup.Handle(), 0, 0, 0, 0, 0, win.SWP_FRAMECHANGED|win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_NOACTIVATE)
	input, err := walk.NewLineEdit(popup)
	if err != nil {
		popup.Dispose()
		s.showError("Rename channel", err)
		return
	}
	input.SetFont(s.bodyFont)
	// The editor shares the caption's fixed row; native layout hints must not
	// grow the owned overlay below that row at fractional DPI.
	input.SetMinMaxSizePixels(walk.Size{Height: hit.name.Height}, walk.Size{Height: hit.name.Height})
	popup.SetMinMaxSizePixels(walk.Size{Height: hit.name.Height}, walk.Size{Height: hit.name.Height})
	input.SetText(hit.channel.Name)
	input.SetMaxLength(96)
	input.SetToolTipText("Channel name · Enter to save · Escape to cancel")
	theme := dialogs.NewThemeSession(popup)
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	edit := &controllerInlineEdit{key: key, revision: s.controllerState.CatalogRevision, originalName: hit.channel.Name, originalIcon: hit.channel.Icon, icon: hit.channel.Icon, popup: popup, input: input, theme: theme}
	s.controllerInline = edit
	edit.boundsHandler = s.mw.BoundsChanged().Attach(s.positionControllerInline)
	input.TextChanged().Attach(func() {
		if s.surface != nil {
			s.surface.Invalidate()
		}
	})
	input.KeyDown().Attach(func(key walk.Key) {
		switch key {
		case walk.KeyReturn:
			s.saveControllerInline()
		case walk.KeyEscape:
			if !s.controllerBusy {
				s.closeControllerInline()
			}
		}
	})
	s.positionControllerInline()
	popup.Show()
	s.positionControllerInline()
	input.SetFocus()
	input.SetTextSelection(0, -1)
	s.surface.Invalidate()
}

func (s *Shell) positionControllerInline() {
	e := s.controllerInline
	if e == nil || s.surface == nil {
		return
	}
	for _, hit := range s.controllerChannelHits {
		if hit.key != e.key {
			continue
		}
		r := hit.name
		// Account for the actual native formatting rectangle, not the outer
		// EDIT HWND. The donor theme installs its own margins on resize.
		e.input.SetBoundsPixels(walk.Rectangle{Width: r.Width + 4, Height: r.Height})
		e.input.SendMessage(win.EM_SETMARGINS, 3, 3|3<<16)
		var textRect win.RECT
		e.input.SendMessage(win.EM_GETRECT, 0, uintptr(unsafe.Pointer(&textRect)))
		lineHeight := controllerEditLineHeight(e.input)
		p := win.POINT{X: int32(r.X) - textRect.Left, Y: int32(r.Y+(r.Height-lineHeight)/2) - textRect.Top}
		win.ClientToScreen(s.surface.Handle(), &p)
		bounds := walk.Rectangle{X: int(p.X), Y: int(p.Y), Width: r.Width + 4, Height: r.Height}
		if e.popup.BoundsPixels() != bounds {
			e.popup.SetBoundsPixels(bounds)
		}
		return
	}
}

func controllerEditLineHeight(input *walk.LineEdit) int {
	dc := win.GetDC(input.Handle())
	defer win.ReleaseDC(input.Handle(), dc)
	font := win.HGDIOBJ(input.SendMessage(win.WM_GETFONT, 0, 0))
	old := win.SelectObject(dc, font)
	defer win.SelectObject(dc, old)
	var metrics win.TEXTMETRIC
	win.GetTextMetrics(dc, &metrics)
	return int(metrics.TmHeight)
}

func (s *Shell) closeControllerInline() {
	e := s.controllerInline
	if e == nil {
		return
	}
	s.controllerInline = nil
	if s.mw != nil {
		s.mw.BoundsChanged().Detach(e.boundsHandler)
	}
	e.theme.Dispose()
	e.popup.Dispose()
	if s.surface != nil && !s.closed.Load() {
		s.surface.SetFocus()
		s.surface.Invalidate()
	}
}

func (s *Shell) canSaveControllerInline() bool {
	e := s.controllerInline
	return e != nil && !s.controllerBusy && !s.controllerState.Busy && strings.TrimSpace(e.input.Text()) != "" && (strings.TrimSpace(e.input.Text()) != e.originalName || e.icon != e.originalIcon)
}

func (s *Shell) saveControllerInline() {
	if !s.canSaveControllerInline() {
		return
	}
	e := s.controllerInline
	fields := map[string]any{"name": strings.TrimSpace(e.input.Text()), "icon": e.icon}
	if s.controllerState.Preview {
		s.saveControllerChannel(e.key, e.revision, fields)
		s.closeControllerInline()
		return
	}
	if s.controller.UpdateChannel == nil {
		return
	}
	e.input.SetEnabled(false)
	s.runControllerOperationThen(func(ctx context.Context) error { return s.controller.UpdateChannel(ctx, e.key, e.revision, fields) }, func(err error) {
		if s.controllerInline != e {
			return
		}
		if err == nil {
			s.closeControllerInline()
		} else {
			e.input.SetEnabled(true)
			e.input.SetFocus()
		}
	})
}

func (s *Shell) pickControllerInlineIcon() {
	e := s.controllerInline
	if e == nil || s.controllerBusy {
		return
	}
	e.popup.SetVisible(false)
	if value, ok := s.chooseControllerIcon(e.icon); ok {
		e.icon = value
		s.surface.Invalidate()
	}
	if s.controllerInline == e {
		e.popup.Show()
		s.positionControllerInline()
		e.input.SetFocus()
	}
}
