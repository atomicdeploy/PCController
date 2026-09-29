package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

type buzzerRuntimeOptions struct {
	path        string
	mirror      bool
	backend     string
	executable  string
	environment appconfig.BuzzerRuntimeOverrides
	flagSet     map[string]bool
}

func addBuzzerRuntimeFlags(
	flags *flag.FlagSet,
	configured appconfig.BuzzerMirror,
) (*buzzerRuntimeOptions, error) {
	options := &buzzerRuntimeOptions{flagSet: make(map[string]bool)}
	override, err := buzzerEnvironmentOverrides(os.LookupEnv)
	if err != nil {
		return nil, err
	}
	options.environment = override
	effective := appconfig.Config{Integrations: appconfig.Integrations{BuzzerMirror: configured}}
	// Use the Store's public validation/application contract without requiring a
	// temporary file: these are only defaults for flag parsing.
	if override.Path != "" {
		switch override.Path {
		case appconfig.BuzzerPathHost, appconfig.BuzzerPathBoth:
			effective.Integrations.BuzzerMirror.Enabled = true
		default:
			effective.Integrations.BuzzerMirror.Enabled = false
		}
	}
	if override.Mirror != nil {
		effective.Integrations.BuzzerMirror.Enabled = *override.Mirror
	}
	if override.Backend != "" {
		effective.Integrations.BuzzerMirror.Backend = override.Backend
	}
	if override.Executable != nil {
		effective.Integrations.BuzzerMirror.Executable = *override.Executable
	}
	options.path = override.Path
	options.mirror = effective.Integrations.BuzzerMirror.Enabled
	options.backend = effective.Integrations.BuzzerMirror.Backend
	options.executable = effective.Integrations.BuzzerMirror.Executable
	flags.StringVar(&options.path, "buzzer-path", options.path, "startup buzzer route: board, host, both, or none (reconciles and verifies MCU Silent)")
	flags.BoolVar(&options.mirror, "buzzer-mirror", options.mirror, "mirror pushed board buzzer events on host renderers")
	flags.StringVar(&options.backend, "buzzer-backend", options.backend, "PC speaker backend: auto, native, external, or off")
	flags.StringVar(&options.executable, "buzzer-executable", options.executable, "external beep executable (blank uses platform PATH discovery)")
	return options, nil
}

func buzzerEnvironmentOverrides(
	lookup func(string) (string, bool),
) (appconfig.BuzzerRuntimeOverrides, error) {
	var result appconfig.BuzzerRuntimeOverrides
	if raw, found := lookup("PCCONTROLLER_BUZZER_PATH"); found {
		path, err := appconfig.NormalizeBuzzerPath(raw)
		if err != nil || path == "" {
			return result, fmt.Errorf("PCCONTROLLER_BUZZER_PATH must be board, host, both, or none")
		}
		result.Path = path
		mirror := path == appconfig.BuzzerPathHost || path == appconfig.BuzzerPathBoth
		result.Mirror = &mirror
	}
	if raw, found := lookup("PCCONTROLLER_BUZZER_MIRROR"); found {
		value, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return result, fmt.Errorf("PCCONTROLLER_BUZZER_MIRROR must be true or false")
		}
		result.Mirror = &value
	}
	if raw, found := lookup("PCCONTROLLER_BUZZER_BACKEND"); found {
		backend, err := appconfig.NormalizeBuzzerBackend(raw)
		if err != nil || backend == "" {
			return result, fmt.Errorf("PCCONTROLLER_BUZZER_BACKEND must be auto, native, external, or off")
		}
		result.Backend = backend
	}
	if raw, found := lookup("PCCONTROLLER_BUZZER_EXECUTABLE"); found {
		value := strings.TrimSpace(raw)
		result.Executable = &value
	}
	return result, nil
}

func (options *buzzerRuntimeOptions) captureOverrides(flags *flag.FlagSet) error {
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "buzzer-path", "buzzer-mirror", "buzzer-backend", "buzzer-executable":
			options.flagSet[value.Name] = true
		}
	})
	result := options.environment
	if options.flagSet["buzzer-path"] {
		path, err := appconfig.NormalizeBuzzerPath(options.path)
		if err != nil || path == "" {
			return fmt.Errorf("--buzzer-path must be board, host, both, or none")
		}
		result.Path = path
		mirror := path == appconfig.BuzzerPathHost || path == appconfig.BuzzerPathBoth
		result.Mirror = &mirror
	}
	if options.flagSet["buzzer-mirror"] {
		value := options.mirror
		result.Mirror = &value
	}
	if options.flagSet["buzzer-backend"] {
		backend, err := appconfig.NormalizeBuzzerBackend(options.backend)
		if err != nil || backend == "" {
			return fmt.Errorf("--buzzer-backend must be auto, native, external, or off")
		}
		result.Backend = backend
	}
	if options.flagSet["buzzer-executable"] {
		value := strings.TrimSpace(options.executable)
		result.Executable = &value
	}
	options.environment = result
	return nil
}

func (options *buzzerRuntimeOptions) apply(store *appconfig.Store) error {
	if options == nil {
		return nil
	}
	return store.SetBuzzerRuntimeOverrides(options.environment)
}
