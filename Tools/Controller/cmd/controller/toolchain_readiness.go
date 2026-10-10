package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/programmer"
)

type detectedToolchainProvider struct {
	name       string
	path       string
	version    string
	compatible bool
	managed    bool
}

var platformIOVersion = regexp.MustCompile(`(?i)version\s+([0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?)`)

func (executor *primaryArtifactExecutor) EnsureToolchain(
	ctx artifacts.Context,
	progress artifacts.ProgressFunc,
) (artifacts.ToolchainReadiness, error) {
	if executor.ensureToolchain != nil {
		return executor.ensureToolchain(ctx, progress)
	}
	return ensureLatestBoardToolchain(ctx, executor.store, executor.paths, progress)
}

func ensureLatestBoardToolchain(
	ctx context.Context,
	store *appconfig.Store,
	paths programmer.HostDataPaths,
	progress artifacts.ProgressFunc,
) (artifacts.ToolchainReadiness, error) {
	if store == nil {
		return artifacts.ToolchainReadiness{}, errors.New("toolchain assurance requires host configuration")
	}
	policy := programmer.DefaultToolchainPolicy()
	if err := policy.Validate(); err != nil {
		return artifacts.ToolchainReadiness{}, fmt.Errorf("validate embedded toolchain policy: %w", err)
	}
	progress("toolchain-resolving", -1, "checking upstream stable toolchain policy")
	resolution, resolveErr := programmer.ResolveToolchainPolicy(ctx, policy, programmer.ToolchainResolveOptions{
		DirectRetry: true,
		ModuleDir:   defaultToolchainModuleDir(),
	})
	profile := resolution.Lock.Firmware
	policyProvider := "pccontroller-policy:" + policy.Name + ":live"
	if resolveErr != nil {
		lock := programmer.DefaultToolchainLock()
		profile = lock.Firmware
		policyProvider = "pccontroller-lock:" + lock.ResolvedAt
	}
	current := store.Current()
	configured := strings.TrimSpace(current.Programming.ToolchainCLI)
	configuredConfig := strings.TrimSpace(current.Programming.ToolchainConfig)
	providers := discoverToolchainProviders(ctx, configured, paths.ToolchainDir, profile)
	selected := selectLatestCompatibleCLI(providers, profile.CLI.Version)
	cliPath := ""
	configPath := toolchainConfigPath(configuredConfig, paths.ToolchainDir)
	if resolveErr == nil {
		selectedPath := ""
		if selected != nil {
			selectedPath = selected.path
		}
		progress("toolchain-provisioning", -1, fmt.Sprintf(
			"ensuring %s %s and %s@%s in the shared PCController tool tree",
			profile.CLI.Dependency, profile.CLI.Version, profile.CoreID, profile.CoreVersion,
		))
		report, bootstrapErr := programmer.BootstrapToolchain(ctx, programmer.ToolchainBootstrapOptions{
			Profile: profile, CLI: selectedPath, InstallDir: paths.ToolchainDir,
			DirectRetry: true,
		}, nil)
		if bootstrapErr == nil {
			cliPath = report.CLIPath
			configPath = report.ConfigPath
		} else if selected == nil {
			return artifacts.ToolchainReadiness{}, bootstrapErr
		} else {
			cliPath = selected.path
			if verifyErr := verifyInstalledArduinoToolchain(ctx, cliPath, configPath, profile); verifyErr != nil {
				return artifacts.ToolchainReadiness{}, fmt.Errorf(
					"provision latest board toolchain: %v; verify existing exact installation: %w",
					bootstrapErr, verifyErr,
				)
			}
			policyProvider += ":verified-local-after-provision-error"
		}
	} else {
		progress("toolchain-provisioning", -1, fmt.Sprintf(
			"upstream resolution unavailable; verifying embedded lock %s %s and %s@%s",
			profile.CLI.Dependency, profile.CLI.Version, profile.CoreID, profile.CoreVersion,
		))
		if selected == nil {
			return artifacts.ToolchainReadiness{}, fmt.Errorf(
				"resolve latest compatible toolchain: %w; no exact embedded-lock CLI is installed",
				resolveErr,
			)
		}
		cliPath = selected.path
	}
	if err := verifyInstalledArduinoToolchain(ctx, cliPath, configPath, profile); err != nil {
		if resolveErr != nil {
			return artifacts.ToolchainReadiness{}, fmt.Errorf(
				"resolve latest compatible toolchain: %v; verify embedded locked installation: %w",
				resolveErr, err,
			)
		}
		return artifacts.ToolchainReadiness{}, err
	}
	actualVersion, err := probeArduinoCLIVersion(ctx, cliPath)
	if err != nil {
		return artifacts.ToolchainReadiness{}, fmt.Errorf("verify selected firmware CLI: %w", err)
	}
	if normalizeToolVersion(actualVersion) != normalizeToolVersion(profile.CLI.Version) {
		return artifacts.ToolchainReadiness{}, fmt.Errorf(
			"selected firmware CLI version %s does not match latest resolved stable %s",
			actualVersion, profile.CLI.Version,
		)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Programming.ToolchainCLI = cliPath
		config.Programming.ToolchainConfig = configPath
		return nil
	}); err != nil {
		return artifacts.ToolchainReadiness{}, fmt.Errorf("save reconciled toolchain selection: %w", err)
	}

	providerNames := []string{policyProvider}
	selectedName := "arduino-cli"
	if sameExecutable(cliPath, managedCLIPath(paths.ToolchainDir, profile)) {
		selectedName = "pccontroller-managed-arduino-cli"
	}
	providerNames = append(providerNames, selectedName+":"+actualVersion+":selected")
	compatibleSources := 2
	for _, provider := range providers {
		if provider.name != "platformio" {
			continue
		}
		state := "observed"
		if provider.compatible {
			state = "compatible"
			compatibleSources++
		}
		providerNames = append(providerNames, "platformio:"+provider.version+":"+state)
	}
	return artifacts.ToolchainReadiness{
		Ready: true, Policy: policy.Name, Provider: selectedName, Version: actualVersion,
		CompatibleSources: compatibleSources, Providers: providerNames,
	}, nil
}

