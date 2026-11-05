//go:build windows

package ui

import (
	"reflect"
	"testing"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

func TestResolveStartupWindowAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode StartupWindowMode
		ui   config.UIConfig
		want StartupWindowAction
	}{
		{
			name: "remembered normal visible",
			mode: StartupRemembered,
			ui: config.UIConfig{
				WindowVisible: true,
				WindowState:   config.WindowStateNormal,
			},
			want: StartupShowNormal,
		},
		{
			name: "remembered minimized visible",
			mode: StartupRemembered,
			ui: config.UIConfig{
				WindowVisible: true,
				WindowState:   config.WindowStateMinimized,
			},
			want: StartupShowMinimized,
		},
		{
			name: "remembered maximized visible",
			mode: StartupRemembered,
			ui: config.UIConfig{
				WindowVisible: true,
				WindowState:   config.WindowStateMaximized,
			},
			want: StartupShowMaximized,
		},
		{
			name: "remembered visibility hidden",
			mode: StartupRemembered,
			ui: config.UIConfig{
				WindowVisible: false,
				WindowState:   config.WindowStateMaximized,
			},
			want: StartupHide,
		},
		{
			name: "start minimized preference means hidden",
			mode: StartupRemembered,
			ui: config.UIConfig{
				StartMinimized: true,
				WindowVisible:  true,
				WindowState:    config.WindowStateMaximized,
			},
			want: StartupHide,
		},
		{
			name: "force show promotes minimized to normal",
			mode: StartupForceShow,
			ui: config.UIConfig{
				StartMinimized: true,
				WindowVisible:  false,
				WindowState:    config.WindowStateMinimized,
			},
			want: StartupShowNormal,
		},
		{
			name: "force show preserves maximized",
			mode: StartupForceShow,
			ui: config.UIConfig{
				StartMinimized: true,
				WindowVisible:  false,
				WindowState:    config.WindowStateMaximized,
			},
			want: StartupShowMaximized,
		},
		{
			name: "force hidden wins",
			mode: StartupForceHidden,
			ui: config.UIConfig{
				WindowVisible: true,
				WindowState:   config.WindowStateMaximized,
			},
			want: StartupHide,
		},
		{
			name: "invalid remembered state falls back to normal",
			mode: StartupRemembered,
			ui: config.UIConfig{
				WindowVisible: true,
				WindowState:   "future-state",
			},
			want: StartupShowNormal,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			before := test.ui
			if got := ResolveStartupWindowAction(test.mode, test.ui); got != test.want {
				t.Fatalf(
					"ResolveStartupWindowAction(%d, %+v) = %d, want %d",
					test.mode,
					test.ui,
					got,
					test.want,
				)
			}
			if !reflect.DeepEqual(test.ui, before) {
				t.Fatalf("resolver mutated remembered UI state: got %+v, want %+v",
					test.ui, before)
			}
		})
	}
}

func TestRememberedPlacementNeverReplacesNormalBoundsWithShowStateGeometry(
	t *testing.T,
) {
	t.Parallel()

	normal := config.WindowBounds{
		X: 96, Y: 80, Width: 380, Height: 600,
	}
	transientMaximizedRect := config.WindowBounds{
		X: -10, Y: -10, Width: 1940, Height: 1060,
	}
	ui := config.Default().UI
	ui.WindowBounds = normal
	ui.WindowState = config.WindowStateNormal

	if !updateRememberedWindowPlacement(
		&ui,
		config.WindowStateMaximized,
		transientMaximizedRect,
		false,
	) {
		t.Fatal("normal to maximized transition was not reported")
	}
	if ui.WindowBounds != normal {
		t.Fatalf(
			"maximized transition replaced normal bounds: got %+v, want %+v",
			ui.WindowBounds,
			normal,
		)
	}

	// Restore state is observed before Windows has necessarily settled the
	// rectangle. The first transition must retain the normal cache; the posted
	// settle message is the only observation allowed to replace it.
	if !updateRememberedWindowPlacement(
		&ui,
		config.WindowStateNormal,
		transientMaximizedRect,
		false,
	) {
		t.Fatal("maximized to normal transition was not reported")
	}
	if ui.WindowBounds != normal {
		t.Fatalf(
			"unsettled restore replaced normal bounds: got %+v, want %+v",
			ui.WindowBounds,
			normal,
		)
	}

	settled := config.WindowBounds{
		X: 220, Y: 140, Width: 412, Height: 520,
	}
	if updateRememberedWindowPlacement(
		&ui,
		config.WindowStateNormal,
		settled,
		true,
	) {
		t.Fatal("settled bounds-only observation reported a state transition")
	}
	if ui.WindowBounds != settled {
		t.Fatalf(
			"settled normal bounds = %+v, want %+v",
			ui.WindowBounds,
			settled,
		)
	}

	if !updateRememberedWindowPlacement(
		&ui,
		config.WindowStateMinimized,
		config.WindowBounds{},
		false,
	) {
		t.Fatal("normal to minimized transition was not reported")
	}
	if ui.WindowBounds != settled {
		t.Fatalf(
			"minimized transition replaced restore bounds: got %+v, want %+v",
			ui.WindowBounds,
			settled,
		)
	}
}

