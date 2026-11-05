//go:build windows

package dialogs

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestVendoredWalkRoutesButtonControlColor(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(
		"..",
		"..",
		"vendor",
		"github.com",
		"lxn",
		"walk",
		"container.go",
	))
	if err != nil {
		t.Fatal(err)
	}

	routed := regexp.MustCompile(
		`case\s+win\.WM_CTLCOLOREDIT,\s*win\.WM_CTLCOLORSTATIC,\s*win\.WM_CTLCOLORBTN:`,
	)
	if !routed.Match(source) {
		t.Fatal("vendored Walk no longer routes WM_CTLCOLORBTN through the container background brush")
	}
}
