//go:build windows

package winfx

import (
	"math"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/design"
)

func TestDialogPagePrintFlagsMatchWin32AndDoNotSkipHiddenPages(t *testing.T) {
	if dialogPagePRFCheckVisible != 0x0001 ||
		dialogPagePRFNonClient != 0x0002 ||
		dialogPagePRFClient != 0x0004 ||
		dialogPagePRFErase != 0x0008 ||
		dialogPagePRFChildren != 0x0010 ||
		dialogPagePRFOwned != 0x0020 {
		t.Fatalf(
			"PRF constants = check:%#x nc:%#x client:%#x erase:%#x children:%#x owned:%#x",
			dialogPagePRFCheckVisible,
			dialogPagePRFNonClient,
			dialogPagePRFClient,
			dialogPagePRFErase,
			dialogPagePRFChildren,
			dialogPagePRFOwned,
		)
	}
	const want = uintptr(0x003E)
	if got := dialogPagePrintFlags(); got != want {
		t.Fatalf("dialog page WM_PRINT flags = %#x, want %#x", got, want)
	}
	if dialogPagePrintFlags()&dialogPagePRFCheckVisible != 0 {
		t.Fatal("hidden destination TabPage would be skipped during offscreen print")
	}
}

func TestDialogPageCaptureKeepsSeededMaterialTransparent(t *testing.T) {
	if dialogPageBkTransparent != 1 {
		t.Fatalf(
			"dialog page capture background mode = %d, want TRANSPARENT (1)",
			dialogPageBkTransparent,
		)
	}
}

func TestDialogPageMotionUsesCanonicalDurationAndElapsedTime(t *testing.T) {
	want := time.Duration(design.Default.Motion.NormalMS) * time.Millisecond
	if got := dialogPageAnimationDuration(); got != want {
		t.Fatalf("dialog page duration = %s, want design token %s", got, want)
	}
	start := time.Unix(100, 0)
	state := dialogPageTransitionState{
		running:  true,
		started:  start,
		duration: 200 * time.Millisecond,
	}
	if got := state.progressAt(start.Add(50 * time.Millisecond)); math.Abs(got-0.25) > 1e-9 {
		t.Fatalf("elapsed page progress = %g, want 0.25", got)
	}
	if got := state.progressAt(start.Add(time.Second)); got != 1 {
		t.Fatalf("late page progress = %g, want clamped 1", got)
	}
}

func TestDialogPageSchedulerRejectsStaleGeneration(t *testing.T) {
	const hwnd = uintptr(0x5353)
	current := &dialogPageFrameScheduler{
		token: 2,
		stop:  make(chan struct{}),
	}
	current.pending.Store(true)
	dialogPageSchedulerMu.Lock()
	dialogPageSchedulers[hwnd] = current
	dialogPageSchedulerMu.Unlock()
	t.Cleanup(func() {
		dialogPageSchedulerMu.Lock()
		if scheduler := dialogPageSchedulers[hwnd]; scheduler != nil {
			delete(dialogPageSchedulers, hwnd)
			close(scheduler.stop)
		}
		dialogPageSchedulerMu.Unlock()
	})

	if consumeDialogPageFrame(hwnd, 1) {
		t.Fatal("stale dialog-page frame generation was accepted")
	}
	if !current.pending.Load() {
		t.Fatal("stale dialog-page frame consumed current pending state")
	}
	if !consumeDialogPageFrame(hwnd, current.token) {
		t.Fatal("current dialog-page frame generation was rejected")
	}
	if current.pending.Load() {
		t.Fatal("current dialog-page frame remained pending after consumption")
	}
}

func TestDialogPageCaptureFailureFallsBackToLiveDestination(t *testing.T) {
	state := &dialogPageTransitionState{
		pages:    []uintptr{0x1111, 0x2222},
		selected: 0,
		target:   0,
		animate:  true,
		duration: 190 * time.Millisecond,
	}
	state.selectionChanged(1, time.Now())
	if state.selected != 1 || state.target != 1 {
		t.Fatalf(
			"failed capture lost logical selection: selected=%d target=%d",
			state.selected,
			state.target,
		)
	}
	if state.running || state.prepared {
		t.Fatalf(
			"failed capture started a blank transition: running=%v prepared=%v",
			state.running,
			state.prepared,
		)
	}
	if state.from.memory != 0 || state.frame.memory != 0 {
		t.Fatal("failed capture retained an uninitialized visual endpoint")
	}
}

func TestDialogPageMotionOffCancelsPreparedOverlayImmediately(t *testing.T) {
	state := &dialogPageTransitionState{
		animate:  true,
		prepared: true,
	}
	transitions := &DialogPageTransitions{state: state}
	transitions.SetMotion(false)
	if state.animate || state.prepared || state.running {
		t.Fatalf(
			"motion-off state = animate:%v prepared:%v running:%v",
			state.animate,
			state.prepared,
			state.running,
		)
	}
}

