//go:build windows

package pathguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestValidateComponentsRejectsIntermediateJunction(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	link := filepath.Join(base, "link")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("Windows junction creation unavailable: %v: %s", err, output)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if err := ValidateComponents(filepath.Join(link, "file.txt"), false); err == nil {
		t.Fatal("intermediate junction was accepted")
	}
}
