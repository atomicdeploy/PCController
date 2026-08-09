package programmer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"pccontroller.local/controller/internal/netpolicy"
)

const (
	RequiredArduinoCore       = "MiniCore:avr"
	MiniCorePackageIndexURL   = "https://mcudude.github.io/MiniCore/package_MCUdude_MiniCore_index.json"
	arduinoNetworkProxyEnvKey = "ARDUINO_NETWORK_PROXY"
)

var RequiredArduinoLibraries = []string{
	"Adafruit PWM Servo Driver Library",
	"Adafruit INA219",
	"rc-switch",
	"TM1637TinyDisplay",
	"DallasTemperature",
	"OneWire",
}

type ToolchainSyncOptions struct {
	ToolchainCLI string
	Environment  []string
	DirectRetry  bool
	DryRun       bool
}

type ToolchainSyncStep struct {
	Name          string  `json:"name"`
	Command       Command `json:"command"`
	UsedProxy     bool    `json:"used_proxy"`
	RetriedDirect bool    `json:"retried_direct"`
	Planned       bool    `json:"planned,omitempty"`
	Succeeded     bool    `json:"succeeded"`
}

type ToolchainSyncReport struct {
	ProxyVariables []string            `json:"proxy_variables,omitempty"`
	Steps          []ToolchainSyncStep `json:"steps"`
}

// DependencyEnvironmentRunner makes environment handling injectable so proxy
// fallback is testable without invoking the dependency CLI or network.
type DependencyEnvironmentRunner interface {
	Run(context.Context, Command, []string, io.Writer) error
}

type DependencyEnvironmentRunnerFunc func(
	context.Context,
	Command,
	[]string,
	io.Writer,
) error

func (function DependencyEnvironmentRunnerFunc) Run(
	ctx context.Context,
	command Command,
	environment []string,
	output io.Writer,
) error {
	return function(ctx, command, environment, output)
}

func runArduinoEnvironment(
	ctx context.Context,
	command Command,
	environment []string,
	output io.Writer,
) error {
	if output == nil {
		output = io.Discard
	}
	process := exec.CommandContext(ctx, command.Name, command.Args...)
	process.Env = environment
	process.Stdout = output
	process.Stderr = output
	process.Stdin = nil
	if err := process.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", command.String(), err)
	}
	return nil
}

func SyncToolchain(
	ctx context.Context,
	options ToolchainSyncOptions,
	output io.Writer,
) (ToolchainSyncReport, error) {
	return SyncToolchainWithRunner(
		ctx,
		options,
		output,
		DependencyEnvironmentRunnerFunc(runArduinoEnvironment),
	)
}

