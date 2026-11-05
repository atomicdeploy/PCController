package tui

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultLaunchOptions(t *testing.T) {
	t.Parallel()

	options := DefaultLaunchOptions()
	if options.Transport.Mode != ModeAuto {
		t.Errorf("transport mode = %q, want %q", options.Transport.Mode, ModeAuto)
	}
	if options.Transport.RequestTimeout != 5*time.Second {
		t.Errorf(
			"transport timeout = %s, want %s",
			options.Transport.RequestTimeout,
			5*time.Second,
		)
	}
	if options.Poll != time.Second {
		t.Errorf("poll = %s, want %s", options.Poll, time.Second)
	}
	if options.Theme != ThemeAuto {
		t.Errorf("theme = %q, want %q", options.Theme, ThemeAuto)
	}
	if options.InitialScreen != ScreenDashboard {
		t.Errorf(
			"initial screen = %q, want %q",
			options.InitialScreen,
			ScreenDashboard,
		)
	}
	if !options.AltScreen {
		t.Error("alternate screen is disabled by default")
	}
	if !options.Mouse {
		t.Error("mouse support is disabled by default")
	}
	if options.Terminal {
		t.Error("device terminal unexpectedly opens by default")
	}
	if options.Help || options.Version {
		t.Error("help or version unexpectedly enabled by default")
	}
}

func TestParseLaunchOptions(t *testing.T) {
	t.Parallel()

	options, err := ParseLaunchOptions([]string{
		"--mode", " API ",
		"--config", "  device-test.json  ",
		"--api-url", "  http://localhost:47891/  ",
		"--api-token", "  local-test-token  ",
		"--poll", "750ms",
		"--timeout", "2s",
		"--theme", " LIGHT ",
		"--no-alt-screen",
		"--no-mouse",
		"--terminal",
		"--help",
		"--version",
	})
	if err != nil {
		t.Fatalf("ParseLaunchOptions returned error: %v", err)
	}

	if options.Transport.Mode != ModeAPI {
		t.Errorf("mode = %q, want %q", options.Transport.Mode, ModeAPI)
	}
	if options.Transport.ConfigPath != "device-test.json" {
		t.Errorf("config path = %q", options.Transport.ConfigPath)
	}
	if options.Transport.APIURL != "http://localhost:47891/" {
		t.Errorf("API URL = %q", options.Transport.APIURL)
	}
	if options.Transport.APIToken != "local-test-token" {
		t.Errorf("API token was not trimmed")
	}
	if options.Transport.RequestTimeout != 2*time.Second {
		t.Errorf("timeout = %s", options.Transport.RequestTimeout)
	}
	if options.Poll != 750*time.Millisecond {
		t.Errorf("poll = %s", options.Poll)
	}
	if options.Theme != ThemeLight {
		t.Errorf("theme = %q, want %q", options.Theme, ThemeLight)
	}
	if options.AltScreen {
		t.Error("alternate screen remains enabled")
	}
	if options.Mouse {
		t.Error("mouse remains enabled")
	}
	if !options.Terminal {
		t.Error("terminal launch flag was not retained")
	}
	if options.InitialScreen != ScreenTerminal {
		t.Errorf(
			"initial screen = %q, want %q",
			options.InitialScreen,
			ScreenTerminal,
		)
	}
	if !options.Help || !options.Version {
		t.Error("help or version flag was not retained")
	}
}

func TestParseLaunchOptionsAcceptsDirectEntryScreens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want InitialScreen
	}{
		{name: "dashboard", args: []string{"dashboard"}, want: ScreenDashboard},
		{name: "commands", args: []string{"commands"}, want: ScreenCommands},
		{name: "command alias", args: []string{"command"}, want: ScreenCommands},
		{name: "settings", args: []string{"settings"}, want: ScreenSettings},
		{name: "configure alias", args: []string{"configure"}, want: ScreenSettings},
		{name: "device", args: []string{"device"}, want: ScreenDevice},
		{name: "device settings alias", args: []string{"device-settings"}, want: ScreenDevice},
		{name: "serving", args: []string{"serving"}, want: ScreenServing},
		{name: "serve alias", args: []string{"serve"}, want: ScreenServing},
		{name: "diagnostics", args: []string{"diagnostics"}, want: ScreenDiagnostics},
		{name: "terminal", args: []string{"terminal"}, want: ScreenTerminal},
		{name: "telnet alias", args: []string{"telnet"}, want: ScreenTerminal},
		{
			name: "positional before options",
			args: []string{"settings", "--mode", "ipc"},
			want: ScreenSettings,
		},
		{
			name: "positional after options",
			args: []string{"--mode", "ipc", "device"},
			want: ScreenDevice,
		},
		{
			name: "screen flag alias",
			args: []string{"--screen", "configure"},
			want: ScreenSettings,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			options, err := ParseLaunchOptions(test.args)
			if err != nil {
				t.Fatalf("ParseLaunchOptions(%q): %v", test.args, err)
			}
			if options.InitialScreen != test.want {
				t.Errorf("initial screen = %q, want %q", options.InitialScreen, test.want)
			}
			if options.Terminal != (test.want == ScreenTerminal) {
				t.Errorf(
					"legacy terminal flag = %t for initial screen %q",
					options.Terminal,
					test.want,
				)
			}
		})
	}
}

