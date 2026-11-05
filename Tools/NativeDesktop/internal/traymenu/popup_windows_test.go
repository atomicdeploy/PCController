//go:build windows

package traymenu

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/winfx"
)

func TestOwnerMustBelongToCallingUIThread(t *testing.T) {
	ready := make(chan win.HWND, 1)
	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)

		className, _ := windows.UTF16FromString("STATIC")
		title, _ := windows.UTF16FromString("traymenu thread test")
		hwnd := win.CreateWindowEx(
			win.WS_EX_TOOLWINDOW,
			&className[0],
			&title[0],
			win.WS_POPUP,
			0,
			0,
			1,
			1,
			0,
			0,
			win.GetModuleHandle(nil),
			nil,
		)
		ready <- hwnd
		<-stop
		if hwnd != 0 {
			win.DestroyWindow(hwnd)
		}
		runtime.KeepAlive(className)
		runtime.KeepAlive(title)
	}()

	owner := <-ready
	if owner == 0 {
		close(stop)
		<-done
		t.Fatal("CreateWindowEx failed")
	}
	defer func() {
		close(stop)
		<-done
	}()

	_, err := ShowAtAnchor(
		Options{
			Owner:    uintptr(owner),
			Backdrop: BackdropSolid,
			Items: []Item{
				{ID: "one", Label: "One"},
			},
		},
		Rect{Left: 0, Top: 0, Right: 0, Bottom: 0},
	)
	if !errors.Is(err, ErrWrongThread) {
		t.Fatalf("ShowAtAnchor error = %v, want ErrWrongThread", err)
	}
}

func TestPopupWindowCacheIsOwnerBoundReusableAndDiscardable(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := registerPopupClass(); err != nil {
		t.Fatalf("register popup classes: %v", err)
	}
	className, _ := windows.UTF16FromString("STATIC")
	title, _ := windows.UTF16FromString("traymenu cache owner")
	owner := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		&className[0],
		&title[0],
		win.WS_POPUP,
		0,
		0,
		1,
		1,
		0,
		0,
		win.GetModuleHandle(nil),
		nil,
	)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if owner == 0 {
		t.Fatal("CreateWindowEx for cache owner failed")
	}
	defer win.DestroyWindow(owner)

	first, reused, err := acquirePopupWindow(owner, baseDPI, false)
	if err != nil {
		if strings.Contains(err.Error(), "create layered foreground failed") {
			t.Skip(
				"go test executable has no Windows compatibility manifest for layered child windows",
			)
		}
		t.Fatalf("acquire first cached popup: %v", err)
	}
	if reused {
		t.Fatal("new popup reported reuse")
	}
	if _, _, err := acquirePopupWindow(owner, baseDPI, false); !errors.Is(
		err,
		errPopupAlreadyActive,
	) {
		t.Fatalf("reentrant acquire error = %v, want already active", err)
	}
	firstHWND, firstOverlay := first.hwnd, first.overlay
	releasePopupWindow(first)

	second, reused, err := acquirePopupWindow(owner, baseDPI, false)
	if err != nil {
		t.Fatalf("reacquire cached popup: %v", err)
	}
	if !reused || second != first ||
		second.hwnd != firstHWND || second.overlay != firstOverlay {
		t.Fatalf(
			"cached popup was not reused: reused=%v first=%p second=%p",
			reused,
			first,
			second,
		)
	}
	releasePopupWindow(second)
	DiscardCached(uintptr(owner))
	if nativeWindowExists(firstHWND) || nativeWindowExists(firstOverlay) {
		t.Fatal("discard left a cached HWND alive")
	}

	ephemeral, reused, err := acquirePopupWindow(owner, baseDPI, true)
	if err != nil {
		t.Fatalf("acquire recreate-on-open popup: %v", err)
	}
	if reused {
		t.Fatal("recreate-on-open popup reported reuse")
	}
	popupWindowMu.Lock()
	cached := popupWindowCache[ephemeral.key]
	popupWindowMu.Unlock()
	if cached != nil {
		t.Fatal("recreate-on-open popup entered the reusable cache")
	}
	destroyPopupWindow(ephemeral)
}

