//go:build windows

package ui

import (
	"errors"

	"pccontroller.local/desktop/internal/winfx"
)

type snapCursorPoint struct {
	X int32
	Y int32
}

type snapAxisAnchor struct {
	active          bool
	bypass          bool
	cursor          int32
	rawLeading      int32
	rawTrailing     int32
	snappedLeading  int32
	snappedTrailing int32
}

type magneticDragSession struct {
	x             snapAxisAnchor
	y             snapAxisAnchor
	controlBypass bool
}

func (s *magneticDragSession) Reset() {
	if s != nil {
		*s = magneticDragSession{}
	}
}

// Apply processes one proposed WM_MOVING rectangle. Windows permits a handler
// to replace that rectangle, but does not promise that the following message
// will contain an unmodified cursor-derived rectangle. Each snapped axis
// therefore retains its own raw acquisition rectangle and cursor coordinate.
//
// handled reports that the caller must write the returned rectangle to lParam
// and return TRUE. It is also true for a breakaway transition even though the
// returned SnapResult no longer reports that released axis as snapped.
func (s *magneticDragSession) Apply(
	target winfx.Rect,
	cursor snapCursorPoint,
	workArea winfx.Rect,
	dpi uint32,
	thresholdDIP int,
	controlDown bool,
	nearby ...winfx.Rect,
) (winfx.Rect, winfx.SnapResult, bool) {
	if s == nil {
		if controlDown {
			return target, winfx.SnapResult{}, false
		}
		snapped, result := winfx.SnapRect(
			target,
			workArea,
			nearby,
			dpi,
			thresholdDIP,
		)
		return snapped, result, result.SnappedX || result.SnappedY
	}
	if !target.Valid() || thresholdDIP <= 0 {
		s.Reset()
		return target, winfx.SnapResult{}, false
	}

	if controlDown {
		return s.detachForControl(target, cursor)
	}
	if s.controlBypass {
		// Ctrl is a temporary, live bypass rather than an axis breakaway.
		// Releasing it during the same system move loop immediately re-arms
		// both axes against the current raw proposal.
		s.controlBypass = false
		s.x = snapAxisAnchor{}
		s.y = snapAxisAnchor{}
	}

	release := snapReleaseDistance(thresholdDIP, dpi)
	raw := target
	handled := false

	if s.x.active {
		delta := int64(cursor.X) - int64(s.x.cursor)
		raw.Left, raw.Right = translateSnapAxis(
			s.x.rawLeading,
			s.x.rawTrailing,
			delta,
		)
		if absSnapDelta(delta) >= release {
			s.x = snapAxisAnchor{bypass: true}
			handled = true
		}
	}
	if s.y.active {
		delta := int64(cursor.Y) - int64(s.y.cursor)
		raw.Top, raw.Bottom = translateSnapAxis(
			s.y.rawLeading,
			s.y.rawTrailing,
			delta,
		)
		if absSnapDelta(delta) >= release {
			s.y = snapAxisAnchor{bypass: true}
			handled = true
		}
	}

	candidate, candidateResult := winfx.SnapRect(
		raw,
		workArea,
		nearby,
		dpi,
		thresholdDIP,
	)
	output := raw
	result := winfx.SnapResult{}
	// A released axis is bypassed only while its raw rectangle remains inside
	// the same magnetic zone. Once the pointer has genuinely left that zone,
	// re-arm the axis for a later edge in this same WM_ENTERSIZEMOVE session.
	// Do not reacquire on the re-arm message itself: that one-message gap is
	// the hysteresis which prevents an immediate stick/release loop.
	xCanAcquire := !s.x.bypass
	yCanAcquire := !s.y.bypass
	if s.x.bypass && !candidateResult.SnappedX {
		s.x.bypass = false
	}
	if s.y.bypass && !candidateResult.SnappedY {
		s.y.bypass = false
	}

	if s.x.active {
		output.Left = s.x.snappedLeading
		output.Right = s.x.snappedTrailing
		result.DX = snapCoordinateDelta(raw.Left, output.Left)
		result.SnappedX = true
		handled = true
	} else if xCanAcquire && candidateResult.SnappedX {
		s.x.acquire(
			cursor.X,
			raw.Left,
			raw.Right,
			candidate.Left,
			candidate.Right,
		)
		output.Left = candidate.Left
		output.Right = candidate.Right
		result.DX = candidateResult.DX
		result.SnappedX = true
		handled = true
	}

	if s.y.active {
		output.Top = s.y.snappedLeading
		output.Bottom = s.y.snappedTrailing
		result.DY = snapCoordinateDelta(raw.Top, output.Top)
		result.SnappedY = true
		handled = true
	} else if yCanAcquire && candidateResult.SnappedY {
		s.y.acquire(
			cursor.Y,
			raw.Top,
			raw.Bottom,
			candidate.Top,
			candidate.Bottom,
		)
		output.Top = candidate.Top
		output.Bottom = candidate.Bottom
		result.DY = candidateResult.DY
		result.SnappedY = true
		handled = true
	}

	return output, result, handled
}

