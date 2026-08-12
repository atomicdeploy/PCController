package appconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/secretstore"
)

type configSecretBackend struct {
	mu          sync.Mutex
	values      map[string]string
	getCalls    int
	failGetCall int
	failGetErr  error
}

func (backend *configSecretBackend) Status() secretstore.Status {
	return secretstore.Status{Provider: "test-vault", Available: true, Durable: true, Scope: "test-user"}
}
func (backend *configSecretBackend) Get(name string) (string, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.getCalls++
	if backend.getCalls == backend.failGetCall {
		if backend.failGetErr != nil {
			return "", backend.failGetErr
		}
		return "", secretstore.ErrUnavailable
	}
	value, ok := backend.values[name]
	if !ok {
		return "", secretstore.ErrNotFound
	}
	return value, nil
}
func (backend *configSecretBackend) Set(name, value string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.values[name] = value
	return nil
}
func (backend *configSecretBackend) Delete(name string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if _, ok := backend.values[name]; !ok {
		return secretstore.ErrNotFound
	}
	delete(backend.values, name)
	return nil
}

func (backend *configSecretBackend) value(name string) (string, bool) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	value, ok := backend.values[name]
	return value, ok
}

func (backend *configSecretBackend) calls() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.getCalls
}

func (backend *configSecretBackend) failGetOnCall(call int, err error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	backend.failGetCall = call
	backend.failGetErr = err
}