// SyncToolchainWithRunner updates indexes and every installed core/library,
// then installs the project's selected core and libraries at their latest
// indexed versions. Proxy values are never printed or persisted.
func SyncToolchainWithRunner(
	ctx context.Context,
	options ToolchainSyncOptions,
	output io.Writer,
	runner DependencyEnvironmentRunner,
) (ToolchainSyncReport, error) {
	if output == nil {
		output = io.Discard
	}
	if runner == nil {
		return ToolchainSyncReport{}, errors.New("toolchain sync requires a command runner")
	}
	executable, err := findExecutable(options.ToolchainCLI, "arduino-cli")
	if err != nil {
		return ToolchainSyncReport{}, err
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	} else {
		environment = append([]string(nil), environment...)
	}
	environment = netpolicy.WithLocalNetworkNoProxy(environment)
	proxyNames := proxyEnvironmentNames(environment)
	environment = withDependencyProxyEnvironment(environment)
	report := ToolchainSyncReport{ProxyVariables: proxyNames}
	if len(proxyNames) == 0 {
		fmt.Fprintln(
			output,
			"Firmware toolchain network mode: no proxy environment variables detected (the dependency CLI configuration may still supply one)",
		)
	} else {
		fmt.Fprintln(
			output,
			"Firmware toolchain network mode: inherited proxy environment (values hidden):",
			strings.Join(proxyNames, ", "),
		)
	}

	steps := []struct {
		name string
		args []string
	}{
		{"update core index", miniCoreIndexArgs("core", "update-index")},
		{"update library index", []string{"lib", "update-index"}},
		{"upgrade every installed core", miniCoreIndexArgs("core", "upgrade")},
		{"upgrade every installed library", []string{"lib", "upgrade"}},
		{"ensure " + RequiredArduinoCore, miniCoreIndexArgs("core", "install", RequiredArduinoCore)},
	}
	for _, library := range RequiredArduinoLibraries {
		steps = append(steps, struct {
			name string
			args []string
		}{"ensure library " + library, []string{"lib", "install", library}})
	}
	steps = append(steps,
		struct {
			name string
			args []string
		}{"installed core inventory", []string{"core", "list"}},
		struct {
			name string
			args []string
		}{"installed library inventory", []string{"lib", "list"}},
	)

	directEnvironment := withoutProxyEnvironment(environment)
	var failures []error
	for _, configured := range steps {
		command := Command{Name: executable, Args: configured.args}
		step := ToolchainSyncStep{
			Name: configured.name, Command: command,
			UsedProxy: len(proxyNames) != 0,
		}
		fmt.Fprintln(output, "\n▶", configured.name)
		fmt.Fprintln(output, command.String())
		if options.DryRun {
			step.Planned = true
			fmt.Fprintln(output, "  dry-run: not executed")
			report.Steps = append(report.Steps, step)
			continue
		}
		runErr := runner.Run(ctx, command, environment, output)
		if runErr != nil && options.DirectRetry {
			step.RetriedDirect = true
			fmt.Fprintln(
				output,
				"⚠ configured-network attempt failed; retrying once without proxy environment variables",
			)
			directErr := runner.Run(ctx, command, directEnvironment, output)
			if directErr == nil {
				fmt.Fprintln(output, "✅ direct retry succeeded; parent environment was not changed")
				runErr = nil
			} else {
				runErr = errors.Join(
					fmt.Errorf("configured-network attempt: %w", runErr),
					fmt.Errorf("direct retry: %w", directErr),
				)
			}
		}
		if runErr != nil {
			fmt.Fprintln(output, "❌", configured.name, "failed:", runErr)
			failures = append(failures, fmt.Errorf("%s: %w", configured.name, runErr))
		} else {
			step.Succeeded = true
			fmt.Fprintln(output, "✅", configured.name)
		}
		report.Steps = append(report.Steps, step)
	}
	if len(failures) != 0 {
		return report, errors.Join(failures...)
	}
	return report, nil
}

func proxyEnvironmentNames(environment []string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || strings.TrimSpace(value) == "" || !isProxyEnvironmentName(name) {
			continue
		}
		canonical := strings.ToUpper(name)
		if !seen[canonical] {
			seen[canonical] = true
			result = append(result, name)
		}
	}
	sort.Slice(result, func(left, right int) bool {
		return strings.ToUpper(result[left]) < strings.ToUpper(result[right])
	})
	return result
}

func withoutProxyEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment))
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if ok && isProxyEnvironmentName(name) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// withDependencyProxyEnvironment translates conventional proxy variables to
// the dependency CLI's configuration environment while preserving the caller's
// complete environment. This lets the CLI use HTTPS_PROXY without persisting a
// secret proxy URL in its configuration file.
func withDependencyProxyEnvironment(environment []string) []string {
	values := make(map[string]string)
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[strings.ToUpper(strings.TrimSpace(name))] = strings.TrimSpace(value)
		}
	}
	if values[arduinoNetworkProxyEnvKey] != "" {
		return append([]string(nil), environment...)
	}
	var proxy string
	for _, name := range []string{"HTTPS_PROXY", "HTTP_PROXY", "ALL_PROXY", "FTP_PROXY"} {
		if values[name] != "" {
			proxy = values[name]
			break
		}
	}
	result := append([]string(nil), environment...)
	if proxy != "" {
		result = append(result, arduinoNetworkProxyEnvKey+"="+proxy)
	}
	return result
}

func isProxyEnvironmentName(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", "NO_PROXY", arduinoNetworkProxyEnvKey:
		return true
	default:
		return false
	}
}

func miniCoreIndexArgs(args ...string) []string {
	result := append([]string(nil), args...)
	return append(result, "--additional-urls", MiniCorePackageIndexURL)
}
