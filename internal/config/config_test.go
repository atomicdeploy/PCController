package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"rayanlamp.local/app/internal/winfx"
)

func TestDefault(t *testing.T) {
	first := Default()
	second := Default()

	if first.Lamp.BaseURL != DefaultBaseURL {
		t.Fatalf("BaseURL = %q, want %q", first.Lamp.BaseURL, DefaultBaseURL)
	}
	if !first.Lamp.WebSocket || first.Lamp.RequestTimeout.Value() != 5*time.Second {
		t.Fatalf("unexpected lamp defaults: %+v", first.Lamp)
	}
	if !first.UI.CloseToTray || !first.UI.PrewarmTrayMenu ||
		!first.UI.ShowTaskbar || !first.UI.Animations ||
		!first.UI.RoundedCorners || !first.UI.LayeredForeground ||
		first.UI.ShowTitleBar || first.UI.MagneticSnap ||
		first.UI.MagneticSnapDistance != 16 ||
		first.UI.TitleBarMode != TitleBarNone ||
		first.UI.BorderMode != BorderAuto ||
		first.UI.RenderBackend != RenderBackendAuto ||
		first.UI.SettingsTab != 0 ||
		!first.UI.WindowVisible ||
		first.UI.WindowState != WindowStateNormal ||
		first.UI.ViewMode != ViewCompact ||
		first.UI.WindowBounds.Width != CompactWindowWidth ||
		first.UI.WindowBounds.Height != CompactWindowHeight ||
		first.UI.SettingsBounds.X != -1 ||
		first.UI.SettingsBounds.Y != -1 ||
		first.UI.SettingsBounds.Width != SettingsWindowWidth ||
		first.UI.SettingsBounds.Height != SettingsWindowHeight {
		t.Fatalf("unexpected UI defaults: %+v", first.UI)
	}
	if first.Notifications.InitialSync || !first.Notifications.CustomMessage ||
		first.Notifications.Delivery != "auto" ||
		first.Notifications.SoundSource != SoundSourceBundled ||
		first.Notifications.SoundFile != "" ||
		first.Notifications.SoundName != "" {
		t.Fatalf("unexpected notification defaults: %+v", first.Notifications)
	}
	if !first.Integration.LocalAPIEnabled ||
		first.Integration.LocalAPIListen != DefaultLocalAPIListen {
		t.Fatalf("unexpected integration defaults: %+v", first.Integration)
	}
	if first.Integration.LocalAPIToken == second.Integration.LocalAPIToken {
		t.Fatal("Default generated duplicate API tokens")
	}
	raw, err := base64.RawURLEncoding.DecodeString(first.Integration.LocalAPIToken)
	if err != nil {
		t.Fatalf("token is not raw URL base64: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("decoded token length = %d, want 32", len(raw))
	}
	if !reflect.DeepEqual(first.Diagnostics.SafeEndpoints, SafeDiagnosticEndpoints()) ||
		!reflect.DeepEqual(first.Diagnostics.ExpertEndpoints, ExpertDiagnosticEndpoints()) {
		t.Fatalf("unexpected diagnostics defaults: %+v", first.Diagnostics)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("Default does not validate: %v", err)
	}
}

func TestDurationJSON(t *testing.T) {
	type value struct {
		Delay Duration `json:"delay"`
	}
	encoded, err := json.Marshal(value{Delay: Duration(1500 * time.Millisecond)})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"delay":"1.5s"}` {
		t.Fatalf("encoded duration = %s", encoded)
	}

	var decoded value
	if err := json.Unmarshal([]byte(`{"delay":"2m3s"}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Delay.Value() != 2*time.Minute+3*time.Second {
		t.Fatalf("decoded duration = %v", decoded.Delay)
	}
	if err := json.Unmarshal([]byte(`{"delay":2500}`), &decoded); err == nil {
		t.Fatal("numeric duration unexpectedly accepted")
	}
}

