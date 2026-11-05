package tui

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

type ThemeMode string

const (
	ThemeAuto  ThemeMode = "auto"
	ThemeDark  ThemeMode = "dark"
	ThemeLight ThemeMode = "light"
)

// InitialScreen identifies the workspace shown when the terminal UI starts.
// Aliases are accepted by ParseInitialScreen, while values stored in
// LaunchOptions are always one of these canonical names.
type InitialScreen string

const (
	ScreenDashboard   InitialScreen = "dashboard"
	ScreenCommands    InitialScreen = "commands"
	ScreenSettings    InitialScreen = "settings"
	ScreenDevice      InitialScreen = "device"
	ScreenServing     InitialScreen = "serving"
	ScreenDiagnostics InitialScreen = "diagnostics"
	ScreenTerminal    InitialScreen = "terminal"
)

// LaunchOptions is the complete terminal-mode surface of RayanLamp.exe.
type LaunchOptions struct {
	Transport     TransportOptions
	Poll          time.Duration
	Theme         ThemeMode
	InitialScreen InitialScreen
	AltScreen     bool
	Mouse         bool
	// Terminal is retained for compatibility with the existing model launch
	// seam. It is true exactly when InitialScreen is ScreenTerminal.
	Terminal bool
	Help     bool
	Version  bool
}

func DefaultLaunchOptions() LaunchOptions {
	return LaunchOptions{
		Transport: TransportOptions{
			Mode:           ModeAuto,
			RequestTimeout: 5 * time.Second,
		},
		Poll:          time.Second,
		Theme:         ThemeAuto,
		InitialScreen: ScreenDashboard,
		AltScreen:     true,
		Mouse:         true,
	}
}

func ParseLaunchOptions(args []string) (LaunchOptions, error) {
	options := DefaultLaunchOptions()
	set := flag.NewFlagSet("RayanLamp.exe tui", flag.ContinueOnError)
	set.SetOutput(io.Discard)

	var mode, theme, screen string
	var noAltScreen, noMouse bool
	var commands, settings, configure, deviceSettings, serving, diagnostics bool
	var terminal, telnet bool
	var positionalScreen string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positionalScreen = args[0]
		args = args[1:]
	}
	set.StringVar(&mode, "mode", string(options.Transport.Mode), "")
	set.StringVar(&options.Transport.ConfigPath, "config", "", "")
	set.StringVar(&options.Transport.APIURL, "api-url", "", "")
	set.StringVar(&options.Transport.APIToken, "api-token", "", "")
	set.DurationVar(&options.Poll, "poll", options.Poll, "")
	set.DurationVar(
		&options.Transport.RequestTimeout,
		"timeout",
		options.Transport.RequestTimeout,
		"",
	)
	set.StringVar(&theme, "theme", string(options.Theme), "")
	set.StringVar(&screen, "screen", "", "")
	set.BoolVar(&noAltScreen, "no-alt-screen", false, "")
	set.BoolVar(&noMouse, "no-mouse", false, "")
	set.BoolVar(&commands, "commands", false, "")
	set.BoolVar(&settings, "settings", false, "")
	set.BoolVar(&configure, "configure", false, "")
	set.BoolVar(&deviceSettings, "device-settings", false, "")
	set.BoolVar(&serving, "serving", false, "")
	set.BoolVar(&diagnostics, "diagnostics", false, "")
	set.BoolVar(&terminal, "terminal", false, "")
	set.BoolVar(&telnet, "telnet", false, "")
	set.BoolVar(&options.Help, "help", false, "")
	set.BoolVar(&options.Help, "h", false, "")
	set.BoolVar(&options.Version, "version", false, "")

	if err := set.Parse(args); err != nil {
		return LaunchOptions{}, fmt.Errorf("%w\n\n%s", err, Usage())
	}
	if set.NArg() > 1 || (positionalScreen != "" && set.NArg() != 0) {
		if positionalScreen != "" {
			return LaunchOptions{}, fmt.Errorf("unexpected argument %q", set.Arg(0))
		}
		return LaunchOptions{}, fmt.Errorf("unexpected argument %q", set.Arg(1))
	}
	if positionalScreen == "" && set.NArg() == 1 {
		positionalScreen = set.Arg(0)
	}
	if positionalScreen != "" {
		if _, err := ParseInitialScreen(positionalScreen); err != nil {
			return LaunchOptions{}, fmt.Errorf("unexpected argument %q", positionalScreen)
		}
	}
	requestedScreens := []string{positionalScreen, screen}
	if commands {
		requestedScreens = append(requestedScreens, string(ScreenCommands))
	}
	if settings || configure {
		requestedScreens = append(requestedScreens, string(ScreenSettings))
	}
	if deviceSettings {
		requestedScreens = append(requestedScreens, string(ScreenDevice))
	}
	if serving {
		requestedScreens = append(requestedScreens, string(ScreenServing))
	}
	if diagnostics {
		requestedScreens = append(requestedScreens, string(ScreenDiagnostics))
	}
	if terminal || telnet {
		requestedScreens = append(requestedScreens, string(ScreenTerminal))
	}
	selectedScreen := ScreenDashboard
	screenSelected := false
	for _, requested := range requestedScreens {
		if strings.TrimSpace(requested) == "" {
			continue
		}
		parsed, err := ParseInitialScreen(requested)
		if err != nil {
			return LaunchOptions{}, err
		}
		if screenSelected && parsed != selectedScreen {
			return LaunchOptions{}, errors.New("choose only one initial TUI screen")
		}
		selectedScreen = parsed
		screenSelected = true
	}
	options.InitialScreen = selectedScreen
	options.Terminal = selectedScreen == ScreenTerminal
	parsedMode, err := ParseTransportMode(mode)
	if err != nil {
		return LaunchOptions{}, err
	}
	options.Transport.Mode = parsedMode
	parsedTheme, err := ParseThemeMode(theme)
	if err != nil {
		return LaunchOptions{}, err
	}
	options.Theme = parsedTheme
	options.AltScreen = !noAltScreen
	options.Mouse = !noMouse
	if options.Poll < 250*time.Millisecond || options.Poll > time.Minute {
		return LaunchOptions{}, errors.New("poll interval must be between 250ms and 1m")
	}
	normalized, err := normalizeTransportOptions(options.Transport)
	if err != nil {
		return LaunchOptions{}, err
	}
	options.Transport = normalized
	return options, nil
}