func TestClampSavedWindowBoundsToWorkArea(t *testing.T) {
	t.Parallel()

	primary := winfx.Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	negative := winfx.Rect{Left: -1920, Top: -120, Right: 0, Bottom: 960}

	tests := []struct {
		name     string
		bounds   config.WindowBounds
		workArea winfx.Rect
		want     config.WindowBounds
	}{
		{
			name:     "fully visible unchanged",
			bounds:   config.WindowBounds{X: 120, Y: 80, Width: 380, Height: 600},
			workArea: primary,
			want:     config.WindowBounds{X: 120, Y: 80, Width: 380, Height: 600},
		},
		{
			name: "exact minimum intersection unchanged",
			bounds: config.WindowBounds{
				X:      int(primary.Right) - 32,
				Y:      int(primary.Bottom) - 32,
				Width:  380,
				Height: 600,
			},
			workArea: primary,
			want: config.WindowBounds{
				X:      int(primary.Right) - 32,
				Y:      int(primary.Bottom) - 32,
				Width:  380,
				Height: 600,
			},
		},
		{
			name: "valid negative monitor coordinates unchanged",
			bounds: config.WindowBounds{
				X: -1880, Y: -80, Width: 380, Height: 600,
			},
			workArea: negative,
			want: config.WindowBounds{
				X: -1880, Y: -80, Width: 380, Height: 600,
			},
		},
		{
			name: "right and bottom offscreen rescued",
			bounds: config.WindowBounds{
				X:      int(primary.Right) - 31,
				Y:      int(primary.Bottom) - 20,
				Width:  380,
				Height: 600,
			},
			workArea: primary,
			want: config.WindowBounds{
				X:      int(primary.Right) - 380,
				Y:      int(primary.Bottom) - 600,
				Width:  380,
				Height: 600,
			},
		},
		{
			name: "left offscreen rescues only failed axis",
			bounds: config.WindowBounds{
				X: -500, Y: 700, Width: 380, Height: 600,
			},
			workArea: primary,
			want: config.WindowBounds{
				X: 0, Y: 700, Width: 380, Height: 600,
			},
		},
		{
			name: "removed negative monitor rescued to new work area",
			bounds: config.WindowBounds{
				X: -1700, Y: 100, Width: 380, Height: 600,
			},
			workArea: primary,
			want: config.WindowBounds{
				X: 0, Y: 100, Width: 380, Height: 600,
			},
		},
		{
			name: "window larger than work area aligns leading edges",
			bounds: config.WindowBounds{
				X: 2000, Y: 1200, Width: 2200, Height: 1400,
			},
			workArea: primary,
			want: config.WindowBounds{
				X: 0, Y: 0, Width: 2200, Height: 1400,
			},
		},
		{
			name: "invalid saved dimensions unchanged",
			bounds: config.WindowBounds{
				X: 2000, Y: 1200, Width: 0, Height: 600,
			},
			workArea: primary,
			want: config.WindowBounds{
				X: 2000, Y: 1200, Width: 0, Height: 600,
			},
		},
		{
			name: "invalid work area unchanged",
			bounds: config.WindowBounds{
				X: 2000, Y: 1200, Width: 380, Height: 600,
			},
			workArea: winfx.Rect{},
			want: config.WindowBounds{
				X: 2000, Y: 1200, Width: 380, Height: 600,
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := clampRememberedWindowBounds(test.bounds, test.workArea)
			if got != test.want {
				t.Fatalf(
					"clampRememberedWindowBounds(%+v, %+v) = %+v, want %+v",
					test.bounds,
					test.workArea,
					got,
					test.want,
				)
			}
		})
	}
}