func TestNotificationSoundSelectionNormalizesAndValidates(t *testing.T) {
	const assetID = "0123456789abcdef0123456789abcdef" +
		"0123456789abcdef0123456789abcdef"

	cfg := Default()
	cfg.Notifications.SoundSource = " CUSTOM "
	cfg.Notifications.SoundFile = assetID
	cfg.Notifications.SoundName = "  Studio chime.wav  "
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.SoundSource != SoundSourceCustom ||
		cfg.Notifications.SoundFile != assetID ||
		cfg.Notifications.SoundName != "Studio chime.wav" {
		t.Fatalf(
			"normalized notification sound = %+v",
			cfg.Notifications,
		)
	}

	tests := []struct {
		name   string
		source string
		file   string
		label  string
		want   string
	}{
		{
			name:   "source",
			source: "network",
			want:   "notifications.sound_source",
		},
		{
			name:   "custom missing asset",
			source: SoundSourceCustom,
			want:   "notifications.sound_file",
		},
		{
			name:   "short asset",
			source: SoundSourceCustom,
			file:   "0123",
			want:   "notifications.sound_file",
		},
		{
			name:   "uppercase asset",
			source: SoundSourceCustom,
			file:   strings.ToUpper(assetID),
			want:   "notifications.sound_file",
		},
		{
			name:   "non-hex asset",
			source: SoundSourceCustom,
			file:   strings.Repeat("g", 64),
			want:   "notifications.sound_file",
		},
		{
			name:   "control in name",
			source: SoundSourceBundled,
			label:  "bad\nname",
			want:   "notifications.sound_name",
		},
		{
			name:   "long name",
			source: SoundSourceBundled,
			label:  strings.Repeat("x", 256),
			want:   "notifications.sound_name",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := Default()
			candidate.Notifications.SoundSource = test.source
			candidate.Notifications.SoundFile = test.file
			candidate.Notifications.SoundName = test.label
			err := candidate.Normalize()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Normalize() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestResolvePathPrecedence(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	t.Setenv(EnvConfigPath, filepath.Join(root, "environment.json"))

	cli, err := ResolvePath(filepath.Join(root, "cli.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cli != filepath.Join(root, "cli.json") {
		t.Fatalf("CLI path = %q", cli)
	}

	environment, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	if environment != filepath.Join(root, "environment.json") {
		t.Fatalf("environment path = %q", environment)
	}

	t.Setenv(EnvConfigPath, "")
	fallback, err := ResolvePath("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "appdata", "RayanLamp", "config.json")
	if fallback != want {
		t.Fatalf("fallback path = %q, want %q", fallback, want)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lamp.BaseURL != DefaultBaseURL || cfg.Integration.LocalAPIToken == "" {
		t.Fatalf("missing file did not return usable defaults: %+v", cfg)
	}
}

func TestLoadOverlayAndNormalize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
	  "lamp": {
	    "base_url": " HTTP://RAYANLAMP.LOCAL/ ",
	    "request_timeout": "8s"
	  },
	  "ui": {
	    "theme": " DARK ",
	    "backdrop": " ACRYLIC "
	  },
	  "notifications": {
	    "delivery": " BALLOON "
	  },
	  "hotkeys": {
	    "toggle": " Ctrl + Alt + K "
	  },
	  "integration": {
	    "local_api_enabled": false,
	    "local_api_listen": "localhost:49999",
	    "local_api_token": "0123456789abcdef0123456789abcdef"
	  },
	  "diagnostics": {
	    "expert_enabled": true
	  }
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lamp.BaseURL != DefaultBaseURL {
		t.Fatalf("normalized URL = %q", cfg.Lamp.BaseURL)
	}
	if cfg.Lamp.RequestTimeout.Value() != 8*time.Second || !cfg.Lamp.WebSocket {
		t.Fatalf("overlay lost lamp defaults: %+v", cfg.Lamp)
	}
	if cfg.UI.Theme != "dark" || cfg.UI.Backdrop != "acrylic" ||
		cfg.UI.ViewMode != ViewCompact || !cfg.UI.CloseToTray ||
		!cfg.UI.RoundedCorners || !cfg.UI.LayeredForeground ||
		cfg.UI.ShowTitleBar || cfg.UI.TitleBarMode != TitleBarNone ||
		cfg.UI.BorderMode != BorderAuto {
		t.Fatalf("unexpected normalized UI: %+v", cfg.UI)
	}
	if cfg.Notifications.Delivery != "balloon" || !cfg.Notifications.Enabled {
		t.Fatalf("unexpected notifications: %+v", cfg.Notifications)
	}
	if cfg.Hotkeys.Toggle != "Ctrl+Alt+K" || cfg.Hotkeys.On == "" {
		t.Fatalf("unexpected hotkeys: %+v", cfg.Hotkeys)
	}
	if cfg.Integration.LocalAPIEnabled ||
		cfg.Integration.LocalAPIListen != "localhost:49999" {
		t.Fatalf("unexpected integration: %+v", cfg.Integration)
	}
	if cfg.Diagnostics.ExpertEnabled || len(cfg.Diagnostics.ExpertEndpoints) != 0 ||
		!reflect.DeepEqual(cfg.Diagnostics.SafeEndpoints, SafeDiagnosticEndpoints()) {
		t.Fatalf("unexpected diagnostics overlay: %+v", cfg.Diagnostics)
	}
}

func TestWindowsAutomationScriptsNormalizeAndValidate(t *testing.T) {
	t.Setenv("RAYANLAMP_AUTOMATION_TEST_ROOT", filepath.Join("C:", "Scripts"))

	cfg := Default()
	cfg.Automation.LockScript = ` ${RAYANLAMP_AUTOMATION_TEST_ROOT}\lock.ps1 `
	cfg.Automation.UnlockScript = ` ${RAYANLAMP_AUTOMATION_TEST_ROOT}\unlock.ps1 `
	cfg.Automation.SuspendScript = ` ${RAYANLAMP_AUTOMATION_TEST_ROOT}\suspend.ps1 `
	cfg.Automation.ResumeScript = ` ${RAYANLAMP_AUTOMATION_TEST_ROOT}\resume.ps1 `
	cfg.Automation.NetworkChangeScript =
		` ${RAYANLAMP_AUTOMATION_TEST_ROOT}\network.ps1 `
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join("C:", "Scripts")
	want := []string{
		filepath.Join(root, "lock.ps1"),
		filepath.Join(root, "unlock.ps1"),
		filepath.Join(root, "suspend.ps1"),
		filepath.Join(root, "resume.ps1"),
		filepath.Join(root, "network.ps1"),
	}
	got := []string{
		cfg.Automation.LockScript,
		cfg.Automation.UnlockScript,
		cfg.Automation.SuspendScript,
		cfg.Automation.ResumeScript,
		cfg.Automation.NetworkChangeScript,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized Windows automation scripts = %#v, want %#v", got, want)
	}

	cfg.Automation.NetworkChangeScript = "network.ps1\nmalicious"
	if err := cfg.Normalize(); err == nil ||
		!strings.Contains(err.Error(), "automation.network_change_script") {
		t.Fatalf("control-character validation error = %v", err)
	}
}

func TestViewModeNormalizationAndLegacyMigration(t *testing.T) {
	tests := []struct {
		name       string
		uiJSON     string
		wantMode   string
		wantWidth  int
		wantHeight int
	}{
		{
			name:       "missing mode old 520 dashboard",
			uiJSON:     `{"window_bounds":{"width":380,"height":520}}`,
			wantMode:   ViewDashboard,
			wantWidth:  DashboardWindowWidth,
			wantHeight: 520,
		},
		{
			name:       "missing mode old 600 dashboard",
			uiJSON:     `{"window_bounds":{"width":380,"height":600}}`,
			wantMode:   ViewDashboard,
			wantWidth:  DashboardWindowWidth,
			wantHeight: DashboardWindowHeight,
		},
		{
			name:       "missing mode compact",
			uiJSON:     `{"window_bounds":{"width":176,"height":176}}`,
			wantMode:   ViewCompact,
			wantWidth:  CompactWindowWidth,
			wantHeight: CompactWindowHeight,
		},
		{
			name:       "explicit compact preserves valid custom bounds",
			uiJSON:     `{"view_mode":" COMPACT ","window_bounds":{"width":380,"height":600}}`,
			wantMode:   ViewCompact,
			wantWidth:  380,
			wantHeight: 600,
		},
		{
			name:       "view mode only clamps compact default to dashboard minimum",
			uiJSON:     `{"view_mode":" DASHBOARD "}`,
			wantMode:   ViewDashboard,
			wantWidth:  DashboardWindowMinWidth,
			wantHeight: DashboardWindowMinHeight,
		},
		{
			name:       "explicit dashboard clamps undersized bounds",
			uiJSON:     `{"view_mode":" DASHBOARD ","window_bounds":{"width":12,"height":34}}`,
			wantMode:   ViewDashboard,
			wantWidth:  DashboardWindowMinWidth,
			wantHeight: DashboardWindowMinHeight,
		},
		{
			name:       "explicit dashboard preserves valid custom bounds",
			uiJSON:     `{"view_mode":"dashboard","window_bounds":{"width":740,"height":980}}`,
			wantMode:   ViewDashboard,
			wantWidth:  740,
			wantHeight: 980,
		},
		{
			name:       "compact clamps oversized bounds",
			uiJSON:     `{"view_mode":"compact","window_bounds":{"width":900,"height":1200}}`,
			wantMode:   ViewCompact,
			wantWidth:  CompactWindowMaxWidth,
			wantHeight: CompactWindowMaxHeight,
		},
		{
			name:       "dashboard clamps oversized bounds",
			uiJSON:     `{"view_mode":"dashboard","window_bounds":{"width":1200,"height":1600}}`,
			wantMode:   ViewDashboard,
			wantWidth:  DashboardWindowMaxWidth,
			wantHeight: DashboardWindowMaxHeight,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			content := `{"ui":` + test.uiJSON + `}`
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.UI.ViewMode != test.wantMode ||
				cfg.UI.WindowBounds.Width != test.wantWidth ||
				cfg.UI.WindowBounds.Height != test.wantHeight {
				t.Fatalf("normalized UI = %+v, want mode %q and %dx%d",
					cfg.UI, test.wantMode, test.wantWidth, test.wantHeight)
			}
		})
	}
}

func TestGlassDWMUIOptionsNormalizeAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.UI.Backdrop = " TABBED "
	cfg.UI.RoundedCorners = false
	cfg.UI.LayeredForeground = false
	cfg.UI.ShowTitleBar = true
	cfg.UI.BorderMode = " NONE "
	cfg.UI.SettingsTab = 4
	cfg.UI.WindowBounds.Width = 412
	cfg.UI.WindowBounds.Height = 333

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.Backdrop != "tabbed" ||
		loaded.UI.RoundedCorners ||
		loaded.UI.LayeredForeground ||
		!loaded.UI.ShowTitleBar ||
		loaded.UI.TitleBarMode != TitleBarCustom ||
		loaded.UI.BorderMode != BorderNone ||
		loaded.UI.SettingsTab != 4 ||
		loaded.UI.WindowBounds.Width != 412 ||
		loaded.UI.WindowBounds.Height != 333 {
		t.Fatalf("round-tripped UI = %+v", loaded.UI)
	}
}

func TestUILocaleDefaultsNormalizesAndRoundTrips(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy-locale.json")
	if err := os.WriteFile(
		legacyPath,
		[]byte(`{"ui":{"theme":"dark"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.UI.Locale != LocaleAuto {
		t.Fatalf("legacy locale = %q, want %q", legacy.UI.Locale, LocaleAuto)
	}

	path := filepath.Join(t.TempDir(), "locale.json")
	cfg := Default()
	cfg.UI.Locale = " FA "
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.Locale != LocalePersian {
		t.Fatalf("round-tripped locale = %q, want %q", loaded.UI.Locale, LocalePersian)
	}

	loaded.UI.Locale = LocaleEnglish
	if err := loaded.Normalize(); err != nil {
		t.Fatal(err)
	}
	if loaded.UI.Locale != LocaleEnglish {
		t.Fatalf("explicit English locale = %q", loaded.UI.Locale)
	}
}

func TestTitleBarModeAndSettingsTabMigrationAndRoundTrip(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy.json")
	legacy := `{"ui":{"show_title_bar":true}}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	migrated, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if migrated.UI.TitleBarMode != TitleBarCustom ||
		!migrated.UI.ShowTitleBar {
		t.Fatalf("legacy title bar migrated to %+v", migrated.UI)
	}

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.UI.TitleBarMode = TitleBarNative
	cfg.UI.ShowTitleBar = true // Normalize must make the new field authoritative.
	cfg.UI.SettingsTab = SettingsTabCount - 1
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.TitleBarMode != TitleBarNative ||
		loaded.UI.ShowTitleBar ||
		loaded.UI.SettingsTab != SettingsTabCount-1 {
		t.Fatalf("round-tripped title/tab settings = %+v", loaded.UI)
	}

	loaded.UI.SettingsTab = SettingsTabCount
	if err := loaded.Normalize(); err != nil {
		t.Fatal(err)
	}
	if loaded.UI.SettingsTab != 0 {
		t.Fatalf("out-of-range settings tab = %d, want 0", loaded.UI.SettingsTab)
	}
}

func TestSettingsBoundsLegacyDefaultClampAndRoundTrip(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy-settings-bounds.json")
	if err := os.WriteFile(
		legacyPath,
		[]byte(`{"ui":{"theme":"dark"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	wantDefault := WindowBounds{
		X:      -1,
		Y:      -1,
		Width:  SettingsWindowWidth,
		Height: SettingsWindowHeight,
	}
	if legacy.UI.SettingsBounds != wantDefault {
		t.Fatalf(
			"legacy settings bounds = %+v, want %+v",
			legacy.UI.SettingsBounds,
			wantDefault,
		)
	}

	path := filepath.Join(t.TempDir(), "settings-bounds.json")
	legacy.UI.SettingsBounds = WindowBounds{
		X:      -1440,
		Y:      120,
		Width:  1180,
		Height: 860,
	}
	if err := Save(path, legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.SettingsBounds != legacy.UI.SettingsBounds {
		t.Fatalf(
			"round-tripped settings bounds = %+v, want %+v",
			loaded.UI.SettingsBounds,
			legacy.UI.SettingsBounds,
		)
	}

	loaded.UI.SettingsBounds.Width = 1
	loaded.UI.SettingsBounds.Height = SettingsWindowMaxHeight + 1
	if err := loaded.Normalize(); err != nil {
		t.Fatal(err)
	}
	if loaded.UI.SettingsBounds.Width != SettingsWindowMinWidth ||
		loaded.UI.SettingsBounds.Height != SettingsWindowMaxHeight {
		t.Fatalf(
			"clamped settings bounds = %+v",
			loaded.UI.SettingsBounds,
		)
	}
}

func TestBorderModeDefaultMigrationNormalizationAndRoundTrip(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy-border.json")
	if err := os.WriteFile(
		legacyPath,
		[]byte(`{"ui":{"title_bar_mode":"none"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.UI.BorderMode != BorderAuto {
		t.Fatalf("missing border mode migrated to %q, want %q", legacy.UI.BorderMode, BorderAuto)
	}

	path := filepath.Join(t.TempDir(), "border.json")
	cfg := Default()
	cfg.UI.BorderMode = " NONE "
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.BorderMode != BorderNone {
		t.Fatalf("round-tripped border mode = %q, want %q", loaded.UI.BorderMode, BorderNone)
	}
}

func TestPrewarmTrayMenuDefaultsForLegacyConfigAndCanBeDisabled(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy-tray-menu.json")
	if err := os.WriteFile(
		legacyPath,
		[]byte(`{"ui":{"theme":"dark"}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.UI.PrewarmTrayMenu {
		t.Fatal("legacy config did not inherit the default-on tray-menu prewarm")
	}

	path := filepath.Join(t.TempDir(), "tray-menu.json")
	cfg := Default()
	cfg.UI.PrewarmTrayMenu = false
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.PrewarmTrayMenu {
		t.Fatal("explicitly disabled tray-menu prewarm did not round-trip")
	}
}

func TestWindowSessionStateDefaultsMigratesNormalizesAndRoundTrips(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy-window-state.json")
	if err := os.WriteFile(
		legacyPath,
		[]byte(`{"ui":{"start_minimized":true}}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	legacy, err := Load(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.UI.WindowVisible ||
		legacy.UI.WindowState != WindowStateNormal ||
		!legacy.UI.StartMinimized {
		t.Fatalf("legacy window state migrated to %+v", legacy.UI)
	}

	path := filepath.Join(t.TempDir(), "window-state.json")
	cfg := Default()
	cfg.UI.WindowVisible = false
	cfg.UI.WindowState = " MAXIMIZED "
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.WindowVisible ||
		loaded.UI.WindowState != WindowStateMaximized {
		t.Fatalf("round-tripped window state = %+v", loaded.UI)
	}

	loaded.UI.WindowState = ""
	if err := loaded.Normalize(); err != nil {
		t.Fatal(err)
	}
	if loaded.UI.WindowState != WindowStateNormal {
		t.Fatalf("empty window state = %q, want %q",
			loaded.UI.WindowState, WindowStateNormal)
	}

	loaded.UI.WindowState = "iconic"
	if err := loaded.Normalize(); err == nil ||
		!strings.Contains(err.Error(), "ui.window_state") {
		t.Fatalf("invalid window state error = %v", err)
	}
}

func TestLegacyNoneBackdropNormalizesToSolid(t *testing.T) {
	cfg := Default()
	cfg.UI.Backdrop = " NONE "
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Backdrop != BackdropSolid {
		t.Fatalf("legacy none backdrop = %q, want %q", cfg.UI.Backdrop, BackdropSolid)
	}
	cfg.UI.Backdrop = BackdropTransparent
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Backdrop != BackdropTransparent {
		t.Fatalf("transparent backdrop = %q", cfg.UI.Backdrop)
	}
}

func TestLoadRejectsUnknownAndTrailingData(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"unknown", `{"lamp":{"mystery":true}}`},
		{"trailing", `{} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid config unexpectedly loaded")
			}
		})
	}
}

func TestSaveLoadRoundTripAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	cfg := Default()
	cfg.UI.AlwaysOnTop = true
	cfg.Lamp.HTTPPoll = Duration(45 * time.Second)

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.UI.Theme = "dark"
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, cfg) {
		t.Fatalf("round trip mismatch:\nloaded: %#v\nwant:   %#v", loaded, cfg)
	}

	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config.json.*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %v", matches)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("mode = %o, want 600", got)
		}
	}
}

func TestUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := Default()
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}

	updated, err := Update(path, func(cfg *Config) error {
		cfg.UI.AlwaysOnTop = true
		cfg.Lamp.BaseURL = "http://example.local/"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.UI.AlwaysOnTop || updated.Lamp.BaseURL != "http://example.local" {
		t.Fatalf("unexpected updated config: %+v", updated)
	}

	sentinel := errors.New("stop")
	if _, err := Update(path, func(cfg *Config) error {
		cfg.UI.Theme = "light"
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("callback error = %v, want sentinel", err)
	}
	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.UI.Theme == "light" {
		t.Fatal("callback error modified persisted config")
	}
	if _, err := Update(path, nil); !errors.Is(err, ErrNilUpdate) {
		t.Fatalf("nil callback error = %v", err)
	}
}

func TestSaveSerializesWithUpdateWithoutRecursiveDeadlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := Default()
	if err := Save(path, original); err != nil {
		t.Fatal(err)
	}

	callbackEntered := make(chan struct{})
	releaseCallback := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		_, err := Update(path, func(cfg *Config) error {
			close(callbackEntered)
			<-releaseCallback
			cfg.UI.AlwaysOnTop = true
			return nil
		})
		updateDone <- err
	}()
	select {
	case <-callbackEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("Update callback did not start")
	}

	fullSave := original
	fullSave.UI.Theme = "dark"
	saveStarted := make(chan struct{})
	saveDone := make(chan error, 1)
	go func() {
		close(saveStarted)
		saveDone <- Save(path, fullSave)
	}()
	<-saveStarted
	select {
	case err := <-saveDone:
		t.Fatalf("Save completed while Update held the persistence lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseCallback)
	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Update deadlocked while persisting")
	}
	select {
	case err := <-saveDone:
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Save remained blocked after Update completed")
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.UI.Theme != "dark" {
		t.Fatalf("serialized full save did not run last: theme = %q", loaded.UI.Theme)
	}
}

func TestDiagnosticsPolicy(t *testing.T) {
	cfg := Default()
	if slices.Contains(SafeDiagnosticEndpoints(), "/datetime/get") {
		t.Fatal("PU-backed /datetime/get remained in the ESP diagnostics policy")
	}
	for _, endpoint := range SafeDiagnosticEndpoints() {
		if err := cfg.Diagnostics.ValidateRequest("GET", endpoint); err != nil {
			t.Errorf("safe endpoint %s rejected: %v", endpoint, err)
		}
	}
	if err := cfg.Diagnostics.ValidateRequest("POST", "/info"); !errors.Is(err, ErrUnsafeDiagnosticMethod) {
		t.Errorf("unsafe diagnostic method error = %v", err)
	}
	if endpoints := ExpertDiagnosticEndpoints(); len(endpoints) != 0 {
		t.Fatalf("retired PU/debug expert endpoints remain available: %v", endpoints)
	}

	cfg.Diagnostics.ExpertEnabled = true
	for _, endpoint := range []string{
		"/crash",
		"/config/ap",
		"/config/st",
		"/sessions_debug",
		"/login",
		"/update",
		"/datetime/get",
		"/pu",
		"/is_exec",
		"/page_id",
		"/eProcessState",
		"/eResponseStatus",
	} {
		if err := cfg.Diagnostics.ValidateEndpoint(endpoint); !errors.Is(err, ErrUnsafeDiagnosticEndpoint) {
			t.Errorf("unsafe endpoint %s error = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{
		"/weight/get",
		"/info?secret=true",
		"http://rayanlamp.local/info",
		"/INFO",
		"",
	} {
		if err := cfg.Diagnostics.ValidateEndpoint(endpoint); !errors.Is(err, ErrUnknownDiagnosticEndpoint) {
			t.Errorf("unknown endpoint %q error = %v", endpoint, err)
		}
	}
}

func TestDiagnosticsLegacyFixedArraysMigrateWithoutRestoringPermissions(t *testing.T) {
	cfg := Default()
	cfg.Diagnostics.SafeEndpoints = append([]string(nil), legacySafeDiagnosticEndpoints[:]...)
	cfg.Diagnostics.ExpertEndpoints = append([]string(nil), legacyExpertDiagnosticEndpoints[:]...)
	cfg.Diagnostics.ExpertEnabled = true
	if err := cfg.Normalize(); err != nil {
		t.Fatalf("migrate legacy diagnostics: %v", err)
	}
	if !slices.Equal(cfg.Diagnostics.SafeEndpoints, SafeDiagnosticEndpoints()) {
		t.Fatalf("migrated safe endpoints = %v", cfg.Diagnostics.SafeEndpoints)
	}
	if len(cfg.Diagnostics.ExpertEndpoints) != 0 || cfg.Diagnostics.ExpertEnabled {
		t.Fatalf("retired diagnostics survived migration: %+v", cfg.Diagnostics)
	}
	for _, retired := range append(
		append([]string(nil), legacyExpertDiagnosticEndpoints[:]...),
		"/datetime/get",
	) {
		if cfg.Diagnostics.EndpointAllowed(retired) {
			t.Errorf("retired endpoint became allowed: %s", retired)
		}
	}
}

func TestDiagnosticsModifiedLegacyArraysDoNotMigrate(t *testing.T) {
	cfg := Default()
	cfg.Diagnostics.SafeEndpoints = append([]string(nil), legacySafeDiagnosticEndpoints[:]...)
	cfg.Diagnostics.SafeEndpoints[0] = "/config/st"
	if err := cfg.Normalize(); err == nil {
		t.Fatal("modified legacy safe array was accepted")
	}

	cfg = Default()
	cfg.Diagnostics.ExpertEndpoints = append([]string(nil), legacyExpertDiagnosticEndpoints[:]...)
	cfg.Diagnostics.ExpertEndpoints[0] = "/config/ap"
	if err := cfg.Normalize(); err == nil {
		t.Fatal("modified legacy expert array was accepted")
	}
}

func TestDiagnosticsAllowlistCannotBeExpanded(t *testing.T) {
	cfg := Default()
	cfg.Diagnostics.SafeEndpoints[0] = "/crash"
	if err := cfg.Validate(); err == nil ||
		!strings.Contains(err.Error(), "diagnostics.safe_endpoints") {
		t.Fatalf("mutated allowlist error = %v", err)
	}
}

func TestNormalizeHotkeysCanonicalizesAliasesAndModifierOrder(t *testing.T) {
	hotkeys := HotkeysConfig{
		Toggle: " meta + shift + control + alt + prtsc ",
		On:     "CONTROL + pgup",
		Off:    "ALT + media_play_pause",
		Show:   "windows + context_menu",
	}

	if err := normalizeHotkeys(&hotkeys); err != nil {
		t.Fatalf("normalizeHotkeys() error = %v", err)
	}

	want := HotkeysConfig{
		Toggle: "Ctrl+Alt+Shift+Win+PrintScreen",
		On:     "Ctrl+PageUp",
		Off:    "Alt+MediaPlayPause",
		Show:   "Win+Apps",
	}
	if hotkeys != want {
		t.Fatalf("normalizeHotkeys() = %+v, want %+v", hotkeys, want)
	}
}

func TestNormalizeHotkeysRejectsSemanticDuplicateAliases(t *testing.T) {
	hotkeys := HotkeysConfig{
		Toggle: "Control+Meta+PrtSc",
		On:     "windows + snapshot + ctrl",
	}

	err := normalizeHotkeys(&hotkeys)
	if err == nil {
		t.Fatal("normalizeHotkeys() accepted semantically duplicate hotkeys")
	}
	if !strings.Contains(err.Error(), "hotkeys.on: duplicates hotkeys.toggle") {
		t.Fatalf("normalizeHotkeys() error = %v, want duplicate context", err)
	}
}

func TestNormalizeHotkeysRejectsInvalidKey(t *testing.T) {
	hotkeys := HotkeysConfig{Toggle: "Ctrl+DefinitelyNotAKey"}

	err := normalizeHotkeys(&hotkeys)
	if !errors.Is(err, winfx.ErrInvalidHotkey) {
		t.Fatalf("normalizeHotkeys() error = %v, want ErrInvalidHotkey", err)
	}
	if !strings.Contains(err.Error(), "hotkeys.toggle") {
		t.Fatalf("normalizeHotkeys() error = %v, want field context", err)
	}
}

func TestNormalizeHotkeysRejectsReservedF12(t *testing.T) {
	hotkeys := HotkeysConfig{Toggle: "Ctrl+Alt+F12"}

	err := normalizeHotkeys(&hotkeys)
	if !errors.Is(err, winfx.ErrReservedHotkey) {
		t.Fatalf("normalizeHotkeys() error = %v, want ErrReservedHotkey", err)
	}
	if !strings.Contains(err.Error(), "hotkeys.toggle") {
		t.Fatalf("normalizeHotkeys() error = %v, want field context", err)
	}
}

func TestNormalizeHotkeysAllowsEmptyBindings(t *testing.T) {
	hotkeys := HotkeysConfig{
		Toggle: "",
		On:     " ",
		Off:    "\t",
		Show:   "\r\n",
	}

	if err := normalizeHotkeys(&hotkeys); err != nil {
		t.Fatalf("normalizeHotkeys() error = %v", err)
	}
	if hotkeys != (HotkeysConfig{}) {
		t.Fatalf("normalizeHotkeys() = %+v, want all empty bindings", hotkeys)
	}
}

func TestValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"scheme", func(c *Config) { c.Lamp.BaseURL = "file:///tmp/lamp" }},
		{"timeout", func(c *Config) { c.Lamp.RequestTimeout = Duration(50 * time.Millisecond) }},
		{"reconnect", func(c *Config) { c.Lamp.ReconnectMax = Duration(time.Millisecond) }},
		{"theme", func(c *Config) { c.UI.Theme = "purple" }},
		{"locale", func(c *Config) { c.UI.Locale = "fa-IR" }},
		{"backdrop", func(c *Config) { c.UI.Backdrop = "glassiest" }},
		{"render backend", func(c *Config) { c.UI.RenderBackend = "vulkan" }},
		{"title bar", func(c *Config) { c.UI.TitleBarMode = "floating" }},
		{"border mode", func(c *Config) { c.UI.BorderMode = "etched" }},
		{"native transparent", func(c *Config) {
			c.UI.Backdrop = BackdropTransparent
			c.UI.TitleBarMode = TitleBarNative
		}},
		{"native borderless", func(c *Config) {
			c.UI.TitleBarMode = TitleBarNative
			c.UI.BorderMode = BorderNone
		}},
		{"view mode", func(c *Config) { c.UI.ViewMode = "cinema" }},
		{"window state", func(c *Config) { c.UI.WindowState = "fullscreen" }},
		{"snap", func(c *Config) { c.UI.MagneticSnapDistance = 1000 }},
		{"bounds x", func(c *Config) { c.UI.WindowBounds.X = 100001 }},
		{"delivery", func(c *Config) { c.Notifications.Delivery = "email" }},
		{"duplicate hotkey", func(c *Config) { c.Hotkeys.Off = c.Hotkeys.On }},
		{"remote API", func(c *Config) { c.Integration.LocalAPIListen = "0.0.0.0:47831" }},
		{"short token", func(c *Config) { c.Integration.LocalAPIToken = "short" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := Default()
			test.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("invalid config unexpectedly validated")
			}
		})
	}
}
