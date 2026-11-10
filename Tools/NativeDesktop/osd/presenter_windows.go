//go:build windows

package osd

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"pccontroller.local/desktop/internal/winfx"
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
const osdInputLeave = win.WM_APP + 0x683
const osdInputCancel = win.WM_APP + 0x684

type command struct {
	request *Request
	dismiss string
	close   bool
	finish  string
	offset  *int
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
	closed   atomic.Bool
	mu       sync.RWMutex
	state    Status
	history  []Status
	err      error
	// Everything below belongs exclusively to the locked window thread.
	rendering                *rendering
	request                  Request
	started                  time.Time
	duration                 time.Duration
	anchor                   image.Point
	hover, pressed           int
	hoverInput, pressedInput win.HWND
	hoverFrom                []float64
	hoverStarted             time.Time
	lastHover                []float64
	exiting                  bool
	exitStarted              time.Time
	exitDuration             time.Duration
	exitOpacity, exitSlide   float64
	sequence                 uint64
	lastOpacity              byte
	lastOffset               []int
	endReason                string
	frameStop                chan struct{}
	frameGate                *atomic.Bool
	frameToken               uint32
	stackOffset              int
	onChange                 func()
	inputs                   []*inputTarget
	inputOrigin              image.Point
	inputsVisible            bool
}

func newWindowPresenter(o Options) *presenter {
	return &presenter{options: o, ready: make(chan struct{}), done: make(chan struct{}), commands: make(chan command, 16), state: Status{Supported: true}, hover: -1, pressed: -1}
}
func (p *presenter) Status() Status { p.mu.RLock(); defer p.mu.RUnlock(); return p.state }
func (p *presenter) Result(id string) (Status, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if id == "" || p.state.ID == id {
		return p.state, true
	}
	for i := len(p.history) - 1; i >= 0; i-- {
		if p.history[i].ID == id {
			return p.history[i], true
		}
	}
	return Status{}, false
}

// Caller holds mu. Bounded receipts preserve a button result when a subsequent
// notification replaces the surface before a waiting script's next read.
func (p *presenter) rememberLocked(reason string) {
	if !p.state.Active {
		return
	}
	p.state.Active = false
	p.state.Dismissal = reason
	p.history = append(p.history, p.state)
	if len(p.history) > 64 {
		p.history = p.history[len(p.history)-64:]
	}
}
func (p *presenter) start(ctx context.Context) error {
	if p.closed.Load() {
		return errors.New("OSD presenter closed")
	}
	p.once.Do(func() { go p.loop() })
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.ready:
	}
	if p.closed.Load() {
		return errors.New("OSD presenter closed")
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
	p.closed.Store(true)
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
	h := win.CreateWindowEx(0x00080000|0x00000020|0x00000080|0x08000000|0x00000008, &osdClass[0], nil, win.WS_POPUP, 0, 0, 1, 1, 0, 0, win.GetModuleHandle(nil), nil)
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
		destroyInputs(p.inputs)
		osdWindows.Delete(h)
		p.hwnd.Store(0)
		p.rendering.close()
		win.DestroyWindow(h)
		p.mu.Lock()
		p.rememberLocked("closed")
		p.mu.Unlock()
	}()
	if p.closed.Load() {
		return
	}
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
	renderRequest := r
	requestedBackdrop := r.Background
	if requestedBackdrop == "current" || requestedBackdrop == "" {
		requestedBackdrop = winfx.BackdropSolid
	}
	var actual string
	if p.rendering != nil && p.request.Background == r.Background {
		actual, _ = winfx.Refresh(uintptr(h), requestedBackdrop, dark, false)
	} else {
		actual, _ = winfx.Apply(uintptr(h), requestedBackdrop, dark, false)
	}
	if actual == winfx.BackdropSolid {
		renderRequest.Background = "current"
	}
	effective := renderRequest.Background
	next, err := prepare(renderRequest, dpi, int(work.Right-work.Left), icon, dark)
	if err != nil {
		return err
	}
	s := float64(dpi) / 96
	margin := int(24 * s)
	if next.base.Bounds().Dy()+2*margin > int(work.Bottom-work.Top) {
		next.close()
		return errors.New("OSD content exceeds the display work area")
	}
	x := int(work.Left) + (int(work.Right-work.Left)-next.base.Bounds().Dx())/2
	y := int(work.Bottom) - next.base.Bounds().Dy() - margin
	if strings.HasPrefix(r.Position, "top-") {
		y = int(work.Top) + margin + p.stackOffset
	} else if r.Position == "center" {
		y = int(work.Top) + (int(work.Bottom-work.Top)-next.base.Bounds().Dy())/2 + p.stackOffset
	} else {
		y -= p.stackOffset
	}
	if strings.HasSuffix(r.Position, "-left") {
		x = int(work.Left) + margin
	}
	if y < int(work.Top)+margin || y+next.base.Bounds().Dy() > int(work.Bottom)-margin {
		next.close()
		return errors.New("OSD stack exceeds the display work area")
	}
	if r.Position == "top-right" || r.Position == "bottom-right" {
		x = int(work.Right) - next.base.Bounds().Dx() - margin
	}
	inputs, err := createInputs(p, next)
	if err != nil {
		next.close()
		return err
	}
	destroyInputs(p.inputs)
	p.inputs = inputs
	p.inputOrigin = image.Pt(-1, -1)
	p.inputsVisible = false
	p.rendering.close()
	p.rendering = next
	p.request = r
	p.anchor = image.Pt(x, y)
	p.hover = -1
	p.pressed = -1
	p.hoverInput, p.pressedInput = 0, 0
	p.hoverFrom = make([]float64, len(r.Actions)+1)
	p.hoverStarted = time.Now()
	p.lastHover = nil
	p.lastOpacity = 0
	p.lastOffset = nil
	p.mu.Lock()
	p.state.DPI = dpi
	p.state.PixelWidth = next.base.Bounds().Dx()
	p.state.PixelHeight = next.base.Bounds().Dy()
	p.state.Background = effective
	p.mu.Unlock()
	win.SetWindowPos(h, win.HWND_TOPMOST, int32(x), int32(y), int32(next.base.Bounds().Dx()), int32(next.base.Bounds().Dy()), win.SWP_NOACTIVATE)
	return nil
}

