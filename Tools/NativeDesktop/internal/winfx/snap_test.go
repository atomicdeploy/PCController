package winfx

import "testing"

func TestSnapRect(t *testing.T) {
	work := Rect{Left: 0, Top: 0, Right: 1920, Bottom: 1040}
	tests := []struct {
		name      string
		moving    Rect
		work      Rect
		nearby    []Rect
		dpi       uint32
		threshold int
		want      Rect
		wantX     bool
		wantY     bool
	}{
		{
			name:   "work area top left",
			moving: Rect{Left: 10, Top: 8, Right: 310, Bottom: 408},
			work:   work, dpi: 96, threshold: 12,
			want:  Rect{Left: 0, Top: 0, Right: 300, Bottom: 400},
			wantX: true, wantY: true,
		},
		{
			name:   "outside threshold",
			moving: Rect{Left: 13, Top: 13, Right: 313, Bottom: 413},
			work:   work, dpi: 96, threshold: 12,
			want: Rect{Left: 13, Top: 13, Right: 313, Bottom: 413},
		},
		{
			name:   "scaled 200 percent",
			moving: Rect{Left: 15, Top: 100, Right: 315, Bottom: 500},
			work:   work, dpi: 192, threshold: 8,
			want:  Rect{Left: 0, Top: 100, Right: 300, Bottom: 500},
			wantX: true,
		},
		{
			name:   "negative monitor coordinates",
			moving: Rect{Left: -1910, Top: 100, Right: -1610, Bottom: 500},
			work:   Rect{Left: -1920, Top: 0, Right: 0, Bottom: 1040},
			dpi:    96, threshold: 12,
			want:  Rect{Left: -1920, Top: 100, Right: -1620, Bottom: 500},
			wantX: true,
		},
		{
			name:   "nearby adjacent edge",
			moving: Rect{Left: 190, Top: 100, Right: 490, Bottom: 500},
			work:   work,
			nearby: []Rect{{Left: 500, Top: 50, Right: 800, Bottom: 600}},
			dpi:    96, threshold: 12,
			want:  Rect{Left: 200, Top: 100, Right: 500, Bottom: 500},
			wantX: true,
		},
		{
			name:   "nearby requires orthogonal overlap",
			moving: Rect{Left: 190, Top: 700, Right: 490, Bottom: 900},
			work:   work,
			nearby: []Rect{{Left: 500, Top: 50, Right: 800, Bottom: 600}},
			dpi:    96, threshold: 12,
			want: Rect{Left: 190, Top: 700, Right: 490, Bottom: 900},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, result := SnapRect(test.moving, test.work, test.nearby, test.dpi, test.threshold)
			if got != test.want {
				t.Fatalf("SnapRect() = %#v, want %#v (result %#v)", got, test.want, result)
			}
			if result.SnappedX != test.wantX || result.SnappedY != test.wantY {
				t.Fatalf("snap flags = (%v,%v), want (%v,%v)", result.SnappedX, result.SnappedY, test.wantX, test.wantY)
			}
			if got.Width() != test.moving.Width() || got.Height() != test.moving.Height() {
				t.Fatalf("SnapRect changed size: before %#v after %#v", test.moving, got)
			}
		})
	}
}

func TestScaledSnapThreshold(t *testing.T) {
	tests := []struct {
		dip  int
		dpi  uint32
		want int32
	}{
		{12, 96, 12},
		{12, 144, 18},
		{8, 192, 16},
		{1, 48, 1},
		{0, 192, 0},
	}
	for _, test := range tests {
		if got := ScaledSnapThreshold(test.dip, test.dpi); got != test.want {
			t.Errorf("ScaledSnapThreshold(%d, %d) = %d, want %d", test.dip, test.dpi, got, test.want)
		}
	}
}

