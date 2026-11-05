package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"rayanlamp.local/app/internal/i18n"
	"rayanlamp.local/app/internal/winfx"
)

const (
	// EnvConfigPath overrides the default configuration path when no CLI path
	// was supplied.
	EnvConfigPath = "RAYANLAMP_CONFIG"

	DefaultBaseURL        = "http://rayanlamp.local"
	DefaultLocalAPIListen = "127.0.0.1:47831"

	LocaleAuto    = string(i18n.Auto)
	LocaleEnglish = string(i18n.English)
	LocalePersian = string(i18n.Persian)

	ViewCompact   = "compact"
	ViewDashboard = "dashboard"

	WindowStateNormal    = "normal"
	WindowStateMinimized = "minimized"
	WindowStateMaximized = "maximized"

	TitleBarNone   = "none"
	TitleBarCustom = "custom"
	TitleBarNative = "native"

	BorderAuto = "auto"
	BorderNone = "none"

	RenderBackendAuto    = "auto"
	RenderBackendDirectX = "directx"
	RenderBackendLegacy  = "legacy"

	BackdropAuto        = "auto"
	BackdropSolid       = "solid"
	BackdropTransparent = "transparent"
	BackdropBlur        = "blur"
	BackdropAcrylic     = "acrylic"
	BackdropMica        = "mica"
	BackdropTabbed      = "tabbed"

	SoundSourceBundled = "bundled"
	SoundSourceCustom  = "custom"

	SettingsTabCount = 6

	CompactWindowWidth    = 176
	CompactWindowHeight   = 176
	DashboardWindowWidth  = 380
	DashboardWindowHeight = 600

	CompactWindowMinWidth  = 128
	CompactWindowMinHeight = 128
	CompactWindowMaxWidth  = 640
	CompactWindowMaxHeight = 640

	DashboardWindowMinWidth  = 340
	DashboardWindowMinHeight = 520
	DashboardWindowMaxWidth  = 900
	DashboardWindowMaxHeight = 1200

	// Settings dimensions are stored as 96-DPI logical pixels. X and Y remain
	// virtual-screen coordinates so a reopened settings window returns to the
	// same monitor while its size follows that monitor's current scale.
	SettingsWindowWidth     = 780
	SettingsWindowHeight    = 640
	SettingsWindowMinWidth  = 520
	SettingsWindowMinHeight = 340
	SettingsWindowMaxWidth  = 4096
	SettingsWindowMaxHeight = 4096

	maxConfigSize = 1 << 20
)

var (
	// ErrNilUpdate is returned when Update is called without a callback.
	ErrNilUpdate = errors.New("config: update callback is nil")

	persistenceMu sync.Mutex
)

// Duration is a time.Duration that is represented as a readable duration
// string in JSON, such as "750ms", "5s", or "1m30s".
type Duration time.Duration

func (d Duration) String() string {
	return time.Duration(d).String()
}

// Value converts d to the standard library duration type.
func (d Duration) Value() time.Duration {
	return time.Duration(d)
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	if d == nil {
		return errors.New("config: cannot unmarshal duration into nil receiver")
	}
	if string(data) == "null" {
		*d = 0
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("duration must be a quoted Go duration: %w", err)
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value, err)
	}
	*d = Duration(parsed)
	return nil
}

type Config struct {
	Lamp          LampConfig          `json:"lamp"`
	UI            UIConfig            `json:"ui"`
	Notifications NotificationsConfig `json:"notifications"`
	Hotkeys       HotkeysConfig       `json:"hotkeys"`
	Automation    AutomationConfig    `json:"automation"`
	Integration   IntegrationConfig   `json:"integration"`
	Diagnostics   DiagnosticsConfig   `json:"diagnostics"`
}

type LampConfig struct {
	BaseURL        string   `json:"base_url"`
	RequestTimeout Duration `json:"request_timeout"`
	WebSocket      bool     `json:"websocket"`
	ReconnectMin   Duration `json:"reconnect_min"`
	ReconnectMax   Duration `json:"reconnect_max"`
	HTTPPoll       Duration `json:"http_poll"`
	TelnetPort     int      `json:"telnet_port"`
}

