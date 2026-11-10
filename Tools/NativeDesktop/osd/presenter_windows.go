//go:build windows

package osd

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	osdUser       = windows.NewLazySystemDLL("user32.dll")
	updateLayered = osdUser.NewProc("UpdateLayeredWindow")
	windowStation = osdUser.NewProc("GetProcessWindowStation")
	stationInfo   = osdUser.NewProc("GetUserObjectInformationW")
	gdiFlush      = windows.NewLazySystemDLL("gdi32.dll").NewProc("GdiFlush")
	osdClassOnce  sync.Once
	osdClassError error
	osdClass      = windows.StringToUTF16("PCController.DesktopOSD.v1")
	osdCallback   = windows.NewCallback(osdWindowProc)
	osdWindows    sync.Map
)

const osdCommand = win.WM_APP + 0x681
const osdFrame = win.WM_APP + 0x682

type command struct {
	request *Request
	dismiss string
	close   bool
	result  chan reply
}
type reply struct {
	status Status
	err    error
}
type presenter struct {
	options  Options
	once     sync.Once
	ready    chan struct{}
	done     chan struct{}
	commands chan command
	hwnd     atomic.Uintptr
	mu       sync.RWMutex
	state    Status
	err      error
	// Everything below belongs exclusively to the locked window thread.
	rendering      *rendering
	request        Request
	started        time.Time
	duration       time.Duration
	anchor         image.Point
	hover, pressed int
	sequence       uint64
	lastOpacity    byte
	lastOffset     [2]int
	frameStop      chan struct{}
	frameGate      *atomic.Bool
	frameToken     uint32
}

func newPresenter(o Options) Presenter {
	return &presenter{options: o, ready: make(chan struct{}), done: make(chan struct{}), commands: make(chan command, 16), state: Status{Supported: true}, hover: -1, pressed: -1}
}
func (p *presenter) Status() Status { p.mu.RLock(); defer p.mu.RUnlock(); return p.state }
func (p *presenter) start(ctx context.Context) error {
	p.once.Do(func() { go p.loop() })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ready:
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.err
}
func (p *presenter) send(ctx context.Context, c command) (Status, error) {
	if err := p.start(ctx); err != nil {
		return p.Status(), err
	}
	c.result = make(chan reply, 1)
	select {
	case <-ctx.Done():
		return p.Status(), ctx.Err()
	case <-p.done:
		return p.Status(), errors.New("OSD presenter closed")
	case p.commands <- c:
	default:
		return p.Status(), errors.New("OSD request queue is full")
	}
	if win.PostMessage(win.HWND(p.hwnd.Load()), osdCommand, 0, 0) == 0 {
		return p.Status(), errors.New("OSD window dispatch failed")
	}
	select {
	case r := <-c.result:
		return r.status, r.err
	case <-p.done:
		return p.Status(), errors.New("OSD presenter closed")
	case <-ctx.Done():
		return p.Status(), fmt.Errorf("OSD outcome unknown after submission: %w", ctx.Err())
	}
}
func (p *presenter) Show(ctx context.Context, r Request) (Status, error) {
	r, err := Normalize(r)
	if err != nil {
		return p.Status(), err
	}
	return p.send(ctx, command{request: &r})
}
func (p *presenter) Dismiss(ctx context.Context, id string) (Status, error) {
	return p.send(ctx, command{dismiss: id})
}
func (p *presenter) Close() {
	if p.hwnd.Load() == 0 {
		return
	}
	win.PostMessage(win.HWND(p.hwnd.Load()), win.WM_CLOSE, 0, 0)
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
	}
}

func interactiveDesktop() bool {
	station, _, _ := windowStation.Call()
	if station == 0 {
		return false
	}
	flags := struct {
		inherit, reserved int32
		flags             uint32
	}{}
	var needed uint32
	ok, _, _ := stationInfo.Call(station, 1, uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&needed)))
	return ok != 0 && flags.flags&1 != 0
}

