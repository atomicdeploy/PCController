package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/productidentity"
)

type tuiConsoleOptions struct {
	build       consolewindow.Settings
	environment consolewindow.Settings
	envSet      map[string]bool
	values      consolewindow.Settings
	flagSet     map[string]bool
}

func addTUIConsoleFlags(flags *flag.FlagSet, configured appconfig.TUIConsole) (*tuiConsoleOptions, error) {
	build, err := buildTUIConsoleDefaults()
	if err != nil {
		return nil, err
	}
	options := &tuiConsoleOptions{
		build: build, envSet: make(map[string]bool), flagSet: make(map[string]bool),
	}
	resolved := options.resolveConfig(configured)
	options.environment = resolved
	if err := options.applyEnvironment(os.LookupEnv); err != nil {
		return nil, err
	}
	options.values = options.environment
	flags.BoolVar(
		&options.values.Enabled, "console-management", options.values.Enabled,
		"manage the attached local console window (use --console-management=false to disable)",
	)
	flags.IntVar(&options.values.Columns, "columns", options.values.Columns, "local TUI console columns")
	flags.IntVar(&options.values.Rows, "rows", options.values.Rows, "local TUI console rows")
	flags.StringVar(&options.values.FontFace, "console-font", options.values.FontFace, "local TUI classic-console font face")
	flags.IntVar(&options.values.FontSize, "console-font-size", options.values.FontSize, "local TUI classic-console font height in pixels")
	return options, nil
}

func buildTUIConsoleDefaults() (consolewindow.Settings, error) {
	enabled, err := strconv.ParseBool(strings.TrimSpace(productidentity.DefaultTUIConsoleEnabled))
	if err != nil {
		return consolewindow.Settings{}, errors.New("build default console management must be true or false")
	}
	columns, err := parseConsoleInteger("build default columns", productidentity.DefaultTUIConsoleColumns)
	if err != nil {
		return consolewindow.Settings{}, err
	}
	rows, err := parseConsoleInteger("build default rows", productidentity.DefaultTUIConsoleRows)
	if err != nil {
		return consolewindow.Settings{}, err
	}
	fontSize, err := parseConsoleInteger("build default font size", productidentity.DefaultTUIConsoleFontSize)
	if err != nil {
		return consolewindow.Settings{}, err
	}
	settings := consolewindow.Settings{
		Enabled: enabled, Columns: columns, Rows: rows,
		FontFace: strings.TrimSpace(productidentity.DefaultTUIConsoleFontFace), FontSize: fontSize,
	}
	if err := consolewindow.Validate(settings); err != nil {
		return consolewindow.Settings{}, fmt.Errorf("invalid TUI console build defaults: %w", err)
	}
	return settings, nil
}

func (options *tuiConsoleOptions) applyEnvironment(lookup func(string) (string, bool)) error {
	for _, item := range []struct {
		name  string
		field string
	}{
		{"PCCONTROLLER_TUI_CONSOLE", "enabled"},
		{"PCCONTROLLER_TUI_COLUMNS", "columns"},
		{"PCCONTROLLER_TUI_ROWS", "rows"},
		{"PCCONTROLLER_TUI_FONT", "font"},
		{"PCCONTROLLER_TUI_FONT_SIZE", "font-size"},
	} {
		raw, found := lookup(item.name)
		if !found {
			continue
		}
		options.envSet[item.field] = true
		switch item.field {
		case "enabled":
			value, err := strconv.ParseBool(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%s must be true or false", item.name)
			}
			options.environment.Enabled = value
		case "columns":
			value, err := parseConsoleInteger(item.name, raw)
			if err != nil {
				return err
			}
			options.environment.Columns = value
		case "rows":
			value, err := parseConsoleInteger(item.name, raw)
			if err != nil {
				return err
			}
			options.environment.Rows = value
		case "font":
			options.environment.FontFace = strings.TrimSpace(raw)
		case "font-size":
			value, err := parseConsoleInteger(item.name, raw)
			if err != nil {
				return err
			}
			options.environment.FontSize = value
		}
	}
	if options.environment.Enabled {
		if err := consolewindow.Validate(options.environment); err != nil {
			return fmt.Errorf("invalid TUI console environment: %w", err)
		}
	}
	return nil
}

