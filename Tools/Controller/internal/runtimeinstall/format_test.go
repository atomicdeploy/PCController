package runtimeinstall

import "testing"

func TestCompatibleFormatAcceptsLivingAndLegacyIdentities(t *testing.T) {
	for _, value := range []string{HostManifestFormat, legacyHostManifestFormat} {
		if !compatibleFormat(value, HostManifestFormat, legacyHostManifestFormat) {
			t.Fatalf("expected compatible host manifest format %q", value)
		}
	}
	if compatibleFormat("pccontroller-host-package-manifest/v2", HostManifestFormat, legacyHostManifestFormat) {
		t.Fatal("unexpected parallel contract generation accepted")
	}
}
