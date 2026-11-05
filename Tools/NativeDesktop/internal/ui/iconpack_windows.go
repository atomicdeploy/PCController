//go:build windows

package ui

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/graphics"
)

const (
	iconResourceVersion   = 0x00030000
	iconLogicalSize       = 16
	productIconResourceID = win.IDI_APPLICATION
)

var uiCreateIconFromResourceEx = syscall.NewLazyDLL("user32.dll").
	NewProc("CreateIconFromResourceEx")

// newProductIconImage exposes the executable's verified IDI_APPLICATION icon
// group as a DPI-aware Walk image. The PE resource is the canonical runtime
// product icon; keeping a second raw ICO in assets.Files would make the same
// multi-resolution payload part of the executable twice.
func newProductIconImage() (walk.Image, error) {
	icon, err := walk.NewIconFromResourceId(productIconResourceID)
	if err != nil {
		return nil, fmt.Errorf(
			"ui: load product icon resource %d: %w",
			productIconResourceID,
			err,
		)
	}
	return icon, nil
}

// newProductIconHandle loads one caller-owned product HICON at the exact
// native-pixel dimensions requested by the taskbar thumbnail toolbar. The
// caller must call DestroyIcon. LR_SHARED must not be added here because these
// handles are replaced on DPI changes and have explicit lifetime ownership.
func newProductIconHandle(width, height int) (win.HICON, error) {
	return loadOwnedProductIcon(
		width,
		height,
		func() win.HINSTANCE { return win.GetModuleHandle(nil) },
		win.LoadImage,
	)
}

func loadOwnedProductIcon(
	width, height int,
	moduleHandle func() win.HINSTANCE,
	loadImage func(
		win.HINSTANCE,
		*uint16,
		uint32,
		int32,
		int32,
		uint32,
	) win.HANDLE,
) (win.HICON, error) {
	if width < 1 || height < 1 {
		return 0, fmt.Errorf(
			"ui: invalid product icon size %dx%d",
			width,
			height,
		)
	}
	if moduleHandle == nil || loadImage == nil {
		return 0, fmt.Errorf("ui: product icon loader is unavailable")
	}
	module := moduleHandle()
	if module == 0 {
		return 0, fmt.Errorf("ui: product icon module is unavailable")
	}
	handle := win.HICON(loadImage(
		module,
		win.MAKEINTRESOURCE(productIconResourceID),
		win.IMAGE_ICON,
		int32(width),
		int32(height),
		win.LR_DEFAULTCOLOR,
	))
	if handle == 0 {
		return 0, fmt.Errorf(
			"ui: load product icon resource %d at %dx%d",
			productIconResourceID,
			width,
			height,
		)
	}
	return handle, nil
}

// newPackedIconImage exposes an in-memory multi-resolution ICO as a DPI-aware
// Walk image. Walk asks the paint callback for the native small/big icon size;
// the callback then gives Windows the exact matching ICO frame rather than
// allowing a single large PNG to be scaled with the default GDI algorithm.
func newPackedIconImage(data []byte) (walk.Image, error) {
	pack, err := graphics.DecodeIconPack(data)
	if err != nil {
		return nil, err
	}

	return walk.NewPaintFuncImagePixels(
		walk.Size{Width: iconLogicalSize, Height: iconLogicalSize},
		func(canvas *walk.Canvas, bounds walk.Rectangle) error {
			if canvas == nil || bounds.Width < 1 || bounds.Height < 1 {
				return fmt.Errorf("ui: invalid packed icon paint bounds")
			}
			icon, err := createPackedIconHandle(
				pack,
				bounds.Width,
				bounds.Height,
			)
			if err != nil {
				return err
			}
			defer win.DestroyIcon(icon)

			if !win.DrawIconEx(
				canvas.HDC(),
				int32(bounds.X),
				int32(bounds.Y),
				icon,
				int32(bounds.Width),
				int32(bounds.Height),
				0,
				0,
				win.DI_NORMAL,
			) {
				return fmt.Errorf("ui: DrawIconEx failed")
			}
			return nil
		},
	), nil
}

// newPackedIconHandle materializes one caller-owned, DPI-appropriate HICON
// from an embedded multi-resolution ICO. The caller must call DestroyIcon.
func newPackedIconHandle(data []byte, width, height int) (win.HICON, error) {
	pack, err := graphics.DecodeIconPack(data)
	if err != nil {
		return 0, err
	}
	return createPackedIconHandle(pack, width, height)
}

func createPackedIconHandle(
	pack *graphics.IconPack,
	width int,
	height int,
) (win.HICON, error) {
	if pack == nil || width < 1 || height < 1 {
		return 0, fmt.Errorf("ui: invalid packed icon size %dx%d", width, height)
	}
	frame, ok := pack.Select(width, height)
	if !ok || len(frame.Data) == 0 {
		return 0, fmt.Errorf("ui: no ICO frame for %dx%d", width, height)
	}
	handle, _, callErr := uiCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&frame.Data[0])),
		uintptr(len(frame.Data)),
		1,
		iconResourceVersion,
		uintptr(width),
		uintptr(height),
		win.LR_DEFAULTCOLOR,
	)
	runtime.KeepAlive(frame.Data)
	if handle == 0 {
		return 0, fmt.Errorf("ui: CreateIconFromResourceEx: %w", callErr)
	}
	return win.HICON(handle), nil
}
