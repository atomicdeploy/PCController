//go:build windows

package ui

import (
	"testing"

	"github.com/lxn/walk"

	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/winfx"
)

func TestMagneticDragReleasesAxisAcquiredAfterFirstSnap(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession

	first, firstResult, handled := drag.Apply(
		winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		snapCursorPoint{X: 50, Y: 150},
		work,
		96,
		10,
		false,
	)
	if !handled || !firstResult.SnappedX || firstResult.SnappedY ||
		first != (winfx.Rect{Left: 0, Top: 100, Right: 100, Bottom: 200}) {
		t.Fatalf("first snap = (%+v, %+v, %v)", first, firstResult, handled)
	}

	// The X coordinate deliberately repeats the corrected rectangle. Y is
	// acquired later, after the session already has an active X anchor.
	second, secondResult, handled := drag.Apply(
		winfx.Rect{Left: 0, Top: 8, Right: 100, Bottom: 108},
		snapCursorPoint{X: 50, Y: 58},
		work,
		96,
		10,
		false,
	)
	if !handled || !secondResult.SnappedX || !secondResult.SnappedY ||
		second != (winfx.Rect{Left: 0, Top: 0, Right: 100, Bottom: 100}) {
		t.Fatalf("second snap = (%+v, %+v, %v)", second, secondResult, handled)
	}

	// Windows may feed the corrected top-left rectangle back. Cursor movement
	// must still release the later Y anchor while preserving X.
	released, releasedResult, handled := drag.Apply(
		second,
		snapCursorPoint{X: 50, Y: 78},
		work,
		96,
		10,
		false,
	)
	want := winfx.Rect{Left: 0, Top: 28, Right: 100, Bottom: 128}
	if !handled || !releasedResult.SnappedX || releasedResult.SnappedY ||
		released != want {
		t.Fatalf(
			"secondary-axis release = (%+v, %+v, %v), want rect %+v with X only",
			released,
			releasedResult,
			handled,
			want,
		)
	}
}

func TestMagneticDragBreakawayUsesRawAcquisitionRect(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	raw := winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200}
	var drag magneticDragSession

	snapped, _, handled := drag.Apply(
		raw,
		snapCursorPoint{X: 100, Y: 150},
		work,
		96,
		10,
		false,
	)
	if !handled || snapped.Left != 0 {
		t.Fatalf("initial snap = (%+v, %v)", snapped, handled)
	}

	released, result, handled := drag.Apply(
		snapped,
		snapCursorPoint{X: 120, Y: 150},
		work,
		96,
		10,
		false,
	)
	want := winfx.Rect{Left: 28, Top: 100, Right: 128, Bottom: 200}
	if !handled || result.SnappedX || released != want {
		t.Fatalf(
			"raw-origin release = (%+v, %+v, %v), want %+v",
			released,
			result,
			handled,
			want,
		)
	}
}

func TestMagneticDragControlPressDetachesAndReleaseReenables(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession
	snapped, _, _ := drag.Apply(
		winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		snapCursorPoint{X: 100, Y: 150},
		work,
		96,
		10,
		false,
	)

	detached, result, handled := drag.Apply(
		snapped,
		snapCursorPoint{X: 110, Y: 150},
		winfx.Rect{},
		96,
		10,
		true,
	)
	wantDetached := winfx.Rect{Left: 18, Top: 100, Right: 118, Bottom: 200}
	if !handled || result != (winfx.SnapResult{}) || detached != wantDetached {
		t.Fatalf(
			"Ctrl detach = (%+v, %+v, %v), want %+v",
			detached,
			result,
			handled,
			wantDetached,
		)
	}

	// Ctrl is a live override, not a permanent gesture bypass. Releasing it
	// near the edge must re-enable acquisition in this same system move loop.
	nearEdge := winfx.Rect{Left: 5, Top: 120, Right: 105, Bottom: 220}
	afterControl, result, handled := drag.Apply(
		nearEdge,
		snapCursorPoint{X: 115, Y: 170},
		work,
		96,
		10,
		false,
	)
	wantSnapped := winfx.Rect{Left: 0, Top: 120, Right: 100, Bottom: 220}
	if !handled || !result.SnappedX || result.SnappedY ||
		afterControl != wantSnapped {
		t.Fatalf(
			"post-Ctrl acquisition = (%+v, %+v, %v), want %+v",
			afterControl,
			result,
			handled,
			wantSnapped,
		)
	}
}

func TestMagneticDragContinuedControlLeavesRawMotionUntouched(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession
	snapped, _, _ := drag.Apply(
		winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		snapCursorPoint{X: 100, Y: 150},
		work,
		96,
		10,
		false,
	)
	detached, _, handled := drag.Apply(
		snapped,
		snapCursorPoint{X: 110, Y: 150},
		winfx.Rect{},
		96,
		10,
		true,
	)
	if !handled || detached.Left != 18 {
		t.Fatalf("initial Ctrl detach = (%+v, %v)", detached, handled)
	}

	raw := winfx.Rect{Left: 240, Top: 180, Right: 340, Bottom: 280}
	continued, result, handled := drag.Apply(
		raw,
		snapCursorPoint{X: 290, Y: 230},
		winfx.Rect{},
		96,
		10,
		true,
	)
	if handled || result != (winfx.SnapResult{}) || continued != raw {
		t.Fatalf(
			"continued Ctrl motion = (%+v, %+v, %v), want untouched %+v",
			continued,
			result,
			handled,
			raw,
		)
	}
}

