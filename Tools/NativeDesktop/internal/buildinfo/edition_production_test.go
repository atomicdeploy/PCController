//go:build rayanlamp_production

package buildinfo

import "testing"

func TestProductionEditionDisablesDeveloperTools(t *testing.T) {
	if Edition != EditionProduction {
		t.Fatalf("Edition = %q, want %q", Edition, EditionProduction)
	}
	if DeveloperTools {
		t.Fatal("production build retained developer tools")
	}
}