func TestParseLaunchOptionsAcceptsDirectEntryFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		flag string
		want InitialScreen
	}{
		{flag: "--commands", want: ScreenCommands},
		{flag: "--settings", want: ScreenSettings},
		{flag: "--configure", want: ScreenSettings},
		{flag: "--device-settings", want: ScreenDevice},
		{flag: "--serving", want: ScreenServing},
		{flag: "--diagnostics", want: ScreenDiagnostics},
		{flag: "--terminal", want: ScreenTerminal},
		{flag: "--telnet", want: ScreenTerminal},
	}

	for _, test := range tests {
		test := test
		t.Run(test.flag, func(t *testing.T) {
			t.Parallel()

			options, err := ParseLaunchOptions([]string{test.flag})
			if err != nil {
				t.Fatalf("ParseLaunchOptions(%q): %v", test.flag, err)
			}
			if options.InitialScreen != test.want {
				t.Errorf("initial screen = %q, want %q", options.InitialScreen, test.want)
			}
		})
	}
}

func TestParseLaunchOptionsRejectsConflictingDirectEntryScreens(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"settings", "--terminal"},
		{"--commands", "--serving"},
		{"--screen", "device", "--diagnostics"},
	} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()

			_, err := ParseLaunchOptions(args)
			if err == nil {
				t.Fatalf("ParseLaunchOptions(%q) unexpectedly succeeded", args)
			}
			if !strings.Contains(err.Error(), "choose only one initial TUI screen") {
				t.Fatalf("error = %q", err)
			}
		})
	}

	// Duplicate aliases that select the same workspace remain harmless.
	if _, err := ParseLaunchOptions([]string{
		"settings",
		"--screen", "configure",
		"--settings",
		"--configure",
	}); err != nil {
		t.Fatalf("equivalent aliases conflict: %v", err)
	}
}

