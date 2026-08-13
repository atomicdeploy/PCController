// Package appconfig owns the PC-side persistent configuration. Hardware
// settings are deliberately excluded: the controller firmware owns those in
// EEPROM and changes them only through explicit protocol commands.
package appconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/fsnotify/fsnotify"

	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/secretstore"
)

const (
	// SchemaVersion identifies the current host configuration schema.
	SchemaVersion = 1
	// DefaultWatchInterval bounds the polling fallback when file notifications
	// are unavailable.
	DefaultWatchInterval = 150 * time.Millisecond
	// The kqueue fsnotify backend enumerates existing directory entries while
	// registering a directory. An atomic configuration write can rename its
	// temporary file between ReadDir and Lstat, which is transient and safe to
	// retry with a fresh watcher.
	filesystemWatchRegistrationAttempts   = 4
	filesystemWatchRegistrationRetryDelay = 10 * time.Millisecond
)

// Config is the persistent host-side configuration root; it never mirrors or
// replaces the MCU's EEPROM-owned settings.
type Config struct {
	Schema        int               `json:"schema"`
	Connection    Connection        `json:"connection"`
	UI            UI                `json:"ui"`
	IPC           IPC               `json:"ipc"`
	Integrations  Integrations      `json:"integrations"`
	Safety        Safety            `json:"safety"`
	RF            RFConfig          `json:"rf"`
	HostMenus     HostMenuConfig    `json:"host_menus"`
	OSActions     hostos.Policy     `json:"os_actions"`
	Paths         Paths             `json:"paths"`
	Programming   Programming       `json:"programming"`
	Scripts       map[string]string `json:"scripts,omitempty"`
	Macros        []Macro           `json:"macros,omitempty"`
	Melodies      []Melody          `json:"melodies,omitempty"`
	StatusEffects []StatusLEDEffect `json:"status_effects,omitempty"`
	Automations   []Automation      `json:"automations,omitempty"`
}

// Connection configures serial discovery, handshake timing, and reconnect behavior.
type Connection struct {
	Port               string          `json:"port,omitempty"`
	VID                string          `json:"vid,omitempty"`
	PID                string          `json:"pid,omitempty"`
	Name               string          `json:"name,omitempty"`
	BaudRate           int             `json:"baud_rate"`
	StartupWaitMS      int             `json:"startup_wait_ms"`
	RequestTimeoutMS   int             `json:"request_timeout_ms"`
	HelloAttempts      int             `json:"hello_attempts"`
	ReconnectInitialMS int             `json:"reconnect_initial_ms"`
	ReconnectMaximumMS int             `json:"reconnect_maximum_ms"`
	ResetOnReconnect   bool            `json:"reset_on_reconnect"`
	LastDevice         *DeviceIdentity `json:"last_device,omitempty"`
}

// DeviceIdentity records the last successfully connected USB serial device.
type DeviceIdentity struct {
	Port         string    `json:"port,omitempty"`
	VID          string    `json:"vid,omitempty"`
	PID          string    `json:"pid,omitempty"`
	SerialNumber string    `json:"serial_number,omitempty"`
	Name         string    `json:"name,omitempty"`
	InstanceID   string    `json:"instance_id,omitempty"`
	LastSeen     time.Time `json:"last_seen,omitempty"`
}

// UI configures host presentation, measurement visibility, and display mirroring.
type UI struct {
	AppTitle               string                            `json:"app_title"`
	Tagline                string                            `json:"tagline"`
	Appearance             Appearance                        `json:"appearance"`
	TUIConsole             TUIConsole                        `json:"tui_console"`
	SeparatePortButtons    bool                              `json:"separate_port_buttons"`
	TableLayout            string                            `json:"table_layout"`
	PeripheralNames        map[string]string                 `json:"peripheral_names,omitempty"`
	PeripheralPresentation map[string]PeripheralPresentation `json:"peripheral_presentation,omitempty"`
	SetupComplete          bool                              `json:"setup_complete"`
	WelcomeMelody          string                            `json:"welcome_melody"`
	StatusIntervalMS       int                               `json:"status_interval_ms"`
	IdleStatusIntervalMS   int                               `json:"idle_status_interval_ms"`
	EventLogLimit          int                               `json:"event_log_limit"`
	HistoryHours           int                               `json:"history_hours"`
	HistorySampleMS        int                               `json:"history_sample_ms"`
	VoltageDecimals        int                               `json:"voltage_decimals"`
	CurrentDecimals        int                               `json:"current_decimals"`
	PowerDecimals          int                               `json:"power_decimals"`
	TemperatureDecimals    int                               `json:"temperature_decimals"`
	ShowSupplyVoltage      bool                              `json:"show_supply_voltage"`
	ShowBusVoltage         bool                              `json:"show_bus_voltage"`
	ShowCurrent            bool                              `json:"show_current"`
	ShowPower              bool                              `json:"show_power"`
	ShowTemperatureLED     bool                              `json:"show_temperature_led"`
	ShowTemperatureBT      bool                              `json:"show_temperature_bt"`
	ShowIO                 bool                              `json:"show_io"`
	ShowDiagnostics        bool                              `json:"show_diagnostics"`
	ShowGraphs             bool                              `json:"show_graphs"`
	LCDServiceEnabled      bool                              `json:"lcd_service_enabled"`
	MirrorPromptToLCD      bool                              `json:"mirror_prompt_to_lcd"`
	LCDPromptDebounceMS    int                               `json:"lcd_prompt_debounce_ms"`
	LCDPriorityHoldMS      int                               `json:"lcd_priority_hold_ms"`
	SegmentScroll          SegmentScroll                     `json:"segment_scroll"`
}

// TUIConsole contains local classic-console presentation preferences. These
// settings are intentionally host-only: remote/SSH terminals retain control of
// their own dimensions and font.
type TUIConsole struct {
	Enabled  bool   `json:"enabled"`
	Columns  int    `json:"columns"`
	Rows     int    `json:"rows"`
	FontFace string `json:"font_face"`
	FontSize int    `json:"font_size"`
}

// Appearance is the host-authoritative presentation preference shared by the
// WebUI and native surfaces. Browser storage is only a startup cache; the
// watched PC configuration remains authoritative.
type Appearance struct {
	Theme          string  `json:"theme"`
	Locale         string  `json:"locale"`
	Direction      string  `json:"direction"`
	ReduceMotion   bool    `json:"reduce_motion"`
	CompactNumbers bool    `json:"compact_numbers"`
	AudioMuted     bool    `json:"audio_muted"`
	AudioVolume    float64 `json:"audio_volume"`
}

// NormalizeAppearance canonicalizes textual preference values without
// changing explicit false, zero, or empty values. Validation decides whether
// the resulting values are supported.
func NormalizeAppearance(value Appearance) Appearance {
	value.Theme = strings.ToLower(strings.TrimSpace(value.Theme))
	value.Locale = strings.ToLower(strings.TrimSpace(value.Locale))
	value.Direction = strings.ToLower(strings.TrimSpace(value.Direction))
	if value.AudioVolume == 0 {
		value.AudioVolume = 0 // canonicalize negative zero for stable hashing.
	}
	return value
}