// ParseInitialScreen accepts the public direct-entry vocabulary and returns a
// stable canonical screen name.
func ParseInitialScreen(value string) (InitialScreen, error) {
	screen := strings.ToLower(strings.TrimSpace(value))
	switch screen {
	case string(ScreenDashboard):
		return ScreenDashboard, nil
	case "command", string(ScreenCommands):
		return ScreenCommands, nil
	case string(ScreenSettings), "configure":
		return ScreenSettings, nil
	case string(ScreenDevice), "device-settings":
		return ScreenDevice, nil
	case "serve", string(ScreenServing):
		return ScreenServing, nil
	case string(ScreenDiagnostics):
		return ScreenDiagnostics, nil
	case string(ScreenTerminal), "telnet":
		return ScreenTerminal, nil
	default:
		return "", fmt.Errorf(
			"invalid TUI screen %q (want dashboard, commands, settings, device, serving, diagnostics, or terminal)",
			value,
		)
	}
}

func ParseThemeMode(value string) (ThemeMode, error) {
	theme := ThemeMode(strings.ToLower(strings.TrimSpace(value)))
	switch theme {
	case ThemeAuto, ThemeDark, ThemeLight:
		return theme, nil
	default:
		return "", fmt.Errorf("invalid theme %q (want auto, dark, or light)", value)
	}
}

func Usage() string {
	return `RayanLamp terminal UI

Usage:
  RayanLamp.exe tui [screen] [options]

Screens:
  dashboard              Lamp status and controls (default)
  commands               Searchable application command workspace
  settings | configure   Complete RayanLamp configuration
  device                 Device information and parameters
  serving | serve        Web dashboard and local API controls
  diagnostics            Parsed and raw diagnostic tools
  terminal | telnet      Interactive device terminal

Connection:
  --mode auto|ipc|api|standalone
                         Auto attaches to the GUI, then becomes headless primary
  --config PATH          Alternate RayanLamp configuration
  --api-url URL          Explicit loopback API base URL
  --api-token TOKEN      Explicit local API bearer token
  --poll 1s              Remote status refresh interval
  --timeout 5s           Per-operation timeout

Appearance:
  --theme auto|dark|light
  --no-alt-screen        Render inline instead of using the alternate screen
  --no-mouse             Disable mouse and wheel reporting

Other:
  --screen NAME          Open a named screen
  --commands             Open the command workspace
  --settings             Open RayanLamp configuration
  --configure            Alias for --settings
  --device-settings      Open device information and parameters
  --serving              Open Web dashboard and local API controls
  --diagnostics          Open diagnostic tools
  --terminal              Open the interactive device terminal immediately
  --telnet                Alias for --terminal
  --help                 Show this help
  --version              Show version information

Keys inside the TUI:
  space/enter/t toggle · o on · f off · r refresh · c reconnect
  :/ctrl+p commands · s settings · v device · w serving
  d diagnostics · x device terminal · ? help · ctrl+t theme
  Ctrl+C quits consistently outside the raw terminal; q is context-local
  Settings: Ctrl+S apply live · Esc categories/dashboard
  Terminal: Esc/Ctrl+] detach · Ctrl+C sends ETX · Ctrl+D disconnect · Ctrl+R/F5 reconnect`
}
