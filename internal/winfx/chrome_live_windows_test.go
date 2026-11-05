//go:build windows

package winfx

import (
	"os"
	"testing"

	"github.com/lxn/win"

	"rayanlamp.local/app/internal/wmipc"
)

// TestLivePrimaryCaptionlessBorderPolicy is an opt-in visual integration
// probe for a separately running RayanLamp instance. It makes regressions in
// the compositor-owned outer border observable without making the ordinary
// unit suite depend on a desktop session.
func TestLivePrimaryCaptionlessBorderPolicy(t *testing.T) {
	if os.Getenv("RAYANLAMP_LIVE_WINDOW_TEST") != "1" {
		t.Skip("set RAYANLAMP_LIVE_WINDOW_TEST=1 with RayanLamp running")
	}
	hwnd := wmipc.FindPrimary()
	if hwnd == 0 {
		t.Fatal("running RayanLamp primary window was not found")
	}
	style, err := getWindowStyle(hwnd)
	if err != nil {
		t.Fatal(err)
	}
	if style&wsCaption != 0 {
		t.Fatal("this probe requires a captionless primary window")
	}
	var outer, client win.RECT
	if !win.GetWindowRect(win.HWND(hwnd), &outer) || !win.GetClientRect(win.HWND(hwnd), &client) {
		t.Fatal("read primary window geometry")
	}
	if outer.Right-outer.Left != client.Right || outer.Bottom-outer.Top != client.Bottom {
		t.Fatalf("captionless client does not fill HWND: outer=%+v client=%+v", outer, client)
	}
	// DWMWA_BORDER_COLOR readback is unsupported on some Windows 11 builds.
	// The UI integration test additionally detects the actual white frame in
	// ordinary screenshots; an attribute value alone is not visual proof.
}
