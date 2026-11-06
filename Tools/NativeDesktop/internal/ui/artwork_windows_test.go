//go:build windows

package ui

import (
	"image"
	"image/color"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/animation"
)

func TestAnimationCacheEvictsSourcesAndRedecodesAfterResize(t *testing.T) {
	decoded := testUIAnimation(3)
	shell := &Shell{animation: decoded}
	defer shell.disposeAnimationBitmaps()

	for index := 0; index < decoded.FrameCount(); index++ {
		if _, err := shell.animationBitmap(index, 2); err != nil {
			t.Fatalf("cache frame %d: %v", index, err)
		}
	}
	if !shell.animationCacheComplete() {
		t.Fatal("active-size animation cache is incomplete")
	}
	if decoded.HasFrames() || len(decoded.Frames) != 0 {
		t.Fatal("raw animation sources remained after cache completion")
	}
	if got := decoded.FrameCount(); got != 3 {
		t.Fatalf("frame count after source eviction = %d, want 3", got)
	}

	originalLoader := loadDeviceAnimation
	loadCount := 0
	loadDeviceAnimation = func() (*animation.Animation, error) {
		loadCount++
		return testUIAnimation(3), nil
	}
	defer func() { loadDeviceAnimation = originalLoader }()

	if _, err := shell.animationBitmap(0, 3); err != nil {
		t.Fatalf("rebuild resized animation cache: %v", err)
	}
	if loadCount != 1 {
		t.Fatalf("animation re-decodes after resize = %d, want 1", loadCount)
	}
	if shell.animationBitmapSize != 3 {
		t.Fatalf(
			"resized animation cache = %d px, want 3",
			shell.animationBitmapSize,
		)
	}
	if shell.animation == decoded {
		t.Fatal("evicted animation sources were not replaced by a fresh decode")
	}
	if !shell.animation.HasFrames() {
		t.Fatal("fresh decoded sources were prematurely released")
	}
}

func TestSelectAnimationCacheSizeInitializesHiddenPaintAndInvalidatesResize(
	t *testing.T,
) {
	shell := &Shell{
		animationBitmapSize:   0,
		animationCachePrepare: true,
		animationCacheSize:    99,
	}
	startEpoch := shell.animationCacheEpoch.Load()
	shell.selectAnimationCacheSize(176)
	if shell.animationBitmapSize != 176 {
		t.Fatalf(
			"hidden-paint cache size = %d, want 176",
			shell.animationBitmapSize,
		)
	}
	if shell.animationCachePrepare || shell.animationCacheSize != 0 {
		t.Fatal("initial size selection retained stale preparation state")
	}
	if shell.animationCacheEpoch.Load() == startEpoch {
		t.Fatal("initial size selection did not invalidate stale workers")
	}

	stableEpoch := shell.animationCacheEpoch.Load()
	shell.selectAnimationCacheSize(176)
	if shell.animationCacheEpoch.Load() != stableEpoch {
		t.Fatal("matching cache size invalidated the active cache")
	}

	shell.animationCachePrepare = true
	shell.animationCacheSize = 176
	shell.selectAnimationCacheSize(240)
	if shell.animationBitmapSize != 240 ||
		shell.animationCachePrepare ||
		shell.animationCacheSize != 0 {
		t.Fatal("resize did not reset and select the new animation cache")
	}
	if shell.animationCacheEpoch.Load() == stableEpoch {
		t.Fatal("resize did not invalidate the old cache worker")
	}
}

func TestDisabledAnimationResourceIsLazyAndReversible(t *testing.T) {
	originalLoader := loadDeviceAnimation
	loadCount := 0
	loadDeviceAnimation = func() (*animation.Animation, error) {
		loadCount++
		return testUIAnimation(2), nil
	}
	defer func() { loadDeviceAnimation = originalLoader }()

	shell := new(Shell)
	if err := shell.setAnimationResourceEnabled(false); err != nil {
		t.Fatal(err)
	}
	if loadCount != 0 {
		t.Fatalf("disabled animation decoded %d times, want 0", loadCount)
	}
	if shell.animation != nil {
		t.Fatal("disabled animation retained a resource")
	}

	if err := shell.setAnimationResourceEnabled(true); err != nil {
		t.Fatal(err)
	}
	if loadCount != 1 || shell.animation == nil ||
		!shell.animation.HasFrames() {
		t.Fatalf(
			"enabled animation load = %d, animation=%v",
			loadCount,
			shell.animation,
		)
	}

	if _, err := shell.animationBitmap(0, 2); err != nil {
		t.Fatal(err)
	}
	cacheEpoch := shell.animationCacheEpoch.Load()
	shell.animationCachePrepare = true
	shell.animationCacheSize = 2
	if err := shell.setAnimationResourceEnabled(false); err != nil {
		t.Fatal(err)
	}
	if shell.animation != nil || shell.animationBitmaps != nil ||
		shell.animationBitmapSize != 0 {
		t.Fatal("disabling animation did not release decoded/native caches")
	}
	if shell.animationCachePrepare || shell.animationCacheSize != 0 {
		t.Fatal("disabling animation retained cache preparation state")
	}
	if shell.animationCacheEpoch.Load() == cacheEpoch {
		t.Fatal("disabling animation did not invalidate cache preparation")
	}

	if err := shell.setAnimationResourceEnabled(true); err != nil {
		t.Fatal(err)
	}
	if loadCount != 2 {
		t.Fatalf("animation reloads after re-enable = %d, want 2", loadCount)
	}
	shell.setAnimationResourceEnabled(false)
}

func testUIAnimation(count int) *animation.Animation {
	frames := make([]image.Image, count)
	delays := make([]time.Duration, count)
	for index := range frames {
		frame := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		frame.SetNRGBA(
			index%4,
			index%4,
			color.NRGBA{
				R: uint8(40 + index),
				G: uint8(80 + index),
				B: uint8(120 + index),
				A: 255,
			},
		)
		frames[index] = frame
		delays[index] = time.Duration(index+1) * time.Millisecond
	}
	return &animation.Animation{
		Frames: frames,
		Delays: delays,
	}
}
