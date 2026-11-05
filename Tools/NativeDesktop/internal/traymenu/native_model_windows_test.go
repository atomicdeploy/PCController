//go:build windows

package traymenu

import (
	"github.com/lxn/win"
	"rayanlamp.local/app/assets"
	"testing"
	"unsafe"
)

func TestNativeMenuRetainsModelDefaultStateAndIcons(t *testing.T) {
	png, err := assets.Files.ReadFile("icons/menu-window-show.png")
	if err != nil {
		t.Fatal(err)
	}
	items := []Item{{ID: "status", Label: "Connected", Disabled: true}, {Separator: true}, {ID: "dashboard", Label: "Dashboard", Default: true, IconPNG: png}, {ID: "compact", Label: "Compact", Checked: true}, {ID: "exit", Label: "Exit"}}
	for _, dpi := range []int32{96, 144, 192} {
		menu, err := buildNativeMenu(Options{Items: items}, dpi)
		if err != nil {
			t.Fatal(err)
		}
		if len(menu.actions) != 4 || len(menu.bitmaps) != 1 || win.GetMenuItemCount(menu.handle) != int32(len(items)) {
			t.Fatal("native presentation dropped rows or artwork")
		}
		var bitmap win.BITMAP
		if win.GetObject(win.HGDIOBJ(menu.bitmaps[0]), unsafe.Sizeof(bitmap), unsafe.Pointer(&bitmap)) == 0 || bitmap.BmBitsPixel != 32 || bitmap.BmWidth != scale(16, dpi) {
			t.Fatal("native icon lost full-color/DPI quality")
		}
		var info win.MENUITEMINFO
		info.CbSize = uint32(unsafe.Sizeof(info))
		info.FMask = win.MIIM_STATE | win.MIIM_BITMAP
		if !win.GetMenuItemInfo(menu.handle, 2, 0, &info) || info.FState&0x1000 == 0 || info.HbmpItem == 0 {
			t.Fatalf("default/artwork lost: %+v", info)
		}
		if !win.GetMenuItemInfo(menu.handle, 3, 0, &info) || info.FState&win.MF_CHECKED == 0 {
			t.Fatal("checked state lost")
		}
		menu.close()
		if menu.handle != 0 || len(menu.bitmaps) != 0 {
			t.Fatal("native menu retained presentation resources")
		}
	}
}

func TestDefaultMenuActionIsPreservedByKeyboardNavigation(t *testing.T) {
	items := []Item{{ID: "first", Label: "Other"}, {ID: "dashboard", Label: "Dashboard", Default: true}}
	if got := newNavigator(items); got.selected != 1 {
		t.Fatal("default keyboard action lost")
	}
	if err := Validate(Options{Items: []Item{{Separator: true, Default: true}}}); err == nil {
		t.Fatal("default separator accepted")
	}
}
