//go:build windows

package winfx

import (
	"math"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"rayanlamp.local/app/internal/design"
)

func TestDialogComboKeepsNativeClassAndWalkModelSemantics(t *testing.T) {
	for _, className := range []string{
		"ComboBox",
		"COMBOBOX",
		"  combobox  ",
	} {
		if !isDialogComboClass(className) {
			t.Fatalf("native combo class %q was not recognized", className)
		}
	}
	for _, className := range []string{"ComboBoxEx32", "ComboLBox", "Edit"} {
		if isDialogComboClass(className) {
			t.Fatalf("non-COMBOBOX class %q was custom-rendered", className)
		}
	}
}

func TestDialogComboKeyboardDropAndMouseLifecycleRunsNativeFirst(
	t *testing.T,
) {
	for _, message := range []uint32{
		dialogComboWMKeyDown, // F4, arrows, Enter.
		dialogComboWMKeyUp,
		dialogComboWMSysKeyDown, // Alt+Down.
		dialogComboWMSysKeyUp,
		dialogComboWMLButtonDown,
		dialogComboWMLButtonUp,
	} {
		if !dialogComboObservesAfterNative(message) {
			t.Fatalf("combo message %#x no longer delegates native-first", message)
		}
	}
	for _, untouched := range []uint32{
		0x0102, // WM_CHAR: dropdown-list type-ahead/editable text.
		0x010D, // WM_IME_STARTCOMPOSITION.
		0x010F, // WM_IME_COMPOSITION.
		0x0087, // WM_GETDLGCODE.
		0x0302, // WM_PASTE for the native editable child.
	} {
		if dialogComboObservesAfterNative(untouched) {
			t.Fatalf("combo text/IME message %#x was intercepted", untouched)
		}
		if result, handled := dialogComboWindowMessage(
			0xC0B0,
			uintptr(untouched),
			0,
			0,
		); handled || result != 0 {
			t.Fatalf(
				"combo type-ahead/IME message %#x was consumed: (%d, %v)",
				untouched,
				result,
				handled,
			)
		}
	}
}

func TestDialogComboPointerHoverRunsNativeBeforeCustomPresentation(
	t *testing.T,
) {
	for _, test := range []struct {
		message uint32
		hot     bool
	}{
		{dialogComboWMMouseMove, true},
		{dialogComboWMMouseLeave, false},
	} {
		hot, ok := dialogComboNativePointerTarget(test.message)
		if !ok || hot != test.hot {
			t.Fatalf(
				"pointer message %#x = (hot:%v ok:%v), want (hot:%v ok:true)",
				test.message,
				hot,
				ok,
				test.hot,
			)
		}
	}
	if _, ok := dialogComboNativePointerTarget(dialogComboWMChar); ok {
		t.Fatal("native type-ahead was misclassified as a pointer lifecycle")
	}
}

func TestDialogComboDropTargetDrivesAnimatedOpenState(t *testing.T) {
	const hwnd = uintptr(0xC0B1)
	dialogComboMu.Lock()
	dialogComboThemes[hwnd] = dialogComboThemeState{Animate: false}
	dialogComboMu.Unlock()
	t.Cleanup(func() {
		dialogComboMu.Lock()
		delete(dialogComboThemes, hwnd)
		dialogComboMu.Unlock()
	})

	dropped := true
	focused := true
	setDialogComboTargets(hwnd, nil, &focused, &dropped)
	state, exists := dialogComboTheme(hwnd)
	if !exists || !state.Dropped || !state.Focused ||
		state.Open != 1 || state.Focus != 1 {
		t.Fatalf("opened keyboard combo state = (%+v, %v)", state, exists)
	}

	dropped = false
	setDialogComboTargets(hwnd, nil, nil, &dropped)
	state, _ = dialogComboTheme(hwnd)
	if state.Dropped || state.Open != 0 {
		t.Fatalf("closed keyboard combo state = %+v", state)
	}
}

func TestDialogComboPopupObservesEveryNativeClosePath(t *testing.T) {
	for _, message := range []uint32{
		dialogComboWMShowWindow,
		dialogComboWMWindowPosChanged,
		dialogComboWMCancelMode,
		dialogComboWMKillFocus,
		dialogComboWMKeyDown, // Escape/Enter.
		dialogComboWMKeyUp,
		dialogComboWMChar, // Native list type-ahead.
		dialogComboWMLButtonUp,
	} {
		if !dialogComboPopupObservesAfterNative(message) {
			t.Fatalf("ComboLBox close message %#x is no longer observed", message)
		}
	}
	if dialogComboPopupObservesAfterNative(dialogComboWMMouseMove) {
		t.Fatal("ordinary ComboLBox hover was unnecessarily intercepted")
	}
}

