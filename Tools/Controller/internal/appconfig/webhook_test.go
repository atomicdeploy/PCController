package appconfig

import "testing"

func TestOutboundWebhookReliabilityConfigurationValidation(t *testing.T) {
	valid := Defaults()
	valid.Integrations.OutboundWebhooks = []Webhook{{
		Name: "automation", Enabled: true, EventKind: "door.*",
		URL: "https://example.test/hook", Method: "POST",
		Headers:      map[string]string{"X-Site": "workshop"},
		BodyTemplate: `{"event":{{event}},"metadata":{{metadata}}}`,
		TimeoutMS:    2500, MaxAttempts: 8,
		RetryInitialMS: 200, RetryMaximumMS: 30_000,
		SigningSecret: "0123456789abcdef",
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid webhook reliability configuration: %v", err)
	}

	tests := map[string]func(*Webhook){
		"attempt limit":        func(value *Webhook) { value.MaxAttempts = 21 },
		"inverted backoff":     func(value *Webhook) { value.RetryInitialMS, value.RetryMaximumMS = 1000, 500 },
		"link-local target":    func(value *Webhook) { value.URL = "http://169.254.169.254/latest/meta-data/" },
		"short signing secret": func(value *Webhook) { value.SigningSecret = "short" },
		"header injection":     func(value *Webhook) { value.Headers = map[string]string{"X-Test": "ok\r\nInjected: yes"} },
		"managed header":       func(value *Webhook) { value.Headers = map[string]string{"Idempotency-Key": "override"} },
		"unknown placeholder":  func(value *Webhook) { value.BodyTemplate = `{"x":"{{unknown}}"}` },
		"unterminated token":   func(value *Webhook) { value.BodyTemplate = `{"x":"{{text"}` },
		"malformed JSON":       func(value *Webhook) { value.BodyTemplate = `{"x":"{{text}}"` },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			candidate.Integrations = cloneIntegrations(valid.Integrations)
			mutate(&candidate.Integrations.OutboundWebhooks[0])
			if err := candidate.Validate(); err == nil {
				t.Fatal("invalid webhook configuration was accepted")
			}
		})
	}
}