func (p *presenter) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(p.done)
	restoreDPI, err := enterNativeDPI()
	if err != nil {
		p.mu.Lock()
		p.err = err
		p.state.LastError = err.Error()
		p.mu.Unlock()
		close(p.ready)
		return
	}
	defer restoreDPI()
	if !interactiveDesktop() {
		p.mu.Lock()
		p.err = ErrUnsupported
		p.state.Supported = false
		p.state.LastError = p.err.Error()
		p.mu.Unlock()
		close(p.ready)
		return
	}
	osdClassOnce.Do(func() {
		c := win.WNDCLASSEX{CbSize: uint32(unsafe.Sizeof(win.WNDCLASSEX{})), LpfnWndProc: osdCallback, HInstance: win.GetModuleHandle(nil), HCursor: win.LoadCursor(0, win.MAKEINTRESOURCE(win.IDC_ARROW)), LpszClassName: &osdClass[0]}
		if win.RegisterClassEx(&c) == 0 {
			osdClassError = errors.New("OSD window class registration failed")
		}
	})
	if osdClassError != nil {
		p.mu.Lock()
		p.err = osdClassError
		p.state.LastError = p.err.Error()
		p.mu.Unlock()
		close(p.ready)
		return
	}
	h := win.CreateWindowEx(0x00080000|0x00000080|0x08000000|0x00000008, &osdClass[0], nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, win.GetModuleHandle(nil), nil)
	if h == 0 {
		p.mu.Lock()
		p.err = ErrUnsupported
		p.state.LastError = p.err.Error()
		p.mu.Unlock()
		close(p.ready)
		return
	}
	p.hwnd.Store(uintptr(h))
	osdWindows.Store(h, p)
	close(p.ready)
	defer func() {
		p.stopFrames()
		osdWindows.Delete(h)
		p.hwnd.Store(0)
		p.rendering.close()
		win.DestroyWindow(h)
		p.mu.Lock()
		p.state.Active = false
		p.mu.Unlock()
	}()
	var msg win.MSG
	for {
		n := win.GetMessage(&msg, 0, 0, 0)
		if n <= 0 {
			return
		}
		win.TranslateMessage(&msg)
		win.DispatchMessage(&msg)
	}
}

func (p *presenter) configure(r Request) error {
	h := win.HWND(p.hwnd.Load())
	var work win.RECT
	if !win.SystemParametersInfo(0x0030, 0, unsafe.Pointer(&work), 0) {
		return errors.New("OSD cannot read the interactive desktop work area")
	}
	dpi := int(win.GetDpiForWindow(h))
	if dpi < 96 {
		dpi = 96
	}
	dpi = min(dpi, 384)
	var animate int32 = 1
	if win.SystemParametersInfo(0x1042, 0, unsafe.Pointer(&animate), 0) && animate == 0 {
		r.ReduceMotion = true
	}
	dark := r.Theme != "light"
	if r.Theme == "system" {
		if key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE); err == nil {
			value, _, readErr := key.GetIntegerValue("AppsUseLightTheme")
			if readErr == nil {
				dark = value == 0
			}
			key.Close()
		}
	}
	var icon image.Image
	var err error
	if p.options.Icon != nil {
		icon, err = p.options.Icon(r.Icon)
		if err != nil {
			return err
		}
	}
	next, err := prepare(r, dpi, int(work.Right-work.Left), icon, dark)
	if err != nil {
		return err
	}
	s := float64(dpi) / 96
	margin := int(24 * s)
	x := int(work.Left) + (int(work.Right-work.Left)-next.base.Bounds().Dx())/2
	y := int(work.Bottom) - next.base.Bounds().Dy() - margin
	if r.Position == "top-center" || r.Position == "top-right" {
		y = int(work.Top) + margin
	}
	if r.Position == "top-right" || r.Position == "bottom-right" {
		x = int(work.Right) - next.base.Bounds().Dx() - margin
	}
	p.rendering.close()
	p.rendering = next
	p.request = r
	p.anchor = image.Pt(x, y)
	p.hover = -1
	p.pressed = -1
	p.lastOpacity = 0
	p.lastOffset = [2]int{-1, -1}
	win.SetWindowPos(h, win.HWND_TOPMOST, int32(x), int32(y), int32(next.base.Bounds().Dx()), int32(next.base.Bounds().Dy()), win.SWP_NOACTIVATE)
	return nil
}

