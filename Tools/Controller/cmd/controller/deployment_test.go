package main

import (
	"bytes"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func TestProgramDeploymentDryRunPrecedenceAndEEPROMProtection(t *testing.T) {
	t.Setenv("PCCONTROLLER_PORT", "")
	t.Setenv("PCCONTROLLER_DEVICE", "")
	for _, test := range []struct {
		name, environment, configured, explicit, required string
		reset                                             bool
	}{
		{"default", "", "", "", "true", false},
		{"config-development", "", "development", "", "false", false},
		{"environment-production", "production", "development", "", "true", false},
		{"explicit-development", "production", "production", "development", "false", false},
		{"development-eeprom-reset", "", "development", "", "true", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("PCCONTROLLER_DEPLOYMENT", test.environment)
			config := appconfig.Defaults()
			config.Programming.Deployment = test.configured
			args := []string{"flash", "candidate.hex", "COM18", "--dry-run"}
			if test.explicit != "" {
				args = append(args, "--deployment", test.explicit)
			}
			if test.reset {
				args = append(args, "--reinitialize-eeprom")
			}
			var output bytes.Buffer
			if err := runProgramWithConfig(args, &output, &output, config); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(output.String(), "complete-backup-required="+test.required) {
				t.Fatal(output.String())
			}
		})
	}
}
