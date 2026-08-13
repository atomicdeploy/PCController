package appconfig

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/secretstore"
)

type configSecretBackend struct {
	values map[string]string
}

func (backend *configSecretBackend) Status() secretstore.Status {
	return secretstore.Status{Provider: "test-vault", Available: true, Durable: true, Scope: "test-user"}
}
func (backend *configSecretBackend) Get(name string) (string, error) {
	value, ok := backend.values[name]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}
func (backend *configSecretBackend) Set(name, value string) error {
	backend.values[name] = value
	return nil
}
func (backend *configSecretBackend) Delete(name string) error {
	if _, ok := backend.values[name]; !ok {
		return secretstore.ErrNotFound
	}
	delete(backend.values, name)
	return nil
}

func secretReferenceConfig() Config {
	value := Defaults()
	value.IPC.AllowRemote = true
	value.IPC.Listen = "0.0.0.0:8787"
	value.IPC.AllowedOrigins = []string{"controller.example:443"}
	value.IPC.AuthTokenRef = "os:ipc.remote"
	value.Integrations.OutboundWebhooks = []Webhook{{
		Name: "events", Enabled: true, URL: "https://events.example/hook", Method: "POST",
		SigningSecretRef: "os:webhooks/events-signing",
		SecretHeaders:    map[string]string{"Authorization": "env:TEST_WEBHOOK_AUTH"},
	}}
	value.Integrations.WebSocketClients = []WebSocketClient{{
		Name: "bridge", Enabled: true, URL: "wss://bridge.example/ipc",
		AuthTokenRef: "os:bridges/main", ForwardEvents: true,
	}}
	return value
}

func TestStoreResolvesReferencesOnlyForRuntimeViews(t *testing.T) {
	t.Setenv("TEST_WEBHOOK_AUTH", "Bearer environment-secret")
	backend := &configSecretBackend{values: map[string]string{
		"ipc.remote":              "0123456789abcdefghijklmn",
		"webhooks/events-signing": "0123456789abcdef",
		"bridges/main":            "bridge-authentication-token",
	}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, secretReferenceConfig()); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	persistent := store.Current()
	if persistent.IPC.AuthToken != "" || persistent.IPC.AuthTokenRef != "os:ipc.remote" {
		t.Fatalf("persistent IPC secret changed: %#v", persistent.IPC)
	}
	runtime, err := store.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.IPC.AuthToken != "" || runtime.IPC.AuthTokenRef != "" {
		t.Fatalf("dormant alpha IPC credential reached runtime: %#v", runtime.IPC)
	}
	if secret, err := store.ResolveSecret("os:ipc.remote"); err != nil || secret != backend.values["ipc.remote"] {
		t.Fatalf("explicit secret resolution failed: value=%q err=%v", secret, err)
	}
	webhook := runtime.Integrations.OutboundWebhooks[0]
	if webhook.SigningSecret != backend.values["webhooks/events-signing"] ||
		webhook.Headers["Authorization"] != "Bearer environment-secret" ||
		len(webhook.SecretHeaders) != 0 {
		t.Fatalf("runtime webhook was not resolved: %#v", webhook)
	}
	peer := runtime.Integrations.WebSocketClients[0]
	if peer.AuthToken != backend.values["bridges/main"] || peer.AuthTokenRef != "" {
		t.Fatalf("runtime peer was not resolved: %#v", peer)
	}
}

func TestRedactedConfigAndStatusNeverContainSecretValues(t *testing.T) {
	value := secretReferenceConfig()
	value.IPC.AuthToken, value.IPC.AuthTokenRef = "plaintext-ipc-secret-012345", ""
	value.Integrations.OutboundWebhooks[0].Headers = make(map[string]string)
	value.Integrations.OutboundWebhooks[0].Headers["X-API-Key"] = "plaintext-header-secret"
	value.Integrations.WebSocketClients[0].AuthToken = ""
	backend := &configSecretBackend{values: map[string]string{
		"webhooks/events-signing": "0123456789abcdef",
		"bridges/main":            "bridge-authentication-token",
	}}
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("TEST_WEBHOOK_AUTH", "Bearer environment-secret")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	encodedRedacted, _ := json.Marshal(store.Redacted())
	encodedStatus, _ := json.Marshal(store.SecretsStatus())
	for _, forbidden := range []string{
		"plaintext-ipc-secret-012345", "plaintext-header-secret",
		"0123456789abcdef", "bridge-authentication-token", "Bearer environment-secret",
	} {
		if strings.Contains(string(encodedRedacted), forbidden) || strings.Contains(string(encodedStatus), forbidden) {
			t.Fatalf("secret %q leaked: redacted=%s status=%s", forbidden, encodedRedacted, encodedStatus)
		}
	}
	if !strings.Contains(string(encodedStatus), `"source":"plaintext-config"`) {
		t.Fatalf("plaintext presence was not reported safely: %s", encodedStatus)
	}
}

func TestSecretMutationNotifiesRuntimeAndRejectsReferencedDelete(t *testing.T) {
	t.Setenv("TEST_WEBHOOK_AUTH", "Bearer environment-secret")
	backend := &configSecretBackend{values: map[string]string{
		"ipc.remote":              "0123456789abcdefghijklmn",
		"webhooks/events-signing": "0123456789abcdef",
		"bridges/main":            "bridge-authentication-token",
	}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, secretReferenceConfig()); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := store.SubscribeRuntime(ctx)
	<-updates
	if err := store.SetSecret("os:ipc.remote", "abcdefghijklmnopqrstuvwx"); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-updates:
		if update.IPC.AuthToken != "" || update.IPC.AuthTokenRef != "" || !update.IPC.AllowRemote {
			t.Fatalf("dormant alpha credential changed runtime: %#v", update.IPC)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime subscriber was not notified")
	}
	if err := store.DeleteSecret("os:ipc.remote"); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("referenced delete error=%v", err)
	}
	if _, err := store.Update(func(config *Config) error {
		config.IPC.AllowRemote = false
		config.IPC.Listen = "127.0.0.1:8787"
		config.IPC.AuthTokenRef = ""
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSecret("os:ipc.remote"); err != nil {
		t.Fatal(err)
	}
	if _, ok := backend.values["ipc.remote"]; ok {
		t.Fatal("unreferenced secret remained in backend")
	}
}

func TestReloadAcceptsMissingDormantAuthenticationReference(t *testing.T) {
	backend := &configSecretBackend{values: map[string]string{}}
	path := filepath.Join(t.TempDir(), "config.json")
	base := Defaults()
	if err := Write(path, base); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	candidate := base
	candidate.IPC.AuthTokenRef = "os:missing"
	if err := Write(path, candidate); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := store.Reload(); err != nil || !changed {
		t.Fatalf("reload changed=%t err=%v", changed, err)
	}
	if store.Current().IPC.AuthTokenRef != "os:missing" || store.CurrentRuntime().IPC.AuthTokenRef != "" || store.CurrentRuntime().IPC.AuthToken != "" {
		t.Fatalf("dormant reference was not persisted-only: current=%#v runtime=%#v", store.Current().IPC, store.CurrentRuntime().IPC)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
