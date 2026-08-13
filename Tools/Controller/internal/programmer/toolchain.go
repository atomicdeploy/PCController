package programmer

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"pccontroller.local/controller/internal/netpolicy"

	"golang.org/x/net/http/httpproxy"
)

const maximumToolchainArchiveBytes int64 = 128 << 20

type ToolchainLibrary struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ToolchainAsset struct {
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Archive string `json:"archive"`
}

type ToolchainCLI struct {
	Dependency string           `json:"dependency"`
	Version    string           `json:"version"`
	Assets     []ToolchainAsset `json:"assets"`
}

// ToolchainProfile is the reproducible machine bootstrap manifest. Public
// commands call this a generic firmware toolchain; dependency-specific names
// stay confined to the manifest and implementation boundary.
type ToolchainProfile struct {
	Name           string             `json:"name"`
	FQBN           string             `json:"fqbn"`
	PackageIndexes []string           `json:"package_indexes"`
	CLI            ToolchainCLI       `json:"cli"`
	CoreID         string             `json:"core_id"`
	CoreVersion    string             `json:"core_version"`
	Libraries      []ToolchainLibrary `json:"libraries"`
	ProvidedTools  []string           `json:"provided_tools"`
}

func DefaultToolchainProfile() ToolchainProfile {
	policy := DefaultToolchainPolicy()
	return ToolchainProfile{
		Name:           policy.Name,
		FQBN:           policy.FQBN,
		PackageIndexes: []string{MiniCorePackageIndexURL},
		CLI: ToolchainCLI{
			Dependency: "arduino-cli", Version: "1.5.1",
			Assets: []ToolchainAsset{
				{GOOS: "windows", GOARCH: "amd64", Archive: "zip", SHA256: "fabe42e0eb04d00e776a66178299ff95a46c623dbc260f997e58fd514853dd40", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_Windows_64bit.zip"},
				{GOOS: "windows", GOARCH: "386", Archive: "zip", SHA256: "885e491c7c7fb8b396151c09daa5c4c56d8b60697d172a5cfe72c939eed50fe3", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_Windows_32bit.zip"},
				{GOOS: "linux", GOARCH: "amd64", Archive: "tar.gz", SHA256: "28a8e119c498a25607821c36cb2dc49e8463941b261a0d99091baa7bc692dd2b", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_Linux_64bit.tar.gz"},
				{GOOS: "linux", GOARCH: "arm64", Archive: "tar.gz", SHA256: "1e69e077479f300614d4551334e0a33f08ee40b04315d83b8e7e0e94f0d0ee62", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_Linux_ARM64.tar.gz"},
				{GOOS: "darwin", GOARCH: "amd64", Archive: "tar.gz", SHA256: "c982e940027996bea9901050e95fae99c59c1dcfee54beedecaf28141e7bf2e7", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_macOS_64bit.tar.gz"},
				{GOOS: "darwin", GOARCH: "arm64", Archive: "tar.gz", SHA256: "cb952e8c1621c95ef5f1d17831c945e3d0ec5973f89c557a7ec8feb9c4f7d4c9", URL: "https://github.com/arduino/arduino-cli/releases/download/v1.5.1/arduino-cli_1.5.1_macOS_ARM64.tar.gz"},
			},
		},
		CoreID: "MiniCore:avr", CoreVersion: "3.1.2",
		Libraries: []ToolchainLibrary{
			{Name: "Adafruit PWM Servo Driver Library", Version: "3.0.3"},
			{Name: "Adafruit INA219", Version: "1.2.3"},
			{Name: "rc-switch", Version: "2.6.4"},
			{Name: "TM1637TinyDisplay", Version: "1.12.2"},
			{Name: "DallasTemperature", Version: "4.0.6"},
			{Name: "OneWire", Version: "2.3.8"},
		},
		ProvidedTools: []string{
			"avr-gcc 7.3.0-atmel3.6.1-arduino7 (installed by MiniCore)",
			"AVRDUDE 8.0-arduino.1 (installed by MiniCore)",
			"Urboot/Urclock UART programmer metadata (installed by MiniCore)",
		},
	}
}

func LoadToolchainProfile(path string) (ToolchainProfile, error) {
	if strings.TrimSpace(path) == "" {
		profile := DefaultToolchainProfile()
		return profile, profile.Validate()
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return ToolchainProfile{}, fmt.Errorf("read toolchain profile: %w", err)
	}
	var profile ToolchainProfile
	if err := json.Unmarshal(content, &profile); err != nil {
		return ToolchainProfile{}, fmt.Errorf("decode toolchain profile: %w", err)
	}
	return profile, profile.Validate()
}

func (profile ToolchainProfile) Validate() error {
	if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.FQBN) == "" {
		return errors.New("toolchain profile requires name and FQBN")
	}
	if profile.CLI.Version == "" || profile.CoreID == "" || profile.CoreVersion == "" {
		return errors.New("toolchain profile requires CLI, core ID, and core versions")
	}
	if len(profile.PackageIndexes) == 0 || len(profile.CLI.Assets) == 0 {
		return errors.New("toolchain profile requires package indexes and CLI assets")
	}
	for _, asset := range profile.CLI.Assets {
		if asset.GOOS == "" || asset.GOARCH == "" || asset.URL == "" ||
			(asset.Archive != "zip" && asset.Archive != "tar.gz") {
			return errors.New("toolchain CLI asset is incomplete")
		}
		if digest, err := hex.DecodeString(asset.SHA256); err != nil || len(digest) != sha256.Size {
			return fmt.Errorf("toolchain CLI asset %s/%s has invalid SHA-256", asset.GOOS, asset.GOARCH)
		}
		parsed, err := url.Parse(asset.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("toolchain CLI asset %s/%s requires an HTTPS URL", asset.GOOS, asset.GOARCH)
		}
	}
	for _, library := range profile.Libraries {
		if strings.TrimSpace(library.Name) == "" || strings.TrimSpace(library.Version) == "" {
			return errors.New("toolchain library entries require name and version")
		}
	}
	return nil
}

type ToolchainBootstrapOptions struct {
	Profile     ToolchainProfile
	CLI         string
	InstallDir  string
	Environment []string
	DirectRetry bool
	DryRun      bool
	GOOS        string
	GOARCH      string
	Runner      DependencyEnvironmentRunner
}

type ToolchainBootstrapReport struct {
	Profile    string `json:"profile"`
	FQBN       string `json:"fqbn"`
	CLIPath    string `json:"cli_path"`
	CLIVersion string `json:"cli_version"`
	// CLIInstalled means the selected executable is already available at return.
	CLIInstalled         bool                `json:"cli_installed"`
	CLIDownloadedThisRun bool                `json:"cli_downloaded_this_run"`
	ConfigPath           string              `json:"config_path"`
	DataDir              string              `json:"data_dir"`
	DownloadsDir         string              `json:"downloads_dir"`
	UserDir              string              `json:"user_dir"`
	ProxyVariables       []string            `json:"proxy_variables,omitempty"`
	Steps                []ToolchainSyncStep `json:"steps"`
}

// ToolchainAdoptReport describes a local, network-free migration from a
// previously verified managed Arduino workspace into an existing shared
// Arduino installation. It never downloads or invokes an external copier.
type ToolchainAdoptReport struct {
	SourceData string   `json:"source_data"`
	TargetData string   `json:"target_data"`
	TargetUser string   `json:"target_user"`
	Packages   []string `json:"packages"`
	Libraries  []string `json:"libraries"`
}

// AdoptToolchain installs complete package-vendor and library directories
// from a verified managed workspace into shared Arduino roots. Each directory
// is staged beside its destination and swapped only after a complete copy, so
// an interrupted migration cannot leave the target with half a core.
func AdoptToolchain(sourceData, sourceUser, targetData, targetUser string) (ToolchainAdoptReport, error) {
	report := ToolchainAdoptReport{SourceData: sourceData, TargetData: targetData, TargetUser: targetUser}
	for _, value := range []struct{ label, path string }{
		{"source data", sourceData}, {"source user", sourceUser},
		{"target data", targetData}, {"target user", targetUser},
	} {
		if strings.TrimSpace(value.path) == "" || !filepath.IsAbs(value.path) {
			return report, fmt.Errorf("%s directory must be absolute", value.label)
		}
	}
	packageRoot := filepath.Join(sourceData, "packages")
	for _, vendor := range []string{"arduino", "builtin", "MiniCore"} {
		source := filepath.Join(packageRoot, vendor)
		if info, err := os.Stat(source); err != nil || !info.IsDir() {
			return report, fmt.Errorf("verified source package %s is unavailable: %w", vendor, err)
		}
		if err := adoptDirectory(source, filepath.Join(targetData, "packages", vendor)); err != nil {
			return report, fmt.Errorf("adopt package %s: %w", vendor, err)
		}
		report.Packages = append(report.Packages, vendor)
	}
	sourceLibraries := filepath.Join(sourceUser, "libraries")
	entries, err := os.ReadDir(sourceLibraries)
	if err != nil {
		return report, fmt.Errorf("read verified source libraries: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := adoptDirectory(
			filepath.Join(sourceLibraries, entry.Name()),
			filepath.Join(targetUser, "libraries", entry.Name()),
		); err != nil {
			return report, fmt.Errorf("adopt library %s: %w", entry.Name(), err)
		}
		report.Libraries = append(report.Libraries, entry.Name())
	}
	return report, nil
}

func adoptDirectory(source, target string) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".pccontroller-adopt-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(stage, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	}); err != nil {
		return err
	}
	backup := target + ".pccontroller-previous"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(stage, target); err != nil {
		_ = os.Rename(backup, target)
		return err
	}
	return os.RemoveAll(backup)
}

// BootstrapToolchain installs a pinned CLI into the host data directory, then
// installs the exact AVR core, compiler/programmer tools, and libraries named
// by the profile. Every child receives the caller's complete environment.
func BootstrapToolchain(
	ctx context.Context,
	options ToolchainBootstrapOptions,
	output io.Writer,
) (ToolchainBootstrapReport, error) {
	if output == nil {
		output = io.Discard
	}
	profile := options.Profile
	if profile.Name == "" {
		profile = DefaultToolchainProfile()
	}
	if err := profile.Validate(); err != nil {
		return ToolchainBootstrapReport{}, err
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	} else {
		environment = append([]string(nil), environment...)
	}
	environment = netpolicy.WithLocalNetworkNoProxy(environment)
	goos, goarch := options.GOOS, options.GOARCH
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	installRoot := strings.TrimSpace(options.InstallDir)
	if installRoot == "" {
		paths, pathErr := DefaultHostDataPaths()
		if pathErr != nil {
			return ToolchainBootstrapReport{}, pathErr
		}
		installRoot = paths.ToolchainDir
	}
	installRoot, err := filepath.Abs(installRoot)
	if err != nil {
		return ToolchainBootstrapReport{}, fmt.Errorf("resolve managed toolchain directory: %w", err)
	}
	workspace := newToolchainWorkspace(installRoot)
	report := ToolchainBootstrapReport{
		Profile: profile.Name, FQBN: profile.FQBN, CLIVersion: profile.CLI.Version,
		ConfigPath: workspace.configPath, DataDir: workspace.dataDir,
		DownloadsDir: workspace.downloadsDir, UserDir: workspace.userDir,
		ProxyVariables: proxyEnvironmentNames(environment),
	}
	cliPath := strings.TrimSpace(options.CLI)
	if cliPath == "" {
		asset, err := profile.cliAsset(goos, goarch)
		if err != nil {
			return report, err
		}
		cliPath = filepath.Join(
			installRoot, profile.CLI.Dependency, profile.CLI.Version,
			goos+"-"+goarch, executableNameForOS(profile.CLI.Dependency, goos),
		)
		if options.DryRun {
			fmt.Fprintf(output, "dry-run: verify/download %s %s for %s/%s to %s\n",
				profile.CLI.Dependency, profile.CLI.Version, goos, goarch, cliPath)
		} else if _, err := os.Stat(cliPath); errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(output, "Installing pinned firmware CLI %s for %s/%s (SHA-256 verified)\n",
				profile.CLI.Version, goos, goarch)
			if err := installToolchainCLI(ctx, asset, cliPath, environment, options.DirectRetry); err != nil {
				return report, err
			}
			report.CLIDownloadedThisRun = true
		} else if err != nil {
			return report, fmt.Errorf("inspect managed firmware CLI: %w", err)
		}
	}
	if options.DryRun {
		if resolved, resolveErr := resolveAvailableToolchainCLI(cliPath); resolveErr == nil {
			cliPath = resolved
			report.CLIInstalled = true
		}
	} else {
		resolved, resolveErr := resolveAvailableToolchainCLI(cliPath)
		if resolveErr != nil {
			return report, resolveErr
		}
		cliPath = resolved
		if err := writeToolchainConfig(workspace, profile.PackageIndexes); err != nil {
			return report, err
		}
		report.CLIInstalled = true
	}
	report.CLIPath = cliPath
	environment = withDependencyProxyEnvironment(
		withToolchainDirectoryEnvironment(environment, workspace),
	)
	runner := options.Runner
	if runner == nil {
		runner = DependencyEnvironmentRunnerFunc(runArduinoEnvironment)
	}
	steps := profile.installSteps()
	directEnvironment := withoutProxyEnvironment(environment)
	for _, configured := range steps {
		arguments := append([]string{"--config-file", workspace.configPath}, configured.args...)
		step := ToolchainSyncStep{
			Name:      configured.name,
			Command:   Command{Name: cliPath, Args: arguments},
			UsedProxy: len(report.ProxyVariables) != 0,
		}
		fmt.Fprintln(output, "\n▶", configured.name)
		fmt.Fprintln(output, step.Command.String())
		if options.DryRun {
			step.Planned = true
			fmt.Fprintln(output, "  dry-run: not executed")
			report.Steps = append(report.Steps, step)
			continue
		}
		runErr := runner.Run(ctx, step.Command, environment, output)
		if runErr != nil && options.DirectRetry {
			step.RetriedDirect = true
			fmt.Fprintln(output, "⚠ configured-network attempt failed; retrying this child once without proxy variables")
			runErr = runner.Run(ctx, step.Command, directEnvironment, output)
		}
		if runErr != nil {
			fmt.Fprintln(output, "❌", configured.name, "failed:", runErr)
			report.Steps = append(report.Steps, step)
			return report, fmt.Errorf("%s: %w", configured.name, runErr)
		}
		step.Succeeded = true
		fmt.Fprintln(output, "✅", configured.name)
		report.Steps = append(report.Steps, step)
	}
	return report, nil
}

type toolchainWorkspace struct {
	configPath   string
	dataDir      string
	downloadsDir string
	userDir      string
}

func newToolchainWorkspace(root string) toolchainWorkspace {
	return toolchainWorkspace{
		configPath:   filepath.Join(root, "firmware-cli.yaml"),
		dataDir:      filepath.Join(root, "data"),
		downloadsDir: filepath.Join(root, "downloads"),
		userDir:      filepath.Join(root, "user"),
	}
}

func writeToolchainConfig(workspace toolchainWorkspace, packageIndexes []string) error {
	for _, directory := range []string{workspace.dataDir, workspace.downloadsDir, workspace.userDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create managed toolchain directory: %w", err)
		}
	}
	var content strings.Builder
	content.WriteString("board_manager:\n  additional_urls:\n")
	for _, index := range packageIndexes {
		fmt.Fprintf(&content, "    - %s\n", strconv.Quote(index))
	}
	content.WriteString("directories:\n")
	fmt.Fprintf(&content, "  data: %s\n", strconv.Quote(workspace.dataDir))
	fmt.Fprintf(&content, "  downloads: %s\n", strconv.Quote(workspace.downloadsDir))
	fmt.Fprintf(&content, "  user: %s\n", strconv.Quote(workspace.userDir))
	if err := writeFileAtomicReplace(workspace.configPath, []byte(content.String()), 0o600); err != nil {
		return fmt.Errorf("write managed toolchain configuration: %w", err)
	}
	return nil
}