// IPC configures authenticated local and optional remote controller transports.
type IPC struct {
	Listen          string             `json:"listen"`
	WebSocketPath   string             `json:"websocket_path"`
	AllowRemote     bool               `json:"allow_remote"`
	AuthToken       string             `json:"auth_token,omitempty"`
	AuthTokenRef    string             `json:"auth_token_ref,omitempty"`
	RemotePrincipal string             `json:"remote_principal"`
	AllowedOrigins  []string           `json:"allowed_origins,omitempty"`
	RemotePolicy    RemoteAccessPolicy `json:"remote_policy"`
	// Socket.IO is a distinct protocol and is never advertised by the plain
	// WebSocket endpoint. This path is reserved for an explicit adapter.
	SocketIOPath string `json:"socket_io_path"`
}

// RemoteAccessPolicy grants authenticated network peers only the capabilities
// the operator selected. Loopback IPC remains the trusted primary-owner API.
// Monitoring and event subscriptions are safe defaults; every mutating or OS
// capability is opt-in.
type RemoteAccessPolicy struct {
	Read              bool `json:"read"`
	Events            bool `json:"events"`
	Messages          bool `json:"messages"`
	BoardCommands     bool `json:"board_commands"`
	HostConfiguration bool `json:"host_configuration"`
	ConnectionControl bool `json:"connection_control"`
	Reset             bool `json:"reset"`
	Programming       bool `json:"programming"`
	Shutdown          bool `json:"shutdown"`
	VirtualKeys       bool `json:"virtual_keys"`
	PowerActions      bool `json:"power_actions"`
	HostAutomations   bool `json:"host_automations"`
	BridgeCalls       bool `json:"bridge_calls"`
	Integrations      bool `json:"integrations"`
}

// DefaultRemoteAccessPolicy allows authenticated observation without granting
// any remote write, reset, programming, OS, or bridge-pivot authority.
func DefaultRemoteAccessPolicy() RemoteAccessPolicy {
	return RemoteAccessPolicy{Read: true, Events: true}
}

// Safety configures host-side guards applied before board control commands.
type Safety struct {
	// MotionDoorPolicy is a PC-side command guard, not an MCU EEPROM mirror.
	// Values are always, open, closed, and never.
	MotionDoorPolicy string `json:"motion_door_policy"`
}

// Paths locates project, script, firmware, and history resources used by the host.
type Paths struct {
	Project          string `json:"project,omitempty"`
	ScriptsDirectory string `json:"scripts_directory,omitempty"`
	FirmwareHex      string `json:"firmware_hex,omitempty"`
	HistoryFile      string `json:"history_file,omitempty"`
}

// Programming selects the host toolchain and default programming transport.
type Programming struct {
	Method          string `json:"method,omitempty"`
	FQBN            string `json:"fqbn,omitempty"`
	Programmer      string `json:"programmer,omitempty"`
	ToolchainCLI    string `json:"toolchain_cli,omitempty"`
	ToolchainConfig string `json:"toolchain_config,omitempty"`
	Avrdude         string `json:"avrdude,omitempty"`
	AvrdudeConf     string `json:"avrdude_conf,omitempty"`
}

// Macro defines a named, host-persisted sequence streamed to the MCU executor.
type Macro struct {
	ID                  byte        `json:"id"`
	Name                string      `json:"name"`
	Category            string      `json:"category,omitempty"`
	Color               string      `json:"color,omitempty"`
	Label               string      `json:"label,omitempty"`
	LCDMessage          string      `json:"lcd_message,omitempty"`
	TimingToleranceUS   uint32      `json:"timing_tolerance_us,omitempty"`
	KeepOutputsOnCancel bool        `json:"keep_outputs_on_cancel,omitempty"`
	RecordingSource     string      `json:"recording_source,omitempty"`
	CaptureDroppedSteps uint16      `json:"capture_dropped_steps,omitempty"`
	CaptureMissingSteps uint16      `json:"capture_missing_steps,omitempty"`
	CaptureImportKey    string      `json:"capture_import_key,omitempty"`
	CaptureBoard        string      `json:"capture_board,omitempty"`
	CaptureID           byte        `json:"capture_id,omitempty"`
	CaptureStartedAtUS  uint32      `json:"capture_started_at_us,omitempty"`
	Steps               []MacroStep `json:"steps"`
}

// MacroStep describes one precisely timed macro operation.
type MacroStep struct {
	// AtUS is the absolute offset from the MCU playback epoch.
	AtUS uint32 `json:"at_us,omitempty"`
	Kind string `json:"kind"`

	Target      byte   `json:"target,omitempty"`
	Value       uint16 `json:"value,omitempty"`
	DurationMS  uint16 `json:"duration_ms,omitempty"`
	FrequencyHz uint16 `json:"frequency_hz,omitempty"`
	Text        string `json:"text,omitempty"`
	Destination string `json:"destination,omitempty"`
	Code        uint32 `json:"code,omitempty"`
	Bits        byte   `json:"bits,omitempty"`
	Protocol    byte   `json:"protocol,omitempty"`
	PulseUS     uint16 `json:"pulse_us,omitempty"`
	Red         byte   `json:"red,omitempty"`
	Green       byte   `json:"green,omitempty"`
	Blue        byte   `json:"blue,omitempty"`
	Brightness  byte   `json:"brightness,omitempty"`
	Opcode      byte   `json:"opcode,omitempty"`
	PayloadHex  string `json:"payload_hex,omitempty"`
}

// Automation binds matching host or board events to ordered host-side actions.
type Automation struct {
	Name       string             `json:"name"`
	Enabled    bool               `json:"enabled"`
	CooldownMS int                `json:"cooldown_ms,omitempty"`
	Match      AutomationMatch    `json:"match"`
	Actions    []AutomationAction `json:"actions"`
}

// AutomationMatch selects the event attributes that trigger an automation.
type AutomationMatch struct {
	Kind       string  `json:"kind"`
	Lifecycle  string  `json:"lifecycle,omitempty"`
	State      string  `json:"state,omitempty"`
	Contains   string  `json:"contains,omitempty"`
	Key        byte    `json:"key,omitempty"`
	Gesture    string  `json:"gesture,omitempty"`
	Source     string  `json:"source,omitempty"`
	RFID       *byte   `json:"rf_id,omitempty"`
	RFCode     *uint32 `json:"rf_code,omitempty"`
	RFProtocol byte    `json:"rf_protocol,omitempty"`
}

// AutomationAction describes one command, macro, process, RF, key, or OS action.
type AutomationAction struct {
	Type       string      `json:"type"`
	Command    string      `json:"command,omitempty"`
	Macro      string      `json:"macro,omitempty"`
	Executable string      `json:"executable,omitempty"`
	Args       []string    `json:"args,omitempty"`
	Script     string      `json:"script,omitempty"`
	Event      string      `json:"event,omitempty"`
	RF         *RFTransmit `json:"rf,omitempty"`
	VirtualKey string      `json:"virtual_key,omitempty"`
	HoldMS     int         `json:"hold_ms,omitempty"`
	Power      string      `json:"power,omitempty"`
	Confirm    string      `json:"confirm,omitempty"`
}

// RFTransmit defines a host-configured 433 MHz transmission payload.
type RFTransmit struct {
	Code     uint32 `json:"code"`
	Bits     byte   `json:"bits"`
	Protocol byte   `json:"protocol"`
	PulseUS  uint16 `json:"pulse_us,omitempty"`
	Repeats  byte   `json:"repeats,omitempty"`
}

