//go:build windows

package ui

import (
	"fmt"
	"image"

	"github.com/lxn/walk"

	"rayanlamp.local/app/internal/animation"
	"rayanlamp.local/app/internal/graphics"
)

const maxScaledArtworkEntries = 80

type artworkID uint8

const (
	artworkSettings artworkID = iota + 1
	artworkDiagnostics
	artworkReconnect
	artworkClose
)

type artworkBitmapKey struct {
	id     artworkID
	width  int
	height int
	dpi    int
}

type artworkBitmapEntry struct {
	bitmap   *walk.Bitmap
	lastUsed uint64
}

func (s *Shell) scaledArtwork(
	id artworkID,
	width, height int,
) (*walk.Bitmap, error) {
	if s == nil || width < 1 || height < 1 {
		return nil, fmt.Errorf("ui: invalid scaled artwork request")
	}
	source := s.artworkSources[id]
	if source == nil {
		return nil, fmt.Errorf("ui: artwork %d is unavailable", id)
	}
	dpi := 96
	if s.surface != nil {
		dpi = s.surface.DPI()
	}
	key := artworkBitmapKey{id: id, width: width, height: height, dpi: dpi}
	s.artworkCacheTick++
	if entry, ok := s.artworkBitmaps[key]; ok && entry.bitmap != nil {
		entry.lastUsed = s.artworkCacheTick
		s.artworkBitmaps[key] = entry
		return entry.bitmap, nil
	}

	scaled, err := graphics.Resize(source, width, height)
	if err != nil {
		return nil, fmt.Errorf("scale artwork %d: %w", id, err)
	}
	bitmap, err := walk.NewBitmapFromImageForDPI(scaled, dpi)
	if err != nil {
		return nil, fmt.Errorf("create artwork %d bitmap: %w", id, err)
	}
	if s.artworkBitmaps == nil {
		s.artworkBitmaps = make(map[artworkBitmapKey]artworkBitmapEntry)
	}
	s.artworkBitmaps[key] = artworkBitmapEntry{
		bitmap:   bitmap,
		lastUsed: s.artworkCacheTick,
	}
	s.trimArtworkCache()
	return bitmap, nil
}

func (s *Shell) trimArtworkCache() {
	for len(s.artworkBitmaps) > maxScaledArtworkEntries {
		var oldestKey artworkBitmapKey
		var oldest artworkBitmapEntry
		found := false
		for key, entry := range s.artworkBitmaps {
			if !found || entry.lastUsed < oldest.lastUsed {
				oldestKey, oldest, found = key, entry, true
			}
		}
		if !found {
			return
		}
		if oldest.bitmap != nil {
			oldest.bitmap.Dispose()
		}
		delete(s.artworkBitmaps, oldestKey)
	}
}

func (s *Shell) animationBitmap(index, size int) (*walk.Bitmap, error) {
	if s == nil || s.animation == nil || index < 0 ||
		index >= s.animation.FrameCount() || size < 1 {
		return nil, fmt.Errorf("ui: invalid animation bitmap request")
	}
	if s.animationBitmapSize != size {
		s.disposeAnimationBitmaps()
		s.animationBitmapSize = size
	}
	if bitmap := s.animationBitmaps[index]; bitmap != nil {
		return bitmap, nil
	}
	if !s.animation.HasFrames() {
		if err := s.ensureAnimationFrameSources(); err != nil {
			return nil, err
		}
	}
	source := s.animation.Frame(index)
	if source == nil {
		return nil, fmt.Errorf(
			"ui: animation frame %d pixels are unavailable",
			index,
		)
	}

	scaled, err := graphics.Resize(source, size, size)
	if err != nil {
		return nil, fmt.Errorf("scale animation frame %d: %w", index, err)
	}
	dpi := 96
	if s.surface != nil {
		dpi = s.surface.DPI()
	}
	return s.storeAnimationBitmap(index, size, dpi, scaled)
}

func (s *Shell) storeAnimationBitmap(
	index, size, dpi int,
	scaled image.Image,
) (*walk.Bitmap, error) {
	if s == nil || s.animation == nil || scaled == nil ||
		index < 0 || index >= s.animation.FrameCount() ||
		size < 1 || s.animationBitmapSize != size {
		return nil, fmt.Errorf("ui: stale animation bitmap request")
	}
	if bitmap := s.animationBitmaps[index]; bitmap != nil {
		return bitmap, nil
	}
	bitmap, err := walk.NewBitmapFromImageForDPI(scaled, dpi)
	if err != nil {
		return nil, fmt.Errorf("create animation frame %d bitmap: %w", index, err)
	}
	if s.animationBitmaps == nil {
		s.animationBitmaps = make(map[int]*walk.Bitmap)
	}
	s.animationBitmaps[index] = bitmap
	s.releaseAnimationFramesIfCached()
	return bitmap, nil
}

