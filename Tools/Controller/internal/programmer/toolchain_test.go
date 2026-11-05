package programmer

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestGeneratedToolchainPolicyMatchesCanonicalProfile(t *testing.T) {
	profile, err := LoadToolchainPolicy(filepath.Join("..", "..", "toolchain-profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if profile.FQBN != DefaultFQBN() {
		t.Fatalf("public profile FQBN=%q want generated fallback %q", profile.FQBN, DefaultFQBN())
	}
	want := DefaultToolchainPolicy()
	if !reflect.DeepEqual(profile, want) {
		t.Fatalf("generated runtime policy drifted from canonical profile:\nfile=%#v\ngenerated=%#v", profile, want)
	}
}

func TestToolchainPolicyRejectsMissingFQBN(t *testing.T) {
	policy := DefaultToolchainPolicy()
	policy.FQBN = "   "
	if err := policy.Validate(); err == nil || !strings.Contains(err.Error(), "name and FQBN") {
		t.Fatalf("missing FQBN returned unhelpful validation error: %v", err)
	}
}

func TestToolchainBootstrapDryRunPinsCoreLibrariesAndManagedCLI(t *testing.T) {
	called := false
	var output strings.Builder
	report, err := BootstrapToolchain(
		context.Background(),
		ToolchainBootstrapOptions{
			Profile: DefaultToolchainProfile(), InstallDir: t.TempDir(), Managed: true,
			GOOS: "windows", GOARCH: "amd64", DryRun: true, Locked: true,
			Environment: []string{
				"PATH=test", "https_proxy=http://secret", "NO_PROXY=localhost",
			},
			Runner: DependencyEnvironmentRunnerFunc(func(
				context.Context, Command, []string, io.Writer,
			) error {
				called = true
				return nil
			}),
		},
		&output,
	)
	if err != nil {
		t.Fatal(err)
	}
	if called || report.CLIInstalled || report.CLIDownloadedThisRun ||
		!strings.HasSuffix(report.CLIPath, "arduino-cli.exe") {
		t.Fatalf("dry-run touched machine or chose wrong CLI: %+v called=%t", report, called)
	}
	if report.ConfigPath == "" || report.DataDir == "" || report.DownloadsDir == "" || report.UserDir == "" {
		t.Fatalf("dry-run omitted managed workspace paths: %+v", report)
	}
	if len(report.ProxyVariables) != 2 || strings.Contains(output.String(), "secret") {
		t.Fatalf("proxy inheritance report leaked or omitted names: %+v\n%s", report, output.String())
	}
	for _, step := range report.Steps {
		if !step.Planned || step.Succeeded {
			t.Fatalf("dry-run step must be planned but unexecuted: %+v", step)
		}
	}
	joined := output.String()
	for _, expected := range []string{
		"MiniCore:avr@3.1.3",
		"Adafruit PWM Servo Driver Library@3.0.3",
		"Adafruit INA219@1.2.3", "rc-switch@2.6.4",
		"TM1637TinyDisplay@1.12.2", "DallasTemperature@4.0.6", "OneWire@2.3.8",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("bootstrap plan missing %q:\n%s", expected, joined)
		}
	}
}