// Defaults returns a complete safe host configuration for a new installation.
func Defaults() Config {
	return Config{
		Schema: SchemaVersion,
		Connection: Connection{
			VID:                "1A86",
			PID:                "7523",
			Name:               "USB-SERIAL CH340",
			BaudRate:           115200,
			StartupWaitMS:      1200,
			RequestTimeoutMS:   1200,
			HelloAttempts:      3,
			ReconnectInitialMS: 1000,
			ReconnectMaximumMS: 15_000,
		},
		UI: UI{
			AppTitle: productidentity.DefaultAppTitle(),
			Tagline:  productidentity.DefaultFirstRunLine(),
			Appearance: Appearance{
				Theme: "system", Locale: "en", Direction: "auto", AudioVolume: 0.42,
			},
			TUIConsole:           productTUIConsoleDefaults(),
			TableLayout:          "compact",
			WelcomeMelody:        "notify",
			StatusIntervalMS:     200,
			IdleStatusIntervalMS: 0,
			EventLogLimit:        2000,
			HistoryHours:         24,
			HistorySampleMS:      1000,
			VoltageDecimals:      2,
			CurrentDecimals:      1,
			PowerDecimals:        2,
			TemperatureDecimals:  1,
			ShowSupplyVoltage:    true,
			ShowBusVoltage:       true,
			ShowCurrent:          true,
			ShowPower:            true,
			ShowTemperatureLED:   true,
			ShowTemperatureBT:    true,
			ShowIO:               true,
			ShowDiagnostics:      true,
			ShowGraphs:           true,
			LCDServiceEnabled:    true,
			MirrorPromptToLCD:    false,
			LCDPromptDebounceMS:  120,
			LCDPriorityHoldMS:    2000,
			SegmentScroll:        DefaultSegmentScroll(),
		},
		IPC: IPC{
			Listen:          "127.0.0.1:8787",
			WebSocketPath:   "/ipc",
			RemotePrincipal: "remote-operator",
			AllowedOrigins:  []string{"localhost:*", "127.0.0.1:*", "[::1]:*"},
			SocketIOPath:    "/socket.io/",
			RemotePolicy:    DefaultRemoteAccessPolicy(),
		},
		Safety:    Safety{MotionDoorPolicy: "always"},
		RF:        DefaultRFConfig(),
		HostMenus: DefaultHostMenus(),
		OSActions: hostos.DefaultPolicy(),
		Integrations: Integrations{
			Keyboard:     DefaultKeyboardControl(),
			Lifecycle:    DefaultLifecycleSafety(),
			StatusLED:    DefaultStatusLEDPolicy(),
			BuzzerMirror: DefaultBuzzerMirror(),
			Hotkeys: []Hotkey{
				{Name: "open-dashboard", Enabled: true, Chord: "F13", Command: "app page dashboard"},
				{Name: "open-controls", Enabled: true, Chord: "F14", Command: "app page controls"},
				{Name: "open-workbench", Enabled: true, Chord: "F15", Command: "app page workbench"},
				{Name: "open-updates", Enabled: true, Chord: "F16", Command: "app page updates"},
				{Name: "open-settings", Enabled: true, Chord: "F17", Command: "app page settings"},
				{Name: "open-events", Enabled: true, Chord: "Ctrl+Alt+P", Command: "app page events"},
				{Name: "emergency-outputs-off", Enabled: true, Chord: "Ctrl+Alt+Shift+S", Command: "relay off"},
			},
			Notifications: Notifications{
				Enabled:          true,
				DoorRunningBeep:  true,
				DoorRunningToast: true,
				ImportantKinds: []string{
					"error", "door", "warning.door-open-running", "motion", "rf.learn.ended", "hot",
				},
				Actions: []NotificationAction{
					{ID: "events", Label: "Open events", Command: "app page events"},
					{ID: "stop", Label: "Stop outputs", Command: "relay off"},
				},
			},
			DataHub: DataHub{BaseURL: "http://127.0.0.1:8080"},
		},
		Scripts:       map[string]string{},
		Macros:        []Macro{},
		Melodies:      DefaultMelodies(),
		StatusEffects: DefaultStatusLEDEffects(),
	}
}

func productTUIConsoleDefaults() TUIConsole {
	enabled, _ := strconv.ParseBool(strings.TrimSpace(productidentity.DefaultTUIConsoleEnabled))
	columns, _ := strconv.Atoi(strings.TrimSpace(productidentity.DefaultTUIConsoleColumns))
	rows, _ := strconv.Atoi(strings.TrimSpace(productidentity.DefaultTUIConsoleRows))
	fontSize, _ := strconv.Atoi(strings.TrimSpace(productidentity.DefaultTUIConsoleFontSize))
	return TUIConsole{
		Enabled: enabled, Columns: columns, Rows: rows,
		FontFace: strings.TrimSpace(productidentity.DefaultTUIConsoleFontFace), FontSize: fontSize,
	}
}

// DefaultPath returns the canonical per-user host configuration path.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user configuration directory: %w", err)
	}
	return filepath.Join(base, productidentity.ConfigDirectory, "config.json"), nil
}

// ResolvePath selects an explicit path, then the environment override, then
// the canonical per-user path.
func ResolvePath(explicit string) (string, error) {
	if explicit == "" {
		explicit = os.Getenv("PCCONTROLLER_CONFIG")
	}
	if explicit == "" {
		return DefaultPath()
	}
	absolute, err := filepath.Abs(explicit)
	if err != nil {
		return "", fmt.Errorf("resolve configuration path: %w", err)
	}
	return absolute, nil
}

