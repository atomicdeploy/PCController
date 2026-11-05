package ipcjson

import "testing"

func TestStripOrderCapabilities(t *testing.T) {
	for command, want := range map[string]string{
		"strip order":      capabilityRead,
		"strip order GRB":  capabilityHostConfig,
		"strip order next": capabilityHostConfig,
		"strip fill 1 2 3": capabilityBoard,
		"strip status":     capabilityRead,
	} {
		if got := commandCapability(command); got != want {
			t.Fatalf("%s: got %s want %s", command, got, want)
		}
	}
}
