//go:build windows

package traymenu

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"

	"rayanlamp.local/app/assets"
)

// TestVisualStyledPopup is an opt-in compositor smoke fixture. It presents
// only app-owned pixels and exists so an external window-only capture tool can
// inspect the live DWM popup; it is skipped during ordinary automated tests.
func TestVisualStyledPopup(t *testing.T) {
	if os.Getenv("RAYANLAMP_VISUAL_POPUP") != "1" {
		t.Skip("set RAYANLAMP_VISUAL_POPUP=1 for the compositor smoke fixture")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := windows.UTF16FromString("STATIC")
	title, _ := windows.UTF16FromString("RayanLamp visual smoke owner")
	owner := win.CreateWindowEx(
		win.WS_EX_TOOLWINDOW,
		&className[0],
		&title[0],
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
	runtime.KeepAlive(className)
	runtime.KeepAlive(title)
	if owner == 0 {
		t.Fatal("create visual-smoke owner")
	}
	defer win.DestroyWindow(owner)

	icon := func(path string) []byte {
		data, err := assets.Files.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return data
	}
	items := []Item{
		{ID: "show", Label: "Lamp is off", Shortcut: "Open", IconPNG: icon("icons/lamp-off.png")},
		{Separator: true},
		{ID: "toggle", Label: "Toggle lamp", Shortcut: "Ctrl+Alt+L", IconPNG: icon("icons/app.png")},
		{ID: "on", Label: "Turn on", IconPNG: icon("icons/lamp-on.png")},
		{ID: "off", Label: "Turn off", IconPNG: icon("icons/lamp-off.png")},
		{Separator: true},
		{ID: "settings", Label: "Settings…", IconPNG: icon("icons/menu-settings.png")},
		{ID: "diagnostics", Label: "Diagnostics…", IconPNG: icon("icons/menu-diagnostics.png")},
		{ID: "reconnect", Label: "Reconnect", Shortcut: "F5", IconPNG: icon("icons/menu-reconnect.png")},
		{Separator: true},
		{ID: "pin", Label: "Always on top", Checked: true, IconPNG: icon("icons/menu-pin.png")},
		{ID: "quit", Label: "Exit Rayan Lamp", IconPNG: icon("icons/menu-exit.png")},
	}

	stopCloser := make(chan struct{})
	defer close(stopCloser)
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopCloser:
				return
			case <-ticker.C:
				popupWindowMu.Lock()
				window := popupActiveWindow[popupWindowKey{
					owner:    owner,
					threadID: win.GetWindowThreadProcessId(owner, nil),
				}]
				var popup win.HWND
				if window != nil {
					popup = window.hwnd
				}
				popupWindowMu.Unlock()
				if popup == 0 {
					continue
				}
				var bounds win.RECT
				if !win.IsWindowVisible(popup) ||
					!win.GetWindowRect(popup, &bounds) ||
					bounds.Right-bounds.Left < 100 ||
					bounds.Bottom-bounds.Top < 100 {
					continue
				}
				if path := os.Getenv("RAYANLAMP_VISUAL_POPUP_RECT"); path != "" {
					_ = os.WriteFile(
						path,
						[]byte(fmt.Sprintf(
							"%d,%d,%d,%d",
							bounds.Left,
							bounds.Top,
							bounds.Right-bounds.Left,
							bounds.Bottom-bounds.Top,
						)),
						0o600,
					)
				}
				time.Sleep(6 * time.Second)
				win.PostMessage(popup, win.WM_KEYDOWN, win.VK_ESCAPE, 0)
				return
			}
		}
	}()

	cursor := win.POINT{
		X: int32(win.GetSystemMetrics(win.SM_CXSCREEN) / 2),
		Y: int32(win.GetSystemMetrics(win.SM_CYSCREEN) / 3),
	}
	result, err := ShowAtAnchor(
		Options{
			Owner:    uintptr(owner),
			Items:    items,
			Theme:    ThemeDark,
			Backdrop: BackdropAcrylic,
		},
		Rect{
			Left: cursor.X, Top: cursor.Y,
			Right: cursor.X, Bottom: cursor.Y,
		},
	)
	if err != nil {
		t.Fatalf("show visual popup: %v", err)
	}
	if !result.Cancelled || result.UsedNativeFallback {
		t.Fatalf("visual popup result = %+v", result)
	}
}
