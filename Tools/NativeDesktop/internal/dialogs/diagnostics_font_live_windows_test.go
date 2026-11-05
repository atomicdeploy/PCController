//go:build windows

package dialogs

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"

	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/model"
)

func TestDiagnosticsPersianFontLifecycle(t *testing.T) {
	if os.Getenv("RAYANLAMP_WINDOW_TESTS") != "1" {
		t.Skip("native window integration is opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cfg := config.Default()
	cfg.UI.Locale, cfg.UI.Theme = "fa", "dark"
	cfg.UI.Animations = false
	visited := false
	err := ShowDiagnostics(nil, cfg, model.Snapshot{}, nil, func(dialog *walk.Dialog, apply func(config.Config)) {
		if dialog == nil {
			return
		}
		dialog.SetCenterInOwnerWhenRun(false)
		_ = dialog.SetBoundsPixels(walk.Rectangle{X: 120, Y: 90, Width: 1050, Height: 900})
		win.SetWindowPos(dialog.Handle(), win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
		dialog.Starting().Attach(func() {
			time.AfterFunc(400*time.Millisecond, func() {
				dialog.Synchronize(func() {
					defer dialog.Cancel()
					assertDialogPhysicalFonts(t, dialog, "fa", 9)
					captureSettingsTestWindow(t, dialog, "persian-diagnostics")
					cfg.UI.Locale = "en"
					apply(cfg)
					assertDialogPhysicalFonts(t, dialog, "en", 9)
					cfg.UI.Locale = "fa"
					apply(cfg)
					assertDialogPhysicalFonts(t, dialog, "fa", 9)
					visited = true
				})
			})
		})
	}, "Font rendering test")
	if err != nil {
		t.Fatal(err)
	}
	if !visited {
		t.Fatal("diagnostics font lifecycle was not exercised")
	}
}