func TestParseInitialScreen(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  InitialScreen
	}{
		{input: " DASHBOARD ", want: ScreenDashboard},
		{input: "commands", want: ScreenCommands},
		{input: "Configure", want: ScreenSettings},
		{input: "device-settings", want: ScreenDevice},
		{input: "serve", want: ScreenServing},
		{input: "DIAGNOSTICS", want: ScreenDiagnostics},
		{input: "telnet", want: ScreenTerminal},
	} {
		test := test
		t.Run(string(test.want)+"_"+test.input, func(t *testing.T) {
			t.Parallel()

			got, err := ParseInitialScreen(test.input)
			if err != nil {
				t.Fatalf("ParseInitialScreen(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("screen = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := ParseInitialScreen("unknown"); err == nil {
		t.Error("unknown screen unexpectedly accepted")
	}
}

func TestParseLaunchOptionsAcceptsTelnetAlias(t *testing.T) {
	t.Parallel()

	options, err := ParseLaunchOptions([]string{"--telnet"})
	if err != nil {
		t.Fatal(err)
	}
	if !options.Terminal {
		t.Error("--telnet did not enable the initial terminal overlay")
	}
	for _, phrase := range []string{
		"--terminal",
		"--telnet",
		"x device terminal",
		"Ctrl+] detach",
	} {
		if !strings.Contains(Usage(), phrase) {
			t.Errorf("usage is missing %q", phrase)
		}
	}
}

func TestParseLaunchOptionsRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "unknown flag",
			args:    []string{"--not-a-real-option"},
			wantErr: "flag provided but not defined",
		},
		{
			name:    "positional argument",
			args:    []string{"unexpected"},
			wantErr: `unexpected argument "unexpected"`,
		},
		{
			name:    "second positional argument",
			args:    []string{"settings", "unexpected"},
			wantErr: `unexpected argument "unexpected"`,
		},
		{
			name:    "bad initial screen",
			args:    []string{"--screen", "unknown"},
			wantErr: "invalid TUI screen",
		},
		{
			name:    "bad mode",
			args:    []string{"--mode", "socket"},
			wantErr: "invalid mode",
		},
		{
			name:    "bad theme",
			args:    []string{"--theme", "sepia"},
			wantErr: "invalid theme",
		},
		{
			name:    "poll too short",
			args:    []string{"--poll", "249ms"},
			wantErr: "poll interval must be between 250ms and 1m",
		},
		{
			name:    "poll too long",
			args:    []string{"--poll", "61s"},
			wantErr: "poll interval must be between 250ms and 1m",
		},
		{
			name:    "timeout too short",
			args:    []string{"--timeout", "99ms"},
			wantErr: "transport timeout must be between 100ms and 1m",
		},
		{
			name:    "timeout too long",
			args:    []string{"--timeout", "61s"},
			wantErr: "transport timeout must be between 100ms and 1m",
		},
		{
			name:    "invalid duration",
			args:    []string{"--poll", "soon"},
			wantErr: "invalid value",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseLaunchOptions(test.args)
			if err == nil {
				t.Fatal("ParseLaunchOptions unexpectedly succeeded")
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestParseLaunchOptionsAcceptsIntervalBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		poll    string
		timeout string
	}{
		{name: "minimum", poll: "250ms", timeout: "100ms"},
		{name: "maximum", poll: "1m", timeout: "1m"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseLaunchOptions([]string{
				"--poll", test.poll,
				"--timeout", test.timeout,
			}); err != nil {
				t.Fatalf("boundary values rejected: %v", err)
			}
		})
	}
}

func TestParseTransportMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  TransportMode
	}{
		{input: "auto", want: ModeAuto},
		{input: " IPC ", want: ModeIPC},
		{input: "Api", want: ModeAPI},
		{input: "STANDALONE", want: ModeStandalone},
	} {
		test := test
		t.Run(string(test.want), func(t *testing.T) {
			t.Parallel()

			got, err := ParseTransportMode(test.input)
			if err != nil {
				t.Fatalf("ParseTransportMode(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("mode = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := ParseTransportMode(""); err == nil {
		t.Error("empty mode unexpectedly accepted")
	}
	if _, err := ParseTransportMode("headless"); err == nil {
		t.Error("unknown mode unexpectedly accepted")
	}
}

func TestParseThemeMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		input string
		want  ThemeMode
	}{
		{input: "auto", want: ThemeAuto},
		{input: " DARK ", want: ThemeDark},
		{input: "Light", want: ThemeLight},
	} {
		test := test
		t.Run(string(test.want), func(t *testing.T) {
			t.Parallel()

			got, err := ParseThemeMode(test.input)
			if err != nil {
				t.Fatalf("ParseThemeMode(%q): %v", test.input, err)
			}
			if got != test.want {
				t.Errorf("theme = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := ParseThemeMode(""); err == nil {
		t.Error("empty theme unexpectedly accepted")
	}
	if _, err := ParseThemeMode("blue"); err == nil {
		t.Error("unknown theme unexpectedly accepted")
	}
}

func TestValidateAction(t *testing.T) {
	t.Parallel()

	for _, action := range []Action{
		ActionToggle,
		ActionOn,
		ActionOff,
		ActionReconnect,
		ActionRefresh,
	} {
		action := action
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()

			if err := validateAction(action); err != nil {
				t.Errorf("validateAction(%q): %v", action, err)
			}
		})
	}

	for _, action := range []Action{"", "status", "show", "TOGGLE"} {
		action := action
		t.Run("reject_"+string(action), func(t *testing.T) {
			t.Parallel()

			err := validateAction(action)
			if err == nil {
				t.Fatalf("validateAction(%q) unexpectedly succeeded", action)
			}
			if !strings.Contains(err.Error(), string(action)) {
				t.Errorf("error %q does not identify rejected action %q", err, action)
			}
		})
	}
}

func TestNormalizeTransportOptions(t *testing.T) {
	t.Parallel()

	got, err := normalizeTransportOptions(TransportOptions{
		ConfigPath: "  config.json  ",
		APIURL:     "  http://localhost:47891  ",
		APIToken:   "  local-token  ",
	})
	if err != nil {
		t.Fatalf("normalizeTransportOptions: %v", err)
	}
	if got.Mode != ModeAuto {
		t.Errorf("default mode = %q, want %q", got.Mode, ModeAuto)
	}
	if got.RequestTimeout != 5*time.Second {
		t.Errorf("default timeout = %s", got.RequestTimeout)
	}
	if got.ConfigPath != "config.json" {
		t.Errorf("config path = %q", got.ConfigPath)
	}
	if got.APIURL != "http://localhost:47891" {
		t.Errorf("API URL = %q", got.APIURL)
	}
	if got.APIToken != "local-token" {
		t.Errorf("API token was not trimmed")
	}
}

func TestUsageDocumentsEveryTransportMode(t *testing.T) {
	t.Parallel()

	usage := Usage()
	for _, text := range []string{
		"PCControllerDesktop.exe tui [screen] [options]",
		"settings | configure",
		"serving | serve",
		"--screen NAME",
		"--commands",
		"--settings",
		"--configure",
		"--device-settings",
		"--serving",
		"--diagnostics",
		"--mode auto|ipc|api|standalone",
		"--api-url URL",
		"--api-token TOKEN",
		"--no-alt-screen",
		"--no-mouse",
	} {
		if !strings.Contains(usage, text) {
			t.Errorf("Usage() does not contain %q", text)
		}
	}
}