func parseConsoleInteger(name, raw string) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	return value, nil
}

func (options *tuiConsoleOptions) captureOverrides(flags *flag.FlagSet) error {
	flags.Visit(func(value *flag.Flag) {
		switch value.Name {
		case "console-management":
			options.flagSet["enabled"] = true
		case "columns":
			options.flagSet["columns"] = true
		case "rows":
			options.flagSet["rows"] = true
		case "console-font":
			options.flagSet["font"] = true
		case "console-font-size":
			options.flagSet["font-size"] = true
		}
	})
	if options.values.Enabled {
		if err := consolewindow.Validate(options.values); err != nil {
			return fmt.Errorf("invalid TUI console flags: %w", err)
		}
	}
	return nil
}

func (options *tuiConsoleOptions) resolveConfig(configured appconfig.TUIConsole) consolewindow.Settings {
	settings := options.build
	packaged := appconfig.Defaults().UI.TUIConsole
	// Package-default values are inheritance points for product builds. Any
	// persisted value differing from the package is an explicit config override.
	if configured.Enabled != packaged.Enabled {
		settings.Enabled = configured.Enabled
	}
	if configured.Columns != packaged.Columns {
		settings.Columns = configured.Columns
	}
	if configured.Rows != packaged.Rows {
		settings.Rows = configured.Rows
	}
	if configured.FontFace != packaged.FontFace {
		settings.FontFace = configured.FontFace
	}
	if configured.FontSize != packaged.FontSize {
		settings.FontSize = configured.FontSize
	}
	return settings
}

func (options *tuiConsoleOptions) resolve(configured appconfig.TUIConsole) consolewindow.Settings {
	settings := options.resolveConfig(configured)
	if options.envSet["enabled"] {
		settings.Enabled = options.environment.Enabled
	}
	if options.envSet["columns"] {
		settings.Columns = options.environment.Columns
	}
	if options.envSet["rows"] {
		settings.Rows = options.environment.Rows
	}
	if options.envSet["font"] {
		settings.FontFace = options.environment.FontFace
	}
	if options.envSet["font-size"] {
		settings.FontSize = options.environment.FontSize
	}
	if options.flagSet["enabled"] {
		settings.Enabled = options.values.Enabled
	}
	if options.flagSet["columns"] {
		settings.Columns = options.values.Columns
	}
	if options.flagSet["rows"] {
		settings.Rows = options.values.Rows
	}
	if options.flagSet["font"] {
		settings.FontFace = options.values.FontFace
	}
	if options.flagSet["font-size"] {
		settings.FontSize = options.values.FontSize
	}
	return settings
}

func (options *tuiConsoleOptions) haveRuntimeFlag() bool {
	return len(options.flagSet) != 0
}

func applyTUIConsole(settings consolewindow.Settings, output io.Writer, strict bool) error {
	result, err := consolewindow.Apply(settings)
	if err != nil {
		if strict {
			return err
		}
		fmt.Fprintln(output, "warning: local TUI console settings were not applied:", err)
		return nil
	}
	if !result.Applied && settings.Enabled && result.Reason != "" {
		// Linux terminal emulators own their presentation. This is expected, not
		// actionable, so do not pollute the TUI with a platform warning.
		if runtime.GOOS == "linux" && strings.Contains(result.Reason, "unavailable on linux") {
			return nil
		}
		if strict {
			return errors.New(result.Reason)
		}
		fmt.Fprintln(output, "notice: local TUI console settings skipped:", result.Reason)
	}
	return nil
}
