//go:build !rayanlamp_production

package buildinfo

import "testing"

func TestDefaultEditionIsDevelop(t *testing.T) {
	if Edition != EditionDevelop {
		t.Fatalf("Edition = %q, want %q", Edition, EditionDevelop)
	}
	if !DeveloperTools {
		t.Fatal("development build disabled developer tools")
	}
}
