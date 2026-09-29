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
	if backend.failGetCall != 0 && backend.getCalls == backend.failGetCall {
		return "", backend.failGetErr
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

func webSocketSecretConfig(reference string) Config {
	value := Defaults()
	value.Integrations.WebSocketClients = []WebSocketClient{{
		Name: "bridge", Enabled: true, URL: "wss://bridge.example/ipc",
		AuthTokenRef: reference, ForwardEvents: true,
	}}
	return value
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
	ipcToken, _ := backend.value("ipc.remote")
	if secret, err := store.ResolveSecret("os:ipc.remote"); err != nil || secret != ipcToken {
		t.Fatalf("explicit secret resolution failed: value=%q err=%v", secret, err)
	}
	webhook := runtime.Integrations.OutboundWebhooks[0]
	webhookSigningSecret, _ := backend.value("webhooks/events-signing")
	if webhook.SigningSecret != webhookSigningSecret ||
		webhook.Headers["Authorization"] != "Bearer environment-secret" ||
		len(webhook.SecretHeaders) != 0 {
		t.Fatalf("runtime webhook was not resolved: %#v", webhook)
	}
	peer := runtime.Integrations.WebSocketClients[0]
	bridgeToken, _ := backend.value("bridges/main")
	if peer.AuthToken != bridgeToken || peer.AuthTokenRef != "" || !peer.Enabled {
		t.Fatalf("optional transition peer credential was not resolved: %#v", peer)
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
	backend := &configSecretBackend{values: map[string]string{"bridges/main": initialToken}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, webSocketSecretConfig("os:bridges/main")); err != nil {
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
		peer := store.CurrentRuntime().Integrations.WebSocketClients[0]
		if peer.AuthToken != wantToken || peer.AuthTokenRef != "" || !peer.Enabled {
			return errors.New("cached runtime did not contain the resolved peer token")
		}
		return nil
	}
	readConcurrently := func(wantToken string) {
		t.Helper()
		start := make(chan struct{})
		failures := make(chan error, clientCount)
		var clients sync.WaitGroup
		for client := 0; client < clientCount; client++ {
			clients.Add(1)
			go func() {
				defer clients.Done()
				<-start
				for read := 0; read < readsPer; read++ {
					if err := assertRuntime(wantToken); err != nil {
						failures <- err
						return
					}
				}
			}()
		}
		close(start)
		clients.Wait()
		close(failures)
		for err := range failures {
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
	if update := <-updates; update.Integrations.WebSocketClients[0].AuthToken != initialToken {
		t.Fatalf("initial runtime subscription did not use cached token")
	}
	if calls := backend.calls(); calls != 1 {
		t.Fatalf("SubscribeRuntime vault Get calls=%d, want cached count 1", calls)
	}
	if err := store.SetSecret("os:bridges/main", rotatedToken); err != nil {
		t.Fatal(err)
	}
	if calls := backend.calls(); calls != 2 {
		t.Fatalf("token rotation vault Get calls=%d, want one cache refresh", calls)
	}
	select {
	case update := <-updates:
		peer := update.Integrations.WebSocketClients[0]
		if peer.AuthToken != rotatedToken || !peer.Enabled {
			t.Fatalf("runtime rotation update was not resolved: %#v", peer)
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
		"bridges/main": "0123456789abcdefghijklmn",
	}}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Write(path, webSocketSecretConfig("os:bridges/main")); err != nil {
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
	persistentPeer := &persistent.Integrations.WebSocketClients[0]
	runtimePeer := &runtime.Integrations.WebSocketClients[0]
	if runtimePeer.AuthToken == "" || runtimePeer.AuthTokenRef != "" {
		t.Fatalf("runtime peer token was not resolved: %#v", runtimePeer)
	}
	persistentPeer.AuthToken, persistentPeer.AuthTokenRef = "", ""
	runtimePeer.AuthToken, runtimePeer.AuthTokenRef = "", ""
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
					Port: "TEST1", VID: "1a86", PID: "7523",
					SerialNumber: "controller-test",
					LastSeen:     time.Date(2026, 9, 29, 5, 30, 0, 0, time.UTC),
				})
				return store.Current(), changed, err
			},
			assert: func(t *testing.T, value Config) {
				t.Helper()
				device := value.Connection.LastDevice
				if device == nil || device.Port != "TEST1" || device.VID != "1A86" ||
					device.SerialNumber != "controller-test" {
					t.Fatalf("remembered device was not committed: %#v", device)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			const token = "0123456789abcdefghijklmn"
			backend := &configSecretBackend{values: map[string]string{"bridges/main": token}}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := Write(path, webSocketSecretConfig("os:bridges/main")); err != nil {
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
			if initial := <-runtimeUpdates; initial.Integrations.WebSocketClients[0].AuthToken != token {
				t.Fatalf("initial runtime token was not resolved")
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
				t.Fatalf("disk/store/return diverged\ndisk: %#v\ncurrent: %#v\nreturned: %#v", disk, current, returned)
			}
			store.mu.RLock()
			storedDigest := store.digest
			store.mu.RUnlock()
			if storedDigest != digest {
				t.Fatal("store did not commit the persisted content digest")
			}
			peer := store.CurrentRuntime().Integrations.WebSocketClients[0]
			if peer.Enabled || peer.AuthToken != "" || peer.AuthTokenRef != "" {
				t.Fatalf("stale peer authorization survived post-write failure: %#v", peer)
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
				peer := update.Integrations.WebSocketClients[0]
				if peer.Enabled || peer.AuthToken != "" || peer.AuthTokenRef != "" {
					t.Fatalf("runtime subscriber received stale authorization: %#v", peer)
				}
			case <-time.After(time.Second):
				t.Fatal("runtime subscriber was not notified")
			}
			reloaded, reloadedChanged, err := store.Reload()
			if err != nil || reloadedChanged || !reflect.DeepEqual(reloaded, disk) {
				t.Fatalf("committed digest was not stable: changed=%t err=%v value=%#v", reloadedChanged, err, reloaded)
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
	if err := store.SetSecret("os:webhooks/events-signing", "abcdefghijklmnop"); err == nil || !errors.Is(err, secretstore.ErrNotFound) {
		t.Fatalf("secret refresh error=%v, want missing-reference failure", err)
	}
	runtime := store.CurrentRuntime()
	if runtime.Integrations.WebSocketClients[0].Enabled ||
		runtime.Integrations.OutboundWebhooks[0].Enabled {
		t.Fatalf("referenced integrations did not fail closed: %#v", runtime.Integrations)
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
