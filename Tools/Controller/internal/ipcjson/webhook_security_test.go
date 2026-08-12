package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestInboundWebhookDropsCredentialBoundariesAndCallerProvenance(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachIsolatedRuntime(runtime, shell.New(8))
	defer client.Shutdown()
	const bearer = "durable-header-credential"
	handler := websocketMux(context.Background(), &Service{
		Client: client, AuthToken: bearer, InboundWebhooks: true,
	})
	body := `{
		"source":"client","target":"host","type":"audit.notice","text":"safe payload",
		"metadata":{
			"business_id":"order-42",
			"access_token":"body-token",
			"header.authorization":"spoofed",
			"query.ticket":"spoofed",
			"security.principal":"spoofed",
			"principal":"spoofed",
			"authentication":"spoofed",
			"correlation_id":"spoofed"
		}
	}`
	request := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:8787/api/webhooks/inbound?project=alpha&token=query-secret&session=session-secret&signature=signed-secret",
		strings.NewReader(body),
	)
	request.RemoteAddr = "127.0.0.1:49210"
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Cookie", "sid=cookie-secret")
	request.Header.Set("Referer", "https://example.test/?token=referrer-secret")
	request.Header.Set("X-PCController-Token", bearer)
	request.Header.Set("X-API-Key", "api-key-secret")
	request.Header.Set("X-Correlation-ID", "correlation-42")
	request.Header.Set("X-Business-Secret", "custom-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var event controllerapi.Event
	if err := json.Unmarshal(response.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	for _, forbidden := range []string{
		bearer, "query-secret", "session-secret", "signed-secret",
		"body-token", "spoofed", "cookie-secret", "referrer-secret",
		"api-key-secret", "custom-secret", "token=", "ticket=",
	} {
		if strings.Contains(serialized, forbidden) {
			t.Errorf("event retained sensitive value %q: %s", forbidden, serialized)
		}
	}
	if event.Text != "safe payload" || event.Source != "webhook" ||
		event.Metadata["business_id"] != "order-42" ||
		event.Metadata["query.project"] != "alpha" ||
		event.Metadata["header.x-correlation-id"] != "correlation-42" ||
		event.Metadata["http.path"] != "/api/webhooks/inbound" ||
		event.Metadata["principal"] != "remote-operator" ||
		event.Metadata["authentication"] != "bearer" {
		t.Fatalf("sanitized event=%#v", event)
	}
}

func TestInboundWebhookEmptyBodyFallbackContainsPathWithoutQuery(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachIsolatedRuntime(runtime, shell.New(8))
	defer client.Shutdown()
	handler := websocketMux(context.Background(), &Service{
		Client: client, InboundWebhooks: true,
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8787/api/webhooks/inbound?signature=must-never-appear",
		nil,
	)
	request.RemoteAddr = "127.0.0.1:49211"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var event controllerapi.Event
	if err := json.Unmarshal(response.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Text != "GET /api/webhooks/inbound" ||
		strings.Contains(event.Text, "?") || strings.Contains(event.Text, "must-never-appear") {
		t.Fatalf("fallback text=%q", event.Text)
	}
	if _, exists := event.Metadata["query.signature"]; exists {
		t.Fatalf("signature metadata leaked: %#v", event.Metadata)
	}
}

func TestInboundWebhookSensitiveNameClassifierIsSeparatorAgnostic(t *testing.T) {
	for _, name := range []string{
		"Access_Token", "x-api-key", "Proxy-Authorization", "session.ticket",
		"webhook-signature", "Set-Cookie", "HTTP_REFERER", "clientCredential",
	} {
		if !inboundWebhookSensitiveName(name) {
			t.Errorf("sensitive name was not classified: %q", name)
		}
	}
	for _, name := range []string{"project", "content-type", "x-request-id", "monkey", "hockey"} {
		if inboundWebhookSensitiveName(name) {
			t.Errorf("ordinary name was classified as sensitive: %q", name)
		}
	}
}
