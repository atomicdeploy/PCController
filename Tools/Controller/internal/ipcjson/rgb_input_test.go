package ipcjson

import "testing"

func TestRGBInputRequiresIntegrationAuthority(t *testing.T) {
	for _, command := range []string{"rgb-input start openrgb --pixels 60", `"rgb-input" start tpm2net --pixels 60`, "rgb-input stop"} {
		if got := commandCapability(command); got != capabilityIntegrations {
			t.Fatalf("%q: %s", command, got)
		}
	}
	if got := commandCapability("rgb-input status"); got != capabilityRead {
		t.Fatal(got)
	}
}