func toolchainConfigPath(configured, installRoot string) string {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		if info, err := os.Stat(configured); err == nil && info.Mode().IsRegular() {
			return configured
		}
	}
	return filepath.Join(installRoot, "firmware-cli.yaml")
}

func verifyInstalledArduinoToolchain(
	ctx context.Context,
	cliPath, configPath string,
	profile programmer.ToolchainProfile,
) error {
	version, err := probeArduinoCLIVersion(ctx, cliPath)
	if err != nil {
		return fmt.Errorf("probe installed firmware CLI: %w", err)
	}
	if normalizeToolVersion(version) != normalizeToolVersion(profile.CLI.Version) {
		return fmt.Errorf("firmware CLI %s is installed; exact %s is required", version, profile.CLI.Version)
	}
	if info, err := os.Stat(configPath); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("managed firmware CLI configuration is unavailable at %s", configPath)
	}
	coreInventory, err := runArduinoCLIJSON(ctx, cliPath, configPath, "core", "list", "--format", "json")
	if err != nil {
		return fmt.Errorf("inventory installed board cores: %w", err)
	}
	if !coreInventoryHas(coreInventory, profile.CoreID, profile.CoreVersion) {
		return fmt.Errorf("installed core %s does not match exact version %s", profile.CoreID, profile.CoreVersion)
	}
	libraryInventory, err := runArduinoCLIJSON(ctx, cliPath, configPath, "lib", "list", "--format", "json")
	if err != nil {
		return fmt.Errorf("inventory installed firmware libraries: %w", err)
	}
	if missing := missingToolchainLibraries(libraryInventory, profile.Libraries); len(missing) != 0 {
		return fmt.Errorf("installed firmware libraries do not match the exact profile: %s", strings.Join(missing, ", "))
	}
	return nil
}

func runArduinoCLIJSON(ctx context.Context, cliPath, configPath string, args ...string) ([]byte, error) {
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	environment, err := arduinoCLIEnvironment(os.Environ(), configPath)
	if err != nil {
		return nil, fmt.Errorf("prepare firmware CLI environment: %w", err)
	}
	arguments := append([]string{"--config-file", configPath}, args...)
	command := exec.CommandContext(probe, cliPath, arguments...)
	command.Env = environment
	command.Stdin = nil
	var stderr bytes.Buffer
	command.Stderr = &stderr
	content, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return content, nil
}

type arduinoCLIConfiguration struct {
	Directories struct {
		Data      string `yaml:"data"`
		Downloads string `yaml:"downloads"`
		User      string `yaml:"user"`
	} `yaml:"directories"`
}