// Load decodes and validates a JSON, YAML, or TOML host configuration and
// returns the exact source-content digest.
func Load(path string) (Config, [sha256.Size]byte, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, [sha256.Size]byte{}, err
	}
	// Decode over defaults so newly added optional fields receive safe values
	// without a schema migration, while explicitly configured false/zero values
	// are still honored in JSON, YAML, and TOML.
	value := Defaults()
	if err := decodeConfig(path, content, &value); err != nil {
		return Config{}, [sha256.Size]byte{}, fmt.Errorf("parse %s: %w", path, err)
	}
	value.RF = canonicalizeRFConfig(value.RF)
	value.HostMenus = normalizeHostMenus(value.HostMenus)
	value.UI.Appearance = NormalizeAppearance(value.UI.Appearance)
	value.UI.TUIConsole.FontFace = strings.TrimSpace(value.UI.TUIConsole.FontFace)
	if err := value.Validate(); err != nil {
		return Config{}, [sha256.Size]byte{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return value, sha256.Sum256(content), nil
}

// LoadOrCreate loads an existing configuration or writes validated defaults
// when the target does not yet exist.
func LoadOrCreate(path string) (Config, [sha256.Size]byte, error) {
	value, digest, err := Load(path)
	if err == nil {
		return value, digest, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Config{}, [sha256.Size]byte{}, err
	}
	value = Defaults()
	if err := Write(path, value); err != nil {
		return Config{}, [sha256.Size]byte{}, err
	}
	return Load(path)
}

// Write validates and persists a host configuration with protected file permissions.
func Write(path string, value Config) error {
	value.RF = canonicalizeRFConfig(value.RF)
	value.HostMenus = normalizeHostMenus(value.HostMenus)
	value.UI.Appearance = NormalizeAppearance(value.UI.Appearance)
	value.UI.TUIConsole.FontFace = strings.TrimSpace(value.UI.TUIConsole.FontFace)
	if err := value.Validate(); err != nil {
		return err
	}
	content, err := encodeConfig(path, value)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	temporaryName := temporary.Name()
	keep := false
	defer func() {
		_ = temporary.Close()
		if !keep {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("protect temporary configuration: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("flush temporary configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		// Windows cannot atomically replace an existing destination. The
		// watcher never writes during ordinary operation, so fall back to a
		// direct write when this is an explicit user save.
		if writeErr := os.WriteFile(path, content, 0o600); writeErr != nil {
			return fmt.Errorf("replace configuration: %w (fallback: %v)", err, writeErr)
		}
		_ = os.Remove(temporaryName)
	}
	keep = true
	return nil
}

// Validate rejects unsafe, ambiguous, or unsupported host configuration values.
func (value Config) Validate() error {
	if value.Schema != SchemaVersion {
		return fmt.Errorf("unsupported schema %d", value.Schema)
	}
	connection := value.Connection
	if connection.BaudRate < 1200 || connection.BaudRate > 2_000_000 {
		return fmt.Errorf("connection.baud_rate must be 1200..2000000")
	}
	if connection.StartupWaitMS < 0 || connection.StartupWaitMS > 30_000 {
		return fmt.Errorf("connection.startup_wait_ms must be 0..30000")
	}
	if connection.RequestTimeoutMS < 100 || connection.RequestTimeoutMS > 30_000 {
		return fmt.Errorf("connection.request_timeout_ms must be 100..30000")
	}
	if connection.HelloAttempts < 1 || connection.HelloAttempts > 10 {
		return fmt.Errorf("connection.hello_attempts must be 1..10")
	}
	if connection.ReconnectInitialMS < 100 || connection.ReconnectInitialMS > 60_000 {
		return fmt.Errorf("connection.reconnect_initial_ms must be 100..60000")
	}
	if connection.ReconnectMaximumMS < connection.ReconnectInitialMS || connection.ReconnectMaximumMS > 300_000 {
		return fmt.Errorf("connection.reconnect_maximum_ms must be reconnect_initial_ms..300000")
	}
	if title := strings.TrimSpace(value.UI.AppTitle); title == "" ||
		utf8.RuneCountInString(title) > 64 || !printableText(title) {
		return fmt.Errorf("ui.app_title must be 1..64 printable characters")
	}
	if tagline := strings.TrimSpace(value.UI.Tagline); tagline == "" ||
		utf8.RuneCountInString(tagline) > 96 || !printableText(tagline) {
		return fmt.Errorf("ui.tagline must be 1..96 printable characters")
	}
	console := value.UI.TUIConsole
	if console.Columns < 56 || console.Columns > 300 {
		return errors.New("ui.tui_console.columns must be 56..300")
	}
	if console.Rows < 18 || console.Rows > 120 {
		return errors.New("ui.tui_console.rows must be 18..120")
	}
	fontFace := strings.TrimSpace(console.FontFace)
	if fontFace == "" || len(utf16.Encode([]rune(fontFace))) > 31 || !printableText(fontFace) {
		return errors.New("ui.tui_console.font_face must be 1..31 printable UTF-16 code units")
	}
	if console.FontSize < 5 || console.FontSize > 72 {
		return errors.New("ui.tui_console.font_size must be 5..72")
	}
	appearance := NormalizeAppearance(value.UI.Appearance)
	switch appearance.Theme {
	case "system", "light", "dark":
	default:
		return errors.New("ui.appearance.theme must be system, light, or dark")
	}
	switch appearance.Locale {
	case "en", "fa":
	default:
		return errors.New("ui.appearance.locale must be en or fa")
	}
	switch appearance.Direction {
	case "auto", "ltr", "rtl":
	default:
		return errors.New("ui.appearance.direction must be auto, ltr, or rtl")
	}
	if math.IsNaN(appearance.AudioVolume) || math.IsInf(appearance.AudioVolume, 0) ||
		appearance.AudioVolume < 0 || appearance.AudioVolume > 1 {
		return errors.New("ui.appearance.audio_volume must be a finite value from 0 to 1")
	}
	if len(value.UI.PeripheralNames) > MaxPeripheralNames {
		return fmt.Errorf("ui.peripheral_names may contain at most %d entries", MaxPeripheralNames)
	}
	switch strings.ToLower(strings.TrimSpace(value.UI.TableLayout)) {
	case "compact", "expanded":
	default:
		return errors.New("ui.table_layout must be compact or expanded")
	}
	for key, name := range value.UI.PeripheralNames {
		key = strings.TrimSpace(key)
		name = strings.TrimSpace(name)
		if key == "" || len(key) > 32 || !printableASCII(key) {
			return fmt.Errorf("ui.peripheral_names key %q must be 1..32 printable ASCII bytes", key)
		}
		if name == "" || utf8.RuneCountInString(name) > 64 || !printableText(name) {
			return fmt.Errorf("ui.peripheral_names[%q] must be 1..64 printable characters", key)
		}
	}
	if len(value.UI.PeripheralPresentation) > MaxPresentedControls {
		return fmt.Errorf("ui.peripheral_presentation may contain at most %d entries", MaxPresentedControls)
	}
	seenOrders := make(map[int]string, len(value.UI.PeripheralPresentation))
	for rawKey, presentation := range value.UI.PeripheralPresentation {
		key := strings.TrimSpace(rawKey)
		if key != rawKey || !IsPresentedControlKey(key) {
			return fmt.Errorf("ui.peripheral_presentation key %q is not a canonical relay, motion side, or PWM ID", rawKey)
		}
		name := strings.TrimSpace(presentation.Name)
		if name != presentation.Name || utf8.RuneCountInString(name) > 64 || (name != "" && !printableText(name)) {
			return fmt.Errorf("ui.peripheral_presentation[%q].name must be at most 64 printable characters without surrounding whitespace", key)
		}
		description := strings.TrimSpace(presentation.Description)
		if description != presentation.Description || utf8.RuneCountInString(description) > MaxPeripheralDescriptionRunes || (description != "" && !printableText(description)) {
			return fmt.Errorf("ui.peripheral_presentation[%q].description must be at most %d printable characters without surrounding whitespace", key, MaxPeripheralDescriptionRunes)
		}
		if presentation.Order != nil {
			order := *presentation.Order
			if order < 0 || order >= MaxPresentedControls {
				return fmt.Errorf("ui.peripheral_presentation[%q].order must be 0..%d", key, MaxPresentedControls-1)
			}
			if previous, duplicate := seenOrders[order]; duplicate {
				return fmt.Errorf("ui.peripheral_presentation order %d is assigned to both %q and %q", order, previous, key)
			}
			seenOrders[order] = key
		}
	}
	if melody := strings.TrimSpace(value.UI.WelcomeMelody); melody == "" || len(melody) > 64 {
		return errors.New("ui.welcome_melody must contain 1..64 characters")
	}
	if value.UI.StatusIntervalMS < 50 || value.UI.StatusIntervalMS > 60_000 {
		return fmt.Errorf("ui.status_interval_ms must be 50..60000")
	}
	if value.UI.IdleStatusIntervalMS != 0 &&
		(value.UI.IdleStatusIntervalMS < 100 || value.UI.IdleStatusIntervalMS > 60_000) {
		return fmt.Errorf("ui.idle_status_interval_ms must be zero or 100..60000")
	}
	if value.UI.EventLogLimit < 50 || value.UI.EventLogLimit > 100_000 {
		return fmt.Errorf("ui.event_log_limit must be 50..100000")
	}
	if value.UI.HistoryHours < 0 || value.UI.HistoryHours > 720 {
		return fmt.Errorf("ui.history_hours must be 0..720")
	}
	if value.UI.HistorySampleMS < 100 || value.UI.HistorySampleMS > 60_000 {
		return fmt.Errorf("ui.history_sample_ms must be 100..60000")
	}
	if value.UI.LCDPromptDebounceMS < 20 || value.UI.LCDPromptDebounceMS > 5000 {
		return fmt.Errorf("ui.lcd_prompt_debounce_ms must be 20..5000")
	}
	if value.UI.LCDPriorityHoldMS < 250 || value.UI.LCDPriorityHoldMS > 60_000 {
		return fmt.Errorf("ui.lcd_priority_hold_ms must be 250..60000")
	}
	if err := validateSegmentScroll(value.UI.SegmentScroll); err != nil {
		return err
	}
	for name, decimals := range map[string]int{
		"voltage":     value.UI.VoltageDecimals,
		"current":     value.UI.CurrentDecimals,
		"power":       value.UI.PowerDecimals,
		"temperature": value.UI.TemperatureDecimals,
	} {
		if decimals < 0 || decimals > 6 {
			return fmt.Errorf("ui.%s_decimals must be 0..6", name)
		}
	}
	if err := value.validateIntegrations(); err != nil {
		return err
	}
	if err := validateRFConfig(value.RF); err != nil {
		return err
	}
	if err := validateHostMenus(value.HostMenus); err != nil {
		return err
	}
	if err := hostos.ValidatePolicy(value.OSActions); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(value.Safety.MotionDoorPolicy)) {
	case "always", "open", "closed", "never":
	default:
		return fmt.Errorf("safety.motion_door_policy must be always, open, closed, or never")
	}
	ids := make(map[byte]bool)
	names := make(map[string]bool)
	for index, macro := range value.Macros {
		if ids[macro.ID] {
			return fmt.Errorf("macros[%d].id %d is duplicated", index, macro.ID)
		}
		ids[macro.ID] = true
		name := strings.ToLower(strings.TrimSpace(macro.Name))
		if name == "" {
			return fmt.Errorf("macros[%d].name is required", index)
		}
		if names[name] {
			return fmt.Errorf("macros[%d].name %q is duplicated", index, macro.Name)
		}
		names[name] = true
		if len(macro.Name) > 64 || !printableASCII(macro.Name) {
			return fmt.Errorf("macros[%d].name must be at most 64 printable ASCII bytes", index)
		}
		if len(macro.Category) > 64 || !printableASCII(macro.Category) {
			return fmt.Errorf("macros[%d].category must be at most 64 printable ASCII bytes", index)
		}
		switch strings.ToLower(strings.TrimSpace(macro.Color)) {
		case "", "red", "blue", "purple", "violet", "green", "white":
		default:
			return fmt.Errorf("macros[%d].color must be red, blue, purple/violet, green, or white", index)
		}
		if macro.TimingToleranceUS > 1_000_000 {
			return fmt.Errorf("macros[%d].timing_tolerance_us must be 0..1000000", index)
		}
		if macro.CaptureImportKey != "" {
			decoded, err := hex.DecodeString(macro.CaptureImportKey)
			if err != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("macros[%d].capture_import_key must be a 64-character SHA-256 hex digest", index)
			}
		}
		if len(macro.CaptureBoard) > 256 || !printableASCII(macro.CaptureBoard) {
			return fmt.Errorf("macros[%d].capture_board must be at most 256 printable ASCII bytes", index)
		}
		if len(macro.Label) > 4 || !printableASCII(macro.Label) {
			return fmt.Errorf("macros[%d].label must be at most four printable ASCII bytes", index)
		}
		if len(macro.LCDMessage) > 40 || !printableASCII(macro.LCDMessage) {
			return fmt.Errorf("macros[%d].lcd_message must be at most 40 printable ASCII bytes", index)
		}
		// Empty definitions are valid PC-side drafts and can be filled by the
		// recorder or file watcher; playback still requires at least one step.
		if len(macro.Steps) > 65535 {
			return fmt.Errorf("macros[%d].steps must contain at most 65535 entries", index)
		}
		var previous uint32
		for stepIndex, step := range macro.Steps {
			due := step.AtUS
			if due > 0x7FFFFFFF || (stepIndex != 0 && due < previous) {
				return fmt.Errorf("macros[%d].steps[%d] offset must be ordered within 0..2147483647 us", index, stepIndex)
			}
			previous = due
			switch strings.ToLower(step.Kind) {
			case "relay":
				if step.Target > 7 || step.Value > 1 {
					return fmt.Errorf("macros[%d].steps[%d] relay requires target 0..7 and value 0..1", index, stepIndex)
				}
			case "motion", "side":
				if step.Target > 1 || step.Value > 2 {
					return fmt.Errorf("macros[%d].steps[%d] motion requires side 0..1 and motion 0..2", index, stepIndex)
				}
			case "pwm", "mosfet":
				if step.Target > 15 || step.Value > 4095 {
					return fmt.Errorf("macros[%d].steps[%d] PWM requires target 0..15 and value 0..4095", index, stepIndex)
				}
			case "relays-off", "pwm-off":
				if step.Target != 0 || step.Value != 0 {
					return fmt.Errorf("macros[%d].steps[%d] all-off target/value must be zero", index, stepIndex)
				}
			// "buzzer" remains readable for existing persisted macros; new
			// macro steps are emitted with the canonical "beep" kind.
			case "beep", "buzzer", "tone":
				frequency := step.FrequencyHz
				if frequency == 0 {
					frequency = step.Value
				}
				if (frequency == 0 && step.DurationMS != 0) ||
					(frequency != 0 && (step.DurationMS == 0 || frequency < 20 || frequency > 20000)) {
					return fmt.Errorf("macros[%d].steps[%d] beep needs either frequency/duration 0/0 (stop) or frequency 20..20000 Hz with nonzero duration_ms", index, stepIndex)
				}
			case "display", "message":
				if len(step.Text) > 40 || !printableASCII(step.Text) {
					return fmt.Errorf("macros[%d].steps[%d] display text must be at most 40 printable ASCII bytes", index, stepIndex)
				}
				switch strings.ToLower(strings.TrimSpace(step.Destination)) {
				case "", "segments", "segment", "lcd", "both":
				default:
					return fmt.Errorf("macros[%d].steps[%d].destination must be segments, lcd, or both", index, stepIndex)
				}
			case "rf", "radio":
				if step.Code == 0 || step.Bits == 0 || step.Bits > 32 || step.Protocol == 0 || step.Protocol > 12 {
					return fmt.Errorf("macros[%d].steps[%d] RF code/bits/protocol are invalid", index, stepIndex)
				}
			case "rgb", "status-led", "addressable", "ws2812", "menu", "menu-action":
				// Exact peripheral bounds are shared with the native encoder and
				// checked again immediately before playback.
			case "raw", "opcode":
				payload, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(step.PayloadHex), " ", ""))
				if err != nil || len(payload) > 48 {
					return fmt.Errorf("macros[%d].steps[%d].payload_hex must encode at most 48 bytes", index, stepIndex)
				}
			default:
				return fmt.Errorf("macros[%d].steps[%d].kind %q is unknown", index, stepIndex, step.Kind)
			}
		}
	}
	if err := validateOutputDefinitions(value.Melodies, value.StatusEffects); err != nil {
		return err
	}
	automationNames := make(map[string]bool)
	for index, automation := range value.Automations {
		name := strings.ToLower(strings.TrimSpace(automation.Name))
		if name == "" {
			return fmt.Errorf("automations[%d].name is required", index)
		}
		if automationNames[name] {
			return fmt.Errorf(
				"automations[%d].name %q is duplicated",
				index,
				automation.Name,
			)
		}
		automationNames[name] = true
		if automation.CooldownMS < 0 || automation.CooldownMS > 3_600_000 {
			return fmt.Errorf(
				"automations[%d].cooldown_ms must be 0..3600000",
				index,
			)
		}
		matchKind := strings.ToLower(strings.TrimSpace(automation.Match.Kind))
		if matchKind == "" {
			return fmt.Errorf("automations[%d].match.kind is required", index)
		}
		if strings.HasPrefix(matchKind, "automation") {
			return fmt.Errorf(
				"automations[%d] cannot match automation result events",
				index,
			)
		}
		if automation.Match.Key > 4 {
			return fmt.Errorf("automations[%d].match.key must be 0..4", index)
		}
		if source := strings.ToLower(strings.TrimSpace(
			automation.Match.Source,
		)); source != "" &&
			source != "physical" && source != "rf" && source != "host" &&
			source != "pc-keyboard" {
			return fmt.Errorf(
				"automations[%d].match.source must be physical, rf, host, or pc-keyboard",
				index,
			)
		}
		if automation.Match.RFProtocol > 12 {
			return fmt.Errorf(
				"automations[%d].match.rf_protocol must be 0..12",
				index,
			)
		}
		if gesture := strings.ToLower(strings.TrimSpace(
			automation.Match.Gesture,
		)); gesture != "" {
			switch gesture {
			case "click", "double", "double-click", "hold", "hold-start",
				"repeat", "hold-repeat", "release", "hold-release",
				"down", "up":
			default:
				return fmt.Errorf(
					"automations[%d].match.gesture %q is unknown",
					index,
					automation.Match.Gesture,
				)
			}
		}
		if len(automation.Actions) == 0 || len(automation.Actions) > 8 {
			return fmt.Errorf(
				"automations[%d].actions must contain 1..8 entries",
				index,
			)
		}
		for actionIndex, action := range automation.Actions {
			switch strings.ToLower(strings.TrimSpace(action.Type)) {
			case "board":
				command := strings.TrimSpace(action.Command)
				if command == "" || len(command) > 512 {
					return fmt.Errorf(
						"automations[%d].actions[%d].command must be 1..512 bytes",
						index,
						actionIndex,
					)
				}
				if strings.HasPrefix(strings.ToLower(command), "automation ") {
					return fmt.Errorf(
						"automations[%d].actions[%d] cannot recursively run automation",
						index,
						actionIndex,
					)
				}
			case "macro":
				if strings.TrimSpace(action.Macro) == "" || len(action.Macro) > 64 {
					return fmt.Errorf(
						"automations[%d].actions[%d].macro must be 1..64 bytes",
						index,
						actionIndex,
					)
				}
			case "rf":
				if action.RF == nil {
					return fmt.Errorf(
						"automations[%d].actions[%d].rf is required",
						index,
						actionIndex,
					)
				}
				repeats := action.RF.Repeats
				if repeats == 0 {
					repeats = 1
				}
				if action.RF.Code == 0 || action.RF.Bits == 0 ||
					action.RF.Bits > 32 || action.RF.Protocol == 0 ||
					action.RF.Protocol > 12 || repeats > 20 {
					return fmt.Errorf(
						"automations[%d].actions[%d].rf has invalid code/bits/protocol/repeats",
						index,
						actionIndex,
					)
				}
			case "host":
				if strings.TrimSpace(action.Executable) == "" {
					return fmt.Errorf(
						"automations[%d].actions[%d].executable is required",
						index,
						actionIndex,
					)
				}
			case "script":
				if strings.TrimSpace(action.Script) == "" {
					return fmt.Errorf(
						"automations[%d].actions[%d].script is required",
						index,
						actionIndex,
					)
				}
			case "emit":
				if strings.TrimSpace(action.Event) == "" {
					return fmt.Errorf(
						"automations[%d].actions[%d].event is required",
						index,
						actionIndex,
					)
				}
			case "virtual-key", "virtual_key", "vk":
				resolved, resolveErr := hostos.ResolveVirtualKey(action.VirtualKey)
				if resolveErr != nil {
					return fmt.Errorf(
						"automations[%d].actions[%d].virtual_key: %w",
						index, actionIndex, resolveErr,
					)
				}
				allowed := false
				for _, key := range value.OSActions.VirtualKeys.Allowed {
					candidate, candidateErr := hostos.ResolveVirtualKey(key)
					if candidateErr == nil && candidate.Code == resolved.Code {
						allowed = true
						break
					}
				}
				if !allowed {
					return fmt.Errorf(
						"automations[%d].actions[%d] virtual key %s is not allowlisted",
						index, actionIndex, resolved.Name,
					)
				}
				if action.HoldMS != 0 && (action.HoldMS < 10 || action.HoldMS > 1000) {
					return fmt.Errorf(
						"automations[%d].actions[%d].hold_ms must be 10..1000",
						index, actionIndex,
					)
				}
			case "power":
				if _, powerErr := hostos.NormalizePowerAction(action.Power); powerErr != nil {
					return fmt.Errorf(
						"automations[%d].actions[%d].power: %w",
						index, actionIndex, powerErr,
					)
				}
				if !value.OSActions.Power.AllowAutomation {
					return fmt.Errorf(
						"automations[%d].actions[%d] power action requires os_actions.power.allow_automation",
						index, actionIndex,
					)
				}
				if value.OSActions.Power.RequireConfirmation &&
					action.Confirm != value.OSActions.Power.ConfirmationToken {
					return fmt.Errorf(
						"automations[%d].actions[%d] power confirmation does not match policy",
						index, actionIndex,
					)
				}
			default:
				return fmt.Errorf(
					"automations[%d].actions[%d].type %q is unknown",
					index,
					actionIndex,
					action.Type,
				)
			}
		}
	}
	return nil
}

