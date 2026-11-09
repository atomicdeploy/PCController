//go:build windows

package ui

import (
	"crypto/sha256"
	"fmt"
	"image"
	"math"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func TestGlassResizeMaterialDigest(t *testing.T) {
	s, _ := glassAnimationFixture()
	hash := sha256.New()
	for i, size := range []image.Point{{640, 720}, {701, 753}, {630, 690}, {640, 720}} {
		s.dark = i%2 == 0
		s.hoverLevels[32] = float64(i) / 4
		s.visualPointer = walk.Point{X: 72, Y: 302}
		frame := s.composeGlassControls(image.NewRGBA(image.Rect(0, 0, size.X, size.Y)))
		hash.Write(frame.Pix)
	}
	// Captured before the resize optimization: preserve every material pixel,
	// including grain, rims, shadows, hover and both palette variants.
	const original = "1b1dc2de9644a386b34b01fd84ade1fc1be82335dec0b501ba62c802d08896ce"
	if got := fmt.Sprintf("%x", hash.Sum(nil)); got != original {
		t.Fatalf("resize changed glass material pixels: %s", got)
	}
}

func TestRoundedRectDistanceMatchesOriginalEquation(t *testing.T) {
	for _, radius := range []float64{0, 1, 10, 21.5} {
		for y := -30.25; y < 70; y += 0.5 {
			for x := -30.25; x < 130; x += 0.5 {
				qx := math.Abs(x-50) - (50 - radius)
				qy := math.Abs(y-20) - (20 - radius)
				want := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
				if got := roundedRectDistance(x, y, 50, 20, 50, 20, radius); got != want {
					t.Fatalf("distance changed at %v,%v radius=%v: %v != %v", x, y, radius, got, want)
				}
			}
		}
	}
}

// Test-owned HWND only: repeatable end-to-end resize cost, no core or hardware.
func TestControllerResizeTiming(t *testing.T) {
	s, cfg := newControllerWindowFixture(t)
	cfg.UI.Animations = false
	s.cfg = cfg
	s.applyConfigUI(cfg, false)
	s.mw.Show()
	pumpControllerFixture(s)
	win.SendMessage(s.mw.Handle(), wmEnterSizeMove, 0, 0)
	defer win.SendMessage(s.mw.Handle(), wmExitSizeMove, 0, 0)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	const frames = 24
	durations := make([]time.Duration, 0, frames)
	requests := make([]time.Duration, 0, frames)
	for i := 0; i < frames; i++ {
		start := time.Now()
		if err := s.mw.SetBoundsPixels(walk.Rectangle{X: 80, Y: 80, Width: 900 + i*3, Height: 700 + i*2}); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, time.Since(start))
		finishResizeFixture(t, s)
		durations = append(durations, time.Since(start))
	}
	runtime.ReadMemStats(&after)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	sort.Slice(requests, func(i, j int) bool { return requests[i] < requests[j] })
	t.Logf("resize %d frames: median=%s p95=%s allocated=%.2f MiB/frame backend=%s", frames, durations[frames/2], durations[frames*95/100], float64(after.TotalAlloc-before.TotalAlloc)/1048576/frames, s.rendering.status.Backend)
	t.Logf("native resize request: median=%s p95=%s", requests[frames/2], requests[frames*95/100])
}

func finishResizeFixture(t *testing.T, s *Shell) {
	t.Helper()
	want := s.mw.ClientBoundsPixels()
	ready := func() bool {
		got := s.surface.ClientBoundsPixels()
		return got.Width == want.Width && got.Height == want.Height
	}
	if !ready() {
		deadline := time.Now().Add(5 * time.Second)
		var check func()
		check = func() {
			time.AfterFunc(time.Millisecond, func() {
				s.mw.Synchronize(func() {
					if ready() || time.Now().After(deadline) {
						win.PostQuitMessage(0)
					} else {
						check()
					}
				})
			})
		}
		check()
		s.mw.Run()
		if !ready() {
			t.Fatalf("resize layout did not reach final client bounds: got=%+v want=%+v", s.surface.ClientBoundsPixels(), want)
		}
	}
	win.UpdateWindow(s.surface.Handle())
}

func TestControllerResizeBurstPreservesFinalGeometry(t *testing.T) {
	for _, backend := range []string{"auto", "legacy"} {
		t.Run(backend, func(t *testing.T) {
			s, cfg := newControllerWindowFixture(t)
			cfg.UI.RenderBackend = backend
			s.cfg = cfg
			s.applyConfigUI(cfg, false)
			s.mw.Show()
			pumpControllerFixture(s)
			s.windowSessionTracking.Store(true)
			win.SendMessage(s.mw.Handle(), wmEnterSizeMove, 0, 0)
			for _, width := range []int{900, 950, 1000, 940, 920, 960} {
				if err := s.mw.SetBoundsPixels(walk.Rectangle{X: 90, Y: 90, Width: width, Height: 700}); err != nil {
					t.Fatal(err)
				}
			}
			win.SendMessage(s.mw.Handle(), wmExitSizeMove, 0, 0)
			finishResizeFixture(t, s)
			got := s.surface.ClientBoundsPixels()
			if s.presentedSurface == nil || s.presentedSurface.Bounds().Dx() != got.Width || s.presentedSurface.Bounds().Dy() != got.Height {
				t.Fatal("resize retained a scaled or old-size final frame")
			}
			if s.Config().UI.WindowBounds.Width != 960 {
				t.Fatal("resize did not persist the final outer window bounds")
			}
			if os.Getenv("PCCONTROLLER_DESKTOP_SCREENSHOTS") != "" {
				captureControllerFixture(t, s, "gui-resize-"+backend)
			}
		})
	}
}
