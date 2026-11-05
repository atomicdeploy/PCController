package assets

import (
	"fmt"
	"testing"

	"rayanlamp.local/app/internal/graphics"
)

func TestEmbeddedIconPacksContainExactDPISizes(t *testing.T) {
	expectedSizes := []int{
		16, 20, 24, 28, 32, 36, 40, 48,
		56, 64, 72, 80, 96, 112, 128, 256,
	}
	paths := []string{
		"icons/lamp-on.ico",
		"icons/lamp-off.ico",
		"icons/lamp-unknown.ico",
		"icons/lamp-connecting.ico",
		"icons/lamp-error.ico",
		"icons/menu-settings.ico",
		"icons/menu-diagnostics.ico",
		"icons/menu-reconnect.ico",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := Files.ReadFile(path)
			if err != nil {
				t.Fatalf("read embedded icon: %v", err)
			}
			pack, err := graphics.DecodeIconPack(data)
			if err != nil {
				t.Fatalf("decode icon pack: %v", err)
			}
			frames := pack.Frames()
			if len(frames) != len(expectedSizes) {
				t.Fatalf("frame count = %d, want %d", len(frames), len(expectedSizes))
			}
			for index, size := range expectedSizes {
				frame := frames[index]
				if frame.Width != size || frame.Height != size {
					t.Fatalf(
						"frame %d = %dx%d, want %s",
						index,
						frame.Width,
						frame.Height,
						fmt.Sprintf("%dx%d", size, size),
					)
				}
				if frame.BitDepth != 32 {
					t.Fatalf("frame %d bit depth = %d, want 32", size, frame.BitDepth)
				}
				if len(frame.Data) == 0 {
					t.Fatalf("frame %d has no encoded payload", size)
				}
			}
		})
	}
}

func TestRuntimeBundleExcludesPEOnlyAndUnusedRasterDuplicates(t *testing.T) {
	for _, path := range []string{
		"RayanLamp.ico",
		"icons/lamp-connecting.png",
		"icons/lamp-error.png",
		"icons/menu-pin.ico",
		"icons/menu-startup.ico",
		"icons/menu-window-show.ico",
		"icons/menu-window-hide.ico",
		"icons/menu-exit.ico",
	} {
		if _, err := Files.ReadFile(path); err == nil {
			t.Errorf("%s is unnecessarily duplicated in the runtime bundle", path)
		}
	}
}
