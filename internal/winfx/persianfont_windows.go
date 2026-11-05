//go:build windows

package winfx

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"rayanlamp.local/app/assets"
)

const (
	PersianFontRegular = "Yekan Bakh VF Regular"
	PersianFontBold    = "Yekan Bakh VF Bold"
)

var bundledPersianFont struct {
	once          sync.Once
	resource      *PrivateFontResource
	regular, bold string
	err           error
}

// EnsurePersianFont registers the single canonical VF once for the process.
// GDI exposes its named instances as separate faces; the bare typographic
// family silently maps to Arial on Windows 11. Registration success alone is
// therefore insufficient: probe the physical faces selected by the mapper.
// Keep the registration until process exit, since Walk caches HFONTs globally
// and a modeless/SDK dialog can outlive a particular Shell.
func EnsurePersianFont() error {
	bundledPersianFont.once.Do(func() {
		data, err := assets.Files.ReadFile("fonts/yekanbakh-vf.ttf")
		if err != nil {
			bundledPersianFont.err = err
			return
		}
		resource, err := AddPrivateFont(data)
		if err != nil {
			bundledPersianFont.err = err
			return
		}
		regular := availablePersianFace([]string{PersianFontRegular, "Yekan Bakh VF"}, win.FW_NORMAL)
		if regular == "" {
			_ = resource.Close()
			bundledPersianFont.err = fmt.Errorf("GDI could not select the embedded Yekan Bakh VF")
			return
		}
		bold := availablePersianFace([]string{PersianFontBold}, win.FW_BOLD)
		if bold == "" {
			bold = regular
		} // Older GDI can synthesize the bold default instance.
		bundledPersianFont.resource = resource
		bundledPersianFont.regular, bundledPersianFont.bold = regular, bold
	})
	return bundledPersianFont.err
}

// PersianFontFamily returns a verified GDI face, with a readable system fallback.
func PersianFontFamily(bold bool) string {
	if EnsurePersianFont() != nil {
		return "Segoe UI"
	}
	if bold {
		return bundledPersianFont.bold
	}
	return bundledPersianFont.regular
}

func availablePersianFace(candidates []string, weight int32) string {
	for _, family := range candidates {
		var spec win.LOGFONT
		spec.LfHeight, spec.LfWeight, spec.LfCharSet = -24, weight, win.DEFAULT_CHARSET
		copy(spec.LfFaceName[:], windows.StringToUTF16(family))
		font := win.CreateFontIndirect(&spec)
		if font == 0 {
			continue
		}
		selected, err := FontFaceName(uintptr(font))
		win.DeleteObject(win.HGDIOBJ(font))
		if err == nil && strings.EqualFold(selected, family) {
			return family
		}
	}
	return ""
}

// FontFaceName reads the actual face selected by GDI, rather than the logical
// family requested in LOGFONT. This detects silent system-font substitution.
func FontFaceName(font uintptr) (string, error) {
	if font == 0 {
		return "", fmt.Errorf("font handle is zero")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dc := win.CreateCompatibleDC(0)
	if dc == 0 {
		return "", fmt.Errorf("CreateCompatibleDC(font face) failed")
	}
	defer win.DeleteDC(dc)
	old := win.SelectObject(dc, win.HGDIOBJ(font))
	if old == 0 {
		return "", fmt.Errorf("SelectObject(font face) failed")
	}
	defer win.SelectObject(dc, old)
	var face [128]uint16
	count, _, err := privateFontGDI32.NewProc("GetTextFaceW").Call(uintptr(dc), uintptr(len(face)), uintptr(unsafe.Pointer(&face[0])))
	if count == 0 {
		return "", boolCallError("GetTextFaceW", err)
	}
	return windows.UTF16ToString(face[:]), nil
}