func (p *presenter) hide() {
	h := win.HWND(p.hwnd.Load())
	p.stopFrames()
	win.KillTimer(h, 2)
	win.ShowWindow(h, win.SW_HIDE)
	p.rendering.close()
	p.rendering = nil
	p.mu.Lock()
	p.state.Active = false
	p.mu.Unlock()
}
func (p *presenter) render(force bool) error {
	if p.rendering == nil {
		return nil
	}
	elapsed := time.Since(p.started)
	if elapsed >= p.duration {
		p.hide()
		return nil
	}
	r := p.rendering
	alpha, slide := Timeline(elapsed, p.duration, p.request.ReduceMotion)
	opacity := byte(alpha * 255)
	offsets := [2]int{int(Marquee(elapsed, float64(r.title.Bounds().Dx()-r.titleRect.Dx()), r.scale, p.request.ReduceMotion)), int(Marquee(elapsed, float64(r.body.Bounds().Dx()-r.bodyRect.Dx()), r.scale, p.request.ReduceMotion))}
	if force || opacity != p.lastOpacity || offsets != p.lastOffset {
		if force || offsets != p.lastOffset {
			im := r.paint(elapsed, p.request.ReduceMotion, p.hover)
			for i := 0; i < len(im.Pix); i += 4 {
				r.surface.pixels[i] = im.Pix[i+2]
				r.surface.pixels[i+1] = im.Pix[i+1]
				r.surface.pixels[i+2] = im.Pix[i]
				r.surface.pixels[i+3] = im.Pix[i+3]
			}
		}
		point := win.POINT{X: int32(p.anchor.X), Y: int32(p.anchor.Y + int(slide*r.scale))}
		size := win.SIZE{CX: int32(r.surface.width), CY: int32(r.surface.height)}
		source := win.POINT{}
		blend := struct{ op, flags, alpha, format byte }{0, 0, opacity, 1}
		ok, _, callErr := updateLayered.Call(p.hwnd.Load(), 0, uintptr(unsafe.Pointer(&point)), uintptr(unsafe.Pointer(&size)), uintptr(r.surface.dc), uintptr(unsafe.Pointer(&source)), 0, uintptr(unsafe.Pointer(&blend)), 2)
		if ok == 0 {
			return fmt.Errorf("OSD UpdateLayeredWindow: %v", callErr)
		}
		p.lastOpacity = opacity
		p.lastOffset = offsets
	}
	h := win.HWND(p.hwnd.Load())
	moving := !p.request.ReduceMotion && (elapsed < 160*time.Millisecond || p.duration-elapsed < 160*time.Millisecond || r.title.Bounds().Dx() > r.titleRect.Dx() || r.body.Bounds().Dx() > r.bodyRect.Dx())
	if moving {
		win.KillTimer(h, 2)
		p.startFrames()
	} else {
		p.stopFrames()
		wait := p.duration - elapsed
		if !p.request.ReduceMotion {
			wait -= 160 * time.Millisecond
		}
		win.SetTimer(h, 2, uint32(max(1, wait.Milliseconds())), 0)
	}
	return nil
}

// The same bounded, posted-frame strategy as the native tray renderer. Go's
// timer avoids coarse USER timer animation; a gate prevents message backlog.
func (p *presenter) startFrames() {
	if p.frameStop != nil {
		return
	}
	stop := make(chan struct{})
	gate := &atomic.Bool{}
	p.frameStop = stop
	p.frameGate = gate
	p.frameToken++
	token := p.frameToken
	h := win.HWND(p.hwnd.Load())
	go func() {
		ticker := time.NewTicker(time.Second / 60)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if gate.CompareAndSwap(false, true) {
					if win.PostMessage(h, osdFrame, uintptr(token), 0) == 0 {
						gate.Store(false)
					}
				}
			}
		}
	}()
}
func (p *presenter) stopFrames() {
	if p.frameStop != nil {
		close(p.frameStop)
		p.frameStop = nil
		p.frameGate = nil
		p.frameToken++
	}
}