func printableASCII(value string) bool {
	for _, char := range []byte(value) {
		if char < 0x20 || char > 0x7E {
			return false
		}
	}
	return true
}

func printableText(value string) bool {
	for _, char := range value {
		if char < 0x20 || char == 0x7F {
			return false
		}
	}
	return true
}

// Store owns the current validated host configuration and its subscribers.
type Store struct {
	path               string
	mu                 sync.RWMutex
	value              Config
	digest             [sha256.Size]byte
	subscribers        map[uint64]chan Config
	runtimeSubscribers map[uint64]chan Config
	nextSubscriber     uint64
	secrets            *secretstore.Resolver
	appTitleOverride   string
	taglineOverride    string
}

// Open resolves and loads a persistent configuration store, creating defaults
// when required.
func Open(path string) (*Store, error) {
	return openWithSecrets(path, secretstore.New(productidentity.StableAppID))
}

func openWithSecrets(path string, secrets *secretstore.Resolver) (*Store, error) {
	resolved, err := ResolvePath(path)
	if err != nil {
		return nil, err
	}
	value, digest, err := LoadOrCreate(resolved)
	if err != nil {
		return nil, err
	}
	store := &Store{
		path: resolved, value: clone(value), digest: digest,
		subscribers:        make(map[uint64]chan Config),
		runtimeSubscribers: make(map[uint64]chan Config),
		secrets:            secrets,
	}
	if _, err := store.Runtime(); err != nil {
		return nil, fmt.Errorf("resolve configuration secrets: %w", err)
	}
	return store, nil
}

