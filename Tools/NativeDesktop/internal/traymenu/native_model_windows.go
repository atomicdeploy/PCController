//go:build windows

package traymenu

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

type nativeMenu struct {
	handle  win.HMENU
	actions map[uint32]ActionID
	bitmaps []win.HBITMAP
}

func (m *nativeMenu) close() {
	if m.handle != 0 {
		win.DestroyMenu(m.handle)
		m.handle = 0
	}
	for _, bitmap := range m.bitmaps {
		win.DeleteObject(win.HGDIOBJ(bitmap))
	}
	m.bitmaps = nil
}

// Build the native presentation from exactly the same command model as the
// custom compositor. It owns only HMENU/HBITMAP presentation resources.
func buildNativeMenu(options Options, dpi int32) (_ *nativeMenu, err error) {
	if err := validateOptions(options); err != nil {
		return nil, err
	}
	m := &nativeMenu{handle: win.CreatePopupMenu(), actions: map[uint32]ActionID{}}
	if m.handle == 0 {
		return nil, errors.New("traymenu: CreatePopupMenu failed")
	}
	defer func() {
		if err != nil {
			m.close()
		}
	}()
	icons, err := prepareIcons(options.Items, scale(16, dpi))
	if err != nil {
		return nil, err
	}
	command := uint32(1)
	for index, item := range options.Items {
		if item.Separator {
			if err = appendNativeMenu(m.handle, win.MF_SEPARATOR, 0, nil); err != nil {
				return nil, err
			}
			continue
		}
		flags := uint32(win.MF_STRING)
		if item.Disabled {
			flags |= win.MF_DISABLED | win.MF_GRAYED
		}
		if item.Checked {
			flags |= win.MF_CHECKED
		}
		label := item.Label
		if shortcut := strings.TrimSpace(item.Shortcut); shortcut != "" {
			if options.RightToLeft {
				shortcut = "\u200e" + shortcut + "\u200e"
			}
			label += "\t" + shortcut
		}
		text, encodeErr := windows.UTF16FromString(label)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if err = appendNativeMenu(m.handle, flags, command, &text[0]); err != nil {
			return nil, err
		}
		runtime.KeepAlive(text)
		if item.Default && !item.Disabled && !win.SetMenuDefaultItem(m.handle, command, false) {
			return nil, errors.New("traymenu: set native default item")
		}
		bitmap, bitmapErr := nativeIconBitmap(icons[index])
		if bitmapErr != nil {
			return nil, fmt.Errorf("traymenu: native item %d icon: %w", index, bitmapErr)
		}
		if bitmap != 0 {
			m.bitmaps = append(m.bitmaps, bitmap)
			info := win.MENUITEMINFO{CbSize: uint32(unsafe.Sizeof(win.MENUITEMINFO{})), FMask: win.MIIM_BITMAP, HbmpItem: bitmap}
			if !win.SetMenuItemInfo(m.handle, command, false, &info) {
				return nil, errors.New("traymenu: set native icon bitmap")
			}
		}
		m.actions[command] = item.ID
		command++
	}
	return m, nil
}

func nativeIconBitmap(icon preparedIcon) (win.HBITMAP, error) {
	if icon.image == nil {
		if icon.handle == 0 {
			return 0, nil
		}
		var info win.ICONINFO
		if !win.GetIconInfo(icon.handle, &info) {
			return 0, errors.New("GetIconInfo failed")
		}
		if info.HbmMask != 0 {
			win.DeleteObject(win.HGDIOBJ(info.HbmMask))
		}
		return info.HbmColor, nil // GetIconInfo returns caller-owned bitmap copies.
	}
	bounds := icon.image.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	header := win.BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(win.BITMAPINFOHEADER{})), BiWidth: int32(width), BiHeight: -int32(height), BiPlanes: 1, BiBitCount: 32, BiCompression: win.BI_RGB}
	var bits unsafe.Pointer
	bitmap := win.CreateDIBSection(0, &header, win.DIB_RGB_COLORS, &bits, 0, 0)
	if bitmap == 0 || bits == nil {
		if bitmap != 0 {
			win.DeleteObject(win.HGDIOBJ(bitmap))
		}
		return 0, errors.New("create 32-bit menu bitmap")
	}
	pixels := unsafe.Slice((*byte)(bits), width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			r, g, b, a := icon.image.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := (y*width + x) * 4
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	return bitmap, nil
}