func TestDialogComboPopupCloseSettlesOwningChevron(t *testing.T) {
	const (
		owner = uintptr(0xC0B4)
		list  = uintptr(0xC0B5)
	)
	dialogComboMu.Lock()
	dialogComboThemes[owner] = dialogComboThemeState{
		Dropped: true,
		Open:    1,
		Animate: false,
	}
	dialogComboMu.Unlock()
	dialogComboPopupMu.Lock()
	dialogComboPopupOwners[list] = owner
	dialogComboPopupMu.Unlock()
	t.Cleanup(func() {
		dialogComboMu.Lock()
		delete(dialogComboThemes, owner)
		dialogComboMu.Unlock()
		dialogComboPopupMu.Lock()
		delete(dialogComboPopupOwners, list)
		dialogComboPopupMu.Unlock()
	})

	// Invalid fake HWNDs report CB_GETDROPPEDSTATE=false, modeling the
	// post-native state after selection, Escape or click-away hid ComboLBox.
	syncDialogComboPopupOwner(list)
	state, exists := dialogComboTheme(owner)
	if !exists || state.Dropped || state.Open != 0 {
		t.Fatalf("popup-close observer left chevron open: (%+v, %v)", state, exists)
	}
}

func TestDialogComboPopupThemeCacheSkipsIdenticalReapplication(t *testing.T) {
	const (
		owner = uintptr(0xC0C1)
		list  = uintptr(0xC0C2)
	)
	t.Cleanup(func() { forgetDialogComboPopup(list, 0) })

	if !rememberDialogComboPopupTheme(list, owner, false) {
		t.Fatal("first popup theme application was incorrectly cached")
	}
	if rememberDialogComboPopupTheme(list, owner, false) {
		t.Fatal("identical popup theme requested a destructive reapply")
	}
	if !rememberDialogComboPopupTheme(list, owner, true) {
		t.Fatal("dark-mode change did not invalidate popup theme cache")
	}
	if rememberDialogComboPopupTheme(list, owner, true) {
		t.Fatal("settled dark popup theme requested another reapply")
	}
}

func TestDialogComboChevronRotatesSmoothlyAndDPIAware(t *testing.T) {
	button := dialogFieldRect{Left: 100, Top: 1, Right: 136, Bottom: 33}
	closedLeft, closedMiddle, closedRight := dialogComboChevronPoints(
		button,
		0,
		96,
	)
	midLeft, midMiddle, midRight := dialogComboChevronPoints(
		button,
		0.5,
		96,
	)
	openLeft, openMiddle, openRight := dialogComboChevronPoints(
		button,
		1,
		96,
	)
	if closedLeft.Y >= closedMiddle.Y || closedRight.Y >= closedMiddle.Y {
		t.Fatalf(
			"closed chevron does not point down: %v %v %v",
			closedLeft,
			closedMiddle,
			closedRight,
		)
	}
	if openLeft.Y <= openMiddle.Y || openRight.Y <= openMiddle.Y {
		t.Fatalf(
			"open chevron does not point up: %v %v %v",
			openLeft,
			openMiddle,
			openRight,
		)
	}
	if midLeft.Y == closedLeft.Y || midMiddle.Y == closedMiddle.Y ||
		midRight.Y == closedRight.Y {
		t.Fatalf(
			"mid chevron collapsed onto an endpoint: %v %v %v",
			midLeft,
			midMiddle,
			midRight,
		)
	}
	left144, _, right144 := dialogComboChevronPoints(button, 0, 144)
	if right144.X-left144.X <= closedRight.X-closedLeft.X {
		t.Fatal("combo chevron did not scale at 144 DPI")
	}
}

func TestDialogComboButtonGeometryHasDPIAwareNativeFallback(t *testing.T) {
	native := dialogFieldRect{Left: 170, Top: 0, Right: 200, Bottom: 34}
	got := dialogComboButtonRect(native, 200, 34, 96)
	if got.Left != 169 || got.Top != 1 ||
		got.Right != 199 || got.Bottom != 33 {
		t.Fatalf("native combo button rect = %+v", got)
	}
	// COMBOBOXINFO can return screen-space coordinates. Only the native
	// button width is portable; its absolute origin must not leak into the
	// combo client's paint coordinates.
	screenOrigin := dialogComboButtonRect(
		dialogFieldRect{Left: 10170, Top: 800, Right: 10200, Bottom: 834},
		200,
		34,
		96,
	)
	if screenOrigin != got {
		t.Fatalf(
			"screen-origin combo rect = %+v, want client rect %+v",
			screenOrigin,
			got,
		)
	}

	fallback := dialogComboButtonRect(
		dialogFieldRect{},
		300,
		51,
		144,
	)
	wantWidth := scaleDialogFieldMetric(32, 144)
	if fallback.Left != 299-wantWidth ||
		fallback.Right != 299 ||
		fallback.Top != 1 ||
		fallback.Bottom != 50 {
		t.Fatalf("fallback combo button rect = %+v", fallback)
	}
}

