package hostui

import (
	"path/filepath"
	"testing"
)

func TestExplicitDesktopExecutableIsResolvedWithoutUsingCurrentProcess(t *testing.T) {
	want := filepath.Join(t.TempDir(), "installed", "controller.exe")
	got, err := resolveDesktopExecutable(want)
	if err != nil {
		t.Fatal(err)
	}
	if !sameWindowsPathForTest(got, want) {
		t.Fatalf("resolved executable=%q want=%q", got, want)
	}
}

func sameWindowsPathForTest(left, right string) bool {
	left, leftErr := filepath.Abs(left)
	right, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(left) == filepath.Clean(right)
}
