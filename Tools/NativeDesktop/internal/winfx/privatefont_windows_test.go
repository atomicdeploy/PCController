//go:build windows

package winfx

import (
	"runtime"
	"testing"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/assets"
)

// Family() on a Walk font reports the requested name even when GDI silently
// substitutes another face. Read the face that Windows actually selected.
func TestEmbeddedPersianFontIsSelectedByGDI(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := EnsurePersianFont(); err != nil {
		t.Fatal(err)
	}
	for _, bold := range []bool{false, true} {
		family := PersianFontFamily(bold)
		weight := int32(win.FW_NORMAL)
		if bold {
			weight = win.FW_BOLD
		}
		var spec win.LOGFONT
		spec.LfHeight, spec.LfWeight = -24, weight
		spec.LfCharSet = win.DEFAULT_CHARSET
		copy(spec.LfFaceName[:], windows.StringToUTF16(family))
		font := win.CreateFontIndirect(&spec)
		if font == 0 {
			t.Fatal("CreateFontIndirect failed")
		}
		face, err := FontFaceName(uintptr(font))
		win.DeleteObject(win.HGDIOBJ(font))
		if err != nil || face != family || family == "Segoe UI" {
			t.Fatalf("GDI selected %q for %q: %v", face, family, err)
		}
		t.Logf("GDI selected %q, weight %d", face, weight)
	}
}

func TestPrivateFontResourceOwnsBackingBytesAndClosesIdempotently(t *testing.T) {
	data, err := assets.Files.ReadFile("fonts/Vazirmatn-Regular.ttf")
	if err != nil {
		t.Fatalf("read embedded Vazirmatn: %v", err)
	}
	resource, err := AddPrivateFont(data)
	if err != nil {
		t.Fatalf("register embedded Vazirmatn: %v", err)
	}
	if resource.handle == 0 || len(resource.data) != len(data) {
		t.Fatalf(
			"private font ownership = handle %#x, bytes %d; want nonzero/%d",
			resource.handle,
			len(resource.data),
			len(data),
		)
	}
	data[0] ^= 0xff
	if resource.data[0] == data[0] {
		t.Fatal("private font retained the caller's mutable byte slice")
	}
	if err := resource.Close(); err != nil {
		t.Fatalf("close private font: %v", err)
	}
	if err := resource.Close(); err != nil {
		t.Fatalf("second close private font: %v", err)
	}
	if resource.handle != 0 || resource.data != nil {
		t.Fatal("closed private font retained native or backing resources")
	}
}