func withToolchainDirectoryEnvironment(environment []string, workspace toolchainWorkspace) []string {
	overrides := map[string]string{
		"ARDUINO_DIRECTORIES_DATA":      workspace.dataDir,
		"ARDUINO_DIRECTORIES_DOWNLOADS": workspace.downloadsDir,
		"ARDUINO_DIRECTORIES_USER":      workspace.userDir,
	}
	result := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, overridden := overrides[strings.ToUpper(name)]; overridden {
				continue
			}
		}
		result = append(result, entry)
	}
	for _, name := range []string{
		"ARDUINO_DIRECTORIES_DATA",
		"ARDUINO_DIRECTORIES_DOWNLOADS",
		"ARDUINO_DIRECTORIES_USER",
	} {
		result = append(result, name+"="+overrides[name])
	}
	return result
}

func resolveAvailableToolchainCLI(path string) (string, error) {
	if strings.ContainsAny(path, `/\\`) || filepath.IsAbs(path) {
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("firmware CLI is unavailable at %s: %w", path, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("firmware CLI path is a directory: %s", path)
		}
		return path, nil
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return "", fmt.Errorf("firmware CLI %q is unavailable: %w", path, err)
	}
	return resolved, nil
}

// managedToolchainCLIArguments binds later compile/programmer discovery calls
// to the same profile-local core, library, download, and user directories that
// bootstrap populated. Unmanaged dependency CLIs retain their normal config.
func managedToolchainCLIArguments(executable string, arguments ...string) []string {
	for index, argument := range arguments {
		if argument == "--config-file" && index+1 < len(arguments) {
			return append([]string(nil), arguments...)
		}
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return append([]string(nil), arguments...)
	}
	directory := filepath.Dir(absolute)
	for depth := 0; depth < 5; depth++ {
		candidate := filepath.Join(directory, "firmware-cli.yaml")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			result := []string{"--config-file", candidate}
			return append(result, arguments...)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return append([]string(nil), arguments...)
}

// toolchainCLIArguments prefers the persisted config returned by bootstrap.
// This is required when an existing global arduino-cli lives outside the
// managed data tree; portable CLIs can still discover their adjacent config.
func toolchainCLIArguments(executable, configuration string, arguments ...string) []string {
	if strings.TrimSpace(configuration) != "" {
		return append([]string{"--config-file", configuration}, arguments...)
	}
	return managedToolchainCLIArguments(executable, arguments...)
}

func (profile ToolchainProfile) cliAsset(goos, goarch string) (ToolchainAsset, error) {
	for _, asset := range profile.CLI.Assets {
		if asset.GOOS == goos && asset.GOARCH == goarch {
			return asset, nil
		}
	}
	return ToolchainAsset{}, fmt.Errorf("toolchain profile has no CLI asset for %s/%s", goos, goarch)
}

func (profile ToolchainProfile) installSteps() []struct {
	name string
	args []string
} {
	withIndexes := func(arguments ...string) []string {
		result := append([]string(nil), arguments...)
		if len(profile.PackageIndexes) != 0 {
			result = append(result, "--additional-urls", strings.Join(profile.PackageIndexes, ","))
		}
		return result
	}
	steps := []struct {
		name string
		args []string
	}{
		{"refresh core index", withIndexes("core", "update-index")},
		{"refresh library index", []string{"lib", "update-index"}},
		{"install pinned core " + profile.CoreID + "@" + profile.CoreVersion,
			withIndexes("core", "install", profile.CoreID+"@"+profile.CoreVersion)},
	}
	for _, library := range profile.Libraries {
		steps = append(steps, struct {
			name string
			args []string
		}{"install pinned library " + library.Name + "@" + library.Version,
			[]string{"lib", "install", library.Name + "@" + library.Version}})
	}
	return append(steps,
		struct {
			name string
			args []string
		}{"core inventory", []string{"core", "list"}},
		struct {
			name string
			args []string
		}{"library inventory", []string{"lib", "list"}},
	)
}

func installToolchainCLI(
	ctx context.Context,
	asset ToolchainAsset,
	destination string,
	environment []string,
	directRetry bool,
) error {
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	archive, err := os.CreateTemp(directory, ".toolchain-download-*")
	if err != nil {
		return err
	}
	archivePath := archive.Name()
	if err := archive.Close(); err != nil {
		return err
	}
	defer os.Remove(archivePath)
	download := func(proxy func(*http.Request) (*url.URL, error)) error {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.Proxy = proxy
		client := &http.Client{Transport: transport, Timeout: 5 * time.Minute}
		return downloadVerified(ctx, client, asset.URL, asset.SHA256, archivePath)
	}
	proxy := proxyFromEnvironmentSlice(environment)
	if err := download(proxy); err != nil {
		if !directRetry {
			return err
		}
		if directErr := download(nil); directErr != nil {
			return errors.Join(
				fmt.Errorf("configured-network tool download: %w", err),
				fmt.Errorf("direct tool download: %w", directErr),
			)
		}
	}
	temporary := destination + ".tmp"
	_ = os.Remove(temporary)
	if err := extractCLIExecutable(archivePath, asset.Archive, temporary); err != nil {
		return err
	}
	defer os.Remove(temporary)
	if err := os.Chmod(temporary, 0o755); err != nil {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return fmt.Errorf("publish firmware CLI: %w", err)
	}
	return nil
}

func proxyFromEnvironmentSlice(environment []string) func(*http.Request) (*url.URL, error) {
	values := make(map[string]string)
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && value != "" {
			values[strings.ToUpper(name)] = value
		}
	}
	all := values["ALL_PROXY"]
	httpProxy := values["HTTP_PROXY"]
	httpsProxy := values["HTTPS_PROXY"]
	if httpProxy == "" {
		httpProxy = all
	}
	if httpsProxy == "" {
		httpsProxy = all
	}
	proxy := (&httpproxy.Config{
		HTTPProxy: httpProxy, HTTPSProxy: httpsProxy, NoProxy: values["NO_PROXY"],
	}).ProxyFunc()
	return func(request *http.Request) (*url.URL, error) { return proxy(request.URL) }
}

