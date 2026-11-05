package animation

import (
	"context"
	"image"
	"testing"
	"time"
)

func TestPlayerPlaysFramesInRequestedDirection(t *testing.T) {
	t.Parallel()

	anim := testAnimation(4, time.Millisecond)
	for _, test := range []struct {
		name    string
		forward bool
		want    []int
	}{
		{name: "forward", forward: true, want: []int{0, 1, 2, 3}},
		{name: "reverse", forward: false, want: []int{3, 2, 1, 0}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var player Player
			frames := make(chan int, len(test.want))
			player.PlayTransition(context.Background(), anim, test.forward, func(index int, _ image.Image) {
				frames <- index
			})
			defer player.Stop()

			for position, want := range test.want {
				select {
				case got := <-frames:
					if got != want {
						t.Fatalf("frame %d = %d, want %d", position, got, want)
					}
				case <-time.After(time.Second):
					t.Fatalf("timed out waiting for frame %d", position)
				}
			}
		})
	}
}

func TestPlayerReplacementSuppressesRemainingStaleFrames(t *testing.T) {
	t.Parallel()

	var player Player
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstFrames := make(chan int, 4)
	secondFrames := make(chan int, 2)

	player.PlayTransition(context.Background(), testAnimation(4, time.Millisecond), true, func(index int, _ image.Image) {
		firstFrames <- index
		if index == 0 {
			close(firstStarted)
			<-releaseFirst
		}
	})
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first playback")
	}

	player.PlayTransition(context.Background(), testAnimation(2, time.Millisecond), true, func(index int, _ image.Image) {
		secondFrames <- index
	})
	close(releaseFirst)
	defer player.Stop()

	for want := 0; want < 2; want++ {
		select {
		case got := <-secondFrames:
			if got != want {
				t.Fatalf("replacement frame = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for replacement frame %d", want)
		}
	}

	if got := <-firstFrames; got != 0 {
		t.Fatalf("in-flight frame = %d, want 0", got)
	}
	select {
	case got := <-firstFrames:
		t.Fatalf("stale playback emitted frame %d after replacement", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPlayerStopSuppressesRemainingFrames(t *testing.T) {
	t.Parallel()

	var player Player
	firstFrame := make(chan struct{})
	releaseFirst := make(chan struct{})
	frames := make(chan int, 3)
	player.PlayTransition(context.Background(), testAnimation(3, time.Millisecond), true, func(index int, _ image.Image) {
		frames <- index
		if index == 0 {
			close(firstFrame)
			<-releaseFirst
		}
	})

	select {
	case <-firstFrame:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first frame")
	}
	player.Stop()
	close(releaseFirst)

	if got := <-frames; got != 0 {
		t.Fatalf("in-flight frame = %d, want 0", got)
	}
	select {
	case got := <-frames:
		t.Fatalf("stopped playback emitted frame %d", got)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestPlayerRetainsFrameOrderAfterPixelEviction(t *testing.T) {
	t.Parallel()

	anim := testAnimation(4, time.Millisecond)
	anim.ReleaseFrames()
	if got := anim.FrameCount(); got != 4 {
		t.Fatalf("frame count after eviction = %d, want 4", got)
	}

	var player Player
	frames := make(chan int, 4)
	player.PlayTransition(
		context.Background(),
		anim,
		true,
		func(index int, frame image.Image) {
			if frame != nil {
				t.Errorf("evicted playback frame %d unexpectedly has pixels", index)
			}
			frames <- index
		},
	)
	defer player.Stop()

	for want := 0; want < 4; want++ {
		select {
		case got := <-frames:
			if got != want {
				t.Fatalf("frame = %d, want %d", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for frame %d", want)
		}
	}
}

func testAnimation(frameCount int, delay time.Duration) *Animation {
	frames := make([]image.Image, frameCount)
	delays := make([]time.Duration, frameCount)
	for index := range frames {
		frames[index] = image.NewRGBA(image.Rect(0, 0, 1, 1))
		delays[index] = delay
	}
	return &Animation{Frames: frames, Delays: delays}
}
