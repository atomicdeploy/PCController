//go:build windows

package headlessweb

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/eventeffects"
	"pccontroller.local/desktop/internal/headless"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/localapi"
	"pccontroller.local/desktop/internal/model"
	"pccontroller.local/desktop/internal/sfx"
)

type fakeController struct {
	mu           sync.Mutex
	cfg          config.Config
	hooks        commands.Hooks
	started      bool
	closed       int
	unsubscribed atomic.Bool
}

func (c *fakeController) Snapshot() model.Snapshot {
	return model.Snapshot{
		State:     model.StateOff,
		DeviceURL: c.Config().Device.BaseURL,
	}
}

func (c *fakeController) Config() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg
}

func (c *fakeController) Subscribe(int) (<-chan model.Event, func()) {
	events := make(chan model.Event)
	return events, func() { c.unsubscribed.Store(true) }
}

func (c *fakeController) Start(context.Context) error {
	c.mu.Lock()
	c.started = true
	c.mu.Unlock()
	return nil
}

func (c *fakeController) Close() error {
	c.mu.Lock()
	c.closed++
	c.mu.Unlock()
	return nil
}

func (c *fakeController) SetHooks(hooks commands.Hooks) error {
	c.mu.Lock()
	c.hooks = hooks
	c.mu.Unlock()
	return nil
}

func (*fakeController) Call(context.Context, ipc.Request) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func (c *fakeController) currentHooks() commands.Hooks {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hooks
}

type fakeEffects struct {
	applied atomic.Int32
	closed  atomic.Int32
}

func (*fakeEffects) PrepareConfig(cfg config.Config) (config.Config, error) {
	return cfg, nil
}
func (e *fakeEffects) ApplyConfig(config.Config) { e.applied.Add(1) }
func (e *fakeEffects) Close()                    { e.closed.Add(1) }
func (*fakeEffects) PlayCount(eventeffects.Cue, int) error {
	return nil
}
func (*fakeEffects) ResolveAudio(string) (sfx.Asset, error) {
	return sfx.Asset{
		ID:   strings.Repeat("0", 64),
		Name: "Test.wav",
		Data: []byte("RIFFtestWAVE"),
	}, nil
}
func (*fakeEffects) ImportAudio(name string, _ []byte) (sfx.ImportedAsset, error) {
	return sfx.ImportedAsset{ID: strings.Repeat("1", 64), Name: name}, nil
}
func (*fakeEffects) ImportAudioFile(string) (sfx.ImportedAsset, error) {
	return sfx.ImportedAsset{
		ID:   strings.Repeat("1", 64),
		Name: "Imported.wav",
	}, nil
}

type fakeStartWithWindowsPersistence struct {
	fixture *hostFixture
	last    bool
}

func (p *fakeStartWithWindowsPersistence) ApplySaved(enabled bool) error {
	if enabled == p.last {
		return nil
	}
	p.last = enabled
	p.fixture.mu.Lock()
	p.fixture.startWithWindowsApplies = append(
		p.fixture.startWithWindowsApplies,
		enabled,
	)
	p.fixture.mu.Unlock()
	return nil
}

type fakeAPIServer struct {
	url        string
	broadcasts atomic.Int32
	closes     atomic.Int32
	closeStart chan struct{}
	closeGate  <-chan struct{}
	closeOnce  sync.Once
}

func (s *fakeAPIServer) BrowserURL() string { return s.url }

func (s *fakeAPIServer) BroadcastConfig(config.Config) {
	s.broadcasts.Add(1)
}