func osdWindowProc(h win.HWND, msg uint32, w, l uintptr) uintptr {
	value, ok := osdWindows.Load(h)
	if !ok {
		return win.DefWindowProc(h, msg, w, l)
	}
	p := value.(*presenter)
	fail := func(err error) {
		if err != nil {
			p.mu.Lock()
			p.state.LastError = err.Error()
			p.mu.Unlock()
			p.hide()
		}
	}
	switch msg {
	case osdCommand:
		for {
			select {
			case c := <-p.commands:
				var err error
				if c.close {
					p.hide()
					c.result <- reply{status: p.Status()}
					win.PostQuitMessage(0)
					return 0
				}
				if c.request != nil {
					err = p.configure(*c.request)
					if err == nil {
						p.sequence++
						if p.request.ID == "" {
							p.request.ID = fmt.Sprintf("osd-%d", p.sequence)
						}
						p.started = time.Now()
						p.duration = time.Duration(p.request.DurationMS) * time.Millisecond
						p.mu.Lock()
						p.state.Active = true
						p.state.ID = p.request.ID
						p.state.ShownAt = p.started
						p.state.ExpiresAt = p.started.Add(p.duration)
						p.state.Accepted++
						p.state.LastError = ""
						p.state.LastAction = ""
						p.mu.Unlock()
						err = p.render(true)
						if err == nil {
							win.ShowWindow(h, win.SW_SHOWNOACTIVATE)
						} else {
							fail(err)
						}
					}
				} else if p.Status().Active {
					if c.dismiss != "" && c.dismiss != p.request.ID {
						err = errors.New("OSD id no longer active")
					} else {
						p.duration = time.Since(p.started) + 160*time.Millisecond
						if p.request.ReduceMotion {
							p.hide()
						} else {
							p.mu.Lock()
							p.state.ExpiresAt = time.Now().Add(160 * time.Millisecond)
							p.mu.Unlock()
							err = p.render(true)
						}
					}
				}
				c.result <- reply{p.Status(), err}
			default:
				return 0
			}
		}
	case win.WM_TIMER:
		fail(p.render(false))
		return 0
	case osdFrame:
		if p.frameGate != nil && uint32(w) == p.frameToken {
			p.frameGate.Store(false)
			fail(p.render(false))
		}
		return 0
	case win.WM_MOUSEACTIVATE:
		return 3 // MA_NOACTIVATE: never steal foreground input.
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_NCHITTEST:
		if p.rendering == nil {
			return 0
		}
		point := win.POINT{X: int32(int16(l & 0xffff)), Y: int32(int16(l >> 16))}
		win.ScreenToClient(h, &point)
		at := image.Pt(int(point.X), int(point.Y))
		if at.In(p.rendering.closeRect) {
			return win.HTCLIENT
		}
		for _, box := range p.rendering.hits {
			if at.In(box) {
				return win.HTCLIENT
			}
		}
		return ^uintptr(0) // HTTRANSPARENT: passive text does not block desktop clicks.
	case win.WM_MOUSEMOVE:
		if p.rendering == nil {
			return 0
		}
		point := image.Pt(int(int16(l&0xffff)), int(int16(l>>16)))
		hover := -1
		for i, r := range p.rendering.hits {
			if point.In(r) {
				hover = i
			}
		}
		if hover != p.hover {
			p.hover = hover
			fail(p.render(true))
			if p.rendering == nil {
				return 0
			}
		}
		cursor := win.IDC_ARROW
		if hover >= 0 || point.In(p.rendering.closeRect) {
			cursor = win.IDC_HAND
		}
		win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(uintptr(cursor))))
		track := win.TRACKMOUSEEVENT{CbSize: uint32(unsafe.Sizeof(win.TRACKMOUSEEVENT{})), DwFlags: 2, HwndTrack: h}
		win.TrackMouseEvent(&track)
		return 0
	case 0x02a3: // WM_MOUSELEAVE
		p.hover = -1
		fail(p.render(true))
		return 0
	case win.WM_LBUTTONDOWN:
		p.pressed = -1
		if p.rendering != nil {
			point := image.Pt(int(int16(l&0xffff)), int(int16(l>>16)))
			for i, box := range p.rendering.hits {
				if point.In(box) {
					p.pressed = i
					p.hover = i
				}
			}
		}
		return 0
	case win.WM_LBUTTONUP:
		if p.rendering == nil {
			return 0
		}
		point := image.Pt(int(int16(l&0xffff)), int(int16(l>>16)))
		if point.In(p.rendering.closeRect) {
			p.hide()
			return 0
		}
		if p.hover >= 0 && p.hover == p.pressed && p.hover < len(p.request.Actions) && point.In(p.rendering.hits[p.hover]) {
			action := p.request.Actions[p.hover]
			p.mu.Lock()
			p.state.LastAction = action.Label
			p.mu.Unlock()
			p.hide()
			if p.options.Activate != nil {
				id := p.request.ID
				go func() {
					if err := p.options.Activate(action.URI); err != nil {
						p.mu.Lock()
						if p.state.ID == id {
							p.state.LastError = err.Error()
						}
						p.mu.Unlock()
					}
				}()
			}
		}
		p.pressed = -1
		return 0
	case win.WM_DISPLAYCHANGE, win.WM_DPICHANGED, win.WM_SETTINGCHANGE:
		if p.rendering != nil {
			fail(p.configure(p.request))
			fail(p.render(true))
		}
		return 0
	case win.WM_CLOSE:
		p.hide()
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(h, msg, w, l)
}
