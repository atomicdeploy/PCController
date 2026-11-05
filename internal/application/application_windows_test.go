//go:build windows

package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"rayanlamp.local/app/internal/cli"
	"rayanlamp.local/app/internal/config"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/ui"
)

type fakePrimaryServer struct {
	mu     sync.Mutex
	closes int
}

func (s *fakePrimaryServer) Close() error {
	s.mu.Lock()
	s.closes++
	s.mu.Unlock()
	return nil
}

func (s *fakePrimaryServer) closeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closes
}

func TestStartupWindowModeForOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		options cli.Options
		want    ui.StartupWindowMode
	}{
		{
			name: "ordinary launch remembers prior session",
			want: ui.StartupRemembered,
		},
		{
			name:    "one shot background launch stays hidden",
			options: cli.Options{StartMinimized: true},
			want:    ui.StartupForceHidden,
		},
		{
			name:    "show command overrides background flag",
			options: cli.Options{Method: "window.show", StartMinimized: true},
			want:    ui.StartupForceShow,
		},
		{
			name:    "settings command is visible",
			options: cli.Options{Method: "settings.show"},
			want:    ui.StartupForceShow,
		},
		{
			name:    "diagnostics command is visible",
			options: cli.Options{Method: "diagnostics.show"},
			want:    ui.StartupForceShow,
		},
		{
			name:    "lamp control bootstraps in background",
			options: cli.Options{Method: "lamp.on"},
			want:    ui.StartupForceHidden,
		},
		{
			name:    "explicit hide bootstraps hidden",
			options: cli.Options{Method: "window.hide"},
			want:    ui.StartupForceHidden,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := startupWindowModeForOptions(test.options); got != test.want {
				t.Fatalf("startup mode = %d, want %d", got, test.want)
			}
		})
	}
}

func TestReadyIPCHandlerWaitsForHooks(t *testing.T) {
	delegateCalled := make(chan struct{}, 1)
	gate := newReadyIPCHandler(func(_ context.Context, request ipc.Request) (any, error) {
		delegateCalled <- struct{}{}
		return request.Method, nil
	})
	request := ipc.Request{ProtocolVersion: ipc.ProtocolVersion, Method: "window.show"}
	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		result, err := gate.Handle(context.Background(), request)
		done <- outcome{result: result, err: err}
	}()
	<-started

	select {
	case <-delegateCalled:
		t.Fatal("IPC request reached dispatcher before shell hooks were ready")
	case <-time.After(50 * time.Millisecond):
	}

	gate.MarkReady()
	gate.MarkReady() // Readiness publication is intentionally idempotent.
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("ready request failed: %v", got.err)
		}
		if got.result != request.Method {
			t.Fatalf("result = %#v, want %q", got.result, request.Method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("IPC request did not resume after hooks became ready")
	}
}

