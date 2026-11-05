//go:build windows

package headless

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"pccontroller.local/desktop/compatibility"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
)

type recordingServer struct {
	closeCount atomic.Int32
	closeErr   error
	onClose    func()
}

func (s *recordingServer) Close() error {
	s.closeCount.Add(1)
	if s.onClose != nil {
		s.onClose()
	}
	return s.closeErr
}

type orderRecorder struct {
	mu    sync.Mutex
	items []string
}

func (r *orderRecorder) add(item string) {
	r.mu.Lock()
	r.items = append(r.items, item)
	r.mu.Unlock()
}

func (r *orderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.items...)
}

func testConfig() config.Config {
	cfg := config.Default()
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	return cfg
}

func testConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "config.json")
}

func testDependencies() dependencies {
	deps := productionDependencies()
	deps.loadConfig = func(string) (config.Config, error) {
		return testConfig(), nil
	}
	deps.saveConfig = func(string, config.Config) error {
		return nil
	}
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return &recordingServer{}, nil
	}
	deps.isIPCRunning = func(context.Context) bool {
		return false
	}
	deps.probeTimeout = 25 * time.Millisecond
	return deps
}

func TestNewClaimsBeforePersistAndStartIsExplicit(t *testing.T) {
	var order orderRecorder
	server := &recordingServer{onClose: func() { order.add("close-pipe") }}
	cfg := testConfig()
	deps := testDependencies()
	deps.newCore = func(cfg config.Config) (*appcore.Core, error) {
		order.add("new-core")
		return appcore.New(cfg)
	}
	deps.newDispatcher = func(
		core *appcore.Core,
		path string,
	) (*commands.Dispatcher, error) {
		order.add("new-dispatcher")
		return commands.New(core, path)
	}
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		order.add("claim-pipe")
		return server, nil
	}
	deps.saveConfig = func(string, config.Config) error {
		order.add("save-config")
		return nil
	}
	deps.startCore = func(*appcore.Core, context.Context) error {
		order.add("start-core")
		return nil
	}
	deps.stopCore = func(*appcore.Core) {
		order.add("stop-core")
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	if runtime.Started() {
		t.Fatal("runtime started before Start")
	}

	events, unsubscribe := runtime.Subscribe(4)
	if events == nil || unsubscribe == nil {
		t.Fatal("Subscribe returned nil values")
	}
	order.add("subscribe")
	defer unsubscribe()

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := order.snapshot()
	want := []string{
		"new-core",
		"new-dispatcher",
		"claim-pipe",
		"save-config",
		"subscribe",
		"start-core",
		"close-pipe",
		"stop-core",
	}
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestNewWithProvidedConfigDoesNotReload(t *testing.T) {
	cfg := testConfig()
	deps := testDependencies()
	deps.loadConfig = func(string) (config.Config, error) {
		t.Fatal("provided configuration was unexpectedly reloaded")
		return config.Config{}, nil
	}
	var saved config.Config
	deps.saveConfig = func(_ string, candidate config.Config) error {
		saved = candidate
		return nil
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	defer runtime.Close()
	if saved.Integration.LocalAPIToken != cfg.Integration.LocalAPIToken {
		t.Fatal("provided API token was replaced")
	}
}

func TestNewLoadsWhenConfigIsNotProvided(t *testing.T) {
	cfg := testConfig()
	cfg.Device.BaseURL = "http://127.0.0.1:12345"
	deps := testDependencies()
	var loadCount atomic.Int32
	deps.loadConfig = func(string) (config.Config, error) {
		loadCount.Add(1)
		return cfg, nil
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	defer runtime.Close()
	if loadCount.Load() != 1 {
		t.Fatalf("load count = %d, want 1", loadCount.Load())
	}
	if runtime.Config().Device.BaseURL != cfg.Device.BaseURL {
		t.Fatalf("base URL = %q, want %q", runtime.Config().Device.BaseURL, cfg.Device.BaseURL)
	}
}

func TestPipeCollisionIsTypedAndNeverPersists(t *testing.T) {
	cfg := testConfig()
	deps := testDependencies()
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return nil, windows.ERROR_ALREADY_EXISTS
	}
	deps.isIPCRunning = func(context.Context) bool {
		t.Fatal("known ownership collision should not require a readiness probe")
		return false
	}
	var saveCount atomic.Int32
	deps.saveConfig = func(string, config.Config) error {
		saveCount.Add(1)
		return nil
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if runtime != nil {
		t.Fatal("runtime returned after pipe collision")
	}
	if !errors.Is(err, ErrPipeBusy) {
		t.Fatalf("error = %v, want ErrPipeBusy", err)
	}
	var busy *PipeBusyError
	if !errors.As(err, &busy) {
		t.Fatalf("error type = %T, want *PipeBusyError", err)
	}
	if saveCount.Load() != 0 {
		t.Fatalf("save count = %d, want 0", saveCount.Load())
	}
}

func TestUnknownListenFailureUsesRunningProbe(t *testing.T) {
	cfg := testConfig()
	listenErr := errors.New("synthetic listen failure")
	deps := testDependencies()
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return nil, listenErr
	}
	deps.isIPCRunning = func(context.Context) bool {
		return true
	}

	_, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if !errors.Is(err, ErrPipeBusy) {
		t.Fatalf("error = %v, want ErrPipeBusy", err)
	}
	if !errors.Is(err, listenErr) {
		t.Fatalf("error = %v, want wrapped listener error", err)
	}
}

func TestSaveFailureReleasesClaimWithoutStarting(t *testing.T) {
	cfg := testConfig()
	saveErr := errors.New("synthetic save failure")
	server := &recordingServer{}
	deps := testDependencies()
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return server, nil
	}
	deps.saveConfig = func(string, config.Config) error {
		return saveErr
	}
	deps.startCore = func(*appcore.Core, context.Context) error {
		t.Fatal("core started after persistence failure")
		return nil
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if runtime != nil {
		t.Fatal("runtime returned after persistence failure")
	}
	if !errors.Is(err, saveErr) {
		t.Fatalf("error = %v, want save failure", err)
	}
	if server.closeCount.Load() != 1 {
		t.Fatalf("pipe close count = %d, want 1", server.closeCount.Load())
	}
}

func TestIPCHandlerWaitsUntilStart(t *testing.T) {
	cfg := testConfig()
	deps := testDependencies()
	var handler ipc.Handler
	deps.listenIPC = func(candidate ipc.Handler) (primaryIPCServer, error) {
		handler = candidate
		return &recordingServer{}, nil
	}
	deps.startCore = func(*appcore.Core, context.Context) error {
		return nil
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	defer runtime.Close()

	request, err := ipc.NewRequest("wait-test", "ping", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	type handlerResult struct {
		value any
		err   error
	}
	result := make(chan handlerResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() {
		value, callErr := handler(ctx, request)
		result <- handlerResult{value: value, err: callErr}
	}()

	select {
	case premature := <-result:
		t.Fatalf("handler completed before Start: %+v", premature)
	case <-time.After(30 * time.Millisecond):
	}

	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	select {
	case completed := <-result:
		if completed.err != nil {
			t.Fatalf("handler error: %v", completed.err)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not complete after Start")
	}
}

func TestSubscribeBeforeStartCapturesInitialHTTPState(t *testing.T) {
	device := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != compatibility.StateEndpoint("get") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ON"))
	}))
	defer device.Close()

	cfg := testConfig()
	cfg.Device.BaseURL = device.URL
	deps := testDependencies()
	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	defer runtime.Close()

	events, unsubscribe := runtime.Subscribe(8)
	defer unsubscribe()
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == model.EventSnapshot && event.Snapshot.State == model.StateOn {
				return
			}
		case <-deadline:
			t.Fatalf("initial state was not observed; snapshot = %+v", runtime.Snapshot())
		}
	}
}

func TestStartFailureClosesRuntimeAndCallLifecycle(t *testing.T) {
	cfg := testConfig()
	startErr := errors.New("synthetic start failure")
	server := &recordingServer{}
	deps := testDependencies()
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return server, nil
	}
	deps.startCore = func(*appcore.Core, context.Context) error {
		return startErr
	}
	var stopCount atomic.Int32
	deps.stopCore = func(*appcore.Core) {
		stopCount.Add(1)
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}
	request, _ := ipc.NewRequest("lifecycle", "status", nil)
	if _, err := runtime.Call(context.Background(), request); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("Call before Start error = %v, want ErrNotStarted", err)
	}
	if err := runtime.Start(context.Background()); !errors.Is(err, startErr) {
		t.Fatalf("Start error = %v, want start failure", err)
	}
	if !runtime.Closed() {
		t.Fatal("runtime remained open after Start failure")
	}
	if _, err := runtime.Call(context.Background(), request); !errors.Is(err, ErrClosed) {
		t.Fatalf("Call after failed Start error = %v, want ErrClosed", err)
	}
	if server.closeCount.Load() != 1 {
		t.Fatalf("pipe close count = %d, want 1", server.closeCount.Load())
	}
	if stopCount.Load() != 1 {
		t.Fatalf("core stop count = %d, want 1", stopCount.Load())
	}
}