type UIConfig struct {
	ControllerExpandedBounds WindowBounds                `json:"controller_expanded_bounds,omitempty"`
	ControllerWidgets        map[string]ControllerWidget `json:"controller_widgets,omitempty"`
	ViewMode                 string                      `json:"view_mode"`
	Theme                    string                      `json:"theme"`
	Locale                   string                      `json:"locale"`
	Backdrop                 string                      `json:"backdrop"`
	RenderBackend            string                      `json:"render_backend"`
	RoundedCorners           bool                        `json:"rounded_corners"`
	LayeredForeground        bool                        `json:"layered_foreground"`
	TitleBarMode             string                      `json:"title_bar_mode"`
	ControllerChrome         string                      `json:"controller_chrome,omitempty"`
	NativeMenus              bool                        `json:"native_menus,omitempty"`
	BorderMode               string                      `json:"border_mode"`
	SettingsTab              int                         `json:"settings_tab"`
	SettingsBounds           WindowBounds                `json:"settings_bounds"`
	// ShowTitleBar is retained for configurations written before the
	// none/custom/native title-bar selector was introduced. Normalize keeps
	// it true only for the custom in-client title bar.
	ShowTitleBar         bool         `json:"show_title_bar"`
	AlwaysOnTop          bool         `json:"always_on_top"`
	CloseToTray          bool         `json:"close_to_tray"`
	PrewarmTrayMenu      bool         `json:"prewarm_tray_menu"`
	StartMinimized       bool         `json:"start_minimized"`
	WindowVisible        bool         `json:"window_visible"`
	WindowState          string       `json:"window_state"`
	ShowTaskbar          bool         `json:"show_taskbar"`
	Animations           bool         `json:"animations"`
	MagneticSnap         bool         `json:"magnetic_snap"`
	MagneticSnapDistance int          `json:"magnetic_snap_distance"`
	WindowBounds         WindowBounds `json:"window_bounds"`
}

type ControllerWidget struct {
	Visible bool         `json:"visible"`
	Bounds  WindowBounds `json:"bounds"`
}

// WindowBounds uses -1 for X or Y to request platform-default centering.
type WindowBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type NotificationsConfig struct {
	Enabled       bool   `json:"enabled"`
	StateChanges  bool   `json:"state_changes"`
	InitialSync   bool   `json:"initial_sync"`
	CustomMessage bool   `json:"custom_message"`
	Sound         bool   `json:"sound"`
	SoundSource   string `json:"sound_source"`
	SoundFile     string `json:"sound_file"`
	SoundName     string `json:"sound_name"`
	Delivery      string `json:"delivery"`
}

type HotkeysConfig struct {
	Toggle string `json:"toggle"`
	On     string `json:"on"`
	Off    string `json:"off"`
	Show   string `json:"show"`
}

type AutomationConfig struct {
	Enabled             bool     `json:"enabled"`
	OnScript            string   `json:"on_script"`
	OffScript           string   `json:"off_script"`
	MessageScript       string   `json:"message_script"`
	LockScript          string   `json:"lock_script"`
	UnlockScript        string   `json:"unlock_script"`
	SuspendScript       string   `json:"suspend_script"`
	ResumeScript        string   `json:"resume_script"`
	NetworkChangeScript string   `json:"network_change_script"`
	WorkingDirectory    string   `json:"working_directory"`
	Timeout             Duration `json:"timeout"`
	CaptureOutput       bool     `json:"capture_output"`
}

type IntegrationConfig struct {
	StartWithWindows bool   `json:"start_with_windows"`
	LocalAPIEnabled  bool   `json:"local_api_enabled"`
	LocalAPIListen   string `json:"local_api_listen"`
	LocalAPIToken    string `json:"local_api_token"`
}

type DiagnosticsConfig struct {
	SafeEndpoints   []string `json:"safe_endpoints"`
	ExpertEnabled   bool     `json:"expert_enabled"`
	ExpertEndpoints []string `json:"expert_endpoints"`
}

