//go:build windows

package cli

import (
	"bytes"
	"strings"
	"testing"

	"pccontroller.local/desktop/internal/ipc"
)

func TestWindowsAliases(t *testing.T) {
	options, err := Parse([]string{"/ON", "/json"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Method != "device.on" || !options.JSON {
		t.Fatalf("unexpected options: %+v", options)
	}
}

func TestWebDashboardWindowsAlias(t *testing.T) {
	options, err := Parse([]string{"/web"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Method != "web.show" {
		t.Fatalf("unexpected method: %s", options.Method)
	}
}

func TestHideWindowsAndGNUAliases(t *testing.T) {
	for _, argument := range []string{"/HIDE", "--hide"} {
		t.Run(argument, func(t *testing.T) {
			options, err := Parse([]string{argument})
			if err != nil {
				t.Fatal(err)
			}
			if options.Method != "window.hide" || options.Params != nil {
				t.Fatalf("unexpected options: %+v", options)
			}
		})
	}
}

func TestNotifyAndEndpoint(t *testing.T) {
	options, err := Parse([]string{"--notify", "hello", "from", "the", "device", "--title", "Controller"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Method != "notifications.show" {
		t.Fatalf("unexpected method: %s", options.Method)
	}
	params := options.Params.(map[string]string)
	if params["message"] != "hello from the device" || params["title"] != "Controller" {
		t.Fatalf("unexpected params: %#v", params)
	}

	options, err = Parse([]string{"--endpoint", "/info"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Method != "diagnostics.fetch" {
		t.Fatalf("unexpected method: %s", options.Method)
	}
}

func TestAudioCommandAndWindowsAlias(t *testing.T) {
	for _, args := range [][]string{
		{"--audio", "--cue", "on", "--count", "2"},
		{"/sound", "/cue", "ON", "/count", "2"},
	} {
		options, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%v): %v", args, err)
		}
		if options.Method != "audio.play" {
			t.Fatalf("Parse(%v) method = %q", args, options.Method)
		}
		params := options.Params.(map[string]any)
		if params["cue"] != "on" || params["count"] != 2 {
			t.Fatalf("Parse(%v) params = %#v", args, params)
		}
	}

	options, err := Parse([]string{"/audio"})
	if err != nil {
		t.Fatal(err)
	}
	params := options.Params.(map[string]any)
	if params["cue"] != "message" || params["count"] != 1 {
		t.Fatalf("default audio params = %#v", params)
	}
}

func TestAudioOptionsAreValidated(t *testing.T) {
	for _, args := range [][]string{
		{"--cue", "on"},
		{"--audio", "--cue", "unknown"},
		{"--audio", "--count", "11"},
		{"--audio", "--count", "-1"},
	} {
		if options, err := Parse(args); err == nil {
			t.Fatalf("Parse(%v) = %+v, want error", args, options)
		}
	}
}

func TestRejectMultipleCommands(t *testing.T) {
	_, err := Parse([]string{"/on", "/off"})
	if err == nil || !strings.Contains(err.Error(), "only one") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestURIActivationMapsToCanonicalCommands(t *testing.T) {
	for _, test := range []struct {
		args   []string
		method string
	}{
		{[]string{"/uri", "pccontroller-desktop://on"}, "device.on"},
		{[]string{"--uri=pccontroller-desktop:/off"}, "device.off"},
		{[]string{"/URI", "PCCONTROLLER-DESKTOP://SETTINGS"}, "settings.show"},
	} {
		options, err := Parse(test.args)
		if err != nil {
			t.Fatalf("Parse(%v): %v", test.args, err)
		}
		if options.Method != test.method || options.Params != nil {
			t.Fatalf("Parse(%v) = %+v, want method %q", test.args, options, test.method)
		}
	}
}

func TestURIActivationRejectsInvalidAndConflictingCommands(t *testing.T) {
	for _, args := range [][]string{
		{"/uri", ""},
		{"/uri", "https://example.com/on"},
		{"/uri", "pccontroller-desktop://quit"},
		{"/uri", "pccontroller-desktop://on", "/off"},
		{"/register-shell", "/uri", "pccontroller-desktop://on"},
	} {
		if options, err := Parse(args); err == nil {
			t.Fatalf("Parse(%v) = %+v, want error", args, options)
		}
	}
}

func TestShellRegistrationSpecialModes(t *testing.T) {
	tests := []struct {
		argument string
		want     SpecialMode
	}{
		{"/register-shell", SpecialRegisterShell},
		{"--unregister-shell", SpecialUnregisterShell},
	}
	for _, test := range tests {
		options, err := Parse([]string{test.argument})
		if err != nil {
			t.Fatal(err)
		}
		if options.Special != test.want || options.HasCommand() {
			t.Fatalf("Parse(%q) = %+v", test.argument, options)
		}
	}

	if _, err := Parse([]string{"/register-com", "/register-shell"}); err == nil {
		t.Fatal("multiple registration modes were accepted")
	}

	options, err := Parse([]string{"/register-shell", "/quiet"})
	if err != nil {
		t.Fatal(err)
	}
	if options.Special != SpecialRegisterShell || !options.Quiet {
		t.Fatalf("quiet shell registration = %+v", options)
	}
}

func TestSelfInstallSpecialModesAndOptions(t *testing.T) {
	install, err := Parse([]string{
		"/install",
		"/with-startup",
		"/with-shell",
		"/with-com",
		"/launch",
		"/force",
		"/quiet",
	})
	if err != nil {
		t.Fatal(err)
	}
	if install.Special != SpecialInstall ||
		!install.InstallWithStartup ||
		!install.InstallWithShell ||
		!install.InstallWithCOM ||
		!install.InstallLaunch ||
		!install.InstallForce ||
		!install.Quiet {
		t.Fatalf("install options = %+v", install)
	}

	uninstall, err := Parse([]string{
		"--uninstall",
		"--remove-user-data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if uninstall.Special != SpecialUninstall ||
		!uninstall.RemoveUserData {
		t.Fatalf("uninstall options = %+v", uninstall)
	}

	status, err := Parse([]string{"/install-status", "/json"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Special != SpecialInstallStatus || !status.JSON {
		t.Fatalf("install status options = %+v", status)
	}
}

func TestSelfInstallModifiersAreModeScoped(t *testing.T) {
	for _, args := range [][]string{
		{"/with-startup"},
		{"/install-status", "/with-shell"},
		{"/uninstall", "/with-com"},
		{"/install", "/remove-user-data"},
		{"/remove-user-data"},
		{"/install", "/uninstall"},
		{"/install", "/on"},
		{"/parent-pid", "12"},
		{"/cleanup-helper", "/parent-pid", "0", "/install-path", `C:\x`},
		{"/cleanup-helper", "/parent-pid", "12"},
	} {
		if options, err := Parse(args); err == nil {
			t.Fatalf("Parse(%v) = %+v, want error", args, options)
		}
	}
}

func TestPrivateCleanupHelperParsing(t *testing.T) {
	options, err := Parse([]string{
		"/cleanup-helper",
		"/parent-pid",
		"4294967295",
		"/install-path",
		`C:\Program Files\PCControllerDesktop`,
		"/remove-user-data",
	})
	if err != nil {
		t.Fatal(err)
	}
	if options.Special != SpecialCleanupHelper ||
		options.CleanupParentPID != ^uint32(0) ||
		options.CleanupInstallPath == "" ||
		!options.RemoveUserData {
		t.Fatalf("cleanup helper options = %+v", options)
	}
	if _, err := Parse([]string{
		"/cleanup-helper",
		"/parent-pid",
		"4294967296",
		"/install-path",
		`C:\x`,
	}); err == nil {
		t.Fatal("out-of-range cleanup parent PID was accepted")
	}
}

func TestCOMServerAcceptsWindowsEmbeddingMarker(t *testing.T) {
	for _, args := range [][]string{
		{"/com-server", "-Embedding"},
		{"-Embedding", "--com-server"},
		{"/COM-SERVER", "/Embedding"},
	} {
		options, err := Parse(args)
		if err != nil {
			t.Fatalf("Parse(%v): %v", args, err)
		}
		if options.Special != SpecialCOMServer || options.HasCommand() {
			t.Fatalf("Parse(%v) = %+v", args, options)
		}
	}

	for _, args := range [][]string{
		{"-Embedding"},
		{"/com-server", "-Embedding", "/Embedding"},
	} {
		if options, err := Parse(args); err == nil {
			t.Fatalf("Parse(%v) = %+v, want error", args, options)
		}
	}
}

func TestPrintResponseUsesCompleteANSIAndEmojiWhenForced(t *testing.T) {
	// Test this policy explicitly rather than inheriting CI/agent TERM=dumb
	// or NO_COLOR, both of which correctly take precedence over FORCE_COLOR.
	for _, name := range []string{"TERM", "NO_COLOR", "NO_EMOJI", "CLICOLOR"} {
		t.Setenv(name, "")
	}
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "always")
	var output bytes.Buffer

	err := PrintResponse(
		&output,
		ipc.Success("test", map[string]string{"state": "on"}),
		Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	if !strings.HasPrefix(rendered, "✅ \x1b[1;32mOK\x1b[0m ") {
		t.Fatalf("colored response prefix = %q", rendered)
	}
	if strings.Count(rendered, "\x1b[") != 2 {
		t.Fatalf("response leaked or omitted an ANSI reset: %q", rendered)
	}
}

func TestPrintResponseIsPlainWhenRedirectedOrNoColor(t *testing.T) {
	t.Setenv("FORCE_COLOR", "0")
	t.Setenv("PCCONTROLLER_DESKTOP_EMOJI", "never")
	var output bytes.Buffer

	err := PrintResponse(
		&output,
		ipc.Success("test", "on"),
		Options{NoColor: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	rendered := output.String()
	if strings.Contains(rendered, "\x1b") ||
		strings.Contains(rendered, "✅") {
		t.Fatalf("plain response contains terminal decoration: %q", rendered)
	}
	if rendered != "OK \"on\"\n" {
		t.Fatalf("plain response = %q", rendered)
	}
}