func secretReferenceConfig() Config {
	value := Defaults()
	value.IPC.AllowRemote = true
	value.IPC.Listen = "0.0.0.0:8787"
	value.IPC.AllowedOrigins = []string{"https://controller.example"}
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
	ipcToken, _ := backend.value("ipc.remote")
	if runtime.IPC.AuthToken != ipcToken || runtime.IPC.AuthTokenRef != "" {
		t.Fatalf("runtime IPC secret was not isolated: %#v", runtime.IPC)
	}
	if secret, err := store.ResolveSecret("os:ipc.remote"); err != nil || secret != ipcToken {
		t.Fatalf("explicit secret resolution failed: value=%q err=%v", secret, err)
	}
	webhook := runtime.Integrations.OutboundWebhooks[0]
	webhookSecret, _ := backend.value("webhooks/events-signing")
	if webhook.SigningSecret != webhookSecret ||
		webhook.Headers["Authorization"] != "Bearer environment-secret" ||
		len(webhook.SecretHeaders) != 0 {
		t.Fatalf("runtime webhook was not resolved: %#v", webhook)
	}
	peer := runtime.Integrations.WebSocketClients[0]
	bridgeToken, _ := backend.value("bridges/main")
	if peer.AuthToken != bridgeToken || peer.AuthTokenRef != "" {
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
		if update.IPC.AuthToken != "abcdefghijklmnopqrstuvwx" {
			t.Fatalf("runtime update token was not refreshed")
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
	if _, ok := backend.value("ipc.remote"); ok {
		t.Fatal("unreferenced secret remained in backend")
	}
}

func TestCurrentRuntimeCachesVaultTokenAcrossConcurrentClients(t *testing.T) {
	const (
		initialToken = "0123456789abcdefghijklmn"
		rotatedToken = "abcdefghijklmnopqrstuvwx"
		clientCount  = 32
		readsPer     = 100
	)
	backend := &configSecretBackend{values: map[string]string{
		"ipc.remote": initialToken,
	}}
	value := Defaults()
	value.IPC.AllowRemote = true
	value.IPC.Listen = "0.0.0.0:8787"
	value.IPC.AllowedOrigins = []string{"https://controller.example"}
	value.IPC.AuthTokenRef = "os:ipc.remote"
	value.IPC.RemotePolicy.BoardCommands = true
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	if calls := backend.calls(); calls != 1 {
		t.Fatalf("open vault Get calls=%d, want 1", calls)
	}

	assertRuntime := func(wantToken string) error {
		runtime := store.CurrentRuntime()
		if runtime.IPC.AuthToken != wantToken || runtime.IPC.AuthTokenRef != "" {
			return errors.New("cached runtime did not contain the resolved token")
		}
		if !runtime.IPC.AllowRemote || runtime.IPC.Listen != value.IPC.Listen ||
			runtime.IPC.RemotePolicy != value.IPC.RemotePolicy {
			return errors.New("cached runtime changed remote authorization policy")
		}
		return nil
	}

	readConcurrently := func(wantToken string) {
		t.Helper()
		start := make(chan struct{})
		errors := make(chan error, clientCount)
		var clients sync.WaitGroup
		for client := 0; client < clientCount; client++ {
			clients.Add(1)
			go func() {
				defer clients.Done()
				<-start
				for read := 0; read < readsPer; read++ {
					if err := assertRuntime(wantToken); err != nil {
						errors <- err
						return
					}
				}
			}()
		}
		close(start)
		clients.Wait()
		close(errors)
		for err := range errors {
			t.Error(err)
		}
	}

	readConcurrently(initialToken)
	if calls := backend.calls(); calls != 1 {
		t.Fatalf("concurrent CurrentRuntime vault Get calls=%d, want cached count 1", calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := store.SubscribeRuntime(ctx)
	if update := <-updates; update.IPC.AuthToken != initialToken {
		t.Fatalf("initial runtime subscription token=%q", update.IPC.AuthToken)
	}
	if calls := backend.calls(); calls != 1 {
		t.Fatalf("SubscribeRuntime vault Get calls=%d, want cached count 1", calls)
	}
	if err := store.SetSecret("os:ipc.remote", rotatedToken); err != nil {
		t.Fatal(err)
	}
	if calls := backend.calls(); calls != 2 {
		t.Fatalf("token rotation vault Get calls=%d, want one cache refresh", calls)
	}
	select {
	case update := <-updates:
		if update.IPC.AuthToken != rotatedToken ||
			update.IPC.RemotePolicy != value.IPC.RemotePolicy {
			t.Fatalf("runtime rotation update weakened config: %#v", update.IPC)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime subscriber was not notified after token rotation")
	}
	readConcurrently(rotatedToken)
	if calls := backend.calls(); calls != 2 {
		t.Fatalf("post-rotation concurrent vault Get calls=%d, want cached count 2", calls)
	}
}

func TestUpdateCachesCanonicalPersistedRuntime(t *testing.T) {
	backend := &configSecretBackend{values: map[string]string{
		"ipc.remote": "0123456789abcdefghijklmn",
	}}
	value := Defaults()
	value.IPC.AllowRemote = true
	value.IPC.Listen = "0.0.0.0:8787"
	value.IPC.AuthTokenRef = "os:ipc.remote"
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, value); err != nil {
		t.Fatal(err)
	}
	store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *Config) error {
		config.UI.TUIConsole.FontFace = "   Consolas   "
		config.UI.Appearance.Theme = " DARK "
		config.RF.DisplayRadix = " HEX "
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls := backend.calls(); calls != 3 {
		t.Fatalf("open plus validated/persisted Update vault Get calls=%d, want 3", calls)
	}

	persistent := store.Current()
	runtime := store.CurrentRuntime()
	if persistent.UI.TUIConsole.FontFace != "Consolas" ||
		persistent.UI.Appearance.Theme != "dark" || persistent.RF.DisplayRadix != "hex" {
		t.Fatalf("persisted config was not canonicalized: %#v", persistent.UI)
	}
	if runtime.IPC.AuthToken == "" || runtime.IPC.AuthTokenRef != "" {
		t.Fatalf("runtime token was not resolved: %#v", runtime.IPC)
	}
	// Ignore the intentional persistent-reference/runtime-plaintext split. All
	// other fields must be the exact canonical value reloaded from disk.
	persistent.IPC.AuthToken, persistent.IPC.AuthTokenRef = "", ""
	runtime.IPC.AuthToken, runtime.IPC.AuthTokenRef = "", ""
	if !reflect.DeepEqual(runtime, persistent) {
		t.Fatalf("cached runtime diverged from persisted canonical config\nruntime: %#v\npersistent: %#v", runtime, persistent)
	}
	if calls := backend.calls(); calls != 3 {
		t.Fatalf("CurrentRuntime re-read vault after Update: calls=%d", calls)
	}
}

func TestPostWriteSecretFailureCommitsDiskAndFailsClosed(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Store) (Config, bool, error)
		assert func(*testing.T, Config)
	}{
		{
			name: "Update",
			mutate: func(store *Store) (Config, bool, error) {
				value, err := store.Update(func(config *Config) error {
					config.UI.Tagline = "persisted before vault failure"
					return nil
				})
				return value, true, err
			},
			assert: func(t *testing.T, value Config) {
				t.Helper()
				if value.UI.Tagline != "persisted before vault failure" {
					t.Fatalf("updated tagline was not committed: %q", value.UI.Tagline)
				}
			},
		},
		{
			name: "RememberDevice",
			mutate: func(store *Store) (Config, bool, error) {
				changed, err := store.RememberDevice(DeviceIdentity{
					Port: "COM18", VID: "1a86", PID: "7523",
					SerialNumber: "controller-1",
					LastSeen:     time.Date(2026, 8, 12, 5, 30, 0, 0, time.UTC),
				})
				return store.Current(), changed, err
			},
			assert: func(t *testing.T, value Config) {
				t.Helper()
				device := value.Connection.LastDevice
				if device == nil || device.Port != "COM18" || device.VID != "1A86" ||
					device.SerialNumber != "controller-1" {
					t.Fatalf("remembered device was not committed: %#v", device)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const token = "0123456789abcdefghijklmn"
			backend := &configSecretBackend{values: map[string]string{
				"ipc.remote": token,
			}}
			value := Defaults()
			value.IPC.AllowRemote = true
			value.IPC.Listen = "0.0.0.0:8787"
			value.IPC.AuthTokenRef = "os:ipc.remote"
			path := filepath.Join(t.TempDir(), "config.json")
			if err := Write(path, value); err != nil {
				t.Fatal(err)
			}
			store, err := openWithSecrets(path, secretstore.NewWithBackend(backend))
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			persistentUpdates := store.Subscribe(ctx)
			runtimeUpdates := store.SubscribeRuntime(ctx)
			<-persistentUpdates
			initialRuntime := <-runtimeUpdates
			if initialRuntime.IPC.AuthToken != token {
				t.Fatalf("initial runtime token=%q", initialRuntime.IPC.AuthToken)
			}

			postWriteFailure := errors.New("deterministic post-write vault failure")
			backend.failGetOnCall(3, postWriteFailure)
			returned, changed, err := test.mutate(store)
			if !changed {
				t.Fatal("persisted mutation reported no change")
			}
			if !errors.Is(err, postWriteFailure) {
				t.Fatalf("mutation error=%v, want post-write vault failure", err)
			}
			if calls := backend.calls(); calls != 3 {
				t.Fatalf("vault Get calls=%d, want open/preflight/post-write", calls)
			}

			disk, digest, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			current := store.Current()
			test.assert(t, disk)
			if !reflect.DeepEqual(current, disk) || !reflect.DeepEqual(returned, disk) {
				t.Fatalf("disk/store/return diverged\ndisk: %#v\ncurrent: %#v\nreturned: %#v",
					disk, current, returned)
			}
			store.mu.RLock()
			storedDigest := store.digest
			store.mu.RUnlock()
			if storedDigest != digest {
				t.Fatal("store did not commit the persisted content digest")
			}
			if disk.IPC.AuthTokenRef != "os:ipc.remote" || disk.IPC.AuthToken != "" {
				t.Fatalf("persistent secret reference changed: %#v", disk.IPC)
			}

			runtime := store.CurrentRuntime()
			if runtime.IPC.AllowRemote || runtime.IPC.AuthToken != "" || runtime.IPC.AuthTokenRef != "" {
				t.Fatalf("stale authorization survived post-write failure: %#v", runtime.IPC)
			}
			select {
			case update := <-persistentUpdates:
				if !reflect.DeepEqual(update, disk) {
					t.Fatalf("persistent subscriber did not receive committed config: %#v", update)
				}
			case <-time.After(time.Second):
				t.Fatal("persistent subscriber was not notified")
			}
			select {
			case update := <-runtimeUpdates:
				if update.IPC.AllowRemote || update.IPC.AuthToken != "" || update.IPC.AuthTokenRef != "" {
					t.Fatalf("runtime subscriber received stale authorization: %#v", update.IPC)
				}
			case <-time.After(time.Second):
				t.Fatal("runtime subscriber was not notified")
			}

			reloaded, reloadedChanged, err := store.Reload()
			if err != nil || reloadedChanged || !reflect.DeepEqual(reloaded, disk) {
				t.Fatalf("committed digest was not stable: changed=%t err=%v value=%#v",
					reloadedChanged, err, reloaded)
			}
			if calls := backend.calls(); calls != 3 {
				t.Fatalf("CurrentRuntime/unchanged Reload re-read vault: calls=%d", calls)
			}
		})
	}
}

func TestSecretRefreshFailureReplacesCachedAuthorizationWithFailClosedView(t *testing.T) {
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
	if err := backend.Delete("bridges/main"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecret("os:ipc.remote", "abcdefghijklmnopqrstuvwx"); err == nil || !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("secret refresh error=%v, want missing-reference failure", err)
	}
	runtime := store.CurrentRuntime()
	if runtime.IPC.AllowRemote || runtime.IPC.AuthToken != "" || runtime.IPC.AuthTokenRef != "" {
		t.Fatalf("stale remote authorization survived failed refresh: %#v", runtime.IPC)
	}
	if runtime.Integrations.WebSocketClients[0].Enabled ||
		runtime.Integrations.OutboundWebhooks[0].Enabled {
		t.Fatalf("referenced integrations did not fail closed: %#v", runtime.Integrations)
	}
}

func TestReloadRejectsMissingReferenceAndRetainsLastGoodConfig(t *testing.T) {
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
	if _, changed, err := store.Reload(); err == nil || changed || !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("reload changed=%t err=%v", changed, err)
	}
	if store.Current().IPC.AuthTokenRef != "" {
		t.Fatal("missing-reference config replaced last good value")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
