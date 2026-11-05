//go:build windows

package winfx

import (
	"strings"
	"testing"
	"unsafe"
)

func TestMakeThumbnailButtonsValidatesAndBuildsNativeLayout(t *testing.T) {
	buttons, err := makeThumbnailButtons([]ThumbnailButton{
		{
			ID:       10,
			Icon:     0x100,
			Tooltip:  "Toggle device",
			Disabled: true,
		},
		{
			ID:      11,
			Icon:    0x200,
			Tooltip: "Reconnect",
			Hidden:  true,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(buttons) != 2 {
		t.Fatalf("native button count = %d, want 2", len(buttons))
	}
	wantMask := uint32(
		thumbnailButtonMaskIcon |
			thumbnailButtonMaskTooltip |
			thumbnailButtonMaskFlags,
	)
	if buttons[0].Mask != wantMask ||
		buttons[0].Flags != thumbnailButtonDisabled ||
		buttons[0].Icon != 0x100 {
		t.Fatalf("first native button = %#v", buttons[0])
	}
	if buttons[1].Flags != thumbnailButtonHidden {
		t.Fatalf("second flags = %#x, want hidden", buttons[1].Flags)
	}
	if got := utf16Text(buttons[0].Tooltip[:]); got != "Toggle device" {
		t.Fatalf("first tooltip = %q", got)
	}

	switch unsafe.Sizeof(thumbnailButton{}) {
	case 540, 552:
	default:
		t.Fatalf(
			"THUMBBUTTON-compatible size = %d, want 540 or 552",
			unsafe.Sizeof(thumbnailButton{}),
		)
	}
}

func TestMakeThumbnailButtonsRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		buttons []ThumbnailButton
	}{
		{name: "empty"},
		{
			name: "too many",
			buttons: []ThumbnailButton{
				{ID: 1, Icon: 1}, {ID: 2, Icon: 1},
				{ID: 3, Icon: 1}, {ID: 4, Icon: 1},
				{ID: 5, Icon: 1}, {ID: 6, Icon: 1},
				{ID: 7, Icon: 1}, {ID: 8, Icon: 1},
			},
		},
		{name: "zero id", buttons: []ThumbnailButton{{Icon: 1}}},
		{
			name: "duplicate id",
			buttons: []ThumbnailButton{
				{ID: 1, Icon: 1},
				{ID: 1, Icon: 2},
			},
		},
		{name: "missing icon", buttons: []ThumbnailButton{{ID: 1}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := makeThumbnailButtons(test.buttons); err == nil {
				t.Fatal("invalid thumbnail buttons unexpectedly succeeded")
			}
		})
	}
}

func TestTaskbarTooltipTruncationPreservesTerminator(t *testing.T) {
	text := strings.Repeat("x", 400)
	buttons, err := makeThumbnailButtons([]ThumbnailButton{{
		ID:      1,
		Icon:    1,
		Tooltip: text,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if buttons[0].Tooltip[259] != 0 {
		t.Fatal("truncated tooltip is not terminated")
	}
	if got := len(utf16Text(buttons[0].Tooltip[:])); got != 259 {
		t.Fatalf("truncated tooltip length = %d, want 259", got)
	}
}

func utf16Text(value []uint16) string {
	length := 0
	for length < len(value) && value[length] != 0 {
		length++
	}
	runes := make([]rune, length)
	for index := 0; index < length; index++ {
		runes[index] = rune(value[index])
	}
	return string(runes)
}
