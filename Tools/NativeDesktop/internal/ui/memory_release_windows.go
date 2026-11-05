//go:build windows

package ui

import (
	"runtime/debug"
	"sync/atomic"
	"time"

	"rayanlamp.local/app/internal/animation"
)

const (
	idleMemoryReleaseThreshold = int64(8 << 20)
	idleMemoryReleaseDelay     = 1500 * time.Millisecond
)

var idleMemoryReleaseEpoch atomic.Uint64

func cancelIdleMemoryRelease() {
	idleMemoryReleaseEpoch.Add(1)
}

// scheduleIdleMemoryRelease asks Go to return a large, newly unreachable image
// arena to Windows after the startup/resize work has gone idle. The delay lets
// the cache worker drop its last local frame references first and coalesces
// rapid resize or configuration changes. This is never run from WM_PAINT.
func scheduleIdleMemoryRelease(releasedBytes int64) {
	if releasedBytes < idleMemoryReleaseThreshold {
		return
	}
	epoch := idleMemoryReleaseEpoch.Add(1)
	time.AfterFunc(idleMemoryReleaseDelay, func() {
		if idleMemoryReleaseEpoch.Load() == epoch {
			debug.FreeOSMemory()
		}
	})
}

func residentAnimationBytes(decoded *animation.Animation) int64 {
	if decoded == nil {
		return 0
	}
	var total int64
	for _, frame := range decoded.Frames {
		if frame == nil {
			continue
		}
		bounds := frame.Bounds()
		width, height := int64(bounds.Dx()), int64(bounds.Dy())
		if width <= 0 || height <= 0 ||
			width > (1<<62)/height/4 {
			continue
		}
		total += width * height * 4
	}
	return total
}