func TestDragSnapSequences(t *testing.T) {
	work := Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	type step struct {
		name       string
		raw        Rect
		want       Rect
		wantResult SnapResult
	}
	tests := []struct {
		name      string
		work      Rect
		nearby    []Rect
		dpi       uint32
		threshold int
		steps     []step
	}{
		{
			name: "leading x edge holds releases both ways and resnaps",
			work: work, dpi: 96, threshold: 10,
			steps: []step{
				{
					name: "snap from inside",
					raw:  Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
					want: Rect{Left: 0, Top: 100, Right: 100, Bottom: 200},
					wantResult: SnapResult{
						DX: -8, SnappedX: true,
					},
				},
				{
					name: "move orthogonally while held at positive boundary",
					raw:  Rect{Left: 30, Top: 170, Right: 130, Bottom: 270},
					want: Rect{Left: 0, Top: 170, Right: 100, Bottom: 270},
					wantResult: SnapResult{
						DX: -30, SnappedX: true,
					},
				},
				{
					name: "release positive",
					raw:  Rect{Left: 31, Top: 190, Right: 131, Bottom: 290},
					want: Rect{Left: 31, Top: 190, Right: 131, Bottom: 290},
				},
				{
					name: "resnap positive",
					raw:  Rect{Left: 9, Top: 210, Right: 109, Bottom: 310},
					want: Rect{Left: 0, Top: 210, Right: 100, Bottom: 310},
					wantResult: SnapResult{
						DX: -9, SnappedX: true,
					},
				},
				{
					name: "hold negative at boundary",
					raw:  Rect{Left: -30, Top: 230, Right: 70, Bottom: 330},
					want: Rect{Left: 0, Top: 230, Right: 100, Bottom: 330},
					wantResult: SnapResult{
						DX: 30, SnappedX: true,
					},
				},
				{
					name: "release negative",
					raw:  Rect{Left: -31, Top: 250, Right: 69, Bottom: 350},
					want: Rect{Left: -31, Top: 250, Right: 69, Bottom: 350},
				},
				{
					name: "resnap negative",
					raw:  Rect{Left: -9, Top: 270, Right: 91, Bottom: 370},
					want: Rect{Left: 0, Top: 270, Right: 100, Bottom: 370},
					wantResult: SnapResult{
						DX: 9, SnappedX: true,
					},
				},
			},
		},
		{
			name: "trailing x edge remembers the moving edge",
			work: work, dpi: 96, threshold: 10,
			steps: []step{
				{
					name: "snap",
					raw:  Rect{Left: 891, Top: 100, Right: 991, Bottom: 200},
					want: Rect{Left: 900, Top: 100, Right: 1000, Bottom: 200},
					wantResult: SnapResult{
						DX: 9, SnappedX: true,
					},
				},
				{
					name: "hold inward",
					raw:  Rect{Left: 870, Top: 120, Right: 970, Bottom: 220},
					want: Rect{Left: 900, Top: 120, Right: 1000, Bottom: 220},
					wantResult: SnapResult{
						DX: 30, SnappedX: true,
					},
				},
				{
					name: "release inward",
					raw:  Rect{Left: 869, Top: 140, Right: 969, Bottom: 240},
					want: Rect{Left: 869, Top: 140, Right: 969, Bottom: 240},
				},
				{
					name: "resnap",
					raw:  Rect{Left: 892, Top: 160, Right: 992, Bottom: 260},
					want: Rect{Left: 900, Top: 160, Right: 1000, Bottom: 260},
					wantResult: SnapResult{
						DX: 8, SnappedX: true,
					},
				},
				{
					name: "hold outward",
					raw:  Rect{Left: 930, Top: 180, Right: 1030, Bottom: 280},
					want: Rect{Left: 900, Top: 180, Right: 1000, Bottom: 280},
					wantResult: SnapResult{
						DX: -30, SnappedX: true,
					},
				},
				{
					name: "release outward",
					raw:  Rect{Left: 931, Top: 200, Right: 1031, Bottom: 300},
					want: Rect{Left: 931, Top: 200, Right: 1031, Bottom: 300},
				},
			},
		},
		{
			name: "y axis holds and releases independently",
			work: work, dpi: 96, threshold: 10,
			steps: []step{
				{
					name: "snap top",
					raw:  Rect{Left: 200, Top: 7, Right: 300, Bottom: 107},
					want: Rect{Left: 200, Top: 0, Right: 300, Bottom: 100},
					wantResult: SnapResult{
						DY: -7, SnappedY: true,
					},
				},
				{
					name: "move x while y held",
					raw:  Rect{Left: 350, Top: 30, Right: 450, Bottom: 130},
					want: Rect{Left: 350, Top: 0, Right: 450, Bottom: 100},
					wantResult: SnapResult{
						DY: -30, SnappedY: true,
					},
				},
				{
					name: "release below",
					raw:  Rect{Left: 400, Top: 31, Right: 500, Bottom: 131},
					want: Rect{Left: 400, Top: 31, Right: 500, Bottom: 131},
				},
				{
					name: "resnap above",
					raw:  Rect{Left: 450, Top: -8, Right: 550, Bottom: 92},
					want: Rect{Left: 450, Top: 0, Right: 550, Bottom: 100},
					wantResult: SnapResult{
						DY: 8, SnappedY: true,
					},
				},
				{
					name: "release above",
					raw:  Rect{Left: 500, Top: -31, Right: 600, Bottom: 69},
					want: Rect{Left: 500, Top: -31, Right: 600, Bottom: 69},
				},
			},
		},
		{
			name: "nearby edge latch stores target and trailing edge",
			work: work,
			nearby: []Rect{
				{Left: 500, Top: 50, Right: 800, Bottom: 600},
			},
			dpi: 96, threshold: 10,
			steps: []step{
				{
					name: "snap adjacent",
					raw:  Rect{Left: 395, Top: 100, Right: 495, Bottom: 200},
					want: Rect{Left: 400, Top: 100, Right: 500, Bottom: 200},
					wantResult: SnapResult{
						DX: 5, SnappedX: true,
					},
				},
				{
					name: "hold exact target despite raw movement",
					raw:  Rect{Left: 420, Top: 300, Right: 520, Bottom: 400},
					want: Rect{Left: 400, Top: 300, Right: 500, Bottom: 400},
					wantResult: SnapResult{
						DX: -20, SnappedX: true,
					},
				},
				{
					name: "release from target",
					raw:  Rect{Left: 431, Top: 320, Right: 531, Bottom: 420},
					want: Rect{Left: 431, Top: 320, Right: 531, Bottom: 420},
				},
			},
		},
		{
			name: "96 dpi uses native ten pixel acquisition",
			work: work, dpi: 96, threshold: 10,
			steps: []step{
				{
					name: "twenty pixels does not acquire",
					raw:  Rect{Left: 20, Top: 100, Right: 120, Bottom: 200},
					want: Rect{Left: 20, Top: 100, Right: 120, Bottom: 200},
				},
			},
		},
		{
			name: "200 percent dpi scales acquisition and release",
			work: work, dpi: 192, threshold: 10,
			steps: []step{
				{
					name: "twenty native pixels acquires",
					raw:  Rect{Left: 20, Top: 100, Right: 120, Bottom: 200},
					want: Rect{Left: 0, Top: 100, Right: 100, Bottom: 200},
					wantResult: SnapResult{
						DX: -20, SnappedX: true,
					},
				},
				{
					name: "sixty native pixels remains held",
					raw:  Rect{Left: 60, Top: 120, Right: 160, Bottom: 220},
					want: Rect{Left: 0, Top: 120, Right: 100, Bottom: 220},
					wantResult: SnapResult{
						DX: -60, SnappedX: true,
					},
				},
				{
					name: "sixty one native pixels releases",
					raw:  Rect{Left: 61, Top: 140, Right: 161, Bottom: 240},
					want: Rect{Left: 61, Top: 140, Right: 161, Bottom: 240},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var snap DragSnap
			for _, step := range test.steps {
				t.Run(step.name, func(t *testing.T) {
					got, result := snap.SnapRect(
						step.raw,
						test.work,
						test.nearby,
						test.dpi,
						test.threshold,
					)
					if got != step.want {
						t.Errorf(
							"SnapRect(%#v) = %#v, want %#v",
							step.raw,
							got,
							step.want,
						)
					}
					if result != step.wantResult {
						t.Errorf(
							"result for %#v = %#v, want %#v",
							step.raw,
							result,
							step.wantResult,
						)
					}
					if got.Width() != step.raw.Width() ||
						got.Height() != step.raw.Height() {
						t.Errorf(
							"size changed: raw %#v, snapped %#v",
							step.raw,
							got,
						)
					}
				})
			}
		})
	}
}

func TestDragSnapReset(t *testing.T) {
	work := Rect{Left: 0, Top: 0, Right: 1000, Bottom: 800}
	snap := new(DragSnap)
	first, firstResult := snap.SnapRect(
		Rect{Left: 8, Top: 100, Right: 108, Bottom: 200},
		work,
		nil,
		96,
		10,
	)
	if first.Left != 0 || !firstResult.SnappedX {
		t.Fatalf("initial snap = (%#v, %#v)", first, firstResult)
	}

	snap.Reset()
	raw := Rect{Left: 25, Top: 100, Right: 125, Bottom: 200}
	got, result := snap.SnapRect(raw, work, nil, 96, 10)
	if got != raw || result.SnappedX || result.SnappedY {
		t.Fatalf("snap after Reset = (%#v, %#v), want unsnapped %#v", got, result, raw)
	}
}