// arduinoCLIEnvironment makes the paths in the reviewed CLI configuration
// authoritative for child-process discovery. Arduino CLI otherwise attempts
// to resolve a Windows Documents known folder before fully applying the YAML;
// virtual service accounts do not have one and consequently expose an empty
// installed-core inventory even when the shared toolchain is intact.
func arduinoCLIEnvironment(base []string, configPath string) ([]string, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config arduinoCLIConfiguration
	if err := yaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("decode %s: %w", configPath, err)
	}
	overrides := map[string]string{}
	for name, value := range map[string]string{
		"ARDUINO_DIRECTORIES_DATA":      config.Directories.Data,
		"ARDUINO_DIRECTORIES_DOWNLOADS": config.Directories.Downloads,
		"ARDUINO_DIRECTORIES_USER":      config.Directories.User,
	} {
		if value = strings.TrimSpace(value); value != "" {
			overrides[name] = value
		}
	}
	if runtime.GOOS == "windows" {
		absoluteConfig, absoluteErr := filepath.Abs(configPath)
		if absoluteErr != nil {
			return nil, absoluteErr
		}
		managedProfile := filepath.Join(filepath.Dir(absoluteConfig), "cli-profile")
		profileRoot := arduinoCLIProfileRoot(config, absoluteConfig)
		if strings.EqualFold(filepath.Clean(profileRoot), filepath.Clean(managedProfile)) {
			for _, directory := range []string{
				filepath.Join(profileRoot, "Documents"),
				filepath.Join(profileRoot, "AppData", "Local"),
				filepath.Join(profileRoot, "AppData", "Roaming"),
			} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					return nil, fmt.Errorf("prepare firmware CLI service profile: %w", err)
				}
			}
		}
		overrides["APPDATA"] = filepath.Join(profileRoot, "AppData", "Roaming")
		overrides["LOCALAPPDATA"] = filepath.Join(profileRoot, "AppData", "Local")
		overrides["HOME"] = profileRoot
		overrides["USERPROFILE"] = profileRoot
		if volume := filepath.VolumeName(profileRoot); volume != "" {
			overrides["HOMEDRIVE"] = volume
			overrides["HOMEPATH"] = strings.TrimPrefix(profileRoot, volume)
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, found := strings.Cut(entry, "=")
		if found {
			if _, overridden := overrides[strings.ToUpper(name)]; overridden {
				continue
			}
		}
		result = append(result, entry)
	}
	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		result = append(result, name+"="+overrides[name])
	}
	return result, nil
}

// arduinoCLIProfileRoot keeps a service subprocess on the same existing owner
// profile when the reviewed sketchbook path explicitly identifies one. The
// Arduino CLI asks Windows for profile known folders before it applies the
// configured directory overrides; a synthetic profile can therefore hide an
// otherwise valid shared core inventory. Managed/non-profile layouts retain a
// service-owned fallback next to the reviewed configuration.
func arduinoCLIProfileRoot(config arduinoCLIConfiguration, absoluteConfig string) string {
	userDirectory := filepath.Clean(strings.TrimSpace(config.Directories.User))
	if userDirectory != "." {
		documents := filepath.Dir(userDirectory)
		if strings.EqualFold(filepath.Base(documents), "Documents") {
			profile := filepath.Dir(documents)
			if info, err := os.Stat(profile); err == nil && info.IsDir() {
				return profile
			}
		}
	}
	return filepath.Join(filepath.Dir(absoluteConfig), "cli-profile")
}

func coreInventoryHas(content []byte, id, version string) bool {
	var inventory struct {
		Platforms []struct {
			ID               string `json:"id"`
			InstalledVersion string `json:"installed_version"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		return false
	}
	for _, platform := range inventory.Platforms {
		if platform.ID == id && normalizeToolVersion(platform.InstalledVersion) == normalizeToolVersion(version) {
			return true
		}
	}
	return false
}

func missingToolchainLibraries(content []byte, required []programmer.ToolchainLibrary) []string {
	var inventory struct {
		Installed []struct {
			Library struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"library"`
		} `json:"installed_libraries"`
	}
	if err := json.Unmarshal(content, &inventory); err != nil {
		return []string{"invalid library inventory"}
	}
	installed := make(map[string]string, len(inventory.Installed))
	for _, entry := range inventory.Installed {
		installed[strings.ToLower(strings.TrimSpace(entry.Library.Name))] = normalizeToolVersion(entry.Library.Version)
	}
	var missing []string
	for _, library := range required {
		if installed[strings.ToLower(strings.TrimSpace(library.Name))] != normalizeToolVersion(library.Version) {
			missing = append(missing, library.Name+"@"+library.Version)
		}
	}
	return missing
}