func TestToolchainBootstrapDefaultsToUnversionedCoreAndLibraries(t *testing.T) {
	profile := DefaultToolchainProfile()
	profile.CoreVersion = ""
	for index := range profile.Libraries {
		profile.Libraries[index].Version = ""
	}
	report, err := BootstrapToolchain(context.Background(), ToolchainBootstrapOptions{
		Profile: profile, InstallDir: t.TempDir(), DryRun: true, Managed: true,
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if report.DependenciesLocked {
		t.Fatal("default bootstrap unexpectedly locked dependencies")
	}
	installs := 0
	for _, step := range report.Steps {
		args := step.Command.Args
		if len(args) >= 5 && args[3] == "install" {
			installs++
			if strings.Contains(args[4], "@") {
				t.Fatalf("default bootstrap retained a dependency pin: %v", args)
			}
		}
	}
	if installs != len(profile.Libraries)+1 {
		t.Fatalf("planned %d installs; missing required core/library", installs)
	}
	if _, err := BootstrapToolchain(context.Background(), ToolchainBootstrapOptions{
		Profile: profile, InstallDir: t.TempDir(), DryRun: true, Locked: true, Managed: true,
	}, io.Discard); err == nil {
		t.Fatal("explicit locked replay accepted missing versions")
	}
}

func TestSystemToolchainSeedPreservesConfigAndDirectories(t *testing.T) {
	root := t.TempDir()
	cli := filepath.Join(root, "arduino-cli")
	config := filepath.Join(root, "arduino.yaml")
	original := []byte("network:\n  connection_timeout: 90s\ndirectories:\n  user: /existing/sketchbook\n")
	for path, content := range map[string][]byte{cli: []byte("fixture"), config: original} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var commands []Command
	report, err := BootstrapToolchain(context.Background(), ToolchainBootstrapOptions{
		ConfigPath: config, CLI: cli, InstallDir: filepath.Join(root, "managed"),
		Cores: []string{"MegaCore:avr", "esp32:esp32"}, Libraries: []string{"FastLED"},
		Indexes:     []string{"https://example.org/package_index.json"},
		Environment: []string{"ARDUINO_DIRECTORIES_DATA=/existing/data", "ARDUINO_DIRECTORIES_USER=/existing/sketchbook"},
		Runner: DependencyEnvironmentRunnerFunc(func(_ context.Context, command Command, env []string, _ io.Writer) error {
			commands = append(commands, command)
			if testEnvironmentValue(env, "ARDUINO_DIRECTORIES_DATA") != "/existing/data" || testEnvironmentValue(env, "ARDUINO_DIRECTORIES_USER") != "/existing/sketchbook" {
				t.Fatalf("system seed redirected the existing Arduino environment: %v", env)
			}
			return nil
		}),
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if report.Scope != "system" || report.ConfigPath != config || report.DataDir != "" || report.UserDir != "" || report.DownloadsDir != "" {
		t.Fatalf("misleading system report: %+v", report)
	}
	got, err := os.ReadFile(config)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatalf("existing Arduino config changed: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "managed")); !os.IsNotExist(err) {
		t.Fatalf("system seed created managed workspace: %v", err)
	}
	var all strings.Builder
	for _, command := range commands {
		if len(command.Args) < 3 || command.Args[0] != "--config-file" || command.Args[1] != config {
			t.Fatalf("lost selected config: %+v", command)
		}
		all.WriteString(strings.Join(command.Args, " ") + "\n")
	}
	for _, wanted := range []string{"core install MiniCore:avr", "core install MegaCore:avr", "core install esp32:esp32", "lib install FastLED", "https://example.org/package_index.json"} {
		if !strings.Contains(all.String(), wanted) {
			t.Fatalf("missing %q in %s", wanted, all.String())
		}
	}
	if strings.Contains(all.String(), "upgrade") {
		t.Fatal("seed upgraded unrelated packages")
	}
	if !strings.Contains(commands[len(commands)-1].String(), "lib list") {
		t.Fatal("inventory did not follow all installs")
	}
}

func TestSystemToolchainSeedDryRunDoesNotCreateFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	report, err := BootstrapToolchain(context.Background(), ToolchainBootstrapOptions{InstallDir: root, DryRun: true}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("dry-run created files: %v", err)
	}
	for _, step := range report.Steps {
		if !step.Planned || step.Succeeded || strings.Contains(step.Command.String(), "--config-file") {
			t.Fatalf("system default config plan incorrect: %+v", step)
		}
	}
}

func TestDefaultBootstrapDoesNotDownloadWhenSystemCLIIsMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root := filepath.Join(t.TempDir(), "not-created")
	_, err := BootstrapToolchain(context.Background(), ToolchainBootstrapOptions{InstallDir: root}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--managed") {
		t.Fatalf("missing explicit opt-in error: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("implicit managed installation created files: %v", err)
	}
}

func TestManagedToolchainPathRecognizesOldVersionsAndRejectsSibling(t *testing.T) {
	root := filepath.Join(t.TempDir(), "toolchain")
	if !IsManagedToolchainPath(filepath.Join(root, "arduino-cli", "old", "cli"), root) {
		t.Fatal("old managed version was treated as system")
	}
	if IsManagedToolchainPath(root+"-other", root) {
		t.Fatal("sibling directory was treated as managed")
	}
}

func TestToolchainSeedRejectsInvalidSelectionsBeforeInstall(t *testing.T) {
	for _, options := range []ToolchainBootstrapOptions{
		{Managed: true, ConfigPath: "missing.yaml"}, {ConfigPath: "missing.yaml"},
		{Cores: []string{"--help"}}, {Cores: []string{"invalid"}}, {Cores: []string{"vendor:"}},
		{Libraries: []string{"--git-url"}}, {Libraries: []string{"FastLED@"}},
		{Locked: true, Cores: []string{"MegaCore:avr"}}, {Locked: true, Libraries: []string{"FastLED"}},
		{Indexes: []string{"http://example.org/index.json"}}, {Indexes: []string{"https://user:secret@example.org/index.json"}},
	} {
		options.DryRun = true
		options.InstallDir = filepath.Join(t.TempDir(), "not-created")
		if _, err := BootstrapToolchain(context.Background(), options, io.Discard); err == nil {
			t.Fatalf("accepted invalid seed: %+v", options)
		}
		if _, err := os.Stat(options.InstallDir); !os.IsNotExist(err) {
			t.Fatal("invalid seed touched installation")
		}
	}
}

func TestToolchainBootstrapExistingCLIIsAvailableAndUsesManagedDirectories(t *testing.T) {
	installRoot := t.TempDir()
	cliPath := filepath.Join(
		installRoot, "arduino-cli", "1.5.1", "windows-amd64", "arduino-cli.exe",
	)
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliPath, []byte("existing verified executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	var commands []Command
	var childEnvironments [][]string
	report, err := BootstrapToolchain(
		context.Background(),
		ToolchainBootstrapOptions{
			Profile: DefaultToolchainProfile(), InstallDir: installRoot, Managed: true,
			GOOS: "windows", GOARCH: "amd64",
			Environment: []string{
				"PATH=test", "HTTPS_PROXY=http://proxy.invalid",
				"ARDUINO_DIRECTORIES_DATA=C:\\global-data",
			},
			Runner: DependencyEnvironmentRunnerFunc(func(
				_ context.Context, command Command, environment []string, _ io.Writer,
			) error {
				commands = append(commands, command)
				childEnvironments = append(childEnvironments, append([]string(nil), environment...))
				return nil
			}),
		},
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !report.CLIInstalled || report.CLIDownloadedThisRun {
		t.Fatalf("existing CLI availability/download semantics are wrong: %+v", report)
	}
	if report.CLIPath != cliPath {
		t.Fatalf("CLI path=%q want %q", report.CLIPath, cliPath)
	}
	configuration, err := os.ReadFile(report.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{report.DataDir, report.DownloadsDir, report.UserDir} {
		if !strings.Contains(string(configuration), strconv.Quote(path)) {
			t.Fatalf("managed config omits %q:\n%s", path, configuration)
		}
	}
	if len(commands) == 0 || len(childEnvironments) != len(commands) {
		t.Fatalf("bootstrap steps were not captured: commands=%d envs=%d", len(commands), len(childEnvironments))
	}
	for index, command := range commands {
		if len(command.Args) < 3 || command.Args[0] != "--config-file" || command.Args[1] != report.ConfigPath {
			t.Fatalf("step %d does not use managed config: %+v", index, command)
		}
		for name, want := range map[string]string{
			"ARDUINO_DIRECTORIES_DATA":      report.DataDir,
			"ARDUINO_DIRECTORIES_DOWNLOADS": report.DownloadsDir,
			"ARDUINO_DIRECTORIES_USER":      report.UserDir,
		} {
			if got := testEnvironmentValue(childEnvironments[index], name); got != want {
				t.Fatalf("step %d %s=%q want %q", index, name, got, want)
			}
		}
	}
	later := managedToolchainCLIArguments(report.CLIPath, "compile", "project")
	if len(later) < 4 || later[0] != "--config-file" || later[1] != report.ConfigPath {
		t.Fatalf("later managed CLI call lost profile-local config: %q", later)
	}
}

func testEnvironmentValue(environment []string, wanted string) string {
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, wanted) {
			return value
		}
	}
	return ""
}

func TestManagedCLINameUsesRequestedTargetOS(t *testing.T) {
	if got := executableNameForOS("arduino-cli", "windows"); got != "arduino-cli.exe" {
		t.Fatalf("Windows managed CLI name=%q want arduino-cli.exe", got)
	}
	if got := executableNameForOS("arduino-cli", "linux"); got != "arduino-cli" {
		t.Fatalf("Linux managed CLI name=%q want arduino-cli", got)
	}
}

func TestExplicitToolchainConfigWorksForGlobalCLI(t *testing.T) {
	globalCLI := filepath.Join(t.TempDir(), "Program Files", "arduino-cli.exe")
	managedConfig := filepath.Join(t.TempDir(), "firmware-cli.yaml")
	arguments := toolchainCLIArguments(globalCLI, managedConfig, "compile", "project")
	want := []string{"--config-file", managedConfig, "compile", "project"}
	if !reflect.DeepEqual(arguments, want) {
		t.Fatalf("explicit managed config arguments=%q want %q", arguments, want)
	}
}

func TestVerifiedToolDownloadAndZipExtraction(t *testing.T) {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	entry, err := writer.Create("release/arduino-cli.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("verified-cli")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive.Bytes())
	server := httptest.NewServer(httpHandlerBytes(archive.Bytes()))
	defer server.Close()
	archivePath := filepath.Join(t.TempDir(), "cli.zip")
	if err := os.WriteFile(archivePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := downloadVerified(
		context.Background(), server.Client(), server.URL,
		hex.EncodeToString(digest[:]), archivePath,
	); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "arduino-cli.exe.tmp")
	if err := extractCLIExecutable(archivePath, "zip", destination); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(destination)
	if err != nil || string(content) != "verified-cli" {
		t.Fatalf("extracted CLI=%q err=%v", content, err)
	}
	if err := downloadVerified(
		context.Background(), server.Client(), server.URL,
		strings.Repeat("0", 64), archivePath,
	); err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("bad download digest was accepted: %v", err)
	}
}

type staticHTTPHandler struct{ content []byte }

func (handler staticHTTPHandler) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(handler.content)
}

func httpHandlerBytes(content []byte) staticHTTPHandler {
	return staticHTTPHandler{content: content}
}