func TestDialogPageSurfaceIsInitializedBeforeControlPrint(t *testing.T) {
	var surface dialogPageSurface
	if err := ensureDialogPageSurface(&surface, 0, 7, 5); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { releaseDialogPageSurface(&surface) })

	gdi := windows.NewLazySystemDLL("gdi32.dll")
	createBrush := gdi.NewProc("CreateSolidBrush")
	deleteObject := gdi.NewProc("DeleteObject")
	getPixel := gdi.NewProc("GetPixel")
	const color = uintptr(0x00604020)
	brush, _, _ := createBrush.Call(color)
	if brush == 0 {
		t.Fatal("CreateSolidBrush failed")
	}
	defer deleteObject.Call(brush)

	if !fillDialogPageSurface(surface.memory, surface.width, surface.height, brush) {
		t.Fatal("dialog page seed fill failed")
	}
	for _, point := range [][2]uintptr{
		{0, 0},
		{uintptr(surface.width - 1), 0},
		{0, uintptr(surface.height - 1)},
		{uintptr(surface.width - 1), uintptr(surface.height - 1)},
	} {
		got, _, _ := getPixel.Call(surface.memory, point[0], point[1])
		if got != color {
			t.Fatalf(
				"seeded page pixel (%d,%d) = %#x, want material brush %#x",
				point[0],
				point[1],
				got,
				color,
			)
		}
	}
}

func TestDialogPageSurfaceReleaseIsCompleteAndIdempotent(t *testing.T) {
	var surface dialogPageSurface
	if err := ensureDialogPageSurface(&surface, 0, 16, 12); err != nil {
		t.Fatal(err)
	}
	if surface.memory == 0 || surface.bitmap == 0 || surface.oldBitmap == 0 {
		t.Fatalf("incomplete retained page surface: %+v", surface)
	}
	releaseDialogPageSurface(&surface)
	if surface != (dialogPageSurface{}) {
		t.Fatalf("released page surface retained resources: %+v", surface)
	}
	releaseDialogPageSurface(&surface)
	if surface != (dialogPageSurface{}) {
		t.Fatalf("second release mutated empty page surface: %+v", surface)
	}
}

func TestDialogPageDisposeDropsRegistrySchedulerAndRenderCaches(t *testing.T) {
	const overlay = uintptr(0x6161)
	state := &dialogPageTransitionState{overlay: overlay}
	for _, surface := range []*dialogPageSurface{
		&state.from,
		&state.to,
		&state.frame,
	} {
		if err := ensureDialogPageSurface(surface, 0, 11, 7); err != nil {
			t.Fatal(err)
		}
	}
	scheduler := &dialogPageFrameScheduler{
		token: 77,
		stop:  make(chan struct{}),
	}
	dialogPageStatesMu.Lock()
	dialogPageStates[overlay] = state
	dialogPageStatesMu.Unlock()
	dialogPageSchedulerMu.Lock()
	dialogPageSchedulers[overlay] = scheduler
	dialogPageSchedulerMu.Unlock()

	transitions := &DialogPageTransitions{state: state}
	transitions.Dispose()
	transitions.Dispose()
	if !state.disposed.Load() || transitions.state != nil {
		t.Fatal("dialog page controller did not become disposed")
	}
	dialogPageStatesMu.RLock()
	registeredState := dialogPageStates[overlay]
	dialogPageStatesMu.RUnlock()
	if registeredState != nil {
		t.Fatal("disposed dialog page controller remains in HWND registry")
	}
	dialogPageSchedulerMu.Lock()
	registeredScheduler := dialogPageSchedulers[overlay]
	dialogPageSchedulerMu.Unlock()
	if registeredScheduler != nil {
		t.Fatal("disposed dialog page scheduler remains registered")
	}
	if state.from != (dialogPageSurface{}) ||
		state.to != (dialogPageSurface{}) ||
		state.frame != (dialogPageSurface{}) {
		t.Fatal("disposed dialog page controller retained GDI render caches")
	}
	select {
	case <-scheduler.stop:
	default:
		t.Fatal("disposed dialog page scheduler was not stopped")
	}
}

func TestDialogPageLayoutAndThemeMessagesDropEveryCache(t *testing.T) {
	messages := []uintptr{
		dialogPageWMSize,
		dialogPageWMDPIChanged,
		dialogPageWMThemeChange,
		dialogPageWMSetting,
	}
	for _, message := range messages {
		state := &dialogPageTransitionState{
			selected: 0,
			target:   0,
			running:  true,
			prepared: true,
		}
		for _, surface := range []*dialogPageSurface{
			&state.from,
			&state.to,
			&state.frame,
		} {
			if err := ensureDialogPageSurface(surface, 0, 9, 6); err != nil {
				t.Fatalf("message %#x: %v", message, err)
			}
		}
		state.hostMessage(0, message, 0, 0)
		if state.running || state.prepared {
			t.Fatalf(
				"message %#x left transition active: running=%v prepared=%v",
				message,
				state.running,
				state.prepared,
			)
		}
		if state.from != (dialogPageSurface{}) ||
			state.to != (dialogPageSurface{}) ||
			state.frame != (dialogPageSurface{}) {
			t.Fatalf("message %#x retained a page render cache", message)
		}
	}
}

func TestDialogPageRefreshAppearanceSettlesAndDropsOldThemeSurfaces(
	t *testing.T,
) {
	state := &dialogPageTransitionState{
		host:     0x7171,
		selected: 0,
		target:   0,
		running:  true,
		prepared: true,
	}
	for _, surface := range []*dialogPageSurface{
		&state.from,
		&state.to,
		&state.frame,
	} {
		if err := ensureDialogPageSurface(surface, 0, 9, 6); err != nil {
			t.Fatal(err)
		}
	}

	(&DialogPageTransitions{state: state}).RefreshAppearance()
	if state.running || state.prepared {
		t.Fatalf(
			"appearance refresh left transition active: running=%v prepared=%v",
			state.running,
			state.prepared,
		)
	}
	if state.from != (dialogPageSurface{}) ||
		state.to != (dialogPageSurface{}) ||
		state.frame != (dialogPageSurface{}) {
		t.Fatal("appearance refresh retained old-theme page surfaces")
	}
}
