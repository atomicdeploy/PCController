package commands

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/buildinfo"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/deviceconfig"
	"pccontroller.local/desktop/internal/ipc"
	"pccontroller.local/desktop/internal/model"
)

func newTestDispatcher(t *testing.T) (*Dispatcher, string) {
	t.Helper()
	cfg := config.Default()
	cfg.Device.BaseURL = "http://127.0.0.1:1"
	core, err := appcore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := New(core, path)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher, path
}

func request(t *testing.T, method string, params any) ipc.Request {
	t.Helper()
	req, err := ipc.NewRequest("test", method, params)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestSimpleCommandsAndHooks(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	var shown, hidden, webOpened, notified, audioPlayed bool
	var audioCue string
	var audioCount int
	dispatcher.SetHooks(Hooks{
		ShowWindow: func() { shown = true },
		HideWindow: func() { hidden = true },
		OpenWebUI:  func() { webOpened = true },
		Notify: func(_, _ string) error {
			notified = true
			return nil
		},
		PlayAudio: func(cue string, count int) error {
			audioPlayed = true
			audioCue = cue
			audioCount = count
			return nil
		},
	})

	for _, method := range []string{"ping", "status", "version", "/show", "window.hide", "web.show"} {
		if _, err := dispatcher.Handler(context.Background(), request(t, method, nil)); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if _, err := dispatcher.Handler(context.Background(), request(t, "notify", map[string]string{
		"title": "Test", "message": "Hello",
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcher.Handler(context.Background(), request(t, "audio.play", nil)); err != nil {
		t.Fatal(err)
	}
	if !shown || !hidden || !webOpened || !notified || !audioPlayed {
		t.Fatalf(
			"hooks not called: show=%v hide=%v web=%v notify=%v audio=%v",
			shown, hidden, webOpened, notified, audioPlayed,
		)
	}
	if audioCue != "message" || audioCount != 1 {
		t.Fatalf("audio args = %q, %d", audioCue, audioCount)
	}
}

func TestBuildIdentityMatchesCompileTimeEdition(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	value, err := dispatcher.Handler(
		context.Background(),
		request(t, "ping", nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	ping, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("ping result type = %T", value)
	}
	if got := ping["edition"]; got != buildinfo.Edition {
		t.Fatalf("ping edition = %#v, want %q", got, buildinfo.Edition)
	}
	if got := ping["developer_tools"]; got != buildinfo.DeveloperTools {
		t.Fatalf(
			"ping developer_tools = %#v, want %v",
			got,
			buildinfo.DeveloperTools,
		)
	}
	if got := versionResult()["edition"]; got != buildinfo.Edition {
		t.Fatalf("version edition = %q, want %q", got, buildinfo.Edition)
	}
}

func TestAudioCommandValidatesCueAndCount(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	var gotCue string
	var gotCount int
	dispatcher.SetHooks(Hooks{
		PlayAudio: func(cue string, count int) error {
			gotCue, gotCount = cue, count
			return nil
		},
	})
	result, err := dispatcher.Handler(
		context.Background(),
		request(t, "audio.play", map[string]any{
			"cue":   "ON",
			"count": 3,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotCue != "on" || gotCount != 3 {
		t.Fatalf("audio hook = %q, %d", gotCue, gotCount)
	}
	values := result.(map[string]any)
	if values["cue"] != "on" || values["count"] != 3 {
		t.Fatalf("audio result = %#v", values)
	}

	for _, params := range []any{
		map[string]any{"cue": "unknown", "count": 1},
		map[string]any{"cue": "message", "count": 0},
		map[string]any{"cue": "error", "count": 11},
		map[string]any{"cue": "on", "count": 1, "extra": true},
	} {
		_, err := dispatcher.Handler(
			context.Background(),
			request(t, "audio.play", params),
		)
		assertCommandCode(t, err, "invalid_argument")
	}
}

func TestAudioImportCommandUsesHostFileImporter(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	const path = `C:\Sounds\custom.wav`
	dispatcher.SetHooks(Hooks{
		ImportAudioFile: func(got string) (AudioImportResult, error) {
			if got != path {
				t.Fatalf("import path = %q", got)
			}
			return AudioImportResult{
				ID:   strings.Repeat("a", 64),
				Name: "custom.wav",
			}, nil
		},
	})
	result, err := dispatcher.Handler(
		context.Background(),
		request(t, "audio.import", map[string]string{"path": path}),
	)
	if err != nil {
		t.Fatal(err)
	}
	imported := result.(AudioImportResult)
	if imported.ID != strings.Repeat("a", 64) ||
		imported.Name != "custom.wav" {
		t.Fatalf("audio import result = %+v", imported)
	}
	for _, params := range []any{
		nil,
		map[string]string{"path": ""},
		map[string]any{"path": path, "extra": true},
	} {
		_, err := dispatcher.Handler(
			context.Background(),
			request(t, "audio.import", params),
		)
		assertCommandCode(t, err, "invalid_argument")
	}
}

func TestMissingShellHooksReturnUnavailable(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	for _, method := range []string{
		"window.show",
		"window.hide",
		"settings.show",
		"diagnostics.show",
		"web.show",
		"quit",
		"notifications.show",
		"audio.play",
		"audio.import",
	} {
		t.Run(method, func(t *testing.T) {
			params := any(nil)
			if method == "notifications.show" {
				params = map[string]string{"message": "test"}
			} else if method == "audio.import" {
				params = map[string]string{"path": `C:\Sounds\custom.wav`}
			}
			_, err := dispatcher.Handler(
				context.Background(),
				request(t, method, params),
			)
			assertCommandCode(t, err, "unavailable")
		})
	}
}

func TestDeviceConfigGetReturnsOnlyTypedPublicProjection(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	result, err := dispatcher.Handler(
		context.Background(),
		request(t, "device.config.get", nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := result.(deviceconfig.Snapshot)
	if !ok {
		t.Fatalf("result type = %T, want deviceconfig.Snapshot", result)
	}
	serialized, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(serialized))
	for _, forbidden := range []string{`"password"`, `"pass"`, `"raw"`, `"endpoints"`} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("device configuration exposed %q: %s", forbidden, serialized)
		}
	}
}

func TestConfigPatchPersists(t *testing.T) {
	dispatcher, path := newTestDispatcher(t)
	patch := json.RawMessage(`{"ui":{"always_on_top":true,"locale":" FA "},"device":{"http_poll":"45s"}}`)
	req := ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		Method:          "config.patch",
		Params:          patch,
	}
	result, err := dispatcher.Handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	cfg := result.(config.Config)
	if !cfg.UI.AlwaysOnTop || cfg.UI.Locale != config.LocalePersian ||
		cfg.Device.HTTPPoll.String() != "45s" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.UI.AlwaysOnTop || loaded.UI.Locale != config.LocalePersian ||
		loaded.Device.HTTPPoll.String() != "45s" {
		t.Fatalf("patch not persisted: %+v", loaded)
	}
}

func TestConfigPatchCanonicalizesTitleBarAliases(t *testing.T) {
	tests := []struct {
		name          string
		initialMode   string
		patch         string
		wantMode      string
		wantLegacyBar bool
	}{
		{
			name:          "modern none overrides stale legacy true",
			initialMode:   config.TitleBarCustom,
			patch:         `{"ui":{"title_bar_mode":"none"}}`,
			wantMode:      config.TitleBarNone,
			wantLegacyBar: false,
		},
		{
			name:          "modern native overrides stale legacy true",
			initialMode:   config.TitleBarCustom,
			patch:         `{"ui":{"title_bar_mode":"native"}}`,
			wantMode:      config.TitleBarNative,
			wantLegacyBar: false,
		},
		{
			name:          "legacy false disables native title bar",
			initialMode:   config.TitleBarNative,
			patch:         `{"ui":{"show_title_bar":false}}`,
			wantMode:      config.TitleBarNone,
			wantLegacyBar: false,
		},
		{
			name:          "legacy true selects custom controls",
			initialMode:   config.TitleBarNative,
			patch:         `{"ui":{"show_title_bar":true}}`,
			wantMode:      config.TitleBarCustom,
			wantLegacyBar: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dispatcher, path := newTestDispatcher(t)
			if _, err := config.Update(path, func(cfg *config.Config) error {
				cfg.UI.TitleBarMode = test.initialMode
				cfg.UI.ShowTitleBar = test.initialMode == config.TitleBarCustom
				return nil
			}); err != nil {
				t.Fatal(err)
			}

			result, err := dispatcher.Handler(context.Background(), ipc.Request{
				ProtocolVersion: ipc.ProtocolVersion,
				Method:          "config.patch",
				Params:          json.RawMessage(test.patch),
			})
			if err != nil {
				t.Fatal(err)
			}
			updated := result.(config.Config)
			if updated.UI.TitleBarMode != test.wantMode ||
				updated.UI.ShowTitleBar != test.wantLegacyBar {
				t.Fatalf(
					"title bar patch = (%q, %t), want (%q, %t)",
					updated.UI.TitleBarMode,
					updated.UI.ShowTitleBar,
					test.wantMode,
					test.wantLegacyBar,
				)
			}
		})
	}
}

func TestConfigPatchPreservesNewerWindowBoundsOnDisk(t *testing.T) {
	dispatcher, path := newTestDispatcher(t)
	latestBounds := config.WindowBounds{
		X: 31, Y: 41, Width: 192, Height: 204,
	}
	if _, err := config.Update(path, func(cfg *config.Config) error {
		cfg.UI.WindowBounds = latestBounds
		cfg.UI.WindowVisible = false
		cfg.UI.WindowState = config.WindowStateMaximized
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	result, err := dispatcher.Handler(context.Background(), ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		Method:          "config.patch",
		Params:          json.RawMessage(`{"notifications":{"sound":false}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated := result.(config.Config)
	if updated.UI.WindowBounds != latestBounds {
		t.Fatalf("runtime patch restored stale bounds: got %+v, want %+v",
			updated.UI.WindowBounds, latestBounds)
	}
	if updated.UI.WindowVisible ||
		updated.UI.WindowState != config.WindowStateMaximized {
		t.Fatalf("runtime patch restored stale session state: %+v", updated.UI)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.WindowBounds != latestBounds {
		t.Fatalf("persisted patch restored stale bounds: got %+v, want %+v",
			loaded.UI.WindowBounds, latestBounds)
	}
	if loaded.UI.WindowVisible ||
		loaded.UI.WindowState != config.WindowStateMaximized {
		t.Fatalf("persisted patch restored stale session state: %+v", loaded.UI)
	}
	if loaded.Notifications.Sound {
		t.Fatal("unrelated notification patch was not applied")
	}
}

func TestConfigPatchPersistsHostPreparedConfiguration(t *testing.T) {
	dispatcher, path := newTestDispatcher(t)
	prepared := false
	dispatcher.SetHooks(Hooks{
		PrepareConfig: func(candidate config.Config) (config.Config, error) {
			prepared = true
			if candidate.Notifications.SoundSource != config.SoundSourceCustom {
				t.Fatalf("prepare candidate source = %q", candidate.Notifications.SoundSource)
			}
			candidate.Notifications.SoundSource = config.SoundSourceBundled
			return candidate, nil
		},
	})
	result, err := dispatcher.Handler(context.Background(), request(
		t,
		"config.patch",
		map[string]any{
			"notifications": map[string]any{
				"sound_source": config.SoundSourceCustom,
				"sound_file": "0123456789abcdef0123456789abcdef" +
					"0123456789abcdef0123456789abcdef",
			},
		},
	))
	if err != nil {
		t.Fatal(err)
	}
	updated := result.(config.Config)
	if !prepared ||
		updated.Notifications.SoundSource != config.SoundSourceBundled ||
		dispatcher.core.Config().Notifications.SoundSource != config.SoundSourceBundled {
		t.Fatalf("prepared runtime config = %+v", updated.Notifications)
	}
	persisted, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Notifications.SoundSource != config.SoundSourceBundled {
		t.Fatalf("persisted source = %q", persisted.Notifications.SoundSource)
	}
}

func TestUnknownCommandIsCoded(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	_, err := dispatcher.Handler(context.Background(), request(t, "nope", nil))
	if err == nil {
		t.Fatal("expected error")
	}
	coded, ok := err.(ipc.CodedError)
	if !ok || coded.Code() != "not_found" {
		t.Fatalf("unexpected error: %#v", err)
	}
}

func TestWebShowHonorsRuntimeAvailabilityPreflight(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	opened := false
	dispatcher.SetHooks(Hooks{
		OpenWebUI: func() { opened = true },
		CanOpenWebUI: func() error {
			return errors.New("listener is unavailable")
		},
	})

	_, err := dispatcher.Handler(
		context.Background(),
		request(t, "web.show", nil),
	)
	assertCommandCode(t, err, "unavailable")
	if opened {
		t.Fatal("Web UI hook ran after a failed availability preflight")
	}
}

func TestNoParameterCommandsAreStrict(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	hookCalls := 0
	dispatcher.SetHooks(Hooks{
		ShowWindow:      func() { hookCalls++ },
		HideWindow:      func() { hookCalls++ },
		OpenSettings:    func() { hookCalls++ },
		OpenDiagnostics: func() { hookCalls++ },
		OpenWebUI:       func() { hookCalls++ },
		PlayAudio:       func(string, int) error { hookCalls++; return nil },
		Quit:            func() { hookCalls++ },
	})
	methods := []string{
		"ping", "status", "device.on", "device.off", "device.toggle", "device.refresh", "reconnect",
		"window.show", "window.hide", "settings.show", "diagnostics.show", "web.show",
		"config.get", "device.config.get", "audio.play", "version", "quit",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, err := dispatcher.Handler(
				context.Background(),
				request(t, method, map[string]bool{"unexpected": true}),
			)
			assertCommandCode(t, err, "invalid_argument")
		})
	}
	if hookCalls != 0 {
		t.Fatalf("hooks called for rejected commands: %d", hookCalls)
	}
}

func TestStructuredParamsRejectNullUnknownAndTrailingJSON(t *testing.T) {
	dispatcher, _ := newTestDispatcher(t)
	notified := false
	dispatcher.SetHooks(Hooks{Notify: func(_, _ string) error {
		notified = true
		return nil
	}})

	tests := []struct {
		name   string
		method string
		params json.RawMessage
		code   string
	}{
		{
			name:   "null patch",
			method: "config.patch",
			params: json.RawMessage(`null`),
			code:   "invalid_config",
		},
		{
			name:   "unknown config field",
			method: "config.patch",
			params: json.RawMessage(`{"ui":{"not_a_setting":true}}`),
			code:   "invalid_config",
		},
		{
			name:   "trailing notification object",
			method: "notifications.show",
			params: json.RawMessage(`{"message":"hello"} {}`),
			code:   "invalid_argument",
		},
		{
			name:   "unknown notification field",
			method: "notifications.show",
			params: json.RawMessage(`{"message":"hello","extra":true}`),
			code:   "invalid_argument",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := dispatcher.Handler(context.Background(), ipc.Request{
				ProtocolVersion: ipc.ProtocolVersion,
				Method:          test.method,
				Params:          test.params,
			})
			assertCommandCode(t, err, test.code)
		})
	}
	if notified {
		t.Fatal("notification hook called for invalid parameters")
	}
}

func TestDeviceFailuresAreCodedButContextErrorsRemainDetectable(t *testing.T) {
	t.Run("HTTP failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "offline", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		dispatcher := dispatcherForURL(t, server.URL, filepath.Join(t.TempDir(), "config.json"))

		_, err := dispatcher.Handler(context.Background(), request(t, "device.on", nil))
		assertCommandCode(t, err, "device_unavailable")
	})

	t.Run("deadline", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer server.Close()
		dispatcher := dispatcherForURL(t, server.URL, filepath.Join(t.TempDir(), "config.json"))

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := dispatcher.Handler(ctx, request(t, "device.on", nil))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want context deadline", err)
		}
	})
}

func TestConfigSaveFailureLeavesRuntimeAndStreamUntouched(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ON"))
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Device.BaseURL = server.URL
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	core, err := appcore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := core.Subscribe(16)
	defer unsubscribe()
	if err := core.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	waitCommandState(t, events, model.StateOn)

	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := New(core, filepath.Join(blocker, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	hookCalled := false
	dispatcher.SetHooks(Hooks{ConfigApplied: func(config.Config) { hookCalled = true }})

	previous := core.Config()
	_, err = dispatcher.Handler(context.Background(), ipc.Request{
		ProtocolVersion: ipc.ProtocolVersion,
		Method:          "config.patch",
		Params:          json.RawMessage(`{"device":{"base_url":"http://127.0.0.1:2"}}`),
	})
	if err == nil {
		t.Fatal("expected config save failure")
	}
	current := core.Config()
	if current.Device.BaseURL != previous.Device.BaseURL {
		t.Fatalf("runtime changed after failed save: %q", current.Device.BaseURL)
	}
	if snapshot := core.Snapshot(); snapshot.State != model.StateOn {
		t.Fatalf("state reset after failed save: %#v", snapshot)
	}
	if hookCalled {
		t.Fatal("ConfigApplied called after failed save")
	}
	select {
	case event := <-events:
		t.Fatalf("failed save emitted runtime event: %#v", event)
	case <-time.After(100 * time.Millisecond):
	}
}

func dispatcherForURL(t *testing.T, baseURL, path string) *Dispatcher {
	t.Helper()
	cfg := config.Default()
	cfg.Device.BaseURL = baseURL
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	core, err := appcore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := New(core, path)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher
}

func assertCommandCode(t *testing.T, err error, expected string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected coded error %q", expected)
	}
	var coded ipc.CodedError
	if !errors.As(err, &coded) || coded.Code() != expected {
		t.Fatalf("error = %#v, want code %q", err, expected)
	}
}

func waitCommandState(t *testing.T, events <-chan model.Event, expected model.OutputState) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == model.EventSnapshot && event.Snapshot.State == expected {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for state %s", expected)
		}
	}
}