func TestPreparedFirstFrameRemainsHiddenUntilRevealGate(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := registerPopupClass(); err != nil {
		t.Fatalf("register popup classes: %v", err)
	}
	className, _ := windows.UTF16FromString("STATIC")
	title, _ := windows.UTF16FromString("traymenu preparation owner")
	owner := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		&className[0],
		&title[0],
		win.WS_POPUP,
		0,
		0,
		1,
		1,
		0,
		0,
		win.GetModuleHandle(nil),
		nil,
	)
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if owner == 0 {
		t.Fatal("CreateWindowEx for preparation owner failed")
	}
	defer win.DestroyWindow(owner)

	window, _, err := acquirePopupWindow(owner, baseDPI, false)
	if err != nil {
		if strings.Contains(err.Error(), "create layered foreground failed") {
			t.Skip(
				"go test executable has no Windows compatibility manifest for layered child windows",
			)
		}
		t.Fatalf("acquire popup: %v", err)
	}
	font, err := newMenuFont(baseDPI)
	if err != nil {
		destroyPopupWindow(window)
		t.Fatalf("create menu font: %v", err)
	}
	defer font.close()

	options := Options{
		Owner:    uintptr(owner),
		Theme:    ThemeLight,
		Backdrop: BackdropSolid,
		Items: []Item{
			{ID: "toggle", Label: "Toggle device"},
		},
	}
	dimensions := buildDimensions(options.Items, baseDPI, font.measure)
	icons, err := prepareIcons(options.Items, dimensions.iconSize)
	if err != nil {
		destroyPopupWindow(window)
		t.Fatalf("prepare icons: %v", err)
	}
	session := &popupSession{
		window:       window,
		hwnd:         window.hwnd,
		overlay:      window.overlay,
		options:      options,
		dimensions:   dimensions,
		font:         font,
		icons:        icons,
		palette:      paletteFor(false),
		navigation:   newNavigator(options.Items),
		hover:        newHoverAnimator(len(options.Items)),
		finalX:       24,
		finalY:       24,
		slideOffset:  scale(8, baseDPI),
		presentation: popupInitialPresentation,
		presenting:   1,
		result: Result{
			Cancelled:    true,
			BackdropUsed: BackdropSolid,
		},
	}
	popupSessions.Store(session.hwnd, session)
	defer func() {
		popupSessions.Delete(session.hwnd)
		if !session.done {
			session.destroyNow()
		}
	}()

	if err := session.prepareHiddenFirstFrame(); err != nil {
		t.Fatalf("prepare hidden first frame: %v", err)
	}
	if session.lifecycle != popupLifecycleHiddenPrepared {
		t.Fatalf("lifecycle = %d, want hidden prepared", session.lifecycle)
	}
	if win.IsWindowVisible(session.hwnd) {
		t.Fatal("parent became visible before the reveal gate")
	}
	if style := uint32(win.GetWindowLong(session.overlay, win.GWL_STYLE)); style&win.WS_VISIBLE == 0 {
		t.Fatal("layered child was not armed while its parent remained hidden")
	}
	if !session.lifecycle.activate() {
		t.Fatal("committed first frame did not open the reveal gate")
	}
}

func TestPopupAppearanceMessagesIncludeSystemAndDWMChanges(t *testing.T) {
	for _, message := range []uint32{
		win.WM_THEMECHANGED,
		win.WM_SETTINGCHANGE,
		wmSysColorChanged,
		wmDWMCompositionChanged,
		wmDWMColorizationChanged,
	} {
		if !popupAppearanceMessage(message) {
			t.Errorf("appearance message %#x was ignored", message)
		}
	}
	for _, message := range []uint32{0, win.WM_PAINT, win.WM_NCDESTROY} {
		if popupAppearanceMessage(message) {
			t.Errorf("unrelated message %#x triggered appearance refresh", message)
		}
	}
}

func TestPopupResolvedAppearanceYieldsToHighContrast(t *testing.T) {
	session := &popupSession{
		dark:         true,
		transparency: true,
		palette:      paletteFor(true),
	}
	if !session.applyResolvedAppearance(winfx.ResolvedAppearance{
		Dark:                false,
		TransparencyEnabled: false,
	}) {
		t.Fatal("ordinary appearance update retired the custom popup")
	}
	if session.dark || session.transparency {
		t.Fatalf(
			"resolved appearance was not applied: dark=%v transparency=%v",
			session.dark,
			session.transparency,
		)
	}
	if session.applyResolvedAppearance(winfx.ResolvedAppearance{
		HighContrast: true,
	}) {
		t.Fatal("High Contrast retained custom owner-drawn popup")
	}
}

func TestPopupResolvedAppearanceHonorsReducedMotionAndAppOverride(t *testing.T) {
	t.Parallel()

	session := &popupSession{
		motionEnabled: true,
		presentation:  120,
		presenting:    1,
		navigation:    navigator{selected: 1},
		hover:         newHoverAnimator(3),
	}
	if !session.applyResolvedAppearance(winfx.ResolvedAppearance{
		MotionEnabled: false,
	}) {
		t.Fatal("reduced-motion update retired the custom popup")
	}
	if session.motionEnabled || session.presenting != 0 ||
		session.presentation != popupPresentationFullyShown {
		t.Fatalf(
			"motion was not settled: enabled=%v presenting=%d presentation=%d",
			session.motionEnabled,
			session.presenting,
			session.presentation,
		)
	}
	if session.hover.levels[1] != hoverLevelMaximum {
		t.Fatalf(
			"selected hover level = %d, want %d",
			session.hover.levels[1],
			hoverLevelMaximum,
		)
	}

	session.options.DisableAnimations = true
	if !session.applyResolvedAppearance(winfx.ResolvedAppearance{
		MotionEnabled: true,
	}) {
		t.Fatal("app motion override retired the custom popup")
	}
	if session.motionEnabled {
		t.Fatal("DisableAnimations was ignored")
	}
}

func TestPopupPresentationTimingHasNoSlowRevealOrDismissTail(t *testing.T) {
	t.Parallel()

	start := time.Unix(400, 0)
	var reveal popupScalarMotion
	reveal.begin(
		popupInitialPresentation,
		popupPresentationFullyShown,
		start,
		popupRevealDuration,
	)
	if reveal.duration > 80*time.Millisecond {
		t.Fatalf("visible reveal duration = %v, want at most 80ms", reveal.duration)
	}

	var dismiss popupScalarMotion
	dismiss.begin(
		popupPresentationFullyShown,
		0,
		start,
		popupDismissDuration,
	)
	first, changed, continuing := dismiss.advance(
		start.Add(time.Duration(popupAnimationInterval) * time.Millisecond),
	)
	if !changed || !continuing || first >= popupPresentationFullyShown {
		t.Fatalf(
			"first dismiss frame = (%d, %v, %v), want immediate progress",
			first,
			changed,
			continuing,
		)
	}
	perceptualEnd, _, _ := dismiss.advance(start.Add(92 * time.Millisecond))
	if perceptualEnd > popupDismissCutoff {
		t.Fatalf(
			"dismissal at 92ms = %d, cutoff %d; material tail would linger",
			perceptualEnd,
			popupDismissCutoff,
		)
	}
}