func (p *presenter) hide(reason string) {
	h := win.HWND(p.hwnd.Load())
	p.stopFrames()
	win.KillTimer(h, 2)
	win.ShowWindow(h, win.SW_HIDE)
	destroyInputs(p.inputs)
	p.inputs = nil
	p.inputsVisible = false
	p.rendering.close()
	p.rendering = nil
	p.mu.Lock()
	p.rememberLocked(reason)
	p.mu.Unlock()
	if p.onChange != nil {
		p.onChange()
	}
}

func (p *presenter) visualAt(now time.Time) (float64, float64) {
	if p.exiting {
		return exitFrame(now.Sub(p.exitStarted), p.exitDuration, p.exitOpacity, p.exitSlide)
	}
	return Timeline(now.Sub(p.started), p.duration, p.request.ReduceMotion)
}

func (p *presenter) setHover(target int, now time.Time) {
	if target == p.hover {
		return
	}
	p.hoverFrom, _ = hoverFrame(p.hoverFrom, p.hover, now.Sub(p.hoverStarted), p.request.ReduceMotion)
	p.hover, p.hoverStarted = target, now
}

func (p *presenter) leaveInput(input win.HWND) bool {
	if p.hoverInput != input {
		return false
	}
	p.hoverInput = 0
	p.setHover(-1, time.Now())
	return true
}

func (p *presenter) hitTarget(at image.Point) int {
	if p.rendering == nil || p.exiting {
		return -1
	}
	for i, box := range p.rendering.hits {
		if at.In(box) {
			return i
		}
	}
	if at.In(p.rendering.closeRect) {
		return len(p.rendering.hits)
	}
	return -1
}

func (p *presenter) beginDismiss(reason string) error {
	if p.rendering == nil || p.exiting {
		return nil
	}
	now := time.Now()
	opacity, slide := p.visualAt(now)
	p.exitDuration = min(exitTime, p.duration-now.Sub(p.started))
	p.exiting, p.exitStarted = true, now
	p.exitOpacity, p.exitSlide = opacity, slide
	p.endReason = reason
	p.pressed, p.pressedInput, p.hoverInput = -1, 0, 0
	p.setHover(-1, now)
	// Reject activation immediately, but retain the small input targets until
	// the visual exit ends. A fast second click must not hit the desktop behind
	// a button that is still visibly fading away.
	if p.request.ReduceMotion || p.exitDuration <= 0 {
		p.hide(reason)
		return nil
	}
	p.mu.Lock()
	p.state.ExpiresAt = now.Add(p.exitDuration)
	p.mu.Unlock()
	return p.render(false)
}

