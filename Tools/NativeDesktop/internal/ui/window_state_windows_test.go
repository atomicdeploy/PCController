//go:build windows

package ui

import (
	"image"
	"testing"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

func TestChromeReassertMessageDoesNotCollideWithNetworkChange(t *testing.T) {
	if wmReassertWindowChrome == winfx.WMSystemNetworkChange {
		t.Fatal("chrome reassert must not consume network-change automation")
	}
}

func TestEffectiveTitleBarModeAndPerPixelPolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ui       config.UIConfig
		wantMode string
		wantPP   bool
	}{
		{
			name: "none layered",
			ui: config.UIConfig{
				TitleBarMode:      config.TitleBarNone,
				LayeredForeground: true,
			},
			wantMode: config.TitleBarNone,
			wantPP:   true,
		},
		{
			name: "custom compatible",
			ui: config.UIConfig{
				TitleBarMode: config.TitleBarCustom,
			},
			wantMode: config.TitleBarCustom,
		},
		{
			name: "transparent forces per pixel",
			ui: config.UIConfig{
				TitleBarMode: config.TitleBarCustom,
				Backdrop:     config.BackdropTransparent,
			},
			wantMode: config.TitleBarCustom,
			wantPP:   true,
		},
		{
			name: "native owns nonclient",
			ui: config.UIConfig{
				TitleBarMode:      config.TitleBarNative,
				Backdrop:          config.BackdropTransparent,
				LayeredForeground: true,
			},
			wantMode: config.TitleBarNative,
			wantPP:   false,
		},
		{
			name: "legacy show title",
			ui: config.UIConfig{
				ShowTitleBar: true,
			},
			wantMode: config.TitleBarCustom,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := effectiveTitleBarMode(test.ui); got != test.wantMode {
				t.Fatalf("effectiveTitleBarMode() = %q, want %q", got, test.wantMode)
			}
			if got := perPixelForegroundForUI(test.ui); got != test.wantPP {
				t.Fatalf("perPixelForegroundForUI() = %t, want %t", got, test.wantPP)
			}
		})
	}
}

func TestAlphaPixelInteractiveUsesPerPixelThreshold(t *testing.T) {
	t.Parallel()

	surface := image.NewRGBA(image.Rect(0, 0, 3, 1))
	surface.Pix[3] = 0
	surface.Pix[7] = minimumInteractiveAlpha - 1
	surface.Pix[11] = minimumInteractiveAlpha

	if alphaPixelInteractive(surface, 0, 0, minimumInteractiveAlpha) {
		t.Fatal("alpha-zero background must pass through")
	}
	if alphaPixelInteractive(surface, 1, 0, minimumInteractiveAlpha) {
		t.Fatal("sub-threshold antialias fringe must pass through")
	}
	if !alphaPixelInteractive(surface, 2, 0, minimumInteractiveAlpha) {
		t.Fatal("opaque foreground must remain interactive")
	}
	if alphaPixelInteractive(surface, -1, 0, minimumInteractiveAlpha) ||
		alphaPixelInteractive(surface, 3, 0, minimumInteractiveAlpha) {
		t.Fatal("coordinates outside the presented surface must pass through")
	}
}

func TestBorderAndTransparentFramelessPolicies(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		ui         config.UIConfig
		wantResize bool
		wantShape  bool
	}{
		{
			name: "legacy/default border remains resizable",
			ui: config.UIConfig{
				Backdrop: config.BackdropTransparent,
			},
			wantResize: true,
		},
		{
			name: "transparent no border owns alpha shape",
			ui: config.UIConfig{
				Backdrop:     config.BackdropTransparent,
				BorderMode:   config.BorderNone,
				TitleBarMode: config.TitleBarNone,
			},
			wantShape: true,
		},
		{
			name: "solid no border has no resize frame",
			ui: config.UIConfig{
				Backdrop:   config.BackdropSolid,
				BorderMode: config.BorderNone,
			},
		},
		{
			name: "transparent automatic keeps resize frame",
			ui: config.UIConfig{
				Backdrop:   config.BackdropTransparent,
				BorderMode: config.BorderAuto,
			},
			wantResize: true,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := windowResizeBorderEnabled(test.ui); got != test.wantResize {
				t.Fatalf(
					"windowResizeBorderEnabled() = %t, want %t",
					got,
					test.wantResize,
				)
			}
			if got := transparentFramelessWindow(test.ui); got != test.wantShape {
				t.Fatalf(
					"transparentFramelessWindow() = %t, want %t",
					got,
					test.wantShape,
				)
			}
		})
	}
}

