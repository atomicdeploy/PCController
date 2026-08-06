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
			Profile: DefaultToolchainProfile(), InstallDir: t.TempDir(),
			GOOS: "windows", GOARCH: "amd64", DryRun: true,
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
		"MiniCore:avr@3.1.2",
		"Adafruit PWM Servo Driver Library@3.0.3",
		"Adafruit INA219@1.2.3", "rc-switch@2.6.4",
		"TM1637TinyDisplay@1.12.2", "DallasTemperature@4.0.6", "OneWire@2.3.8",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("bootstrap plan missing %q:\n%s", expected, joined)
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
			Profile: DefaultToolchainProfile(), InstallDir: installRoot,
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
