//go:build windows

package hostui

import (
	"context"
	"github.com/lxn/win"
	"image/png"
	"os"
	"path/filepath"
	"pccontroller.local/desktop/osd"
	"testing"
	"time"
)

// Explicitly opt-in; default tests never create a desktop window or actuate.
func TestOSDLivePreviewNoFocusAndExpiry(t *testing.T) {
	if os.Getenv("PCCONTROLLER_OSD_LIVE") != "1" {
		t.Skip("explicit desktop presentation verification only")
	}
	request := OSDRequest{ID: "osd-verification", Title: "Desktop OSD preview", Text: "Long captions scroll smoothly while the action buttons remain in place — without taking focus from your application.", Icon: "app", DurationMS: 900, Actions: []OSDAction{{Label: "Open events", URI: "pccontroller://page/events"}, {Label: "Preferences", URI: "pccontroller://page/settings"}}}
	icon, err := loadOSDIcon(request.Icon)
	if err != nil {
		t.Fatal(err)
	}
	if output := os.Getenv("PCCONTROLLER_OSD_CAPTURE"); output != "" {
		if !filepath.IsAbs(output) {
			t.Fatal("capture directory must be absolute")
		}
		if err = os.MkdirAll(output, 0700); err != nil {
			t.Fatal(err)
		}
		for _, dark := range []bool{true, false} {
			im, e := osd.RenderPreview(request, 144, dark, icon)
			if e != nil {
				t.Fatal(e)
			}
			name := "osd-dark.png"
			if !dark {
				name = "osd-light.png"
			}
			f, e := os.Create(filepath.Join(output, name))
			if e != nil {
				t.Fatal(e)
			}
			e = png.Encode(f, im)
			closeErr := f.Close()
			if e != nil || closeErr != nil {
				t.Fatal(e, closeErr)
			}
		}
	}
	activated := false
	presenter := osd.New(osd.Options{Icon: loadOSDIcon, Activate: func(string) error { activated = true; return nil }})
	defer presenter.Close()
	before := win.GetForegroundWindow()
	status, err := presenter.Show(context.Background(), request)
	if err != nil || !status.Active {
		t.Fatal(status, err)
	}
	if win.GetForegroundWindow() != before {
		t.Error("OSD stole focus")
	}
	if _, err = presenter.Dismiss(context.Background(), "obsolete-id"); err == nil || !presenter.Status().Active {
		t.Fatal("stale dismiss closed a newer OSD")
	}
	deadline := time.Now().Add(2 * time.Second)
	for presenter.Status().Active && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if presenter.Status().Active || activated {
		t.Fatal("OSD did not expire or executed an action without a click", presenter.Status())
	}
}