func TestMagneticDragResetClearsAnchorsAndControlBypass(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	for _, reason := range []string{"drag end", "settings close"} {
		reason := reason
		t.Run(reason, func(t *testing.T) {
			t.Parallel()

			var drag magneticDragSession
			snapped, _, _ := drag.Apply(
				winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
				snapCursorPoint{X: 100, Y: 150},
				work,
				96,
				10,
				false,
			)
			drag.Apply(
				snapped,
				snapCursorPoint{X: 110, Y: 150},
				winfx.Rect{},
				96,
				10,
				true,
			)
			if !drag.controlBypass {
				t.Fatal("test setup did not enter Ctrl bypass")
			}

			drag.Reset()
			if drag != (magneticDragSession{}) {
				t.Fatalf("Reset retained stale state: %+v", drag)
			}

			right, result, handled := drag.Apply(
				winfx.Rect{Left: 892, Top: 240, Right: 992, Bottom: 340},
				snapCursorPoint{X: 942, Y: 290},
				work,
				96,
				10,
				false,
			)
			want := winfx.Rect{Left: 900, Top: 240, Right: 1000, Bottom: 340}
			if !handled || !result.SnappedX || result.SnappedY || right != want {
				t.Fatalf(
					"new drag after Reset = (%+v, %+v, %v), want %+v",
					right,
					result,
					handled,
					want,
				)
			}
		})
	}
}

func TestAsyncKeyStatePressedUsesOnlyCurrentStateBit(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		raw  uintptr
		want bool
	}{
		{name: "up", raw: 0},
		{name: "historical low bit only", raw: 0x0001},
		{name: "currently down", raw: 0x8000, want: true},
		{name: "down and pressed since call", raw: 0x8001, want: true},
		{name: "upper uintptr bits ignored", raw: ^uintptr(0) &^ 0xffff},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := asyncKeyStatePressed(test.raw); got != test.want {
				t.Fatalf(
					"asyncKeyStatePressed(%#x) = %v, want %v",
					test.raw,
					got,
					test.want,
				)
			}
		})
	}
}

func TestMagneticDragRearmsAfterLeavingZoneInSameGesture(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession
	left, result, handled := drag.Apply(
		winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		snapCursorPoint{X: 50, Y: 150},
		work,
		96,
		10,
		false,
	)
	if !handled || !result.SnappedX || left.Left != work.Left {
		t.Fatalf("left acquisition = (%+v, %+v, %v)", left, result, handled)
	}

	// Break away far enough to release the left anchor. The raw rectangle is
	// outside every X snap zone, so this message also re-arms X for the next
	// WM_MOVING proposal without snapping again immediately.
	released, result, handled := drag.Apply(
		left,
		snapCursorPoint{X: 80, Y: 150},
		work,
		96,
		10,
		false,
	)
	if !handled || result.SnappedX || released.Left != 38 {
		t.Fatalf("left release = (%+v, %+v, %v)", released, result, handled)
	}

	// Windows can now propose a rectangle at another edge without ending the
	// system move loop. Magnetic snap must remain active for that same drag.
	right, result, handled := drag.Apply(
		winfx.Rect{Left: 892, Top: 100, Right: 992, Bottom: 200},
		snapCursorPoint{X: 942, Y: 150},
		work,
		96,
		10,
		false,
	)
	want := winfx.Rect{Left: 900, Top: 100, Right: 1000, Bottom: 200}
	if !handled || !result.SnappedX || right != want {
		t.Fatalf(
			"same-gesture right acquisition = (%+v, %+v, %v), want %+v",
			right,
			result,
			handled,
			want,
		)
	}
}

func TestMagneticDragCornerAxesReleaseIndependently(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession
	corner, result, _ := drag.Apply(
		winfx.Rect{Left: 8, Top: 8, Right: 108, Bottom: 108},
		snapCursorPoint{X: 100, Y: 100},
		work,
		96,
		10,
		false,
	)
	if !result.SnappedX || !result.SnappedY {
		t.Fatalf("corner did not acquire both axes: (%+v, %+v)", corner, result)
	}

	released, result, handled := drag.Apply(
		corner,
		snapCursorPoint{X: 120, Y: 103},
		work,
		96,
		10,
		false,
	)
	want := winfx.Rect{Left: 28, Top: 0, Right: 128, Bottom: 100}
	if !handled || result.SnappedX || !result.SnappedY || released != want {
		t.Fatalf(
			"independent corner release = (%+v, %+v, %v), want %+v with Y only",
			released,
			result,
			handled,
			want,
		)
	}
}