func defaultConfig() Config {
	return Config{
		Lamp: LampConfig{
			BaseURL:        DefaultBaseURL,
			RequestTimeout: Duration(5 * time.Second),
			WebSocket:      true,
			ReconnectMin:   Duration(time.Second),
			ReconnectMax:   Duration(30 * time.Second),
			HTTPPoll:       Duration(30 * time.Second),
			TelnetPort:     23,
		},
		UI: UIConfig{
			ViewMode:          ViewCompact,
			Theme:             "system",
			Locale:            LocaleAuto,
			Backdrop:          BackdropAcrylic,
			RenderBackend:     RenderBackendAuto,
			RoundedCorners:    true,
			LayeredForeground: true,
			TitleBarMode:      TitleBarNone,
			BorderMode:        BorderAuto,
			SettingsTab:       0,
			SettingsBounds: WindowBounds{
				X:      -1,
				Y:      -1,
				Width:  SettingsWindowWidth,
				Height: SettingsWindowHeight,
			},
			ShowTitleBar:         false,
			AlwaysOnTop:          false,
			CloseToTray:          true,
			PrewarmTrayMenu:      true,
			StartMinimized:       false,
			WindowVisible:        true,
			WindowState:          WindowStateNormal,
			ShowTaskbar:          true,
			Animations:           true,
			MagneticSnap:         false,
			MagneticSnapDistance: 16,
			WindowBounds: WindowBounds{
				X:      -1,
				Y:      -1,
				Width:  CompactWindowWidth,
				Height: CompactWindowHeight,
			},
		},
		Notifications: NotificationsConfig{
			Enabled:       true,
			StateChanges:  true,
			InitialSync:   false,
			CustomMessage: true,
			Sound:         true,
			SoundSource:   SoundSourceBundled,
			Delivery:      "auto",
		},
		Hotkeys: HotkeysConfig{
			Toggle: "Ctrl+Alt+L",
			On:     "Ctrl+Alt+Shift+O",
			Off:    "Ctrl+Alt+Shift+F",
			Show:   "Ctrl+Alt+Shift+L",
		},
		Automation: AutomationConfig{
			Enabled:       false,
			Timeout:       Duration(15 * time.Second),
			CaptureOutput: true,
		},
		Integration: IntegrationConfig{
			StartWithWindows: false,
			LocalAPIEnabled:  true,
			LocalAPIListen:   DefaultLocalAPIListen,
		},
		Diagnostics: DiagnosticsConfig{
			SafeEndpoints:   SafeDiagnosticEndpoints(),
			ExpertEnabled:   false,
			ExpertEndpoints: ExpertDiagnosticEndpoints(),
		},
	}
}

// Default returns a complete configuration with a fresh cryptographically
// random local API bearer token.
func Default() Config {
	cfg := defaultConfig()
	token, err := NewAPIToken()
	if err != nil {
		panic(fmt.Sprintf("config: secure random source unavailable: %v", err))
	}
	cfg.Integration.LocalAPIToken = token
	return cfg
}

// NewAPIToken returns a 256-bit URL-safe bearer token.
func NewAPIToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", fmt.Errorf("generate API token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ResolvePath applies config path precedence: a non-empty CLI path,
// RAYANLAMP_CONFIG, then %AppData%\RayanLamp\config.json. Relative explicit
// paths are made absolute.
func ResolvePath(cliPath string) (string, error) {
	path := strings.TrimSpace(cliPath)
	if path == "" {
		path = strings.TrimSpace(os.Getenv(EnvConfigPath))
	}
	if path == "" {
		root := strings.TrimSpace(os.Getenv("APPDATA"))
		if root == "" {
			var err error
			root, err = os.UserConfigDir()
			if err != nil {
				return "", fmt.Errorf("resolve user config directory: %w", err)
			}
		}
		path = filepath.Join(root, "RayanLamp", "config.json")
	}

	path = filepath.Clean(os.ExpandEnv(path))
	if !filepath.IsAbs(path) {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("resolve absolute config path: %w", err)
		}
		path = absolute
	}
	return path, nil
}

