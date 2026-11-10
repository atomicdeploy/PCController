//go:build windows

package osd

import (
	"errors"
	"image"
	"sync"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	inputClassOnce  sync.Once
	inputClassError error
	inputClass      = windows.StringToUTF16("PCController.DesktopOSD.Input.v1")
	inputCallback   = windows.NewCallback(inputWindowProc)
	inputWindows    sync.Map
	inputAlpha      = osdUser.NewProc("SetLayeredWindowAttributes")
)

type inputTarget struct {
	hwnd  win.HWND
	owner *presenter
	rect  image.Rectangle
}

func inputWindowProc(h win.HWND, msg uint32, w, l uintptr) uintptr {
	value, ok := inputWindows.Load(h)
	if !ok {
		return win.DefWindowProc(h, msg, w, l)
	}
	target := value.(*inputTarget)
	switch msg {
	case win.WM_MOUSEACTIVATE:
		return 3
	case win.WM_NCHITTEST:
		return win.HTCLIENT
	case win.WM_MOUSEMOVE, win.WM_LBUTTONDOWN, win.WM_LBUTTONUP:
		if msg == win.WM_LBUTTONDOWN {
			win.SetCapture(h)
		}
		if msg == win.WM_LBUTTONUP {
			win.ReleaseCapture()
		}
		x, y := int(int16(l&0xffff))+target.rect.Min.X, int(int16(l>>16))+target.rect.Min.Y
		point := uintptr(uint16(x)) | uintptr(uint16(y))<<16
		win.PostMessage(win.HWND(target.owner.hwnd.Load()), msg, w, point)
		if msg == win.WM_MOUSEMOVE {
			track := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), DwFlags: 2, HwndTrack: h}
			win.TrackMouseEvent(&track)
		}
		return 0
	case 0x02a3:
		win.PostMessage(win.HWND(target.owner.hwnd.Load()), msg, 0, 0)
		return 0
	}
	return win.DefWindowProc(h, msg, w, l)
}

func createInputs(p *presenter, r *rendering) ([]*inputTarget, error) {
	inputClassOnce.Do(func() {
		c := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: inputCallback, HInstance: win.GetModuleHandle(nil), HCursor: win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_HAND)), HbrBackground: win.HBRUSH(win.GetStockObject(win.BLACK_BRUSH)), LpszClassName: &inputClass[0]}
		if win.RegisterClassEx(&c) == 0 {
			inputClassError = errors.New("OSD input class registration failed")
		}
	})
	if inputClassError != nil {
		return nil, inputClassError
	}
	var result []*inputTarget
	boxes := append(append([]image.Rectangle(nil), r.hits...), r.closeRect)
	for _, box := range boxes {
		h := win.CreateWindowEx(0x00080000|0x00000080|0x08000000, &inputClass[0], nil, win.WS_POPUP, 0, 0, int32(box.Dx()), int32(box.Dy()), win.HWND(p.hwnd.Load()), 0, win.GetModuleHandle(nil), nil)
		if h == 0 {
			destroyInputs(result)
			return nil, errors.New("OSD input window creation failed")
		}
		target := &inputTarget{hwnd: h, owner: p, rect: box}
		result = append(result, target)
		inputWindows.Store(h, target)
		// A 1/255-alpha input-only surface: the decorated button remains on the
		// separate visual window. Zero alpha would pass its clicks through too.
		if ok, _, _ := inputAlpha.Call(uintptr(h), 0, 1, 2); ok == 0 {
			destroyInputs(result)
			return nil, errors.New("OSD input transparency failed")
		}
	}
	return result, nil
}
func destroyInputs(targets []*inputTarget) {
	for _, target := range targets {
		inputWindows.Delete(target.hwnd)
		win.DestroyWindow(target.hwnd)
	}
}

func (p *presenter) positionInputs(origin image.Point, visible bool) {
	if origin == p.inputOrigin && visible == p.inputsVisible {
		return
	}
	for _, target := range p.inputs {
		if !visible {
			win.ShowWindow(target.hwnd, win.SW_HIDE)
			continue
		}
		win.SetWindowPos(target.hwnd, win.HWND_TOPMOST, int32(origin.X+target.rect.Min.X), int32(origin.Y+target.rect.Min.Y), int32(target.rect.Dx()), int32(target.rect.Dy()), win.SWP_NOACTIVATE|win.SWP_SHOWWINDOW)
	}
	p.inputOrigin = origin
	p.inputsVisible = visible
}
