package main

import (
	"bytes"
	"flag"
	"os"
	"runtime"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/productidentity"
)

func clearTUIConsoleEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"PCCONTROLLER_TUI_CONSOLE", "PCCONTROLLER_TUI_COLUMNS",
		"PCCONTROLLER_TUI_ROWS", "PCCONTROLLER_TUI_FONT",
		"PCCONTROLLER_TUI_FONT_SIZE",
	} {
		value, found := os.LookupEnv(name)
		_ = os.Unsetenv(name)
		t.Cleanup(func() {
			if found {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
	}
}

func TestTUIConsolePrecedenceFlagEnvironmentConfigBuildPackage(t *testing.T) {
	clearTUIConsoleEnvironment(t)
	oldColumns := productidentity.DefaultTUIConsoleColumns
	productidentity.DefaultTUIConsoleColumns = "144"
	t.Cleanup(func() { productidentity.DefaultTUIConsoleColumns = oldColumns })

	configured := appconfig.Defaults().UI.TUIConsole
	configured.Rows = 46
	configured.FontFace = "Lucida Console"
	t.Setenv("PCCONTROLLER_TUI_ROWS", "48")
	t.Setenv("PCCONTROLLER_TUI_FONT_SIZE", "20")
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options, err := addTUIConsoleFlags(flags, configured)
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse([]string{"--columns", "156", "--console-font", "Consolas"}); err != nil {
		t.Fatal(err)
	}
	if err := options.captureOverrides(flags); err != nil {
		t.Fatal(err)
	}
	got := options.resolve(configured)
	want := consolewindow.Settings{
		Enabled: true, Columns: 156, Rows: 48, FontFace: "Consolas", FontSize: 20,
	}
	if got != want {
		t.Fatalf("resolved=%#v want=%#v", got, want)
	}
}

func TestTUIConsoleBuildDefaultReplacesPackageInheritancePoint(t *testing.T) {
	clearTUIConsoleEnvironment(t)
	oldColumns := productidentity.DefaultTUIConsoleColumns
	productidentity.DefaultTUIConsoleColumns = "144"
	t.Cleanup(func() { productidentity.DefaultTUIConsoleColumns = oldColumns })
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options, err := addTUIConsoleFlags(flags, appconfig.Defaults().UI.TUIConsole)
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := options.captureOverrides(flags); err != nil {
		t.Fatal(err)
	}
	if got := options.resolve(appconfig.Defaults().UI.TUIConsole).Columns; got != 144 {
		t.Fatalf("build columns=%d", got)
	}
}

func TestExplicitConfigMayRestorePackagedValueOverBuildOverride(t *testing.T) {
	clearTUIConsoleEnvironment(t)
	oldColumns := productidentity.DefaultTUIConsoleColumns
	productidentity.DefaultTUIConsoleColumns = "144"
	t.Cleanup(func() { productidentity.DefaultTUIConsoleColumns = oldColumns })

	configured := appconfig.Defaults().UI.TUIConsole
	configured.Columns = 132
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	options, err := addTUIConsoleFlags(flags, configured)
	if err != nil {
		t.Fatal(err)
	}
	if err := flags.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := options.captureOverrides(flags); err != nil {
		t.Fatal(err)
	}
	if got := options.resolve(configured).Columns; got != 132 {
		t.Fatalf("configured columns=%d, want 132 over build default 144", got)
	}
}

func TestExplicitConsoleFlagMakesRemoteSkipActionable(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "client 123 server 22")
	settings := consolewindow.Settings{
		Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18,
	}
	var output bytes.Buffer
	if err := applyTUIConsole(settings, &output, false); err != nil || !strings.Contains(output.String(), "SSH") {
		t.Fatalf("non-strict output=%q err=%v", output.String(), err)
	}
	output.Reset()
	if err := applyTUIConsole(settings, &output, true); err == nil || !strings.Contains(err.Error(), "SSH") {
		t.Fatalf("strict output=%q err=%v", output.String(), err)
	}
}

func TestUnavailableLinuxConsoleIsSilent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-specific terminal-emulator behavior")
	}
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_CLIENT", "")
	t.Setenv("SSH_TTY", "")
	settings := consolewindow.Settings{
		Enabled: true, Columns: 132, Rows: 40, FontFace: "Consolas", FontSize: 18,
	}
	var output bytes.Buffer
	if err := applyTUIConsole(settings, &output, false); err != nil || output.Len() != 0 {
		t.Fatalf("output=%q err=%v", output.String(), err)
	}
	if err := applyTUIConsole(settings, &output, true); err == nil || !strings.Contains(err.Error(), "unavailable on linux") {
		t.Fatalf("strict output=%q err=%v", output.String(), err)
	}
}

func TestInvalidTUIConsoleEnvironmentFailsClearly(t *testing.T) {
	clearTUIConsoleEnvironment(t)
	t.Setenv("PCCONTROLLER_TUI_COLUMNS", "wide")
	_, err := addTUIConsoleFlags(flag.NewFlagSet("test", flag.ContinueOnError), appconfig.Defaults().UI.TUIConsole)
	if err == nil || !strings.Contains(err.Error(), "PCCONTROLLER_TUI_COLUMNS") {
		t.Fatalf("invalid environment error=%v", err)
	}
}