func TestReadyIPCHandlerHonorsStartupCancellation(t *testing.T) {
	delegateCalled := false
	gate := newReadyIPCHandler(func(context.Context, ipc.Request) (any, error) {
		delegateCalled = true
		return nil, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := gate.Handle(ctx, ipc.Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
	if delegateCalled {
		t.Fatal("canceled startup request reached dispatcher")
	}
}

func TestNewLosingGateNeverPersistsGeneratedConfig(t *testing.T) {
	oldListen := listenPrimaryIPC
	oldRunning := isPrimaryIPCRunning
	oldSave := savePrimaryConfig
	t.Cleanup(func() {
		listenPrimaryIPC = oldListen
		isPrimaryIPCRunning = oldRunning
		savePrimaryConfig = oldSave
	})

	listenErr := errors.New("test pipe already claimed")
	listenPrimaryIPC = func(ipc.Handler) (primaryIPCServer, error) {
		return nil, listenErr
	}
	isPrimaryIPCRunning = func(context.Context) bool { return true }
	saveCalls := 0
	savePrimaryConfig = func(string, config.Config) error {
		saveCalls++
		return nil
	}

	configPath := filepath.Join(t.TempDir(), "first-run", "config.json")
	app, err := New(cli.Options{ConfigPath: configPath, Timeout: time.Second})
	if app != nil {
		t.Fatal("losing startup unexpectedly returned an application")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v, want ErrAlreadyRunning", err)
	}
	if saveCalls != 0 {
		t.Fatalf("losing startup persisted configuration %d time(s)", saveCalls)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("losing startup created config: %v", statErr)
	}
}

func TestNewClosesClaimedGateWhenConfigPersistenceFails(t *testing.T) {
	oldListen := listenPrimaryIPC
	oldRunning := isPrimaryIPCRunning
	oldSave := savePrimaryConfig
	t.Cleanup(func() {
		listenPrimaryIPC = oldListen
		isPrimaryIPCRunning = oldRunning
		savePrimaryConfig = oldSave
	})

	server := &fakePrimaryServer{}
	gateClaimed := false
	listenPrimaryIPC = func(ipc.Handler) (primaryIPCServer, error) {
		gateClaimed = true
		return server, nil
	}
	isPrimaryIPCRunning = func(context.Context) bool { return false }
	saveErr := errors.New("test config persistence failure")
	savePrimaryConfig = func(string, config.Config) error {
		if !gateClaimed {
			t.Fatal("configuration persisted before single-instance gate was claimed")
		}
		return saveErr
	}

	app, err := New(cli.Options{
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Timeout:    time.Second,
	})
	if app != nil {
		t.Fatal("failed startup unexpectedly returned an application")
	}
	if !errors.Is(err, saveErr) {
		t.Fatalf("error = %v, want config persistence failure", err)
	}
	if got := server.closeCount(); got != 1 {
		t.Fatalf("claimed IPC gate closed %d times, want exactly once", got)
	}
}

func TestCloseIsConcurrentAndIdempotent(t *testing.T) {
	logger, file, err := newLogger(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{logger: logger, logFile: file}

	var callers sync.WaitGroup
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			app.Close()
		}()
	}
	callers.Wait()
	if !app.isClosed() {
		t.Fatal("application not marked closed")
	}
	if _, err := file.WriteString("after close"); err == nil {
		t.Fatal("log file remained writable after Close")
	}
}

func TestCloseWaitsForTrackedWorkers(t *testing.T) {
	logger, file, err := newLogger(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{logger: logger, logFile: file}
	if !app.startWorker() {
		t.Fatal("unable to start test worker")
	}
	release := make(chan struct{})
	go func() {
		defer app.workers.Done()
		<-release
	}()

	closed := make(chan struct{})
	go func() {
		app.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before tracked worker")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after worker exit")
	}
	if app.startWorker() {
		app.workers.Done()
		t.Fatal("worker accepted after Close")
	}
}

func TestClosedAppRejectsAPIStartAndRestart(t *testing.T) {
	logger, file, err := newLogger(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	app := &App{logger: logger, logFile: file}
	app.Close()

	if err := app.startLocalAPI(config.Default()); !errors.Is(err, ErrClosed) {
		t.Fatalf("startLocalAPI error = %v, want ErrClosed", err)
	}
	// This would dereference a nil dispatcher if a post-close restart were
	// incorrectly admitted.
	enabled := config.Default()
	enabled.Integration.LocalAPIEnabled = true
	app.scheduleLocalAPIRestart(enabled)
	if app.apiServer != nil {
		t.Fatal("post-close restart installed an API server")
	}
}

func TestLocalAPIConfigAppliedRestartsOnlyForListenerSecurityChanges(t *testing.T) {
	base := config.Default()
	base.Integration.LocalAPIEnabled = false
	app := &App{
		apiSettings:    localAPISettingsFrom(base),
		apiSettingsSet: true,
	}

	ordinary := base
	ordinary.UI.AlwaysOnTop = !ordinary.UI.AlwaysOnTop
	ordinary.Integration.StartWithWindows = !ordinary.Integration.StartWithWindows
	app.localAPIConfigApplied(ordinary)
	if revision := app.apiRevision.Load(); revision != 0 {
		t.Fatalf("ordinary config change scheduled API restart revision %d", revision)
	}

	rebound := ordinary
	rebound.Integration.LocalAPIListen = "127.0.0.1:47832"
	app.localAPIConfigApplied(rebound)
	app.workers.Wait()
	if revision := app.apiRevision.Load(); revision != 1 {
		t.Fatalf("listen change restart revision = %d, want 1", revision)
	}

	retokened := rebound
	retokened.Integration.LocalAPIToken += "-rotated"
	app.localAPIConfigApplied(retokened)
	app.workers.Wait()
	if revision := app.apiRevision.Load(); revision != 2 {
		t.Fatalf("token change restart revision = %d, want 2", revision)
	}
}

func TestConfigPreviewNeverBlocksBehindListenerRestart(t *testing.T) {
	app := &App{}
	app.apiMu.Lock()
	done := make(chan struct{})
	go func() {
		app.broadcastLocalAPIPreview(config.Default())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		app.apiMu.Unlock()
		t.Fatal("configuration preview blocked behind apiMu")
	}
	app.apiMu.Unlock()
}

func TestNewLoggerRotatesLargeLog(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	logPath := filepath.Join(dir, "RayanLamp.log")
	data := make([]byte, (2<<20)+1)
	if err := os.WriteFile(logPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	logger, file, err := newLogger(configPath)
	if err != nil {
		t.Fatal(err)
	}
	logger.Print("fresh")
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Stat(logPath + ".previous")
	if err != nil {
		t.Fatalf("rotated log: %v", err)
	}
	if previous.Size() != int64(len(data)) {
		t.Fatalf("rotated size = %d, want %d", previous.Size(), len(data))
	}
	current, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(current) == 0 {
		t.Fatal("new log is empty")
	}
}