func TestDialogComboPaletteHasDistinctHoverOpenFocusAndDisabledStates(
	t *testing.T,
) {
	for _, dark := range []bool{false, true} {
		palette := dialogComboColors(dark)
		if palette.Button == palette.ButtonHot ||
			palette.ButtonHot == palette.ButtonOpen ||
			palette.Border == palette.BorderHot ||
			palette.BorderHot == palette.BorderFocus ||
			palette.Glyph == palette.GlyphDisabled {
			t.Fatalf("dark=%v combo palette collapsed interaction states", dark)
		}
	}
}

func TestDialogComboGeometryUsesCanonicalControlRadius(t *testing.T) {
	want96 := int32(design.Default.Radii.ControlPX)
	if got := dialogComboRadius(220, 40, 96); got != want96 {
		t.Fatalf("96-DPI combo radius = %d, want token %d", got, want96)
	}
	want144 := scaleDialogFieldMetric(want96, 144)
	if got := dialogComboRadius(330, 60, 144); got != want144 {
		t.Fatalf("144-DPI combo radius = %d, want %d", got, want144)
	}
}

func TestDialogComboRoundedRegionMatchesPaintDomainAndIsSymmetric(
	t *testing.T,
) {
	gdi := windows.NewLazySystemDLL("gdi32.dll")
	getRgnBox := gdi.NewProc("GetRgnBox")
	ptInRegion := gdi.NewProc("PtInRegion")

	for _, dpi := range []int32{96, 144, 192} {
		width := scaleDialogFieldMetric(220, dpi)
		height := scaleDialogFieldMetric(40, dpi)
		radius := dialogComboRadius(width, height, dpi)
		region := createDialogComboRoundedRegion(width, height, radius)
		if region == 0 {
			t.Fatalf("dpi %d: rounded combo region creation failed", dpi)
		}

		var bounds dialogFieldRect
		kind, _, _ := getRgnBox.Call(
			region,
			uintptr(unsafe.Pointer(&bounds)),
		)
		if kind == 0 {
			dialogComboDeleteObject.Call(region)
			t.Fatalf("dpi %d: GetRgnBox failed", dpi)
		}
		want := dialogFieldRect{Right: width, Bottom: height}
		if bounds != want {
			dialogComboDeleteObject.Call(region)
			t.Fatalf(
				"dpi %d: region bounds = %+v, want exact painted domain %+v",
				dpi,
				bounds,
				want,
			)
		}

		for y := int32(0); y < height; y++ {
			for x := int32(0); x < width; x++ {
				inside, _, _ := ptInRegion.Call(
					region,
					uintptr(x),
					uintptr(y),
				)
				horizontal, _, _ := ptInRegion.Call(
					region,
					uintptr(width-1-x),
					uintptr(y),
				)
				vertical, _, _ := ptInRegion.Call(
					region,
					uintptr(x),
					uintptr(height-1-y),
				)
				if (inside != 0) != (horizontal != 0) ||
					(inside != 0) != (vertical != 0) {
					dialogComboDeleteObject.Call(region)
					t.Fatalf(
						"dpi %d: asymmetric region at (%d,%d)",
						dpi,
						x,
						y,
					)
				}
			}
		}
		dialogComboDeleteObject.Call(region)
	}
}

func TestDialogComboAnimationAdvancesHoverFocusAndChevronTogether(
	t *testing.T,
) {
	const hwnd = uintptr(0xC0B2)
	start := time.Unix(800, 0)
	dialogComboMu.Lock()
	dialogComboThemes[hwnd] = dialogComboThemeState{
		Hot:      true,
		Focused:  true,
		Dropped:  true,
		Animate:  true,
		LastTick: start,
	}
	dialogComboMu.Unlock()
	t.Cleanup(func() {
		dialogComboMu.Lock()
		delete(dialogComboThemes, hwnd)
		dialogComboMu.Unlock()
	})

	if !advanceDialogComboAnimation(hwnd, start.Add(30*time.Millisecond)) {
		t.Fatal("combo animation settled before an intermediate frame")
	}
	state, exists := dialogComboTheme(hwnd)
	want := 30.0 / float64(design.Default.Motion.FastMS)
	if !exists ||
		math.Abs(state.Hover-want) > 1e-9 ||
		math.Abs(state.Focus-want) > 1e-9 ||
		math.Abs(state.Open-want) > 1e-9 {
		t.Fatalf("combo motion = (%+v, %v), want progress %g", state, exists, want)
	}
}