func downloadVerified(
	ctx context.Context,
	client *http.Client,
	sourceURL, expectedHash, destination string,
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %s", response.Status)
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maximumToolchainArchiveBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maximumToolchainArchiveBytes {
		return errors.New("toolchain download exceeds 128 MiB safety limit")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("toolchain download SHA-256 mismatch: got %s", actual)
	}
	return nil
}

func extractCLIExecutable(archivePath, format, destination string) error {
	// Downloads are extracted to an adjacent .tmp path and atomically renamed.
	// Match the final executable name inside the upstream archive, not the
	// transaction suffix on the local destination.
	wantedExecutable := strings.TrimSuffix(filepath.Base(destination), ".tmp")
	switch format {
	case "zip":
		reader, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, entry := range reader.File {
			if strings.EqualFold(filepath.Base(entry.Name), wantedExecutable) && !entry.FileInfo().IsDir() {
				source, err := entry.Open()
				if err != nil {
					return err
				}
				err = copyExecutable(source, destination)
				_ = source.Close()
				return err
			}
		}
	case "tar.gz":
		file, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer file.Close()
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer compressed.Close()
		reader := tar.NewReader(compressed)
		for {
			header, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if strings.EqualFold(filepath.Base(header.Name), wantedExecutable) && header.Typeflag == tar.TypeReg {
				return copyExecutable(io.LimitReader(reader, header.Size), destination)
			}
		}
	default:
		return fmt.Errorf("unsupported toolchain archive %q", format)
	}
	return errors.New("firmware CLI executable is absent from verified archive")
}

func copyExecutable(source io.Reader, destination string) error {
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, source)
	closeErr := file.Close()
	return errors.Join(copyErr, closeErr)
}