func (s *fakeAPIServer) Close(ctx context.Context) error {
	s.closes.Add(1)
	if s.closeStart != nil {
		s.closeOnce.Do(func() { close(s.closeStart) })
	}
	if s.closeGate != nil {
		select {
		case <-s.closeGate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

type hostFixture struct {
	controller *fakeController
	effects    *fakeEffects
	cfg        config.Config

	mu                           sync.Mutex
	apiOptions                   []localapi.Options
	servers                      []*fakeAPIServer
	created                      chan *fakeAPIServer
	opened                       chan string
	closeGate                    <-chan struct{}
	startWithWindowsRegistered   bool
	startWithWindowsProbeCalls   int
	startWithWindowsFactoryCalls int
	startWithWindowsApplies      []bool
}

func newHostFixture() *hostFixture {
	cfg := config.Default()
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	cfg.Integration.LocalAPIEnabled = false
	controller := &fakeController{cfg: cfg}
	return &hostFixture{
		controller: controller,
		effects:    &fakeEffects{},
		cfg:        cfg,
		created:    make(chan *fakeAPIServer, 8),
		opened:     make(chan string, 8),
	}
}

func (f *hostFixture) dependencies() dependencies {
	return dependencies{
		resolveConfig: func(string) (string, error) {
			return `C:\Temp\PCControllerDesktop\config.json`, nil
		},
		loadConfig: func(string) (config.Config, error) {
			return f.cfg, nil
		},
		executable: func() (string, error) {
			return `C:\Apps\PCControllerDesktop\PCControllerDesktop.exe`, nil
		},
		reconcileStartWithWindows: func(
			_ string,
			_ bool,
		) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.startWithWindowsProbeCalls++
			return f.startWithWindowsRegistered, nil
		},
		newStartWithWindowsPersistence: func(
			_ string,
			initial bool,
		) (startWithWindowsPersistence, error) {
			f.mu.Lock()
			f.startWithWindowsFactoryCalls++
			f.mu.Unlock()
			return &fakeStartWithWindowsPersistence{
				fixture: f,
				last:    initial,
			}, nil
		},
		newController: func(options headless.Options) (controller, error) {
			f.controller.mu.Lock()
			f.controller.cfg = *options.Config
			f.controller.mu.Unlock()
			return f.controller, nil
		},
		newAPI: func(options localapi.Options) (apiServer, error) {
			f.mu.Lock()
			index := len(f.servers) + 1
			server := &fakeAPIServer{
				url:        "http://127.0.0.1:49" + string(rune('0'+index)) + "/app/",
				closeStart: make(chan struct{}),
				closeGate:  f.closeGate,
			}
			f.apiOptions = append(f.apiOptions, options)
			f.servers = append(f.servers, server)
			f.mu.Unlock()
			f.created <- server
			return server, nil
		},
		newEffects: func(
			string,
			config.Config,
			<-chan model.Event,
			eventeffects.LogFunc,
		) (effectsService, error) {
			return f.effects, nil
		},
		openURL: func(url string) error {
			f.opened <- url
			return nil
		},
	}
}

func (f *hostFixture) apiCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.servers)
}

func waitValue[T any](t *testing.T, channel <-chan T, label string) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(2 * time.Second):
		var zero T
		t.Fatalf("timed out waiting for %s", label)
		return zero
	}
}

func TestHostIsGUIFreePrimaryWithBrowserRoutes(t *testing.T) {
	fixture := newHostFixture()
	host, err := newWithDependencies(Options{
		Listen: "localhost:49123",
	}, fixture.dependencies())
	if err != nil {
		t.Fatal(err)
	}

	fixture.controller.mu.Lock()
	cfg := fixture.controller.cfg
	started := fixture.controller.started
	fixture.controller.mu.Unlock()
	if !started ||
		!cfg.Integration.LocalAPIEnabled ||
		cfg.Integration.LocalAPIListen != "localhost:49123" {
		t.Fatalf("controller config/start = %#v, %v", cfg.Integration, started)
	}

	fixture.mu.Lock()
	apiOptions := fixture.apiOptions[0]
	fixture.mu.Unlock()
	for _, capability := range []string{
		"native_shell",
		"window_control",
		"notifications",
		"global_hotkeys",
		"hotkey_capture",
		"hotkey_probe",
	} {
		if apiOptions.Capabilities[capability] {
			t.Errorf("headless capability %q = true", capability)
		}
	}
	if apiOptions.HotkeyCapture != nil ||
		apiOptions.HotkeyProbe != nil ||
		apiOptions.AppHandler == nil ||
		apiOptions.EventSource != fixture.controller {
		t.Fatalf("headless API wiring = %#v", apiOptions)
	}

	hooks := fixture.controller.currentHooks()
	if hooks.HideWindow != nil || hooks.Notify != nil {
		t.Fatal("headless host installed unavailable native hooks")
	}
	hooks.ShowWindow()
	if opened := waitValue(t, fixture.opened, "dashboard route"); opened !=
		"http://127.0.0.1:491/app/" {
		t.Fatalf("dashboard URL = %q", opened)
	}
	hooks.OpenSettings()
	if opened := waitValue(t, fixture.opened, "settings route"); opened !=
		"http://127.0.0.1:491/app/#/settings" {
		t.Fatalf("settings URL = %q", opened)
	}
	hooks.OpenDiagnostics()
	if opened := waitValue(t, fixture.opened, "diagnostics route"); opened !=
		"http://127.0.0.1:491/app/#/diagnostics" {
		t.Fatalf("diagnostics URL = %q", opened)
	}

	hooks.Quit()
	if err := host.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fixture.effects.closed.Load() != 1 ||
		!fixture.controller.unsubscribed.Load() {
		t.Fatal("headless effects/subscription were not closed")
	}
	fixture.controller.mu.Lock()
	closed := fixture.controller.closed
	fixture.controller.mu.Unlock()
	if closed != 1 {
		t.Fatalf("controller closed %d times", closed)
	}
}