func TestDialogComboSchedulerRejectsStaleGeneration(t *testing.T) {
	const hwnd = uintptr(0xC0B3)
	current := &dialogComboAnimationScheduler{
		token: 9,
		stop:  make(chan struct{}),
	}
	current.pending.Store(true)
	dialogComboSchedulerMu.Lock()
	dialogComboSchedulers[hwnd] = current
	dialogComboSchedulerMu.Unlock()
	t.Cleanup(func() {
		dialogComboSchedulerMu.Lock()
		if scheduler := dialogComboSchedulers[hwnd]; scheduler != nil {
			delete(dialogComboSchedulers, hwnd)
			close(scheduler.stop)
		}
		dialogComboSchedulerMu.Unlock()
	})

	if consumeDialogComboAnimationFrame(hwnd, 8) {
		t.Fatal("stale combo-animation generation was accepted")
	}
	if !current.pending.Load() {
		t.Fatal("stale combo frame consumed the current pending frame")
	}
	if !consumeDialogComboAnimationFrame(hwnd, current.token) {
		t.Fatal("current combo-animation generation was rejected")
	}
	if current.pending.Load() {
		t.Fatal("current combo frame remained pending after consumption")
	}
}

func TestDialogComboPrintClientUsesSuppliedCompositorDC(t *testing.T) {
	if dialogComboWMPrintClient != 0x0318 {
		t.Fatalf(
			"combo print-client message = %#x, want WM_PRINTCLIENT",
			dialogComboWMPrintClient,
		)
	}
	if printDialogCombo(0, 1, 0) {
		t.Fatal("combo print renderer accepted an unthemed HWND")
	}
}

func TestDialogComboAtomicFrameRendersOffscreenThenCommitsOnce(
	t *testing.T,
) {
	const (
		surface = uintptr(0xB0FF)
		target  = uintptr(0xC011)
	)
	var events []string
	commits := 0
	presented := composeDialogComboAtomicFrame(
		surface,
		target,
		func(dc uintptr) {
			if dc != surface {
				t.Fatalf("native renderer received live DC %#x", dc)
			}
			events = append(events, "native")
		},
		func(dc uintptr) bool {
			if dc != surface {
				t.Fatalf("chrome renderer received live DC %#x", dc)
			}
			events = append(events, "chrome")
			return true
		},
		func(source, destination uintptr) bool {
			commits++
			if source != surface || destination != target {
				t.Fatalf(
					"commit = (%#x -> %#x), want (%#x -> %#x)",
					source,
					destination,
					surface,
					target,
				)
			}
			events = append(events, "commit")
			return true
		},
	)
	if !presented {
		t.Fatal("complete combo frame was not presented")
	}
	if commits != 1 {
		t.Fatalf("combo frame committed %d times, want exactly once", commits)
	}
	want := []string{"native", "chrome", "commit"}
	if len(events) != len(want) {
		t.Fatalf("combo frame events = %v, want %v", events, want)
	}
	for index := range want {
		if events[index] != want[index] {
			t.Fatalf("combo frame events = %v, want %v", events, want)
		}
	}
}

func TestDialogComboAtomicFrameNeverLeaksPartialChrome(t *testing.T) {
	commits := 0
	presented := composeDialogComboAtomicFrame(
		0xB0FF,
		0xC011,
		func(uintptr) {},
		func(uintptr) bool { return false },
		func(uintptr, uintptr) bool {
			commits++
			return true
		},
	)
	if presented {
		t.Fatal("incomplete combo frame was reported as presented")
	}
	if commits != 0 {
		t.Fatalf("incomplete combo frame committed %d partial frames", commits)
	}
	if composeDialogComboAtomicFrame(
		0xC011,
		0xC011,
		func(uintptr) {},
		func(uintptr) bool { return true },
		func(uintptr, uintptr) bool { return true },
	) {
		t.Fatal("live target DC was accepted as its own offscreen surface")
	}
}

func TestDialogComboNativePrintFlagsAlwaysRequestCompleteClient(t *testing.T) {
	const supplied = uintptr(0x00000002)
	got := dialogComboNativePrintFlags(supplied)
	want := supplied |
		dialogComboPRFClient |
		dialogComboPRFEraseBkgnd |
		dialogComboPRFChildren
	if got != want {
		t.Fatalf("native combo print flags = %#x, want %#x", got, want)
	}
}

func TestDialogComboNativeStructureLayouts(t *testing.T) {
	size := unsafe.Sizeof(dialogComboBoxInfo{})
	if size != 52 && size != 64 {
		t.Fatalf("COMBOBOXINFO-compatible size = %d, want 52 or 64", size)
	}
	paintSize := unsafe.Sizeof(dialogComboPaintStruct{})
	if paintSize != 64 && paintSize != 72 {
		t.Fatalf("PAINTSTRUCT-compatible size = %d, want 64 or 72", paintSize)
	}
}