func (s *magneticDragSession) detachForControl(
	target winfx.Rect,
	cursor snapCursorPoint,
) (winfx.Rect, winfx.SnapResult, bool) {
	handled := false
	if s.x.active {
		target.Left, target.Right = translateSnapAxis(
			s.x.rawLeading,
			s.x.rawTrailing,
			int64(cursor.X)-int64(s.x.cursor),
		)
		handled = true
	}
	if s.y.active {
		target.Top, target.Bottom = translateSnapAxis(
			s.y.rawLeading,
			s.y.rawTrailing,
			int64(cursor.Y)-int64(s.y.cursor),
		)
		handled = true
	}
	s.x = snapAxisAnchor{}
	s.y = snapAxisAnchor{}
	s.controlBypass = true
	return target, winfx.SnapResult{}, handled
}

func (a *snapAxisAnchor) acquire(
	cursor int32,
	rawLeading int32,
	rawTrailing int32,
	snappedLeading int32,
	snappedTrailing int32,
) {
	*a = snapAxisAnchor{
		active:          true,
		cursor:          cursor,
		rawLeading:      rawLeading,
		rawTrailing:     rawTrailing,
		snappedLeading:  snappedLeading,
		snappedTrailing: snappedTrailing,
	}
}

func snapReleaseDistance(thresholdDIP int, dpi uint32) int64 {
	release := int64(winfx.ScaledSnapThreshold(thresholdDIP, dpi)) + 4
	if release < 8 {
		return 8
	}
	return release
}

func translateSnapAxis(leading, trailing int32, delta int64) (int32, int32) {
	const (
		minimum = int64(-1 << 31)
		maximum = int64(1<<31 - 1)
	)
	width := int64(trailing) - int64(leading)
	if width <= 0 {
		return leading, trailing
	}
	minimumDelta := minimum - int64(leading)
	maximumDelta := maximum - int64(trailing)
	if delta < minimumDelta {
		delta = minimumDelta
	}
	if delta > maximumDelta {
		delta = maximumDelta
	}
	return int32(int64(leading) + delta), int32(int64(leading) + delta + width)
}

func snapCoordinateDelta(from, to int32) int32 {
	const (
		minimum = int64(-1 << 31)
		maximum = int64(1<<31 - 1)
	)
	delta := int64(to) - int64(from)
	if delta < minimum {
		return int32(minimum)
	}
	if delta > maximum {
		return int32(maximum)
	}
	return int32(delta)
}

func absSnapDelta(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

type movingMonitorLookup func(winfx.Rect, uint32) (winfx.MonitorMetrics, error)

func monitorMetricsForMovingRect(
	target winfx.Rect,
	fallbackDPI uint32,
	lookup movingMonitorLookup,
) (winfx.MonitorMetrics, error) {
	if lookup == nil {
		return winfx.MonitorMetrics{}, errors.New("ui: moving monitor lookup is nil")
	}
	return lookup(target, fallbackDPI)
}