func (s *Shell) animationCacheComplete() bool {
	if s == nil || s.animation == nil ||
		s.animationBitmapSize < 1 {
		return false
	}
	count := s.animation.FrameCount()
	if count == 0 || len(s.animationBitmaps) != count {
		return false
	}
	for index := 0; index < count; index++ {
		if s.animationBitmaps[index] == nil {
			return false
		}
	}
	return true
}

func (s *Shell) releaseAnimationFramesIfCached() {
	if s == nil || s.animation == nil || !s.animationCacheComplete() {
		return
	}
	releasedBytes := residentAnimationBytes(s.animation)
	s.animation.ReleaseFrames()
	scheduleIdleMemoryRelease(releasedBytes)
}

// prepareAnimationCacheAsync completes any frames that WM_PAINT coalesced
// during playback. Scaling runs off the UI thread; creation of Walk/GDI
// bitmaps remains serialized on the owner thread. Frame callbacks and delays
// therefore retain their existing timing while decoded 512-pixel sources can
// be released as soon as the active-size native cache is complete.
func (s *Shell) prepareAnimationCacheAsync(
	active *animation.Animation,
	size int,
) {
	if s == nil || active == nil || active != s.animation ||
		size < 1 || !active.HasFrames() ||
		s.mw == nil || s.closed.Load() {
		s.releaseAnimationFramesIfCached()
		return
	}
	s.selectAnimationCacheSize(size)
	if s.animationCacheComplete() {
		s.releaseAnimationFramesIfCached()
		return
	}
	if s.animationCachePrepare && s.animationCacheSize == size {
		return
	}

	token := s.animationCacheEpoch.Add(1)
	s.animationCachePrepare = true
	s.animationCacheSize = size
	frames := append([]image.Image(nil), active.Frames...)
	missing := make([]int, 0, len(frames))
	for index := range frames {
		if s.animationBitmaps[index] == nil {
			missing = append(missing, index)
		}
	}
	if len(missing) == 0 {
		s.animationCachePrepare = false
		s.animationCacheSize = 0
		s.releaseAnimationFramesIfCached()
		return
	}
	dpi := 96
	if s.surface != nil {
		dpi = s.surface.DPI()
	}

	go func() {
		for _, index := range missing {
			if s.closed.Load() ||
				s.animationCacheEpoch.Load() != token {
				break
			}
			source := frames[index]
			if source == nil {
				break
			}
			scaled, err := graphics.Resize(source, size, size)
			if err != nil {
				s.logf(
					"prepare animation frame %d at %d px: %v",
					index,
					size,
					err,
				)
				break
			}
			if s.closed.Load() ||
				s.animationCacheEpoch.Load() != token ||
				s.mw == nil {
				break
			}
			var storeErr error
			s.mw.Synchronize(func() {
				if s.closed.Load() ||
					s.animationCacheEpoch.Load() != token ||
					s.animation != active ||
					s.animationBitmapSize != size {
					return
				}
				_, storeErr = s.storeAnimationBitmap(
					index,
					size,
					dpi,
					scaled,
				)
			})
			if storeErr != nil {
				s.logf(
					"cache animation frame %d at %d px: %v",
					index,
					size,
					storeErr,
				)
				break
			}
		}
		if s.closed.Load() || s.mw == nil {
			return
		}
		s.mw.Synchronize(func() {
			if s.animationCacheEpoch.Load() != token {
				return
			}
			s.animationCachePrepare = false
			s.animationCacheSize = 0
			if s.animation == active &&
				s.animationBitmapSize == size {
				s.releaseAnimationFramesIfCached()
			}
		})
	}()
}

func (s *Shell) selectAnimationCacheSize(size int) {
	if s == nil || size < 1 || s.animationBitmapSize == size {
		return
	}
	s.disposeAnimationBitmaps()
	s.animationBitmapSize = size
}

func (s *Shell) disposeAnimationBitmaps() {
	if s == nil {
		return
	}
	s.animationCacheEpoch.Add(1)
	s.animationCachePrepare = false
	s.animationCacheSize = 0
	for index, bitmap := range s.animationBitmaps {
		if bitmap != nil {
			bitmap.Dispose()
		}
		delete(s.animationBitmaps, index)
	}
	s.animationBitmaps = nil
	s.animationBitmapSize = 0
}

func (s *Shell) disposeArtworkBitmaps() {
	if s == nil {
		return
	}
	for key, entry := range s.artworkBitmaps {
		if entry.bitmap != nil {
			entry.bitmap.Dispose()
		}
		delete(s.artworkBitmaps, key)
	}
	s.artworkBitmaps = nil
}
