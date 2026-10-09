package appconfig

import "testing"

func TestLocalIntegrationTargetsAreNarrowlyValidated(t *testing.T) {
	value := Defaults()
	value.Integrations.DataHub.Enabled = true
	if err := value.Validate(); err != nil {
		t.Fatalf("loopback data-hub default should validate: %v", err)
	}

	value.Integrations.DataHub.BaseURL = "https://example.com"
	if err := value.Validate(); err == nil {
		t.Fatal("public data-hub target should be rejected")
	}
	value.Integrations.DataHub.BaseURL = "http://127.0.0.1:8080"
	value.Integrations.LocalDevice = LocalDevice{
		Enabled: true,
		BaseURL: "http://192.168.1.50",
	}
	if err := value.Validate(); err != nil {
		t.Fatalf("private local-device target should validate: %v", err)
	}
	value.Integrations.LocalDevice.BaseURL = "https://example.com"
	if err := value.Validate(); err == nil {
		t.Fatal("public local-device target should be rejected")
	}
}

func TestEnabledLocalIntegrationRequiresTarget(t *testing.T) {
	value := Defaults()
	value.Integrations.LocalDevice.Enabled = true
	if err := value.Validate(); err == nil {
		t.Fatal("enabled local-device integration without a URL should fail")
	}
	value.Integrations.LocalDevice.Enabled = false
	value.Integrations.DataHub = DataHub{Enabled: true}
	if err := value.Validate(); err == nil {
		t.Fatal("enabled data-hub integration without a URL should fail")
	}
}
