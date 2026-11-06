//go:build windows

package ui

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"

	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/vector"
	"pccontroller.local/desktop/internal/winfx"
)

const controllerTaskbarFirst uint32 = 0x52B1

func (s *Shell) currentTaskbarButtons(icons taskbarButtonIcons) []winfx.ThumbnailButton {
	if s.controller == nil {
		return taskbarButtonsForLocalizer(s.snapshot, s.taskbarSuspended, icons, s.localizerSnapshot())
	}
	state := s.controllerState
	disabled := !state.Connected || !state.CanNavigate || state.Busy || s.controllerBusy || s.taskbarSuspended
	labels := []string{"Previous menu", "Next menu", "Decrease", "Increase"}
	buttons := make([]winfx.ThumbnailButton, 4)
	for i := range buttons {
		buttons[i] = winfx.ThumbnailButton{ID: controllerTaskbarFirst + uint32(i), Icon: uintptr(icons.controller[i]), Tooltip: labels[i], Disabled: disabled}
	}
	return buttons
}

func (s *Shell) currentTaskbarPresentation() taskbarPresentation {
	if s.controller == nil {
		return taskbarPresentationForLocalizer(s.snapshot, s.taskbarSuspended, s.localizerSnapshot())
	}
	p := taskbarPresentation{tooltip: s.controller.Title, progress: winfx.TaskbarProgressNone}
	switch {
	case s.taskbarSuspended:
		p.progress, p.current, p.total = winfx.TaskbarProgressPaused, 100, 100
		p.tooltip += " — Suspended"
	case s.controllerState.Error != "":
		p.progress, p.current, p.total = winfx.TaskbarProgressError, 100, 100
		p.tooltip += " — " + s.controllerState.Error
	case s.controllerBusy || s.controllerState.Busy:
		p.progress = winfx.TaskbarProgressIndeterminate
		p.tooltip += " — Working"
	case s.controllerState.Connected:
		p.tooltip += " — Connected"
	default:
		p.tooltip += " — Offline"
	}
	return p
}

func controllerTaskbarAction(wParam uintptr) (actionID, bool) {
	if uint16(wParam>>16) != taskbarThumbnailClicked {
		return actionNone, false
	}
	id := uint32(wParam & 0xffff)
	if id < controllerTaskbarFirst || id >= controllerTaskbarFirst+4 {
		return actionNone, false
	}
	return actionKey1 + actionID(id-controllerTaskbarFirst), true
}

func newControllerTaskbarIcon(index, width, height int) (win.HICON, error) {
	paths := []string{"M15 5L8 12L15 19", "M9 5L16 12L9 19", "M5 12H19", "M5 12H19M12 5V19"}
	data := []byte(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="%s" fill="none" stroke="#9079bf" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`, paths[index]))
	raster, err := vector.RenderSVG(data, width, height)
	if err != nil {
		return 0, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, raster); err != nil {
		return 0, err
	}
	// One PNG-backed ICO frame, consumed by the existing donor icon loader.
	pack := make([]byte, 22+encoded.Len())
	binary.LittleEndian.PutUint16(pack[2:], 1)
	binary.LittleEndian.PutUint16(pack[4:], 1)
	pack[6], pack[7] = byte(width), byte(height)
	binary.LittleEndian.PutUint16(pack[10:], 1)
	binary.LittleEndian.PutUint16(pack[12:], 32)
	binary.LittleEndian.PutUint32(pack[14:], uint32(encoded.Len()))
	binary.LittleEndian.PutUint32(pack[18:], 22)
	copy(pack[22:], encoded.Bytes())
	return newPackedIconHandle(pack, width, height)
}
