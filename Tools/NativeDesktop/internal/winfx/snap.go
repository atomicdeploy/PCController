package winfx

// Rect mirrors the four LONG fields in a Win32 RECT and therefore also
// supports negative virtual-screen coordinates.
type Rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

func (r Rect) Width() int32  { return r.Right - r.Left }
func (r Rect) Height() int32 { return r.Bottom - r.Top }

func (r Rect) Valid() bool {
	return r.Right > r.Left && r.Bottom > r.Top
}

// SnapResult describes the translation applied to the moving rectangle.
type SnapResult struct {
	DX       int32
	DY       int32
	SnappedX bool
	SnappedY bool
}

const snapReleaseMultiplier int64 = 3

type snapEdge uint8

const (
	snapEdgeLeading snapEdge = iota
	snapEdgeTrailing
)

type snapChoice struct {
	delta  int64
	target int32
	edge   snapEdge
	have   bool
}

type snapLatch struct {
	target int32
	edge   snapEdge
	active bool
}

// DragSnap keeps the magnetic state for one window-move gesture. Its zero
// value is ready for use. An acquired axis remains pinned to the same target
// and the same moving edge until the raw WM_MOVING rectangle is more than
// three times the configured snap threshold away.
//
// Call Reset when a move gesture ends or magnetic snapping is bypassed (for
// example, while Ctrl is held), so the next gesture starts without stale
// latches.
type DragSnap struct {
	x snapLatch
	y snapLatch
}

// Reset releases both snapped axes.
func (s *DragSnap) Reset() {
	if s != nil {
		*s = DragSnap{}
	}
}

// ScaledSnapThreshold converts a threshold in 96-DPI logical pixels to native
// pixels. A positive logical threshold always remains at least one pixel.
func ScaledSnapThreshold(thresholdDIP int, dpi uint32) int32 {
	if thresholdDIP <= 0 {
		return 0
	}
	if dpi == 0 {
		dpi = 96
	}
	scaled := (int64(thresholdDIP)*int64(dpi) + 48) / 96
	if scaled < 1 {
		scaled = 1
	}
	if scaled > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(scaled)
}

// SnapRect magnetically translates moving toward a monitor work area and
// nearby window edges. It preserves size and chooses the smallest translation
// independently on each axis. Nearby vertical edges are considered only when
// the windows overlap vertically; nearby horizontal edges require horizontal
// overlap. Work-area edges win equal-distance ties.
//
// This function performs a single stateless calculation. WM_MOVING callers
// should use DragSnap.SnapMovingRect to get breakaway hysteresis.
func SnapRect(
	moving Rect,
	workArea Rect,
	nearby []Rect,
	dpi uint32,
	thresholdDIP int,
) (Rect, SnapResult) {
	if !moving.Valid() {
		return moving, SnapResult{}
	}
	threshold := int64(ScaledSnapThreshold(thresholdDIP, dpi))
	if threshold <= 0 {
		return moving, SnapResult{}
	}

	x, y := snapChoices(moving, workArea, nearby, threshold)
	return applySnapChoices(moving, x, y)
}

func snapChoices(
	moving Rect,
	workArea Rect,
	nearby []Rect,
	threshold int64,
) (snapChoice, snapChoice) {
	var x, y snapChoice
	consider := func(best *snapChoice, delta int64, target int32, edge snapEdge) {
		if abs64(delta) > threshold {
			return
		}
		if !best.have || abs64(delta) < abs64(best.delta) {
			*best = snapChoice{
				delta:  delta,
				target: target,
				edge:   edge,
				have:   true,
			}
		}
	}
	if workArea.Valid() {
		consider(
			&x,
			int64(workArea.Left)-int64(moving.Left),
			workArea.Left,
			snapEdgeLeading,
		)
		consider(
			&x,
			int64(workArea.Right)-int64(moving.Right),
			workArea.Right,
			snapEdgeTrailing,
		)
		consider(
			&y,
			int64(workArea.Top)-int64(moving.Top),
			workArea.Top,
			snapEdgeLeading,
		)
		consider(
			&y,
			int64(workArea.Bottom)-int64(moving.Bottom),
			workArea.Bottom,
			snapEdgeTrailing,
		)
	}

	for _, other := range nearby {
		if !other.Valid() || other == moving {
			continue
		}
		if rangesOverlap(moving.Top, moving.Bottom, other.Top, other.Bottom) {
			// Adjacent edges.
			consider(
				&x,
				int64(other.Left)-int64(moving.Right),
				other.Left,
				snapEdgeTrailing,
			)
			consider(
				&x,
				int64(other.Right)-int64(moving.Left),
				other.Right,
				snapEdgeLeading,
			)
			// Same-side alignment is useful for stacked utility windows.
			consider(
				&x,
				int64(other.Left)-int64(moving.Left),
				other.Left,
				snapEdgeLeading,
			)
			consider(
				&x,
				int64(other.Right)-int64(moving.Right),
				other.Right,
				snapEdgeTrailing,
			)
		}
		if rangesOverlap(moving.Left, moving.Right, other.Left, other.Right) {
			consider(
				&y,
				int64(other.Top)-int64(moving.Bottom),
				other.Top,
				snapEdgeTrailing,
			)
			consider(
				&y,
				int64(other.Bottom)-int64(moving.Top),
				other.Bottom,
				snapEdgeLeading,
			)
			consider(
				&y,
				int64(other.Top)-int64(moving.Top),
				other.Top,
				snapEdgeLeading,
			)
			consider(
				&y,
				int64(other.Bottom)-int64(moving.Bottom),
				other.Bottom,
				snapEdgeTrailing,
			)
		}
	}
	return x, y
}

