//go:build windows

package winfx

import (
	"testing"

	"github.com/lxn/win"
)

func TestDialogPlacementConvertsTaskbarWorkspaceToScreen(t *testing.T) {
	for _, test := range []struct {
		name          string
		monitor, work win.RECT
		want          Rect
	}{
		{"top taskbar", win.RECT{Right: 1920, Bottom: 1080}, win.RECT{Top: 48, Right: 1920, Bottom: 1080}, Rect{Left: 100, Top: 148, Right: 900, Bottom: 748}},
		{"left taskbar", win.RECT{Right: 1920, Bottom: 1080}, win.RECT{Left: 64, Right: 1920, Bottom: 1080}, Rect{Left: 164, Top: 100, Right: 964, Bottom: 700}},
		{"bottom taskbar", win.RECT{Right: 1920, Bottom: 1080}, win.RECT{Right: 1920, Bottom: 1032}, Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}},
		{"negative monitor", win.RECT{Left: -1920, Right: 0, Bottom: 1080}, win.RECT{Left: -1920, Top: 48, Right: 0, Bottom: 1080}, Rect{Left: 100, Top: 148, Right: 900, Bottom: 748}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := dialogWorkspaceToScreen(Rect{Left: 100, Top: 100, Right: 900, Bottom: 700}, test.monitor, test.work)
			if got != test.want {
				t.Fatalf("workspace conversion = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestCenteredDialogRectUsesTargetDPIAndWorkArea(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		owner     Rect
		workArea  Rect
		widthDIP  int
		heightDIP int
		dpi       uint32
		want      Rect
	}{
		{
			name:      "mixed DPI owner near primary corner",
			owner:     Rect{Left: 100, Top: 100, Right: 500, Bottom: 500},
			workArea:  Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1080},
			widthDIP:  780,
			heightDIP: 640,
			dpi:       144,
			want:      Rect{Left: 0, Top: 0, Right: 1170, Bottom: 960},
		},
		{
			name:      "negative-coordinate secondary monitor",
			owner:     Rect{Left: -1200, Top: 200, Right: -800, Bottom: 600},
			workArea:  Rect{Left: -1600, Top: 0, Right: 0, Bottom: 900},
			widthDIP:  780,
			heightDIP: 640,
			dpi:       96,
			want:      Rect{Left: -1390, Top: 80, Right: -610, Bottom: 720},
		},
		{
			name:      "oversized high DPI height keeps caption reachable",
			owner:     Rect{Left: 600, Top: 200, Right: 1200, Bottom: 800},
			workArea:  Rect{Left: 0, Top: 40, Right: 1920, Bottom: 1040},
			widthDIP:  780,
			heightDIP: 640,
			dpi:       192,
			want:      Rect{Left: 120, Top: 40, Right: 1680, Bottom: 1040},
		},
		{
			name:      "invalid DPI falls back to 96",
			owner:     Rect{Left: 0, Top: 0, Right: 800, Bottom: 800},
			workArea:  Rect{Left: 0, Top: 0, Right: 1600, Bottom: 1000},
			widthDIP:  780,
			heightDIP: 640,
			dpi:       0,
			want:      Rect{Left: 10, Top: 80, Right: 790, Bottom: 720},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := centeredDialogRect(
				test.owner,
				test.workArea,
				test.widthDIP,
				test.heightDIP,
				test.dpi,
			)
			if got != test.want {
				t.Fatalf("centeredDialogRect() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestDialogMinimumTrackSizeCannotExceedMonitorWorkArea(t *testing.T) {
	t.Parallel()

	width, height := clampDialogMinimumTrackSize(1560, 1280, 1920, 1000)
	if width != 1560 || height != 1000 {
		t.Fatalf(
			"clamped high-DPI minimum = %dx%d, want 1560x1000",
			width,
			height,
		)
	}
	width, height = clampDialogMinimumTrackSize(0, -3, 1920, 1000)
	if width != 1 || height != 1 {
		t.Fatalf("invalid minimum = %dx%d, want safe 1x1", width, height)
	}
}

func TestRestoredDialogRectScalesAndRescuesSavedPlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		placement DialogPlacement
		workArea  Rect
		dpi       uint32
		want      Rect
	}{
		{
			name: "same negative-coordinate monitor",
			placement: DialogPlacement{
				X: -1500, Y: 90, Width: 900, Height: 700,
			},
			workArea: Rect{
				Left: -1920, Top: 0, Right: 0, Bottom: 1080,
			},
			dpi:  96,
			want: Rect{Left: -1500, Top: 90, Right: -600, Bottom: 790},
		},
		{
			name: "saved logical size follows new monitor DPI",
			placement: DialogPlacement{
				X: 2000, Y: 120, Width: 780, Height: 640,
			},
			workArea: Rect{
				Left: 1920, Top: 0, Right: 4480, Bottom: 1400,
			},
			dpi: 144,
			want: Rect{
				Left: 2000, Top: 120, Right: 3170, Bottom: 1080,
			},
		},
		{
			name: "removed monitor is rescued into nearest work area",
			placement: DialogPlacement{
				X: -4000, Y: -900, Width: 900, Height: 700,
			},
			workArea: Rect{
				Left: 0, Top: 40, Right: 1920, Bottom: 1040,
			},
			dpi:  96,
			want: Rect{Left: 0, Top: 40, Right: 900, Bottom: 740},
		},
		{
			name: "oversized restore remains wholly reachable",
			placement: DialogPlacement{
				X: 300, Y: 200, Width: 2000, Height: 1800,
			},
			workArea: Rect{
				Left: 0, Top: 0, Right: 1280, Bottom: 720,
			},
			dpi:  192,
			want: Rect{Left: 0, Top: 0, Right: 1280, Bottom: 720},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := restoredDialogRect(
				test.placement,
				test.workArea,
				test.dpi,
			); got != test.want {
				t.Fatalf(
					"restoredDialogRect() = %+v, want %+v",
					got,
					test.want,
				)
			}
		})
	}
}

func TestDialogDimensionDPIStorageRoundTrip(t *testing.T) {
	t.Parallel()

	for _, dpi := range []uint32{96, 120, 144, 192, 288, 0, 900} {
		for _, logical := range []int{520, 640, 780, 1180} {
			physical := scaleDialogDimension(logical, dpi)
			if got := unscaleDialogDimension(physical, dpi); got != logical {
				t.Fatalf(
					"DPI %d logical %d -> physical %d -> logical %d",
					dpi,
					logical,
					physical,
					got,
				)
			}
		}
	}
}

func TestDialogGeometryDefersOnlySizeLoopAdvisories(t *testing.T) {
	t.Parallel()

	for _, message := range []uint32{
		dialogGeometryWMEnterSizeMove,
		dialogGeometryWMExitSizeMove,
	} {
		if !deferDialogSizingMessage(message) {
			t.Fatalf("message %#x did not select deferred live layout", message)
		}
	}
	for _, message := range []uint32{
		0x0003, // WM_MOVE
		0x0005, // WM_SIZE
		0x0047, // WM_WINDOWPOSCHANGED
		dialogGeometryWMGetMinMaxInfo,
		0x02E0, // WM_DPICHANGED
		wmNCDestroy,
	} {
		if deferDialogSizingMessage(message) {
			t.Fatalf("message %#x was incorrectly consumed", message)
		}
	}
}
