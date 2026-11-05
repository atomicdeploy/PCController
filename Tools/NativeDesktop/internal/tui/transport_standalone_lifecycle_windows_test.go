//go:build windows

package tui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/config"
)

type standaloneLifecycleAPIServer struct {
	url    string
	closes atomic.Int32
}

func (server *standaloneLifecycleAPIServer) BrowserURL() string {
	return server.url
}

func (*standaloneLifecycleAPIServer) BroadcastConfig(config.Config) {}

func (server *standaloneLifecycleAPIServer) Close(context.Context) error {
	server.closes.Add(1)
	return nil
}

func TestStandaloneInitialAPIRejectsGenerationChangedDuringStartup(
	t *testing.T,
) {
	cfg := config.Default()
	cfg.Integration.LocalAPIListen = "127.0.0.1:49001"
	started := make(chan struct{})
	release := make(chan struct{})
	initial := &standaloneLifecycleAPIServer{
		url: "http://127.0.0.1:49001/app/",
	}
	transport := &standaloneTransport{
		apiSettings: standaloneAPISettingsFromConfig(cfg),
		apiFactory: func(config.Config) (standaloneAPIServer, error) {
			close(started)
			<-release
			return initial, nil
		},
	}

	done := make(chan struct{})
	go func() {
		transport.initializeAPI(cfg, 0)
		close(done)
	}()
	waitStandaloneLifecycleSignal(t, started, "initial listener startup")

	newer := cfg
	newer.Integration.LocalAPIListen = "127.0.0.1:49002"
	transport.apiMu.Lock()
	transport.apiSettings = standaloneAPISettingsFromConfig(newer)
	transport.apiRevision.Add(1)
	transport.apiMu.Unlock()
	close(release)
	waitStandaloneLifecycleSignal(t, done, "stale initial listener cleanup")

	transport.apiMu.Lock()
	active := transport.api
	apiErr := transport.apiErr
	transport.apiMu.Unlock()
	if active != nil {
		t.Fatal("stale initial listener was published")
	}
	if apiErr != nil {
		t.Fatalf("stale initial listener replaced current status with %v", apiErr)
	}
	if closes := initial.closes.Load(); closes != 1 {
		t.Fatalf("stale initial listener closed %d times, want 1", closes)
	}
}

func TestStandaloneReconfigureRejectsGenerationChangedDuringStartup(
	t *testing.T,
) {
	cfg := config.Default()
	cfg.Integration.LocalAPIListen = "127.0.0.1:49011"
	started := make(chan struct{})
	release := make(chan struct{})
	existing := &standaloneLifecycleAPIServer{
		url: "http://127.0.0.1:49010/app/",
	}
	replacement := &standaloneLifecycleAPIServer{
		url: "http://127.0.0.1:49011/app/",
	}
	transport := &standaloneTransport{
		api:         existing,
		apiSettings: standaloneAPISettingsFromConfig(cfg),
		apiFactory: func(config.Config) (standaloneAPIServer, error) {
			close(started)
			<-release
			return replacement, nil
		},
	}
	transport.apiRevision.Store(1)

	done := make(chan struct{})
	go func() {
		transport.reconfigureAPI(cfg, 1)
		close(done)
	}()
	waitStandaloneLifecycleSignal(t, started, "replacement listener startup")

	newer := cfg
	newer.Integration.LocalAPIListen = "127.0.0.1:49012"
	transport.apiMu.Lock()
	transport.apiSettings = standaloneAPISettingsFromConfig(newer)
	transport.apiRevision.Add(1)
	transport.apiMu.Unlock()
	close(release)
	waitStandaloneLifecycleSignal(t, done, "stale replacement cleanup")

	transport.apiMu.Lock()
	active := transport.api
	apiErr := transport.apiErr
	transport.apiMu.Unlock()
	if active != nil {
		t.Fatal("stale replacement listener was published")
	}
	if apiErr != nil {
		t.Fatalf("stale replacement replaced current status with %v", apiErr)
	}
	if closes := existing.closes.Load(); closes != 1 {
		t.Fatalf("existing listener closed %d times, want 1", closes)
	}
	if closes := replacement.closes.Load(); closes != 1 {
		t.Fatalf("stale replacement closed %d times, want 1", closes)
	}
}

func waitStandaloneLifecycleSignal(
	t *testing.T,
	signal <-chan struct{},
	name string,
) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}
