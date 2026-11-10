package appconfig

import (
	"testing"

	"pccontroller.local/controller/internal/mqttbridge"
	"pccontroller.local/controller/internal/secretstore"
)

func TestMQTTSecretIsIsolatedFromOtherTransports(t *testing.T) {
	value := Defaults()
	const ref = "env:PCCONTROLLER_FIXTURE_MQTT_SECRET"
	value.Integrations.MQTT = mqttbridge.Config{Enabled: true, Broker: "mqtts://broker.example", Topic: "pccontroller/fixture", Username: "fixture", PasswordRef: ref}
	if err := value.Validate(); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveConfigSecrets(value, nil)
	if err != nil || !resolved.Integrations.MQTT.Enabled || resolved.Integrations.MQTT.PasswordRef != ref {
		t.Fatalf("missing MQTT credential invalidated the host or allowed anonymous fallback: %v", err)
	}
	t.Setenv("PCCONTROLLER_FIXTURE_MQTT_SECRET", "test-only-secret")
	resolved, err = resolveConfigSecrets(value, &secretstore.Resolver{})
	if err != nil {
		t.Fatal(err)
	}
	if mqttbridge.Available && (resolved.Integrations.MQTT.PasswordRef != "" || resolved.Integrations.MQTT.Password != "test-only-secret") {
		t.Fatal("runtime did not resolve MQTT credential")
	}
	if !mqttbridge.Available && resolved.Integrations.MQTT.Password != "" {
		t.Fatal("build without MQTT read the broker secret")
	}
	if Redacted(resolved).Integrations.MQTT.Password != "" || value.Integrations.MQTT.Password != "" {
		t.Fatal("runtime credential escaped")
	}
}
