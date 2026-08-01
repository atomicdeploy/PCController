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

type ArduinoUpdateOptions struct {
	ArduinoCLI  string
	Environment []string
	DirectRetry bool
	DryRun      bool
}

type ArduinoUpdateStep struct {
	Name          string  `json:"name"`
	Command       Command `json:"command"`
	UsedProxy     bool    `json:"used_proxy"`
	RetriedDirect bool    `json:"retried_direct"`
	Succeeded     bool    `json:"succeeded"`
}

type ArduinoUpdateReport struct {
	ProxyVariables []string            `json:"proxy_variables,omitempty"`
	Steps          []ArduinoUpdateStep `json:"steps"`
}

// ArduinoEnvironmentRunner makes environment handling injectable so proxy
// fallback is fully testable without running Arduino CLI or using the network.
type ArduinoEnvironmentRunner interface {
	Run(context.Context, Command, []string, io.Writer) error
}

type ArduinoEnvironmentRunnerFunc func(
	context.Context,
	Command,
	[]string,
	io.Writer,
) error

func (function ArduinoEnvironmentRunnerFunc) Run(
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

func UpdateArduino(
	ctx context.Context,
	options ArduinoUpdateOptions,
	output io.Writer,
) (ArduinoUpdateReport, error) {
	return UpdateArduinoWithRunner(
		ctx,
		options,
		output,
		ArduinoEnvironmentRunnerFunc(runArduinoEnvironment),
	)
}

// UpdateArduinoWithRunner updates indexes and every installed core/library,
// then installs the project's selected core and libraries at their latest
// indexed versions. Proxy values are never printed or persisted.
func UpdateArduinoWithRunner(
	ctx context.Context,
	options ArduinoUpdateOptions,
	output io.Writer,
	runner ArduinoEnvironmentRunner,
) (ArduinoUpdateReport, error) {
	if output == nil {
		output = io.Discard
	}
	if runner == nil {
		return ArduinoUpdateReport{}, errors.New("Arduino update requires a command runner")
	}
	executable, err := findExecutable(options.ArduinoCLI, "arduino-cli")
	if err != nil {
		return ArduinoUpdateReport{}, err
	}
	environment := options.Environment
	if environment == nil {
		environment = os.Environ()
	} else {
		environment = append([]string(nil), environment...)
	}
	proxyNames := proxyEnvironmentNames(environment)
	report := ArduinoUpdateReport{ProxyVariables: proxyNames}
	if len(proxyNames) == 0 {
		fmt.Fprintln(
			output,
			"Arduino network mode: no proxy environment variables detected (Arduino CLI configuration may still supply one)",
		)
	} else {
		fmt.Fprintln(
			output,
			"Arduino network mode: inherited proxy environment (values hidden):",
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
		step := ArduinoUpdateStep{
			Name: configured.name, Command: command,
			UsedProxy: len(proxyNames) != 0,
		}
		fmt.Fprintln(output, "\n▶", configured.name)
		fmt.Fprintln(output, command.String())
		if options.DryRun {
			fmt.Fprintln(output, "  dry-run: not executed")
			report.Steps = append(report.Steps, step)
			continue
		}
		runErr := runner.Run(ctx, command, environment, output)
		if runErr != nil && options.DirectRetry {
			step.RetriedDirect = true
			fmt.Fprintln(
				output,
				"⚠ configured-network attempt failed; retrying once with proxy environment removed and Arduino CLI proxy disabled for this child process only",
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
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		name, _, ok := strings.Cut(entry, "=")
		if ok && isProxyEnvironmentName(name) {
			continue
		}
		result = append(result, entry)
	}
	// An empty process-local override suppresses a proxy persisted in
	// arduino-cli.yaml without altering the user's configuration file.
	result = append(result, arduinoNetworkProxyEnvKey+"=")
	return result
}

func isProxyEnvironmentName(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "FTP_PROXY", arduinoNetworkProxyEnvKey:
		return true
	default:
		return false
	}
}

func miniCoreIndexArgs(args ...string) []string {
	result := append([]string(nil), args...)
	return append(result, "--additional-urls", MiniCorePackageIndexURL)
}
