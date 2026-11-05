//go:build windows

package winfx

import (
	"math"
	"testing"
	"time"

	"rayanlamp.local/app/internal/design"
)

func TestDialogFieldClassesKeepNativeEditImplementations(t *testing.T) {
	for _, className := range []string{
		"EDIT",
		"RichEdit",
		"RichEdit20W",
		"RichEdit50W",
		"  edit  ",
	} {
		if !isDialogFieldClass(className) {
			t.Fatalf("native edit class %q was not recognized", className)
		}
	}
	for _, className := range []string{
		"ComboBox",
		"Button",
		"Static",
	} {
		if isDialogFieldClass(className) {
			t.Fatalf("non-edit class %q was treated as an edit", className)
		}
	}
}

func TestDialogFieldChromeRemovesOnlyLegacyExtendedEdges(t *testing.T) {
	const unrelated = uint32(0x00400008)
	original := unrelated |
		dialogFieldWSExClientEdge |
		dialogFieldWSExStaticEdge
	got := dialogFieldChromeExStyle(original)
	if got != unrelated {
		t.Fatalf(
			"custom edit ex-style = %#x, want unrelated bits %#x",
			got,
			unrelated,
		)
	}
	const changedWhileThemed = uint32(0x08000008)
	restored := dialogFieldRestoreExStyle(changedWhileThemed, original)
	wantRestored := changedWhileThemed |
		dialogFieldWSExClientEdge |
		dialogFieldWSExStaticEdge
	if restored != wantRestored {
		t.Fatalf(
			"restored edit ex-style = %#x, want preserved runtime bits %#x",
			restored,
			wantRestored,
		)
	}
}

func TestDialogFieldTraitsPreserveCuePasswordNumberAndMultilineSemantics(
	t *testing.T,
) {
	style := uint32(
		dialogFieldESMultiline |
			dialogFieldESPassword |
			dialogFieldESReadOnly |
			0x0002, // ES_RIGHT, used by Walk's numberLineEdit.
	)
	traits := dialogFieldTraits(style)
	if !traits.Multiline || !traits.Password || !traits.ReadOnly {
		t.Fatalf("field traits = %+v, want every native EDIT trait", traits)
	}

	// Styling changes GWL_EXSTYLE only. The EDIT style containing password,
	// right-alignment/number presentation and read-only validation remains
	// exactly owned by Walk and USER32.
	if style != uint32(
		dialogFieldESMultiline|
			dialogFieldESPassword|
			dialogFieldESReadOnly|
			0x0002,
	) {
		t.Fatal("test style unexpectedly changed")
	}
}

func TestDialogFieldLeavesNativeTextIMEClipboardAndAccessibilityMessages(
	t *testing.T,
) {
	const (
		wmGetDlgCode          = 0x0087
		wmChar                = 0x0102
		wmIMEStartComposition = 0x010D
		wmIMEComposition      = 0x010F
		wmCut                 = 0x0300
		wmCopy                = 0x0301
		wmPaste               = 0x0302
		wmUndo                = 0x0304
		wmGetObject           = 0x003D
		emSetSel              = 0x00B1
		emSetPasswordChar     = 0x00CC
		emSetCueBanner        = 0x1501
	)
	for _, message := range []uint32{
		wmGetDlgCode,
		wmChar, // Walk NumberEdit filtering and normal text input.
		wmIMEStartComposition,
		wmIMEComposition,
		wmCut,
		wmCopy,
		wmPaste,
		wmUndo,
		wmGetObject,
		emSetSel,
		emSetPasswordChar,
		emSetCueBanner,
	} {
		if result, handled := dialogFieldWindowMessage(
			0xF13D,
			uintptr(message),
			0,
			0,
		); handled || result != 0 {
			t.Fatalf(
				"native EDIT message %#x was consumed: (%d, %v)",
				message,
				result,
				handled,
			)
		}
	}
}

func TestDialogFieldPrintClientUsesSuppliedCompositorDC(t *testing.T) {
	if dialogFieldWMPrintClient != 0x0318 {
		t.Fatalf(
			"field print-client message = %#x, want WM_PRINTCLIENT",
			dialogFieldWMPrintClient,
		)
	}
	if paintDialogFieldChrome(0, 1) {
		t.Fatal("field print renderer accepted an unthemed HWND")
	}
}