// Path returns the resolved backing-file path.
func (store *Store) Path() string {
	return store.path
}

// Current returns an isolated copy of the active configuration.
func (store *Store) Current() Config {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.effectiveLocked()
}

// SetPresentationOverrides applies process-lifetime branding without writing
// it to the watched configuration. Empty values leave the corresponding
// configuration value authoritative. Callers resolve precedence before this
// method, so command-line flags can override environment variables cleanly.
func (store *Store) SetPresentationOverrides(appTitle, tagline string) error {
	appTitle = strings.TrimSpace(appTitle)
	tagline = strings.TrimSpace(tagline)
	store.mu.Lock()
	defer store.mu.Unlock()
	candidate := clone(store.value)
	if appTitle != "" {
		candidate.UI.AppTitle = appTitle
	}
	if tagline != "" {
		candidate.UI.Tagline = tagline
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("presentation override: %w", err)
	}
	store.appTitleOverride = appTitle
	store.taglineOverride = tagline
	store.notifyLocked(store.value)
	store.notifyRuntimeLocked(store.value)
	return nil
}

func (store *Store) effectiveLocked() Config {
	value := clone(store.value)
	if store.appTitleOverride != "" {
		value.UI.AppTitle = store.appTitleOverride
	}
	if store.taglineOverride != "" {
		value.UI.Tagline = store.taglineOverride
	}
	return value
}