func (p *presenter) render(force bool) error {
	if p.rendering == nil {
		return nil
	}
	now := time.Now()
	elapsed := now.Sub(p.started)
	if (p.exiting && now.Sub(p.exitStarted) >= p.exitDuration) || (!p.exiting && elapsed >= p.duration) {
		p.hide(p.endReason)
		return nil
	}
	r := p.rendering
	alpha, slide := p.visualAt(now)
	opacity := byte(alpha * 255)
	offsets := r.offsets(elapsed, p.request.ReduceMotion)
	hover, hoverMoving := hoverFrame(p.hoverFrom, p.hover, now.Sub(p.hoverStarted), p.request.ReduceMotion)
	hoverChanged := !slices.Equal(hover, p.lastHover)
	if force || opacity != p.lastOpacity || !slices.Equal(offsets, p.lastOffset) || hoverChanged {
		if force || !slices.Equal(offsets, p.lastOffset) || hoverChanged {
			im := r.paint(elapsed, p.request.ReduceMotion, hover)
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
		p.positionInputs(image.Pt(int(point.X), int(point.Y)), opacity > 0)
		p.lastOpacity = opacity
		p.lastOffset = offsets
		p.lastHover = hover
	}
	h := win.HWND(p.hwnd.Load())
	moving := !p.request.ReduceMotion && (p.exiting || hoverMoving || elapsed < exitTime || p.duration-elapsed < exitTime || r.scrolls())
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
			p.hide("failed")
		}
	}
	switch msg {
	case osdCommand:
		for {
			select {
			case c := <-p.commands:
				var err error
				if c.finish != "" {
					p.hide(c.finish)
					c.result <- reply{status: p.Status()}
					continue
				}
				if c.offset != nil {
					delta := *c.offset - p.stackOffset
					if strings.HasPrefix(p.request.Position, "bottom-") {
						delta = -delta
					}
					p.stackOffset = *c.offset
					p.anchor.Y += delta
					err = p.render(true)
					c.result <- reply{status: p.Status(), err: err}
					continue
				}
				if c.close {
					p.hide("closed")
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
						p.exiting = false
						p.endReason = "expired"
						p.duration = time.Duration(p.request.DurationMS) * time.Millisecond
						p.mu.Lock()
						p.rememberLocked("replaced")
						p.state.Active = true
						p.state.ID = p.request.ID
						p.state.ShownAt = p.started
						p.state.ExpiresAt = p.started.Add(p.duration)
						p.state.Accepted++
						p.state.LastError = ""
						p.state.LastAction = ""
						p.state.LastActionID = ""
						p.state.Dismissal = ""
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
						err = p.beginDismiss("dismissed")
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
		hover := p.hitTarget(point)
		if hover != p.hover {
			p.setHover(hover, time.Now())
			fail(p.render(false))
			if p.rendering == nil {
				return 0
			}
		}
		cursor := win.IDC_ARROW
		if hover >= 0 || point.In(p.rendering.closeRect) {
			cursor = win.IDC_HAND
		}
		win.SetCursor(win.LoadCursor(0, win.MAKEINTRESOURCE(uintptr(cursor))))
		return 0
	case osdInputLeave:
		if p.leaveInput(win.HWND(w)) {
			fail(p.render(false))
		}
		return 0
	case osdInputCancel:
		if p.pressedInput == win.HWND(w) {
			p.pressed, p.pressedInput = -1, 0
			p.setHover(-1, time.Now())
			fail(p.render(false))
		}
		return 0
	case win.WM_LBUTTONDOWN:
		p.pressed = -1
		if p.rendering != nil {
			point := image.Pt(int(int16(l&0xffff)), int(int16(l>>16)))
			p.pressed = p.hitTarget(point)
			p.setHover(p.pressed, time.Now())
			fail(p.render(false))
		}
		return 0
	case win.WM_LBUTTONUP:
		if p.rendering == nil {
			return 0
		}
		point := image.Pt(int(int16(l&0xffff)), int(int16(l>>16)))
		pressed := p.pressed
		p.pressed, p.pressedInput = -1, 0
		target := p.hitTarget(point)
		if target < 0 || target != pressed {
			p.setHover(target, time.Now())
			fail(p.render(false))
			return 0
		}
		if target == len(p.rendering.hits) {
			fail(p.beginDismiss("dismissed"))
			return 0
		}
		if target < len(p.request.Actions) {
			action := p.request.Actions[target]
			p.mu.Lock()
			p.state.LastAction = action.Label
			p.state.LastActionID = action.ID
			p.mu.Unlock()
			fail(p.beginDismiss("action"))
			if p.options.Activate != nil && action.URI != "" {
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
			if p.onChange != nil {
				p.onChange()
			}
		}
		return 0
	case win.WM_CLOSE:
		p.hide("closed")
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(h, msg, w, l)
}
