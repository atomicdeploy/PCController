//go:build windows

package traymenu

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const tpmLayoutRTL = uint32(0x8000)

func showNative(options Options, anchor, workArea Rect) (Result, error) {
	menu := win.CreatePopupMenu()
	if menu == 0 {
		return Result{}, errors.New("traymenu: CreatePopupMenu failed")
	}
	defer win.DestroyMenu(menu)

	commandActions := make(map[uint32]ActionID, len(options.Items))
	command := uint32(1)
	for index, item := range options.Items {
		if item.Separator {
			if err := appendNativeMenu(menu, win.MF_SEPARATOR, 0, nil); err != nil {
				return Result{}, fmt.Errorf("traymenu: append separator %d: %w", index, err)
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
				// TPM_LAYOUTRTL mirrors the native popup, but shortcut names
				// are technical key sequences and must remain logically LTR.
				shortcut = "\u200e" + shortcut + "\u200e"
			}
			label += "\t" + shortcut
		}
		text, err := windows.UTF16FromString(label)
		if err != nil {
			return Result{}, fmt.Errorf("traymenu: encode native item %d: %w", index, err)
		}
		if err := appendNativeMenu(menu, flags, command, &text[0]); err != nil {
			return Result{}, fmt.Errorf("traymenu: append native item %d: %w", index, err)
		}
		runtime.KeepAlive(text)
		commandActions[command] = item.ID
		command++
	}

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
