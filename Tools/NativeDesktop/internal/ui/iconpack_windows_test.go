//go:build windows

package ui

import (
	"strconv"
	"testing"

	"github.com/lxn/walk"

	"rayanlamp.local/app/assets"
)

func TestPackedSettingsIconPaintsCommonCaptionSizes(t *testing.T) {
	data, err := assets.Files.ReadFile("icons/menu-settings.ico")
	if err != nil {
		t.Fatal(err)
	}
	icon, err := newPackedIconImage(data)
	if err != nil {
		t.Fatal(err)
	}
	defer icon.Dispose()

	for _, size := range []int{16, 20, 24, 28, 32, 40, 48, 64} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			bitmap, bitmapErr := walk.NewBitmapFromImageWithSize(
				icon,
				walk.Size{Width: size, Height: size},
			)
			if bitmapErr != nil {
				t.Fatalf("paint %dx%d icon: %v", size, size, bitmapErr)
			}
			bitmap.Dispose()
		})
	}
}
