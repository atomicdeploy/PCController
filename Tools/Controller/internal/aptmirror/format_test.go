package aptmirror

import "testing"

func TestConfigFormatAcceptsLivingAndLegacyIdentities(t *testing.T) {
	for _, value := range []string{ConfigFormat, legacyConfigFormat} {
		if !compatibleFormat(value, ConfigFormat, legacyConfigFormat) {
			t.Fatalf("expected compatible config format %q", value)
		}
	}
	if compatibleFormat("pccontroller-ubuntu-apt-mirrors/v2", ConfigFormat, legacyConfigFormat) {
		t.Fatal("unexpected parallel contract generation accepted")
	}
}