func TestCloseIsConcurrentAndIdempotent(t *testing.T) {
	cfg := testConfig()
	server := &recordingServer{}
	deps := testDependencies()
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return server, nil
	}
	var stopCount atomic.Int32
	deps.stopCore = func(*appcore.Core) {
		stopCount.Add(1)
	}

	runtime, err := newWithDependencies(Options{
		ConfigPath: testConfigPath(t),
		Config:     &cfg,
	}, deps)
	if err != nil {
		t.Fatalf("newWithDependencies: %v", err)
	}

	const callers = 16
	var workers sync.WaitGroup
	workers.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer workers.Done()
			_ = runtime.Close()
		}()
	}
	workers.Wait()
	if server.closeCount.Load() != 1 {
		t.Fatalf("pipe close count = %d, want 1", server.closeCount.Load())
	}
	if stopCount.Load() != 1 {
		t.Fatalf("core stop count = %d, want 1", stopCount.Load())
	}
}

func TestNewRejectsUnresolvedConfigPath(t *testing.T) {
	cfg := testConfig()
	deps := testDependencies()
	var listenCount atomic.Int32
	deps.listenIPC = func(ipc.Handler) (primaryIPCServer, error) {
		listenCount.Add(1)
		return &recordingServer{}, nil
	}
	_, err := newWithDependencies(Options{
		ConfigPath: "relative-config.json",
		Config:     &cfg,
	}, deps)
	if err == nil {
		t.Fatal("expected unresolved-path error")
	}
	if listenCount.Load() != 0 {
		t.Fatalf("listen count = %d, want 0", listenCount.Load())
	}
}
