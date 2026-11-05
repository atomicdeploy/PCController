package animation

import (
	"slices"
	"testing"
	"time"

	"rayanlamp.local/app/assets"
)

func TestDecodeEmbeddedTransition(t *testing.T) {
	file, err := assets.Files.Open("lamp-transition.apng")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	anim, err := DecodeReader(file)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(anim.Frames); got < 24 {
		// ImageMagick may add disposal/compositing frames while optimizing the
		// APNG, but it must preserve at least the authored 24-frame sequence.
		t.Fatalf("frame count = %d, want at least 24", got)
	}
	if len(anim.Delays) != len(anim.Frames) {
		t.Fatalf("delays = %d, frames = %d", len(anim.Delays), len(anim.Frames))
	}
	for i, frame := range anim.Frames {
		if frame.Bounds().Dx() != 512 || frame.Bounds().Dy() != 512 {
			t.Fatalf("frame %d bounds = %v", i, frame.Bounds())
		}
	}
}

func TestReleaseFramesRetainsPlaybackManifest(t *testing.T) {
	data, err := assets.Files.ReadFile("lamp-transition.apng")
	if err != nil {
		t.Fatal(err)
	}
	anim, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	frameCount := anim.FrameCount()
	delays := append([]time.Duration(nil), anim.Delays...)
	loopCount := anim.LoopCount
	if frameCount == 0 || !anim.HasFrames() {
		t.Fatal("decoded animation did not expose resident frames")
	}

	anim.ReleaseFrames()
	if anim.HasFrames() {
		t.Fatal("decoded frame pixels remained resident after release")
	}
	if len(anim.Frames) != 0 {
		t.Fatalf("resident frames = %d, want 0", len(anim.Frames))
	}
	if got := anim.FrameCount(); got != frameCount {
		t.Fatalf("frame count after release = %d, want %d", got, frameCount)
	}
	if !slices.Equal(anim.Delays, delays) {
		t.Fatal("frame delays changed when decoded pixels were released")
	}
	if anim.LoopCount != loopCount {
		t.Fatalf(
			"loop count after release = %d, want %d",
			anim.LoopCount,
			loopCount,
		)
	}
	for index := 0; index < frameCount; index++ {
		if frame := anim.Frame(index); frame != nil {
			t.Fatalf("frame %d remained resident after release", index)
		}
	}
}
