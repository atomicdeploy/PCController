package programmer

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestArduinoUpdatePlansAllUpgradesAndRequiredDependencies(t *testing.T) {
	var commands []Command
	_, err := UpdateArduinoWithRunner(
		context.Background(),
		ArduinoUpdateOptions{
			ArduinoCLI: "arduino-cli", Environment: []string{"PATH=test"},
		},
		io.Discard,
		ArduinoEnvironmentRunnerFunc(func(
			_ context.Context, command Command, _ []string, _ io.Writer,
		) error {
			commands = append(commands, command)
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	joined := make([]string, len(commands))
	for index, command := range commands {
		joined[index] = strings.Join(command.Args, "|")
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{
		"core|update-index|--additional-urls|" + MiniCorePackageIndexURL,
		"lib|update-index",
		"core|upgrade|--additional-urls|" + MiniCorePackageIndexURL,
		"lib|upgrade",
		"core|install|MiniCore:avr|--additional-urls|" + MiniCorePackageIndexURL,
		"lib|install|Adafruit PWM Servo Driver Library",
		"lib|install|Adafruit INA219", "lib|install|rc-switch",
		"lib|install|TM1637TinyDisplay", "lib|install|DallasTemperature",
		"lib|install|OneWire", "core|list", "lib|list",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in commands:\n%s", want, all)
		}
	}
}

func TestArduinoUpdateRetriesWithoutProxyWithoutMutatingParentValues(t *testing.T) {
	environment := []string{
		"PATH=test", "HTTPS_PROXY=http://secret-proxy", "http_proxy=http://other-secret",
	}
	var attempts [][]string
	report, err := UpdateArduinoWithRunner(
		context.Background(),
		ArduinoUpdateOptions{
			ArduinoCLI: "arduino-cli", Environment: environment, DirectRetry: true,
		},
		io.Discard,
		ArduinoEnvironmentRunnerFunc(func(
			_ context.Context, _ Command, childEnvironment []string, _ io.Writer,
		) error {
			attempts = append(attempts, append([]string(nil), childEnvironment...))
			if len(attempts) == 1 {
				return errors.New("proxy unavailable")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) < 2 || len(report.ProxyVariables) != 2 ||
		!report.Steps[0].RetriedDirect || !report.Steps[0].Succeeded {
		t.Fatalf("attempts=%d report=%+v", len(attempts), report)
	}
	if active := proxyEnvironmentNames(attempts[1]); len(active) != 0 {
		t.Fatalf("direct retry retained active proxy variables %v: %#v", active, attempts[1])
	}
	if !containsExact(attempts[1], arduinoNetworkProxyEnvKey+"=") {
		t.Fatalf("direct retry did not disable persisted Arduino proxy: %#v", attempts[1])
	}
	if environment[1] != "HTTPS_PROXY=http://secret-proxy" ||
		environment[2] != "http_proxy=http://other-secret" {
		t.Fatal("caller environment slice was mutated")
	}
}

func TestArduinoUpdateRetriesDirectWhenOnlyPersistedArduinoProxyMayExist(t *testing.T) {
	var attempts [][]string
	report, err := UpdateArduinoWithRunner(
		context.Background(),
		ArduinoUpdateOptions{
			ArduinoCLI: "arduino-cli", Environment: []string{"PATH=test"}, DirectRetry: true,
		},
		io.Discard,
		ArduinoEnvironmentRunnerFunc(func(
			_ context.Context, _ Command, childEnvironment []string, _ io.Writer,
		) error {
			attempts = append(attempts, append([]string(nil), childEnvironment...))
			if len(attempts) == 1 {
				return errors.New("configured Arduino proxy unavailable")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) < 2 || !report.Steps[0].RetriedDirect || !report.Steps[0].Succeeded {
		t.Fatalf("attempts=%d report=%+v", len(attempts), report)
	}
	if !containsExact(attempts[1], arduinoNetworkProxyEnvKey+"=") {
		t.Fatalf("direct retry did not disable persisted Arduino proxy: %#v", attempts[1])
	}
}

func TestProxyEnvironmentNamesIgnoreEmptyOverrides(t *testing.T) {
	names := proxyEnvironmentNames([]string{
		"HTTP_PROXY=", "HTTPS_PROXY=https://proxy", "ARDUINO_NETWORK_PROXY=",
	})
	if got, want := strings.Join(names, ","), "HTTPS_PROXY"; got != want {
		t.Fatalf("proxy names = %q, want %q", got, want)
	}
}

func containsExact(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestArduinoUpdateOutputNeverLeaksProxyValue(t *testing.T) {
	var output strings.Builder
	_, err := UpdateArduinoWithRunner(
		context.Background(),
		ArduinoUpdateOptions{
			ArduinoCLI:  "arduino-cli",
			Environment: []string{"PATH=test", "ALL_PROXY=socks5://top-secret"},
			DryRun:      true,
		},
		&output,
		ArduinoEnvironmentRunnerFunc(func(
			context.Context, Command, []string, io.Writer,
		) error {
			t.Fatal("dry-run executed a command")
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "top-secret") ||
		!strings.Contains(output.String(), "ALL_PROXY") {
		t.Fatalf("unsafe or unclear proxy report:\n%s", output.String())
	}
}
