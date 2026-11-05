//go:build windows

package dialogs

import (
	"os"
	"strings"
	"testing"
)

func TestSettingsHighDPISurfaceKeepsPreparedBoundsWithoutCompositedStyle(
	t *testing.T,
) {
	source, err := os.ReadFile("settings_windows.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, required := range []string{
		"decl.ScrollView{",
		"SetCenterInOwnerWhenRun(false)",
		"PrepareOwnedDialogPlacement(",
		"CaptureDialogPlacement(",
		"PersistWindowBounds",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Settings first-show/high-DPI contract lost %q", required)
		}
	}
	if strings.Contains(text, "DoubleBuffering: true") {
		t.Fatal("Settings reintroduced blanket WS_EX_COMPOSITED double buffering")
	}
}