func applySnapChoices(moving Rect, x, y snapChoice) (Rect, SnapResult) {
	result := SnapResult{
		SnappedX: x.have,
		SnappedY: y.have,
	}
	if x.have {
		result.DX = int32(x.delta)
		moving.Left += result.DX
		moving.Right += result.DX
	}
	if y.have {
		result.DY = int32(y.delta)
		moving.Top += result.DY
		moving.Bottom += result.DY
	}
	return moving, result
}

// SnapRect applies drag-session hysteresis to an unmodified incoming move
// rectangle. Snapping is acquired within thresholdDIP. Once acquired, each
// axis stays attached to its original moving edge and target coordinate until
// the raw rectangle breaks away by more than three times that threshold.
func (s *DragSnap) SnapRect(
	moving Rect,
	workArea Rect,
	nearby []Rect,
	dpi uint32,
	thresholdDIP int,
) (Rect, SnapResult) {
	if s == nil {
		return SnapRect(moving, workArea, nearby, dpi, thresholdDIP)
	}
	if !moving.Valid() {
		s.Reset()
		return moving, SnapResult{}
	}
	threshold := int64(ScaledSnapThreshold(thresholdDIP, dpi))
	if threshold <= 0 {
		s.Reset()
		return moving, SnapResult{}
	}

	releaseThreshold := threshold * snapReleaseMultiplier
	const maxInt32 = int64(^uint32(0) >> 1)
	if releaseThreshold > maxInt32 {
		releaseThreshold = maxInt32
	}

	x, y := snapChoices(moving, workArea, nearby, threshold)
	x = s.x.choose(moving.Left, moving.Right, releaseThreshold, x)
	y = s.y.choose(moving.Top, moving.Bottom, releaseThreshold, y)
	return applySnapChoices(moving, x, y)
}

func (l *snapLatch) choose(
	leading int32,
	trailing int32,
	releaseThreshold int64,
	candidate snapChoice,
) snapChoice {
	if l.active {
		position := leading
		if l.edge == snapEdgeTrailing {
			position = trailing
		}
		delta := int64(l.target) - int64(position)
		if abs64(delta) <= releaseThreshold {
			return snapChoice{
				delta:  delta,
				target: l.target,
				edge:   l.edge,
				have:   true,
			}
		}
		*l = snapLatch{}
	}
	if candidate.have {
		*l = snapLatch{
			target: candidate.target,
			edge:   candidate.edge,
			active: true,
		}
	}
	return candidate
}

// SnapMovingRect is a pointer-oriented convenience for a WM_MOVING handler
// that has already converted lParam to a RECT-compatible *Rect. The bool is
// true when either axis is inside a magnetic snap zone.
func SnapMovingRect(
	target *Rect,
	workArea Rect,
	nearby []Rect,
	dpi uint32,
	thresholdDIP int,
) (SnapResult, bool) {
	if target == nil {
		return SnapResult{}, false
	}
	snapped, result := SnapRect(*target, workArea, nearby, dpi, thresholdDIP)
	*target = snapped
	return result, result.SnappedX || result.SnappedY
}

// SnapMovingRect is the stateful counterpart of the package-level
// SnapMovingRect and is intended for successive WM_MOVING messages.
func (s *DragSnap) SnapMovingRect(
	target *Rect,
	workArea Rect,
	nearby []Rect,
	dpi uint32,
	thresholdDIP int,
) (SnapResult, bool) {
	if target == nil {
		return SnapResult{}, false
	}
	snapped, result := s.SnapRect(
		*target,
		workArea,
		nearby,
		dpi,
		thresholdDIP,
	)
	*target = snapped
	return result, result.SnappedX || result.SnappedY
}

func rangesOverlap(a1, a2, b1, b2 int32) bool {
	return int64(a1) < int64(b2) && int64(b1) < int64(a2)
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