// Update applies one atomic PC-side configuration mutation and persists it.
// Board EEPROM remains exclusively controlled by explicit native commands.
func (store *Store) Update(change func(*Config) error) (Config, error) {
	if change == nil {
		return store.Current(), errors.New("configuration update callback is nil")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value := clone(store.value)
	if err := change(&value); err != nil {
		return clone(store.value), err
	}
	if _, err := resolveConfigSecrets(value, store.secrets); err != nil {
		return clone(store.value), fmt.Errorf("resolve configuration secrets: %w", err)
	}
	if err := Write(store.path, value); err != nil {
		return clone(store.value), err
	}
	loaded, digest, err := Load(store.path)
	if err != nil {
		return clone(store.value), err
	}
	store.value = clone(loaded)
	store.digest = digest
	store.notifyLocked(loaded)
	store.notifyRuntimeLocked(loaded)
	return store.effectiveLocked(), nil
}

// Subscribe receives the current PC-side configuration immediately and every
// successfully validated Update/Reload thereafter. One fsnotify owner can thus
// fan changes out to integrations without each consumer polling the file.
func (store *Store) Subscribe(ctx context.Context) <-chan Config {
	channel := make(chan Config, 1)
	store.mu.Lock()
	store.nextSubscriber++
	id := store.nextSubscriber
	store.subscribers[id] = channel
	channel <- store.effectiveLocked()
	store.mu.Unlock()
	go func() {
		<-ctx.Done()
		store.mu.Lock()
		if current, ok := store.subscribers[id]; ok {
			delete(store.subscribers, id)
			close(current)
		}
		store.mu.Unlock()
	}()
	return channel
}

// SubscribeRuntime receives configurations with every secret reference
// resolved in memory. Persistent snapshots and ordinary subscribers retain
// references and never receive the resolved values.
func (store *Store) SubscribeRuntime(ctx context.Context) <-chan Config {
	channel := make(chan Config, 1)
	store.mu.Lock()
	store.nextSubscriber++
	id := store.nextSubscriber
	store.runtimeSubscribers[id] = channel
	channel <- store.runtimeLocked()
	store.mu.Unlock()
	go func() {
		<-ctx.Done()
		store.mu.Lock()
		if current, ok := store.runtimeSubscribers[id]; ok {
			delete(store.runtimeSubscribers, id)
			close(current)
		}
		store.mu.Unlock()
	}()
	return channel
}

func (store *Store) notifyLocked(value Config) {
	if store.appTitleOverride != "" {
		value.UI.AppTitle = store.appTitleOverride
	}
	if store.taglineOverride != "" {
		value.UI.Tagline = store.taglineOverride
	}
	for _, subscriber := range store.subscribers {
		copyValue := clone(value)
		select {
		case subscriber <- copyValue:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- copyValue:
			default:
			}
		}
	}
}

func (store *Store) notifyRuntimeLocked(value Config) {
	if store.appTitleOverride != "" {
		value.UI.AppTitle = store.appTitleOverride
	}
	if store.taglineOverride != "" {
		value.UI.Tagline = store.taglineOverride
	}
	runtime, err := resolveConfigSecrets(value, store.secrets)
	if err != nil {
		runtime = failClosedRuntime(value)
	}
	for _, subscriber := range store.runtimeSubscribers {
		copyValue := clone(runtime)
		select {
		case subscriber <- copyValue:
		default:
			select {
			case <-subscriber:
			default:
			}
			select {
			case subscriber <- copyValue:
			default:
			}
		}
	}
}

func (store *Store) runtimeLocked() Config {
	effective := store.effectiveLocked()
	runtime, err := resolveConfigSecrets(effective, store.secrets)
	if err != nil {
		return failClosedRuntime(effective)
	}
	return runtime
}

// UpdateUI atomically replaces and persists the host UI section.
func (store *Store) UpdateUI(value UI) (Config, error) {
	store.mu.RLock()
	if store.appTitleOverride != "" {
		value.AppTitle = store.value.UI.AppTitle
	}
	if store.taglineOverride != "" {
		value.Tagline = store.value.UI.Tagline
	}
	store.mu.RUnlock()
	return store.Update(func(config *Config) error {
		config.UI = value
		return nil
	})
}

// RememberDevice persists only PC-side transport identity. It never changes
// or mirrors MCU EEPROM settings.
func (store *Store) RememberDevice(identity DeviceIdentity) (bool, error) {
	identity.Port = strings.TrimSpace(identity.Port)
	identity.VID = strings.ToUpper(strings.TrimSpace(identity.VID))
	identity.PID = strings.ToUpper(strings.TrimSpace(identity.PID))
	identity.SerialNumber = strings.TrimSpace(identity.SerialNumber)
	identity.Name = strings.TrimSpace(identity.Name)
	identity.InstanceID = strings.TrimSpace(identity.InstanceID)
	identity.LastSeen = identity.LastSeen.UTC()

	store.mu.Lock()
	defer store.mu.Unlock()
	current := store.value.Connection.LastDevice
	// LastSeen alone is not a meaningful config change and would cause a write
	// on every reconnect retry. Refresh it only when the remembered identity or
	// current COM assignment actually changes.
	same := current != nil &&
		current.Port == identity.Port &&
		strings.EqualFold(current.VID, identity.VID) &&
		strings.EqualFold(current.PID, identity.PID) &&
		current.SerialNumber == identity.SerialNumber &&
		current.Name == identity.Name &&
		strings.EqualFold(current.InstanceID, identity.InstanceID)
	if same {
		return false, nil
	}
	value := clone(store.value)
	value.Connection.LastDevice = &identity
	if _, err := resolveConfigSecrets(value, store.secrets); err != nil {
		return false, fmt.Errorf("resolve configuration secrets: %w", err)
	}
	if err := Write(store.path, value); err != nil {
		return false, err
	}
	loaded, digest, err := Load(store.path)
	if err != nil {
		return false, err
	}
	store.value = clone(loaded)
	store.digest = digest
	store.notifyLocked(loaded)
	store.notifyRuntimeLocked(loaded)
	return true, nil
}