func TestDialogFieldPaletteAndMotionHaveDistinctPremiumStates(t *testing.T) {
	for _, dark := range []bool{false, true} {
		palette := dialogFieldColors(dark)
		if palette.Border == palette.BorderHot ||
			palette.BorderHot == palette.BorderFocus ||
			palette.Inner == palette.InnerFocus {
			t.Fatalf("dark=%v field palette lacks interaction depth", dark)
		}
		normal := dialogFieldAnimatedBorder(
			palette,
			0,
			0,
			true,
			false,
		)
		middle := dialogFieldAnimatedBorder(
			palette,
			0.5,
			0,
			true,
			false,
		)
		hot := dialogFieldAnimatedBorder(
			palette,
			1,
			0,
			true,
			false,
		)
		focused := dialogFieldAnimatedBorder(
			palette,
			1,
			1,
			true,
			false,
		)
		if middle == normal || middle == hot || focused == hot {
			t.Fatalf(
				"dark=%v field motion collapsed: normal=%#x mid=%#x hot=%#x focus=%#x",
				dark,
				normal,
				middle,
				hot,
				focused,
			)
		}
		if disabled := dialogFieldAnimatedBorder(
			palette,
			1,
			1,
			false,
			false,
		); disabled != palette.BorderDisabled {
			t.Fatalf(
				"dark=%v disabled border = %#x, want %#x",
				dark,
				disabled,
				palette.BorderDisabled,
			)
		}
	}
}

func TestDialogFieldGeometryUsesCanonicalDPIAwareControlRadius(t *testing.T) {
	want96 := int32(design.Default.Radii.ControlPX)
	if got := dialogFieldRadius(200, 60, false, 96); got != want96 {
		t.Fatalf("96-DPI field radius = %d, want token %d", got, want96)
	}
	want144 := scaleDialogFieldMetric(want96, 144)
	if got := dialogFieldRadius(300, 90, true, 144); got != want144 {
		t.Fatalf("144-DPI field radius = %d, want %d", got, want144)
	}
	if got := dialogFieldRadius(40, 12, false, 192); got != 6 {
		t.Fatalf("short field radius = %d, want height-clamped 6", got)
	}
	if got := scaleDialogFieldMetric(9, 144); got != 14 {
		t.Fatalf("9 at 144 DPI = %d, want 14", got)
	}
}

func TestDialogFieldAnimationAdvancesHoverAndKeyboardFocusTogether(
	t *testing.T,
) {
	const hwnd = uintptr(0xF13D)
	start := time.Unix(700, 0)
	dialogFieldMu.Lock()
	dialogFieldThemes[hwnd] = dialogFieldThemeState{
		Hot:      true,
		Focused:  true,
		Animate:  true,
		LastTick: start,
	}
	dialogFieldMu.Unlock()
	t.Cleanup(func() {
		dialogFieldMu.Lock()
		delete(dialogFieldThemes, hwnd)
		dialogFieldMu.Unlock()
	})

	if !advanceDialogFieldAnimation(hwnd, start.Add(30*time.Millisecond)) {
		t.Fatal("field animation settled before an intermediate frame")
	}
	state, exists := dialogFieldTheme(hwnd)
	want := 30.0 / float64(design.Default.Motion.FastMS)
	if !exists ||
		math.Abs(state.Hover-want) > 1e-9 ||
		math.Abs(state.Focus-want) > 1e-9 {
		t.Fatalf("field motion = (%+v, %v), want progress %g", state, exists, want)
	}
}

func TestDialogFieldSchedulerRejectsStaleGeneration(t *testing.T) {
	const hwnd = uintptr(0xF13E)
	current := &dialogFieldAnimationScheduler{
		token: 2,
		stop:  make(chan struct{}),
	}
	current.pending.Store(true)
	dialogFieldSchedulerMu.Lock()
	dialogFieldSchedulers[hwnd] = current
	dialogFieldSchedulerMu.Unlock()
	t.Cleanup(func() {
		dialogFieldSchedulerMu.Lock()
		if scheduler := dialogFieldSchedulers[hwnd]; scheduler != nil {
			delete(dialogFieldSchedulers, hwnd)
			close(scheduler.stop)
		}
		dialogFieldSchedulerMu.Unlock()
	})

	if consumeDialogFieldAnimationFrame(hwnd, 1) {
		t.Fatal("stale field-animation generation was accepted")
	}
	if !current.pending.Load() {
		t.Fatal("stale field frame consumed the current pending frame")
	}
	if !consumeDialogFieldAnimationFrame(hwnd, current.token) {
		t.Fatal("current field-animation generation was rejected")
	}
	if current.pending.Load() {
		t.Fatal("current field frame remained pending after consumption")
	}
}

func TestDialogFieldRetainedPaintBufferCompatibility(t *testing.T) {
	buffer := &dialogFieldPaintBuffer{
		width:  220,
		height: 34,
		dpi:    144,
		memory: 1,
		bitmap: 2,
	}
	if !dialogFieldPaintBufferCompatible(buffer, 220, 34, 144) {
		t.Fatal("matching retained field buffer was rejected")
	}
	if dialogFieldPaintBufferCompatible(buffer, 221, 34, 144) ||
		dialogFieldPaintBufferCompatible(buffer, 220, 35, 144) ||
		dialogFieldPaintBufferCompatible(buffer, 220, 34, 192) {
		t.Fatal("stale field buffer survived size or DPI change")
	}
}