func TestMagneticDragOrthogonalMotionDoesNotRelease(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	var drag magneticDragSession
	snapped, _, _ := drag.Apply(
		winfx.Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		snapCursorPoint{X: 100, Y: 150},
		work,
		96,
		10,
		false,
	)
	moved, result, handled := drag.Apply(
		winfx.Rect{Left: 0, Top: 500, Right: 100, Bottom: 600},
		snapCursorPoint{X: 100, Y: 550},
		work,
		96,
		10,
		false,
	)
	want := winfx.Rect{Left: 0, Top: 500, Right: 100, Bottom: 600}
	if !handled || !result.SnappedX || result.SnappedY || moved != want ||
		snapped.Left != 0 {
		t.Fatalf(
			"orthogonal move = (%+v, %+v, %v), want %+v with X",
			moved,
			result,
			handled,
			want,
		)
	}
}

func TestMagneticDragNegativeCoordinatesAndScaledRelease(t *testing.T) {
	t.Parallel()

	work := winfx.Rect{Left: -1920, Top: 0, Right: 0, Bottom: 1040}
	var drag magneticDragSession
	snapped, result, _ := drag.Apply(
		winfx.Rect{Left: -1905, Top: 100, Right: -1805, Bottom: 200},
		snapCursorPoint{X: -1800, Y: 150},
		work,
		192,
		10,
		false,
	)
	if !result.SnappedX || snapped.Left != -1920 {
		t.Fatalf("negative-coordinate snap = (%+v, %+v)", snapped, result)
	}

	held, result, _ := drag.Apply(
		snapped,
		snapCursorPoint{X: -1777, Y: 150},
		work,
		192,
		10,
		false,
	)
	if !result.SnappedX || held.Left != -1920 {
		t.Fatalf("scaled hold at 23px = (%+v, %+v)", held, result)
	}

	released, result, handled := drag.Apply(
		held,
		snapCursorPoint{X: -1776, Y: 150},
		work,
		192,
		10,
		false,
	)
	want := winfx.Rect{Left: -1881, Top: 100, Right: -1781, Bottom: 200}
	if !handled || result.SnappedX || released != want {
		t.Fatalf(
			"scaled release at 24px = (%+v, %+v, %v), want %+v",
			released,
			result,
			handled,
			want,
		)
	}
}

func TestMonitorLookupUsesProposedRectangleAndDestinationDPI(t *testing.T) {
	t.Parallel()

	proposedOnMonitorB := winfx.Rect{
		Left: 1935, Top: 80, Right: 2135, Bottom: 280,
	}
	workB := winfx.Rect{
		Left: 1920, Top: 0, Right: 3840, Bottom: 1040,
	}
	var received winfx.Rect
	var receivedFallback uint32
	metrics, err := monitorMetricsForMovingRect(
		proposedOnMonitorB,
		96,
		func(target winfx.Rect, fallbackDPI uint32) (winfx.MonitorMetrics, error) {
			received = target
			receivedFallback = fallbackDPI
			return winfx.MonitorMetrics{WorkArea: workB, DPI: 192}, nil
		},
	)
	if err != nil {
		t.Fatalf("monitor lookup: %v", err)
	}
	if received != proposedOnMonitorB {
		t.Fatalf("lookup received %+v, want proposed %+v", received, proposedOnMonitorB)
	}
	if receivedFallback != 96 {
		t.Fatalf("lookup fallback DPI = %d, want prior-window 96", receivedFallback)
	}
	if metrics.WorkArea != workB || metrics.DPI != 192 {
		t.Fatalf(
			"monitor metrics = %+v, want work area %+v at 192 DPI",
			metrics,
			workB,
		)
	}

	// Fifteen native pixels is outside a 10-DIP zone at 96 DPI, but inside
	// that zone on monitor B at 192 DPI. This proves acquisition uses the
	// proposed monitor's scale rather than the prior window monitor's scale.
	var drag magneticDragSession
	snapped, result, handled := drag.Apply(
		proposedOnMonitorB,
		snapCursorPoint{X: 2000, Y: 180},
		metrics.WorkArea,
		metrics.DPI,
		10,
		false,
	)
	if !handled || !result.SnappedX || snapped.Left != workB.Left {
		t.Fatalf(
			"monitor B snap = (%+v, %+v, %v), want left edge %d",
			snapped,
			result,
			handled,
			workB.Left,
		)
	}
}

func TestLiveViewBoundsNeverReplaysPersistedPosition(t *testing.T) {
	t.Parallel()

	current := walk.Rectangle{X: 741, Y: -120, Width: 200, Height: 200}
	ui := config.Default().UI
	ui.WindowBounds = config.WindowBounds{
		X: 32, Y: 48,
		Width: 288, Height: 244,
	}
	got := liveViewBounds(current, ui)
	if got.X != current.X || got.Y != current.Y {
		t.Fatalf(
			"live preview restored stale position (%d,%d), want current (%d,%d)",
			got.X,
			got.Y,
			current.X,
			current.Y,
		)
	}
	if got.Width != ui.WindowBounds.Width ||
		got.Height != ui.WindowBounds.Height {
		t.Fatalf(
			"live preview size = %dx%d, want %dx%d",
			got.Width,
			got.Height,
			ui.WindowBounds.Width,
			ui.WindowBounds.Height,
		)
	}
}
