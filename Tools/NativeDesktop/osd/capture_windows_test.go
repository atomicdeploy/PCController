//go:build windows

package osd

import (
	"context"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var patternCallback = windows.NewCallback(func(h win.HWND, msg uint32, w, l uintptr) uintptr {
	if msg == win.WM_NCHITTEST {
		return ^uintptr(0)
	}
	if msg != win.WM_PAINT {
		return win.DefWindowProc(h, msg, w, l)
	}
	var paint win.PAINTSTRUCT
	dc := win.BeginPaint(h, &paint)
	defer win.EndPaint(h, &paint)
	var bounds win.RECT
	win.GetClientRect(h, &bounds)
	makeBrush := windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateSolidBrush")
	a, _, _ := makeBrush.Call(0x6a4632)
	b, _, _ := makeBrush.Call(0xbdbea8)
	brushes := []win.HBRUSH{win.HBRUSH(a), win.HBRUSH(b)}
	fillRect := osdUser.NewProc("FillRect")
	defer win.DeleteObject(win.HGDIOBJ(brushes[0]))
	defer win.DeleteObject(win.HGDIOBJ(brushes[1]))
	for y := int32(0); y < bounds.Bottom; y += 12 {
		for x := int32(0); x < bounds.Right; x += 12 {
			r := win.RECT{Left: x, Top: y, Right: min(x+12, bounds.Right), Bottom: min(y+12, bounds.Bottom)}
			fillRect.Call(uintptr(dc), uintptr(unsafe.Pointer(&r)), uintptr(brushes[(x/12+y/12)%2]))
		}
	}
	return 0
})

// Captures only a source-owned checkerboard window covered by our own OSD.
// No user windows, desktop content, input injection or board output is captured.
func TestOSDLiveBackgroundCaptures(t *testing.T) {
	output := os.Getenv("PCCONTROLLER_OSD_CAPTURE")
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" || output == "" {
		t.Skip("explicit source-owned compositor capture only")
	}
	if !filepath.IsAbs(output) {
		t.Fatal("capture directory must be absolute")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	restore, err := enterNativeDPI()
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	name := windows.StringToUTF16("PCController.OSD.PatternFixture.v1")
	class := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: patternCallback, HInstance: win.GetModuleHandle(nil), LpszClassName: &name[0]}
	if win.RegisterClassEx(&class) == 0 {
		t.Fatal("pattern class registration failed")
	}
	defer win.UnregisterClass(&name[0])
	h := win.CreateWindowEx(0x00000080|0x08000000|0x00000008, &name[0], nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, win.GetModuleHandle(nil), nil)
	if h == 0 {
		t.Fatal("pattern fixture window failed")
	}
	defer win.DestroyWindow(h)
	scale := float64(win.GetDpiForWindow(h)) / 96
	px := func(v int) int32 { return int32(math.Round(float64(v) * scale)) }
	var work win.RECT
	win.SystemParametersInfo(0x0030, 0, unsafe.Pointer(&work), 0)
	w, height := px(540), px(130)
	x, y := work.Left+(work.Right-work.Left-w)/2, work.Bottom-height-px(24)
	win.SetWindowPos(h, win.HWND_TOPMOST, x, y, w, height, win.SWP_NOACTIVATE)
	win.ShowWindow(h, win.SW_SHOWNOACTIVATE)
	win.UpdateWindow(h)
	p := newWindowPresenter(Options{})
	defer p.Close()
	for _, background := range []string{"current", "transparent", "acrylic", "blur"} {
		status, e := p.Show(context.Background(), Request{Title: "PCController", Text: "Background preview\nSecond line", Background: background, DurationMS: 2000, ReduceMotion: true, Theme: "dark"})
		if e != nil {
			t.Fatal(e)
		}
		t.Logf("requested=%s effective=%s DPI=%d", background, status.Background, status.DPI)
		time.Sleep(120 * time.Millisecond)
		windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmFlush").Call()
		dib, e := newDIB(int(w), int(height))
		if e != nil {
			t.Fatal(e)
		}
		screen := win.GetDC(0)
		ok := win.BitBlt(dib.dc, 0, 0, w, height, screen, x, y, win.SRCCOPY|0x40000000)
		win.ReleaseDC(0, screen)
		gdiFlush.Call()
		if !ok {
			dib.close()
			t.Fatal("source-owned capture failed")
		}
		im := image.NewRGBA(image.Rect(0, 0, int(w), int(height)))
		for i := 0; i < len(im.Pix); i += 4 {
			im.Pix[i] = dib.pixels[i+2]
			im.Pix[i+1] = dib.pixels[i+1]
			im.Pix[i+2] = dib.pixels[i]
			im.Pix[i+3] = 255
		}
		dib.close()
		file, e := os.Create(filepath.Join(output, "osd-live-"+background+".png"))
		if e != nil {
			t.Fatal(e)
		}
		e = png.Encode(file, im)
		closeErr := file.Close()
		if e != nil || closeErr != nil {
			t.Fatal(e, closeErr)
		}
	}
}