func discoverToolchainProviders(
	ctx context.Context,
	configured, installRoot string,
	profile programmer.ToolchainProfile,
) []detectedToolchainProvider {
	var result []detectedToolchainProvider
	seen := make(map[string]bool)
	addArduino := func(path string, managed bool) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		resolved := resolveExecutable(path)
		if resolved == "" {
			return
		}
		key := executableKey(resolved)
		if seen[key] {
			return
		}
		seen[key] = true
		version, err := probeArduinoCLIVersion(ctx, resolved)
		if err != nil {
			return
		}
		result = append(result, detectedToolchainProvider{
			name: "arduino-cli", path: resolved, version: version,
			compatible: normalizeToolVersion(version) == normalizeToolVersion(profile.CLI.Version),
			managed:    managed,
		})
	}
	addArduino(configured, false)
	addArduino(managedCLIPath(installRoot, profile), true)
	if path, err := exec.LookPath(executableName("arduino-cli")); err == nil {
		addArduino(path, false)
	}

	for _, name := range []string{"platformio", "pio"} {
		path, err := exec.LookPath(executableName(name))
		if err != nil {
			continue
		}
		key := executableKey(path)
		if seen[key] {
			continue
		}
		seen[key] = true
		version, versionErr := probePlatformIOVersion(ctx, path)
		if versionErr != nil {
			continue
		}
		result = append(result, detectedToolchainProvider{
			name: "platformio", path: path, version: version,
			compatible: platformIOHasAVR(ctx, path),
		})
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].name != result[right].name {
			return result[left].name < result[right].name
		}
		if result[left].compatible != result[right].compatible {
			return result[left].compatible
		}
		return result[left].path < result[right].path
	})
	return result
}

func selectLatestCompatibleCLI(providers []detectedToolchainProvider, required string) *detectedToolchainProvider {
	var selected *detectedToolchainProvider
	for index := range providers {
		provider := &providers[index]
		if provider.name != "arduino-cli" || !provider.compatible ||
			normalizeToolVersion(provider.version) != normalizeToolVersion(required) {
			continue
		}
		if selected == nil || (!provider.managed && selected.managed) {
			selected = provider
		}
	}
	return selected
}

func probeArduinoCLIVersion(ctx context.Context, path string) (string, error) {
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probe, path, "version", "--format", "json")
	command.Stdin = nil
	content, err := command.Output()
	if err != nil {
		return "", err
	}
	var value struct {
		Version string `json:"VersionString"`
	}
	if err := json.Unmarshal(content, &value); err != nil {
		return "", err
	}
	value.Version = normalizeToolVersion(value.Version)
	if value.Version == "" {
		return "", errors.New("firmware CLI returned no semantic version")
	}
	return value.Version, nil
}

func probePlatformIOVersion(ctx context.Context, path string) (string, error) {
	probe, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(probe, path, "--version")
	command.Stdin = nil
	content, err := command.CombinedOutput()
	if err != nil {
		return "", err
	}
	match := platformIOVersion.FindStringSubmatch(string(content))
	if len(match) != 2 {
		return "", errors.New("PlatformIO returned no semantic version")
	}
	return normalizeToolVersion(match[1]), nil
}

func platformIOHasAVR(ctx context.Context, path string) bool {
	probe, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	command := exec.CommandContext(probe, path, "boards", "--installed", "--json-output")
	command.Stdin = nil
	content, err := command.Output()
	return err == nil && platformIOAVRInstalled(content)
}

func platformIOAVRInstalled(content []byte) bool {
	var boards []struct {
		Platform string `json:"platform"`
	}
	if err := json.Unmarshal(content, &boards); err != nil {
		return false
	}
	for _, board := range boards {
		if strings.EqualFold(strings.TrimSpace(board.Platform), "atmelavr") {
			return true
		}
	}
	return false
}

func managedCLIPath(root string, profile programmer.ToolchainProfile) string {
	return filepath.Join(
		root, profile.CLI.Dependency, profile.CLI.Version,
		runtime.GOOS+"-"+runtime.GOARCH, executableName(profile.CLI.Dependency),
	)
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		return name + ".exe"
	}
	return name
}

func resolveExecutable(path string) string {
	if strings.ContainsAny(path, `/\\`) || filepath.IsAbs(path) {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			if absolute, absoluteErr := filepath.Abs(path); absoluteErr == nil {
				return absolute
			}
			return path
		}
		return ""
	}
	resolved, err := exec.LookPath(path)
	if err != nil {
		return ""
	}
	return resolved
}

func executableKey(path string) string {
	absolute, err := filepath.Abs(path)
	if err == nil {
		path = absolute
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func sameExecutable(left, right string) bool {
	return executableKey(left) == executableKey(right)
}

func normalizeToolVersion(value string) string {
	return strings.TrimPrefix(strings.TrimSpace(value), "v")
}