// Load reads a configuration overlaid on Default, so omitted JSON fields retain
// their defaults. A missing file is not an error and returns Default.
func Load(path string) (Config, error) {
	resolved, err := ResolvePath(path)
	if err != nil {
		return Config{}, err
	}

	cfg := Default()
	file, err := os.Open(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open config %q: %w", resolved, err)
	}
	defer file.Close()

	if info, statErr := file.Stat(); statErr != nil {
		return Config{}, fmt.Errorf("stat config %q: %w", resolved, statErr)
	} else if info.Size() > maxConfigSize {
		return Config{}, fmt.Errorf("config %q exceeds %d bytes", resolved, maxConfigSize)
	}

	data, err := io.ReadAll(io.LimitReader(file, maxConfigSize+1))
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", resolved, err)
	}
	if len(data) > maxConfigSize {
		return Config{}, fmt.Errorf("config %q exceeds %d bytes", resolved, maxConfigSize)
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", resolved, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode config %q: trailing data: %w", resolved, err)
	}

	// Load overlays JSON on defaults, so clear the default only when an older
	// file omitted view_mode. normalizeUI can then distinguish a legacy
	// 380x520/380x600 dashboard from a compact configuration.
	if !jsonNestedFieldPresent(data, "ui", "view_mode") {
		cfg.UI.ViewMode = ""
	}
	// An older configuration only had show_title_bar. Clear the overlaid
	// default when title_bar_mode is absent so normalizeUI can migrate true to
	// the custom title bar and false to no title bar.
	if !jsonNestedFieldPresent(data, "ui", "title_bar_mode") {
		cfg.UI.TitleBarMode = ""
	}
	if err := cfg.Normalize(); err != nil {
		return Config{}, fmt.Errorf("validate config %q: %w", resolved, err)
	}
	return cfg, nil
}

func jsonNestedFieldPresent(data []byte, object, field string) bool {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(root[object], &nested); err != nil {
		return false
	}
	_, present := nested[field]
	return present
}

// Save normalizes and validates cfg, then atomically replaces the destination
// with a same-directory temporary file. The file is owner-only (0600 on POSIX,
// protected current-user ACL on Windows).
func Save(path string, cfg Config) error {
	persistenceMu.Lock()
	defer persistenceMu.Unlock()
	return saveUnlocked(path, cfg)
}

