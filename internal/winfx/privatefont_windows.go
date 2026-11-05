//go:build windows

package winfx

import (
	"bytes"
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	privateFontGDI32        = windows.NewLazySystemDLL("gdi32.dll")
	addFontMemResourceEx    = privateFontGDI32.NewProc("AddFontMemResourceEx")
	removeFontMemResourceEx = privateFontGDI32.NewProc("RemoveFontMemResourceEx")
)

// PrivateFontResource owns a process-private font registered from memory.
// The backing bytes are intentionally retained until Close: Windows may keep
// reading the supplied buffer while the resource remains registered.
type PrivateFontResource struct {
	handle uintptr
	data   []byte
	once   sync.Once
	err    error
}

// AddPrivateFont registers font data for this process without installing it
// on the system. The caller must retain the returned resource until all GDI
// and Walk fonts using the family have been disposed.
func AddPrivateFont(data []byte) (*PrivateFontResource, error) {
	if len(data) == 0 {
		return nil, errors.New("AddFontMemResourceEx: font data is empty")
	}
	owned := bytes.Clone(data)
	var fontCount uint32
	handle, _, callErr := addFontMemResourceEx.Call(
		uintptr(unsafe.Pointer(&owned[0])),
		uintptr(len(owned)),
		0,
		uintptr(unsafe.Pointer(&fontCount)),
	)
	if handle == 0 || fontCount == 0 {
		return nil, boolCallError("AddFontMemResourceEx", callErr)
	}
	return &PrivateFontResource{handle: handle, data: owned}, nil
}

// Close unregisters the private font. It is safe to call more than once.
func (resource *PrivateFontResource) Close() error {
	if resource == nil {
		return nil
	}
	resource.once.Do(func() {
		ok, _, callErr := removeFontMemResourceEx.Call(resource.handle)
		if ok == 0 {
			resource.err = boolCallError("RemoveFontMemResourceEx", callErr)
		}
		resource.handle = 0
		resource.data = nil
	})
	return resource.err
}
