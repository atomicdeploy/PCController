//go:build windows

package dialogs

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/lxn/walk"
	"github.com/lxn/win"
	"pccontroller.local/desktop/internal/config"
)

func TestSettingsPageViewportLayout(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_WINDOW_TESTS") != "1" {
		t.Skip("native window integration is opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	deactivate := activateSettingsTestManifest(t)
	defer deactivate()
	owner, err := walk.NewMainWindow()
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Dispose()
	cfg := config.Default()
	cfg.UI.Locale, cfg.UI.Theme, cfg.UI.Animations = "en", "dark", false
	session, err := newSettingsDialogSession(owner, cfg, SettingsActions{ControllerMode: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.dialog.Dispose()
	session.shown = true
	session.dialog.Show()
	pumpSettingsTestMessages(owner, 300*time.Millisecond)
	content := session.controls.tabs.Parent().(*walk.Composite)
	viewport := content.Parent().(*walk.ScrollView)
	for _, locale := range []string{"en", "fa", "en"} {
		cfg.UI.Locale = locale
		applySettingsLocalization(session, cfg)
		for _, width := range []int{config.SettingsWindowWidth, config.SettingsWindowMinWidth, 960} {
			bounds := session.dialog.BoundsPixels()
			bounds.Width = session.dialog.IntFrom96DPI(width)
			if err := session.dialog.SetBoundsPixels(bounds); err != nil {
				t.Fatal(err)
			}
			pumpSettingsTestMessages(owner, 150*time.Millisecond)
			for index := 0; index < session.controls.tabs.Pages().Len(); index++ {
				context := fmt.Sprintf("%s/%d/tab%d", locale, width, index)
				_ = session.controls.tabs.SetCurrentIndex(index)
				pumpSettingsTestMessages(owner, 100*time.Millisecond)
				client := viewport.ClientBoundsPixels()
				if win.GetWindowLong(viewport.Handle(), win.GWL_STYLE)&win.WS_HSCROLL == 0 && content.SizePixels().Width > client.Width {
					t.Fatalf("%s: content extends underneath vertical scrollbar: content=%+v viewport=%+v", context, content.BoundsPixels(), client)
				}
				page := session.controls.tabs.Pages().At(index)
				if page.SizePixels().Width < 100 {
					t.Fatalf("%s: tab content collapsed", context)
				}
				if index == 0 {
					label := page.Children().At(0).BoundsPixels()
					field := session.controls.viewMode.BoundsPixels()
					// Compare actual HWND heights, not Walk's requested rectangle:
					// USER32 clamps a dropdown to its native selection height.
					delta := (label.Y*2 + label.Height) - (field.Y*2 + field.Height)
					if delta < -1 || delta > 1 {
						t.Fatalf("%s: label/input centers drift: label=%+v input=%+v", context, label, field)
					}
					if locale == "en" && width >= config.SettingsWindowWidth && field.X > page.SizePixels().Width*45/100 {
						t.Fatalf("%s: label column consumes excessive width: input=%+v page=%+v", context, field, page.SizePixels())
					}
				}
			}
		}
	}
}
