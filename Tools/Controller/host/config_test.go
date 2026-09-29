package host

import (
	"pccontroller.local/controller/internal/appconfig"
	"testing"
)

func TestControllerConfigRetainsToolchainConfiguration(t *testing.T) {
	config := appconfig.Defaults()
	config.Programming.ToolchainCLI = "cli"
	config.Programming.ToolchainConfig = "selected-data.yaml"
	config.Programming.Avrdude = "avr"
	config.Programming.AvrdudeConf = "avr.conf"
	got, err := controllerOptionsFromConfig(config, "config.json")
	if err != nil || got.ToolchainConfig != "selected-data.yaml" || got.ToolchainCLI != "cli" || got.Avrdude != "avr" || got.AvrdudeConf != "avr.conf" {
		t.Fatalf("configured paths lost: %#v %v", got, err)
	}
}