// Reload validates the backing file and publishes it only when its content changed.
func (store *Store) Reload() (Config, bool, error) {
	// Serialize the disk read with Update's write/load/commit transaction. If
	// Reload reads before taking the mutex, an older fsnotify snapshot can wait
	// behind a newer Update and then incorrectly replace the newer value.
	store.mu.Lock()
	defer store.mu.Unlock()
	value, digest, err := Load(store.path)
	if err != nil {
		return Config{}, false, err
	}
	if digest == store.digest {
		return store.effectiveLocked(), false, nil
	}
	if _, err := resolveConfigSecrets(value, store.secrets); err != nil {
		return Config{}, false, fmt.Errorf("resolve configuration secrets: %w", err)
	}
	store.value = clone(value)
	store.digest = digest
	store.notifyLocked(value)
	store.notifyRuntimeLocked(value)
	return store.effectiveLocked(), true, nil
}

// Watch applies validated filesystem changes, using polling only when native
// notifications are unavailable or stop unexpectedly.
func (store *Store) Watch(
	ctx context.Context,
	interval time.Duration,
	onChange func(Config),
	onError func(error),
) {
	if interval <= 0 {
		interval = DefaultWatchInterval
	}
	watcher, err := openFilesystemWatcher(ctx, filepath.Dir(store.path))
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		if onError != nil {
			onError(fmt.Errorf("filesystem watcher unavailable; using polling fallback: %w", err))
		}
		store.watchPolling(ctx, interval, onChange, onError)
		return
	}
	defer watcher.Close()

	const debounce = 60 * time.Millisecond
	var timer *time.Timer
	var timerChannel <-chan time.Time
	lastReloadError := ""
	schedule := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(debounce)
		}
		timerChannel = timer.C
	}
	reload := func() {
		value, changed, reloadErr := store.Reload()
		if reloadErr != nil {
			reportDistinctReloadError(&lastReloadError, reloadErr, onError)
			return
		}
		lastReloadError = ""
		if changed && onChange != nil {
			onChange(value)
		}
	}
	// Reconcile once after the same debounce used for filesystem events. This
	// closes the gap between Open and watcher registration without racing an
	// editor's atomic replacement or Windows fallback write while it still has
	// the destination open with restrictive sharing flags.
	schedule()

	// A slow safety poll covers unusual network filesystems that acknowledge a
	// directory watch but omit replacement events. Normal local edits apply
	// through fsnotify after the short atomic-write debounce.
	safetyPoll := time.NewTicker(5 * time.Second)
	defer safetyPoll.Stop()
	target := filepath.Clean(store.path)
	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				store.watchPolling(ctx, interval, onChange, onError)
				return
			}
			if filepath.Clean(event.Name) == target &&
				event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) != 0 {
				schedule()
			}
		case watchErr, ok := <-watcher.Errors:
			if !ok {
				store.watchPolling(ctx, interval, onChange, onError)
				return
			}
			if onError != nil {
				onError(fmt.Errorf("filesystem watcher: %w", watchErr))
			}
		case <-timerChannel:
			timerChannel = nil
			reload()
		case <-safetyPoll.C:
			reload()
		}
	}
}

func openFilesystemWatcher(ctx context.Context, directory string) (*fsnotify.Watcher, error) {
	var watcher *fsnotify.Watcher
	err := retryFilesystemWatchRegistration(
		ctx,
		filesystemWatchRegistrationAttempts,
		filesystemWatchRegistrationRetryDelay,
		func() error {
			candidate, err := fsnotify.NewWatcher()
			if err != nil {
				return err
			}
			if err := candidate.Add(directory); err != nil {
				// Add can partially register a kqueue directory before its entry
				// scan fails. Never reuse that uncertain watcher on retry.
				_ = candidate.Close()
				return err
			}
			watcher = candidate
			return nil
		},
	)
	return watcher, err
}

func retryFilesystemWatchRegistration(
	ctx context.Context,
	attempts int,
	delay time.Duration,
	register func() error,
) error {
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := register()
		if err == nil {
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) || attempt == attempts-1 {
			return err
		}
		if delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func (store *Store) watchPolling(
	ctx context.Context,
	interval time.Duration,
	onChange func(Config),
	onError func(error),
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastReloadError := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			value, changed, err := store.Reload()
			if err != nil {
				reportDistinctReloadError(&lastReloadError, err, onError)
				continue
			}
			lastReloadError = ""
			if changed && onChange != nil {
				onChange(value)
			}
		}
	}
}

// reportDistinctReloadError emits a rejected file state once, then stays
// quiet until either the error changes or a valid configuration is loaded.
func reportDistinctReloadError(last *string, err error, onError func(error)) {
	if err == nil {
		*last = ""
		return
	}
	message := err.Error()
	if message == *last {
		return
	}
	*last = message
	if onError != nil {
		onError(err)
	}
}

func clone(value Config) Config {
	copyValue := value
	if value.Connection.LastDevice != nil {
		identity := *value.Connection.LastDevice
		copyValue.Connection.LastDevice = &identity
	}
	copyValue.Scripts = make(map[string]string, len(value.Scripts))
	for name, path := range value.Scripts {
		copyValue.Scripts[name] = path
	}
	copyValue.Macros = make([]Macro, len(value.Macros))
	for index, macro := range value.Macros {
		copyValue.Macros[index] = macro
		copyValue.Macros[index].Steps = append([]MacroStep(nil), macro.Steps...)
	}
	copyValue.Melodies = cloneMelodies(value.Melodies)
	copyValue.StatusEffects = append(
		[]StatusLEDEffect(nil),
		value.StatusEffects...,
	)
	copyValue.IPC.AllowedOrigins = append([]string(nil), value.IPC.AllowedOrigins...)
	copyValue.Integrations = cloneIntegrations(value.Integrations)
	copyValue.RF = cloneRFConfig(value.RF)
	copyValue.HostMenus = cloneHostMenus(value.HostMenus)
	copyValue.OSActions = hostos.ClonePolicy(value.OSActions)
	copyValue.Automations = make([]Automation, len(value.Automations))
	for index, automation := range value.Automations {
		copyValue.Automations[index] = automation
		copyValue.Automations[index].Actions = make(
			[]AutomationAction,
			len(automation.Actions),
		)
		for actionIndex, action := range automation.Actions {
			copyValue.Automations[index].Actions[actionIndex] = action
			copyValue.Automations[index].Actions[actionIndex].Args =
				append([]string(nil), action.Args...)
			if action.RF != nil {
				rf := *action.RF
				copyValue.Automations[index].Actions[actionIndex].RF = &rf
			}
		}
		if automation.Match.RFID != nil {
			rfID := *automation.Match.RFID
			copyValue.Automations[index].Match.RFID = &rfID
		}
		if automation.Match.RFCode != nil {
			code := *automation.Match.RFCode
			copyValue.Automations[index].Match.RFCode = &code
		}
	}
	return copyValue
}