func TestTransparentFramelessWindowOwnsItsCorners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ui   config.UIConfig
		want bool
	}{
		{
			name: "ordinary rounded window delegates corners to DWM",
			ui: config.UIConfig{
				Backdrop:       config.BackdropBlur,
				BorderMode:     config.BorderAuto,
				RoundedCorners: true,
			},
			want: true,
		},
		{
			name: "transparent resize frame may retain DWM rounding",
			ui: config.UIConfig{
				Backdrop:       config.BackdropTransparent,
				BorderMode:     config.BorderAuto,
				RoundedCorners: true,
			},
			want: true,
		},
		{
			name: "transparent frameless alpha shape suppresses DWM rounding",
			ui: config.UIConfig{
				Backdrop:       config.BackdropTransparent,
				BorderMode:     config.BorderNone,
				TitleBarMode:   config.TitleBarNone,
				RoundedCorners: true,
			},
		},
		{
			name: "rounding remains disabled when user turns it off",
			ui: config.UIConfig{
				Backdrop:       config.BackdropBlur,
				BorderMode:     config.BorderAuto,
				RoundedCorners: false,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := dwmWindowCornerRounding(test.ui); got != test.want {
				t.Fatalf(
					"dwmWindowCornerRounding() = %t, want %t",
					got,
					test.want,
				)
			}
		})
	}
}

func TestSurfacePaintModeRestoresSafePreCompactBuffering(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		ui       config.UIConfig
		backdrop string
		want     walk.PaintMode
	}{
		{
			name: "native solid is buffered",
			ui: config.UIConfig{
				TitleBarMode: config.TitleBarNative,
			},
			backdrop: winfx.BackdropSolid,
			want:     walk.PaintBuffered,
		},
		{
			name: "compatible solid is buffered",
			ui: config.UIConfig{
				TitleBarMode: config.TitleBarNone,
			},
			backdrop: winfx.BackdropSolid,
			want:     walk.PaintBuffered,
		},
		{
			name: "layered solid stays no erase",
			ui: config.UIConfig{
				TitleBarMode:      config.TitleBarNone,
				LayeredForeground: true,
			},
			backdrop: winfx.BackdropSolid,
			want:     walk.PaintNoErase,
		},
		{
			name: "native DWM material preserves target",
			ui: config.UIConfig{
				TitleBarMode: config.TitleBarNative,
			},
			backdrop: winfx.BackdropAcrylic,
			want:     walk.PaintNoErase,
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := surfacePaintMode(test.ui, test.backdrop); got != test.want {
				t.Fatalf(
					"surfacePaintMode(%+v, %q) = %v, want %v",
					test.ui,
					test.backdrop,
					got,
					test.want,
				)
			}
		})
	}
}

func TestTaskbarIconMessagingKeepsBigIconIndependent(t *testing.T) {
	t.Parallel()

	type call struct {
		message uint32
		wParam  uintptr
		lParam  uintptr
	}
	var calls []call
	send := func(
		_ win.HWND,
		message uint32,
		wParam uintptr,
		lParam uintptr,
	) uintptr {
		calls = append(calls, call{
			message: message,
			wParam:  wParam,
			lParam:  lParam,
		})
		if message == win.WM_GETICON && wParam == iconBig {
			return 0x5241
		}
		return 0
	}

	got := queryTaskbarIcon(send, win.HWND(7))
	if got != 0x5241 {
		t.Fatalf("queryTaskbarIcon() = %#x, want %#x", got, uintptr(0x5241))
	}
	if len(calls) != 1 ||
		calls[0].message != win.WM_GETICON ||
		calls[0].wParam != iconBig {
		t.Fatalf("taskbar icon query calls = %#v", calls)
	}

	calls = nil
	if !applyTaskbarIcon(send, win.HWND(7), got) {
		t.Fatal("applyTaskbarIcon rejected a valid big icon")
	}
	if len(calls) != 1 ||
		calls[0].message != win.WM_SETICON ||
		calls[0].wParam != iconBig ||
		calls[0].lParam != got {
		t.Fatalf("taskbar icon apply calls = %#v", calls)
	}
}

func TestSplitWindowIconsLeavesProductIconCanonical(t *testing.T) {
	t.Parallel()

	var calls []string
	var appliedBig uintptr
	err := applySplitWindowIcons(
		func() error {
			calls = append(calls, "set-state")
			return nil
		},
		func() uintptr {
			calls = append(calls, "query-state-big")
			return 0x5241
		},
		func() error {
			calls = append(calls, "restore-product")
			return nil
		},
		func(icon uintptr) {
			calls = append(calls, "apply-state-big")
			appliedBig = icon
		},
	)
	if err != nil {
		t.Fatalf("applySplitWindowIcons(): %v", err)
	}
	want := []string{
		"set-state",
		"query-state-big",
		"restore-product",
		"apply-state-big",
	}
	if len(calls) != len(want) {
		t.Fatalf("split icon calls = %v, want %v", calls, want)
	}
	for index := range want {
		if calls[index] != want[index] {
			t.Fatalf("split icon calls = %v, want %v", calls, want)
		}
	}
	if appliedBig != 0x5241 {
		t.Fatalf("applied taskbar icon = %#x, want %#x", appliedBig, uintptr(0x5241))
	}
}
