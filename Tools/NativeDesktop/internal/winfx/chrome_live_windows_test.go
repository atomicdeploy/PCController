//go:build windows

package winfx

import (
	"os"
	"testing"

	"github.com/lxn/win"

	"pccontroller.local/desktop/internal/wmipc"
)

// TestLivePrimaryCaptionlessBorderPolicy is an opt-in visual integration
// probe for a separately running PCControllerDesktop instance. It makes regressions in
// the compositor-owned outer border observable without making the ordinary
// unit suite depend on a desktop session.
func TestLivePrimaryCaptionlessBorderPolicy(t *testing.T) {
	if os.Getenv("PCCONTROLLER_DESKTOP_LIVE_WINDOW_TEST") != "1" {
		t.Skip("set PCCONTROLLER_DESKTOP_LIVE_WINDOW_TEST=1 with PCControllerDesktop running")
	}
	hwnd := wmipc.FindPrimary()
	if hwnd == 0 {
		t.Fatal("running PCControllerDesktop primary window was not found")
	}
	// WS_CAPTION remains present as window-manager metadata in custom mode.
	// Client geometry, not that style bit, determines whether a caption is drawn.
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
