//go:build windows

package traymenu

import (
	"errors"
	"runtime"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const tpmLayoutRTL = uint32(0x8000)

func showNative(options Options, anchor, workArea Rect) (Result, error) {
	native, err := buildNativeMenu(options, monitorDPI(monitorFor(anchor, options.Owner), options.Owner))
	if err != nil {
		return Result{}, err
	}
	defer native.close()
	menu, commandActions := native.handle, native.actions

	owner := win.HWND(options.Owner)
	var temporaryOwner win.HWND
	if owner == 0 {
		owner = win.GetForegroundWindow()
	}
	if owner == 0 {
		className, _ := windows.UTF16FromString("STATIC")
		windowName, _ := windows.UTF16FromString("RayanLamp tray menu")
		temporaryOwner = win.CreateWindowEx(
			win.WS_EX_TOOLWINDOW,
			&className[0],
			&windowName[0],
			win.WS_POPUP,
			0,
			0,
			1,
			1,
			0,
			0,
			win.GetModuleHandle(nil),
			nil,
		)
		owner = temporaryOwner
		runtime.KeepAlive(className)
		runtime.KeepAlive(windowName)
	}
	if temporaryOwner != 0 {
		defer win.DestroyWindow(temporaryOwner)
	}
	if owner == 0 {
		return Result{}, errors.New("traymenu: native menu has no owner window")
	}

	flags := uint32(win.TPM_RETURNCMD | win.TPM_NONOTIFY)
	if options.RightToLeft {
		flags |= tpmLayoutRTL
	}
	alignRight := popupAlignsRight(
		anchor,
		workArea,
		options.Alignment,
		options.RightToLeft,
	)
	x := anchor.Left
	if alignRight {
		x = anchor.Right
		flags |= win.TPM_RIGHTALIGN
	} else {
		flags |= win.TPM_LEFTALIGN
	}
	y := anchor.Bottom
	if anchor.Bottom > workArea.Top+(workArea.Bottom-workArea.Top)/2 {
		y = anchor.Top
		flags |= win.TPM_BOTTOMALIGN
	}

	win.SetForegroundWindow(owner)
	selected := win.TrackPopupMenu(menu, flags, x, y, 0, owner, nil)
	win.PostMessage(owner, 0, 0, 0) // Required for reliable click-away dismissal.
	action, ok := commandActions[selected]
	if selected == 0 || !ok {
		return Result{
			Cancelled:          true,
			UsedNativeFallback: true,
			BackdropUsed:       BackdropSolid,
		}, nil
	}
	return Result{
		Action:             action,
		UsedNativeFallback: true,
		BackdropUsed:       BackdropSolid,
	}, nil
}