// saveUnlocked contains the persistence implementation shared by Save and
// Update. The caller must hold persistenceMu. Keeping this separate prevents
// Update from recursively acquiring the non-reentrant mutex while ensuring
// full saves and read/modify/write updates cannot replace each other
// concurrently.
func saveUnlocked(path string, cfg Config) error {
	resolved, err := ResolvePath(path)
	if err != nil {
		return err
	}
	if err := cfg.Normalize(); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory %q: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(resolved)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config in %q: %w", dir, err)
	}
	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if err := secureFile(tempPath); err != nil {
		_ = temp.Close()
		return fmt.Errorf("secure temporary config ACL: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("flush temporary config: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}

	if err := atomicReplace(tempPath, resolved); err != nil {
		return fmt.Errorf("replace config %q: %w", resolved, err)
	}
	keepTemp = false

	if err := secureFile(resolved); err != nil {
		return fmt.Errorf("secure config %q: %w", resolved, err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("flush config directory %q: %w", dir, err)
	}
	return nil
}

// Save writes c using the package-level atomic Save implementation.
func (c Config) Save(path string) error {
	return Save(path, c)
}

// Update serializes in-process read/modify/write operations, validates the
// callback result, and atomically persists it. Callback errors do not alter the
// existing file.
func Update(path string, callback func(*Config) error) (Config, error) {
	if callback == nil {
		return Config{}, ErrNilUpdate
	}

	persistenceMu.Lock()
	defer persistenceMu.Unlock()

	cfg, err := Load(path)
	if err != nil {
		return Config{}, err
	}
	if err := callback(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.Normalize(); err != nil {
		return Config{}, fmt.Errorf("validate updated config: %w", err)
	}
	if err := saveUnlocked(path, cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks a configuration without changing the caller's value.
func (c Config) Validate() error {
	copy := c
	return copy.Normalize()
}

// Normalize canonicalizes user-editable strings and validates all invariants.
func (c *Config) Normalize() error {
	if c == nil {
		return errors.New("config is nil")
	}

	if err := normalizeLamp(&c.Lamp); err != nil {
		return err
	}
	if err := normalizeUI(&c.UI); err != nil {
		return err
	}
	if err := normalizeNotifications(&c.Notifications); err != nil {
		return err
	}
	if err := normalizeHotkeys(&c.Hotkeys); err != nil {
		return err
	}
	if err := normalizeAutomation(&c.Automation); err != nil {
		return err
	}
	if err := normalizeIntegration(&c.Integration); err != nil {
		return err
	}
	if err := c.Diagnostics.normalize(); err != nil {
		return err
	}
	return nil
}

func normalizeAutomation(automation *AutomationConfig) error {
	automation.OnScript = strings.TrimSpace(os.ExpandEnv(automation.OnScript))
	automation.OffScript = strings.TrimSpace(os.ExpandEnv(automation.OffScript))
	automation.MessageScript = strings.TrimSpace(os.ExpandEnv(automation.MessageScript))
	automation.LockScript = strings.TrimSpace(os.ExpandEnv(automation.LockScript))
	automation.UnlockScript = strings.TrimSpace(os.ExpandEnv(automation.UnlockScript))
	automation.SuspendScript = strings.TrimSpace(os.ExpandEnv(automation.SuspendScript))
	automation.ResumeScript = strings.TrimSpace(os.ExpandEnv(automation.ResumeScript))
	automation.NetworkChangeScript = strings.TrimSpace(
		os.ExpandEnv(automation.NetworkChangeScript),
	)
	automation.WorkingDirectory = strings.TrimSpace(os.ExpandEnv(automation.WorkingDirectory))
	if automation.Timeout == 0 {
		automation.Timeout = Duration(15 * time.Second)
	}
	if automation.Timeout.Value() < time.Second || automation.Timeout.Value() > 10*time.Minute {
		return errors.New("automation.timeout: must be between 1s and 10m")
	}
	for name, path := range map[string]string{
		"on_script":             automation.OnScript,
		"off_script":            automation.OffScript,
		"message_script":        automation.MessageScript,
		"lock_script":           automation.LockScript,
		"unlock_script":         automation.UnlockScript,
		"suspend_script":        automation.SuspendScript,
		"resume_script":         automation.ResumeScript,
		"network_change_script": automation.NetworkChangeScript,
	} {
		if strings.IndexFunc(path, unicode.IsControl) >= 0 {
			return fmt.Errorf("automation.%s: control characters are not allowed", name)
		}
	}
	if strings.IndexFunc(automation.WorkingDirectory, unicode.IsControl) >= 0 {
		return errors.New("automation.working_directory: control characters are not allowed")
	}
	return nil
}

func normalizeLamp(lamp *LampConfig) error {
	rawURL := strings.TrimSpace(lamp.BaseURL)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("lamp.base_url: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("lamp.base_url: scheme must be http or https")
	}
	if parsed.Host == "" {
		return errors.New("lamp.base_url: host is required")
	}
	if parsed.User != nil {
		return errors.New("lamp.base_url: embedded credentials are not allowed")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("lamp.base_url: query and fragment are not allowed")
	}
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "/" {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimRight(parsed.Path, "/")
	}
	lamp.BaseURL = strings.TrimRight(parsed.String(), "/")

	requestTimeout := lamp.RequestTimeout.Value()
	if requestTimeout < 100*time.Millisecond || requestTimeout > 5*time.Minute {
		return errors.New("lamp.request_timeout: must be between 100ms and 5m")
	}
	reconnectMin := lamp.ReconnectMin.Value()
	reconnectMax := lamp.ReconnectMax.Value()
	if reconnectMin < 100*time.Millisecond || reconnectMin > 5*time.Minute {
		return errors.New("lamp.reconnect_min: must be between 100ms and 5m")
	}
	if reconnectMax < reconnectMin || reconnectMax > 15*time.Minute {
		return errors.New("lamp.reconnect_max: must be >= reconnect_min and <= 15m")
	}
	httpPoll := lamp.HTTPPoll.Value()
	if httpPoll < 0 || httpPoll > 24*time.Hour {
		return errors.New("lamp.http_poll: must be 0 (disabled) or <= 24h")
	}
	if httpPoll > 0 && httpPoll < time.Second {
		return errors.New("lamp.http_poll: non-zero interval must be >= 1s")
	}
	if lamp.TelnetPort == 0 {
		lamp.TelnetPort = defaultConfig().Lamp.TelnetPort
	}
	if lamp.TelnetPort < 1 || lamp.TelnetPort > 65535 {
		return errors.New("lamp.telnet_port: must be between 1 and 65535")
	}
	return nil
}

func normalizeUI(ui *UIConfig) error {
	defaults := defaultConfig().UI
	ui.ViewMode = strings.ToLower(strings.TrimSpace(ui.ViewMode))
	if ui.ViewMode == "" {
		if ui.WindowBounds.Width == DashboardWindowWidth &&
			(ui.WindowBounds.Height == 520 || ui.WindowBounds.Height == DashboardWindowHeight) {
			ui.ViewMode = ViewDashboard
		} else {
			ui.ViewMode = ViewCompact
		}
	}
	if !oneOf(ui.ViewMode, ViewCompact, ViewDashboard) {
		return errors.New("ui.view_mode: must be compact or dashboard")
	}

	ui.Theme = strings.ToLower(strings.TrimSpace(ui.Theme))
	if ui.Theme == "" {
		ui.Theme = defaults.Theme
	}
	if !oneOf(ui.Theme, "system", "light", "dark") {
		return errors.New("ui.theme: must be system, light, or dark")
	}

	ui.Locale = strings.ToLower(strings.TrimSpace(ui.Locale))
	if ui.Locale == "" {
		ui.Locale = defaults.Locale
	}
	locale, err := i18n.ParsePreference(ui.Locale)
	if err != nil {
		return errors.New("ui.locale: must be auto, en, or fa")
	}
	ui.Locale = string(locale)

	ui.Backdrop = strings.ToLower(strings.TrimSpace(ui.Backdrop))
	if ui.Backdrop == "" {
		ui.Backdrop = defaults.Backdrop
	}
	// "none" was the original spelling for a fully painted, effects-disabled
	// surface. Keep reading it while writing the clearer "solid" value.
	if ui.Backdrop == "none" || ui.Backdrop == "off" ||
		ui.Backdrop == "disabled" {
		ui.Backdrop = BackdropSolid
	}
	if !oneOf(
		ui.Backdrop,
		BackdropAuto,
		BackdropSolid,
		BackdropTransparent,
		BackdropBlur,
		BackdropAcrylic,
		BackdropMica,
		BackdropTabbed,
	) {
		return errors.New("ui.backdrop: must be auto, solid, transparent, blur, acrylic, mica, or tabbed")
	}

	ui.RenderBackend = strings.ToLower(strings.TrimSpace(ui.RenderBackend))
	if ui.RenderBackend == "" {
		ui.RenderBackend = defaults.RenderBackend
	}
	if !oneOf(
		ui.RenderBackend,
		RenderBackendAuto,
		RenderBackendDirectX,
		RenderBackendLegacy,
	) {
		return errors.New("ui.render_backend: must be auto, directx, or legacy")
	}

	ui.TitleBarMode = strings.ToLower(strings.TrimSpace(ui.TitleBarMode))
	if ui.TitleBarMode == "" ||
		(ui.TitleBarMode == TitleBarNone && ui.ShowTitleBar) {
		if ui.ShowTitleBar {
			ui.TitleBarMode = TitleBarCustom
		} else {
			ui.TitleBarMode = TitleBarNone
		}
	}
	if !oneOf(
		ui.TitleBarMode,
		TitleBarNone,
		TitleBarCustom,
		TitleBarNative,
	) {
		return errors.New("ui.title_bar_mode: must be none, custom, or native")
	}
	ui.ShowTitleBar = ui.TitleBarMode == TitleBarCustom
	if ui.Backdrop == BackdropTransparent &&
		ui.TitleBarMode == TitleBarNative {
		return errors.New("ui: transparent backdrop requires none or custom title bar")
	}

	ui.BorderMode = strings.ToLower(strings.TrimSpace(ui.BorderMode))
	if ui.BorderMode == "" {
		ui.BorderMode = defaults.BorderMode
	}
	if !oneOf(ui.BorderMode, BorderAuto, BorderNone) {
		return errors.New("ui.border_mode: must be auto or none")
	}
	if ui.BorderMode == BorderNone &&
		ui.TitleBarMode == TitleBarNative {
		return errors.New("ui: border mode none requires none or custom title bar")
	}

	if ui.SettingsTab < 0 || ui.SettingsTab >= SettingsTabCount {
		ui.SettingsTab = 0
	}
	// Configurations written before settings geometry was persisted decode
	// this object as all-zero. Treat that exact shape as "not recorded" rather
	// than as a tiny window at the virtual-screen origin.
	if ui.SettingsBounds == (WindowBounds{}) {
		ui.SettingsBounds = defaults.SettingsBounds
	} else {
		if ui.SettingsBounds.Width <= 0 {
			ui.SettingsBounds.Width = defaults.SettingsBounds.Width
		}
		if ui.SettingsBounds.Height <= 0 {
			ui.SettingsBounds.Height = defaults.SettingsBounds.Height
		}
		ui.SettingsBounds.Width = clampInt(
			ui.SettingsBounds.Width,
			SettingsWindowMinWidth,
			SettingsWindowMaxWidth,
		)
		ui.SettingsBounds.Height = clampInt(
			ui.SettingsBounds.Height,
			SettingsWindowMinHeight,
			SettingsWindowMaxHeight,
		)
	}
	if ui.SettingsBounds.X < -100000 ||
		ui.SettingsBounds.X > 100000 ||
		ui.SettingsBounds.Y < -100000 ||
		ui.SettingsBounds.Y > 100000 {
		return errors.New("ui.settings_bounds: x and y must be between -100000 and 100000")
	}

	ui.WindowState = strings.ToLower(strings.TrimSpace(ui.WindowState))
	if ui.WindowState == "" {
		ui.WindowState = defaults.WindowState
	}
	if !oneOf(
		ui.WindowState,
		WindowStateNormal,
		WindowStateMinimized,
		WindowStateMaximized,
	) {
		return errors.New("ui.window_state: must be normal, minimized, or maximized")
	}

	if ui.MagneticSnapDistance == 0 {
		ui.MagneticSnapDistance = defaults.MagneticSnapDistance
	}
	if ui.MagneticSnapDistance < 1 || ui.MagneticSnapDistance > 128 {
		return errors.New("ui.magnetic_snap_distance: must be between 1 and 128 pixels")
	}

	switch ui.ViewMode {
	case ViewDashboard:
		ui.WindowBounds.Width = clampInt(
			ui.WindowBounds.Width,
			DashboardWindowMinWidth,
			DashboardWindowMaxWidth,
		)
		ui.WindowBounds.Height = clampInt(
			ui.WindowBounds.Height,
			DashboardWindowMinHeight,
			DashboardWindowMaxHeight,
		)
	default:
		ui.WindowBounds.Width = clampInt(
			ui.WindowBounds.Width,
			CompactWindowMinWidth,
			CompactWindowMaxWidth,
		)
		ui.WindowBounds.Height = clampInt(
			ui.WindowBounds.Height,
			CompactWindowMinHeight,
			CompactWindowMaxHeight,
		)
	}
	if ui.WindowBounds.X < -100000 || ui.WindowBounds.X > 100000 ||
		ui.WindowBounds.Y < -100000 || ui.WindowBounds.Y > 100000 {
		return errors.New("ui.window_bounds: x and y must be between -100000 and 100000")
	}
	return nil
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func normalizeNotifications(notifications *NotificationsConfig) error {
	notifications.Delivery = strings.ToLower(strings.TrimSpace(notifications.Delivery))
	if notifications.Delivery == "" {
		notifications.Delivery = "auto"
	}
	if !oneOf(notifications.Delivery, "auto", "toast", "balloon") {
		return errors.New("notifications.delivery: must be auto, toast, or balloon")
	}

	notifications.SoundSource = strings.ToLower(
		strings.TrimSpace(notifications.SoundSource),
	)
	if notifications.SoundSource == "" {
		notifications.SoundSource = SoundSourceBundled
	}
	if !oneOf(
		notifications.SoundSource,
		SoundSourceBundled,
		SoundSourceCustom,
	) {
		return errors.New(
			"notifications.sound_source: must be bundled or custom",
		)
	}

	notifications.SoundFile = strings.TrimSpace(notifications.SoundFile)
	if notifications.SoundFile != strings.ToLower(notifications.SoundFile) ||
		(notifications.SoundFile != "" &&
			!validSHA256AssetID(notifications.SoundFile)) {
		return errors.New(
			"notifications.sound_file: must be a lowercase SHA-256 asset ID",
		)
	}
	if notifications.SoundSource == SoundSourceCustom &&
		notifications.SoundFile == "" {
		return errors.New(
			"notifications.sound_file: is required for custom sound source",
		)
	}

	notifications.SoundName = strings.TrimSpace(notifications.SoundName)
	if strings.IndexFunc(notifications.SoundName, unicode.IsControl) >= 0 {
		return errors.New(
			"notifications.sound_name: control characters are not allowed",
		)
	}
	if utf8.RuneCountInString(notifications.SoundName) > 255 {
		return errors.New(
			"notifications.sound_name: must not exceed 255 characters",
		)
	}
	return nil
}

func validSHA256AssetID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') &&
			(character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func normalizeHotkeys(hotkeys *HotkeysConfig) error {
	values := []struct {
		name  string
		value *string
	}{
		{"toggle", &hotkeys.Toggle},
		{"on", &hotkeys.On},
		{"off", &hotkeys.Off},
		{"show", &hotkeys.Show},
	}
	for _, item := range values {
		normalized, err := normalizeHotkey(*item.value)
		if err != nil {
			return fmt.Errorf("hotkeys.%s: %w", item.name, err)
		}
		*item.value = normalized
	}

	seen := make(map[string]string)
	for _, item := range values {
		if *item.value == "" {
			continue
		}
		key := strings.ToLower(*item.value)
		if previous, exists := seen[key]; exists {
			return fmt.Errorf("hotkeys.%s: duplicates hotkeys.%s", item.name, previous)
		}
		seen[key] = item.name
	}
	return nil
}

func normalizeHotkey(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := winfx.ParseHotkey(value)
	if err != nil {
		return "", err
	}
	if parsed.Key == 0x7B { // VK_F12 is reserved for the debugger.
		return "", winfx.ErrReservedHotkey
	}
	return parsed.String(), nil
}

func normalizeIntegration(integration *IntegrationConfig) error {
	defaults := defaultConfig().Integration
	integration.LocalAPIListen = strings.TrimSpace(integration.LocalAPIListen)
	if integration.LocalAPIListen == "" {
		integration.LocalAPIListen = defaults.LocalAPIListen
	}

	host, portText, err := net.SplitHostPort(integration.LocalAPIListen)
	if err != nil {
		return fmt.Errorf("integration.local_api_listen: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("integration.local_api_listen: port must be between 1 and 65535")
	}
	host = strings.TrimSpace(host)
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return errors.New("integration.local_api_listen: host must be loopback")
		}
		host = ip.String()
	} else {
		host = "localhost"
	}
	integration.LocalAPIListen = net.JoinHostPort(host, portText)

	integration.LocalAPIToken = strings.TrimSpace(integration.LocalAPIToken)
	if integration.LocalAPIToken == "" {
		integration.LocalAPIToken, err = NewAPIToken()
		if err != nil {
			return err
		}
	}
	if len(integration.LocalAPIToken) < 32 || len(integration.LocalAPIToken) > 256 {
		return errors.New("integration.local_api_token: must contain 32 to 256 characters")
	}
	if strings.IndexFunc(integration.LocalAPIToken, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return errors.New("integration.local_api_token: whitespace and control characters are not allowed")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
