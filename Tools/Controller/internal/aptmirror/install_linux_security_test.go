//go:build linux

package aptmirror

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedControllerExecutableSurvivesPathSwapAndRejectsReplacement(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "controller")
	replacement := filepath.Join(root, "replacement")
	pinnedName := filepath.Join(root, "pinned-original")
	original := []byte("reviewed-controller-bytes")
	malicious := []byte("replacement-after-validation")
	if err := os.WriteFile(executable, original, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, malicious, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(replacement, 0o775); err != nil {
		t.Fatal(err)
	}

	content, err := readPinnedExecutableWithPolicy(executable, true, uint32(os.Geteuid()), func() {
		if err := os.Rename(executable, pinnedName); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, executable); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("installer copied swapped path bytes %q instead of pinned bytes %q", content, original)
	}
	if current, err := os.ReadFile(executable); err != nil || string(current) != string(malicious) {
		t.Fatalf("fixture did not swap the visible path: content=%q err=%v", current, err)
	}
	if _, err := readPinnedExecutableWithPolicy(executable, true, uint32(os.Geteuid()), nil); err == nil ||
		!strings.Contains(err.Error(), "not group/world writable") {
		t.Fatalf("group-writable replacement was trusted: %v", err)
	}
}

func TestPinnedControllerExecutableRejectsSymlinkEvenForDryRun(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "controller-real")
	link := filepath.Join(root, "controller-link")
	if err := os.WriteFile(target, []byte("controller"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPinnedExecutable(link, false); err == nil || !strings.Contains(err.Error(), "no-follow") {
		t.Fatalf("symlink executable was accepted: %v", err)
	}
}