func TestHostReconcilesStartupReadOnlyAndPersistsOnlySavedChanges(t *testing.T) {
	fixture := newHostFixture()
	fixture.cfg.Integration.StartWithWindows = false
	fixture.startWithWindowsRegistered = true

	host, err := newWithDependencies(Options{}, fixture.dependencies())
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	if !fixture.controller.Config().Integration.StartWithWindows {
		t.Fatal("owned installer Run value was not reconciled into serve config")
	}
	fixture.mu.Lock()
	probeCalls := fixture.startWithWindowsProbeCalls
	factoryCalls := fixture.startWithWindowsFactoryCalls
	applies := append([]bool(nil), fixture.startWithWindowsApplies...)
	fixture.mu.Unlock()
	if probeCalls != 1 || factoryCalls != 1 {
		t.Fatalf(
			"startup dependencies: probes=%d factories=%d, want 1/1",
			probeCalls,
			factoryCalls,
		)
	}
	if len(applies) != 0 {
		t.Fatalf("serve startup mutated Run registration: %v", applies)
	}

	hooks := fixture.controller.currentHooks()
	unchanged := fixture.controller.Config()
	hooks.ConfigApplied(unchanged)
	fixture.mu.Lock()
	applies = append([]bool(nil), fixture.startWithWindowsApplies...)
	fixture.mu.Unlock()
	if len(applies) != 0 {
		t.Fatalf("unchanged saved config mutated Run registration: %v", applies)
	}

	changed := unchanged
	changed.Integration.StartWithWindows = false
	hooks.ConfigApplied(changed)
	fixture.mu.Lock()
	applies = append([]bool(nil), fixture.startWithWindowsApplies...)
	fixture.mu.Unlock()
	if len(applies) != 1 || applies[0] {
		t.Fatalf("saved disable registry calls = %v, want [false]", applies)
	}
}

func TestConfigRebindIsAsynchronousAndCanDisableListener(t *testing.T) {
	closeGate := make(chan struct{})
	fixture := newHostFixture()
	fixture.closeGate = closeGate
	host, err := newWithDependencies(Options{}, fixture.dependencies())
	if err != nil {
		t.Fatal(err)
	}
	initial := waitValue(t, fixture.created, "initial API")
	hooks := fixture.controller.currentHooks()

	unchanged := fixture.controller.Config()
	unchanged.UI.AlwaysOnTop = !unchanged.UI.AlwaysOnTop
	hooks.ConfigApplied(unchanged)
	if initial.broadcasts.Load() != 1 || fixture.apiCount() != 1 {
		t.Fatalf(
			"non-listener config caused restart: broadcasts=%d APIs=%d",
			initial.broadcasts.Load(),
			fixture.apiCount(),
		)
	}

	rebound := unchanged
	rebound.Integration.LocalAPIListen = "127.0.0.1:49124"
	returned := make(chan struct{})
	go func() {
		hooks.ConfigApplied(rebound)
		close(returned)
	}()
	waitValue(t, returned, "ConfigApplied return")
	waitValue(t, initial.closeStart, "old listener shutdown")
	select {
	case <-fixture.created:
		t.Fatal("replacement listener started before graceful close completed")
	default:
	}
	close(closeGate)
	replacement := waitValue(t, fixture.created, "replacement API")
	if initial.closes.Load() != 1 || fixture.apiCount() != 2 {
		t.Fatalf("listener rebind counts = closes %d, APIs %d", initial.closes.Load(), fixture.apiCount())
	}

	disabled := rebound
	disabled.Integration.LocalAPIEnabled = false
	hooks.ConfigApplied(disabled)
	waitValue(t, replacement.closeStart, "replacement disable")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, unavailable := host.BrowserURL(); unavailable != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dashboard remained available after disabling listener")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fixture.apiCount() != 2 {
		t.Fatalf("disabling listener created API %d", fixture.apiCount())
	}
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseSignalsDashboardBeforeWaitingForWorkers(t *testing.T) {
	fixture := newHostFixture()
	host, err := newWithDependencies(Options{}, fixture.dependencies())
	if err != nil {
		t.Fatal(err)
	}
	server := waitValue(t, fixture.created, "initial API")

	releaseWorker := make(chan struct{})
	host.workers.Add(1)
	go func() {
		defer host.workers.Done()
		<-releaseWorker
	}()

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- host.Close()
	}()
	waitValue(t, server.closeStart, "dashboard shutdown signal")

	select {
	case closeErr := <-closeDone:
		t.Fatalf("Close returned before worker joined: %v", closeErr)
	case <-time.After(75 * time.Millisecond):
	}
	close(releaseWorker)
	select {
	case closeErr := <-closeDone:
		if closeErr != nil {
			t.Fatalf("Close: %v", closeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after worker joined")
	}
}
