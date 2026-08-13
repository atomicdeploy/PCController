package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/installer"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/secretstore"
)

func runConfig(args []string, stdout io.Writer, store *appconfig.Store) error {
	return runConfigWithInput(args, os.Stdin, stdout, store)
}

func runConfigWithInput(args []string, stdin io.Reader, stdout io.Writer, store *appconfig.Store) error {
	action := "show"
	if len(args) != 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "path":
		if len(args) != 1 {
			return configUsageError()
		}
		fmt.Fprintln(stdout, store.Path())
	case "show":
		if len(args) > 1 {
			return configUsageError()
		}
		encoded, err := json.MarshalIndent(store.Redacted(), "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(encoded))
	case "validate":
		if len(args) != 1 {
			return configUsageError()
		}
		if _, _, err := appconfig.Load(store.Path()); err != nil {
			return err
		}
		if _, err := store.Runtime(); err != nil {
			return fmt.Errorf("resolve configured secrets: %w", err)
		}
		fmt.Fprintln(stdout, "valid:", store.Path())
	case "secret", "secrets":
		return runConfigSecrets(args[1:], stdin, stdout, store)
	default:
		return configUsageError()
	}
	return nil
}

func isConfigMaintenance(args []string) bool {
	if len(args) == 1 && (args[0] == "--open-user-data" || args[0] == "--open-config-dir" || args[0] == "--clear-settings") {
		return true
	}
	if len(args) < 2 || !strings.EqualFold(args[0], "config") {
		return false
	}
	switch strings.ToLower(args[1]) {
	case "open", "clear", "delete", "reset":
		return true
	case "path":
		return len(args) == 3
	default:
		return false
	}
}

func runConfigMaintenance(args []string, configPath string, stdout io.Writer) error {
	if len(args) == 1 {
		switch args[0] {
		case "--open-user-data":
			args = []string{"config", "open", "data"}
		case "--open-config-dir":
			args = []string{"config", "open", "config"}
		case "--clear-settings":
			args = []string{"config", "clear", "--confirm"}
		}
	}
	if len(args) < 2 || !strings.EqualFold(args[0], "config") {
		return configUsageError()
	}
	action := strings.ToLower(args[1])
	switch action {
	case "path", "open":
		if len(args) > 3 {
			return configUsageError()
		}
		kind := "config"
		if len(args) == 3 {
			kind = strings.ToLower(args[2])
		}
		path, err := configMaintenancePath(kind, configPath)
		if err != nil {
			return err
		}
		if action == "path" {
			fmt.Fprintln(stdout, path)
			return nil
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s directory: %w", kind, err)
		}
		if err := openDirectory(path); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "opened:", path)
		return nil
	case "clear", "delete", "reset":
		if len(args) != 3 || !strings.EqualFold(args[2], "--confirm") {
			return errors.New("clearing all host settings requires: config clear --confirm")
		}
		resolved, err := appconfig.ResolvePath(configPath)
		if err != nil {
			return err
		}
		removedSecrets := 0
		if store, openErr := appconfig.Open(resolved); openErr == nil {
			removed, purgeErr := store.PurgeConfiguredSecrets()
			removedSecrets = len(removed)
			if purgeErr != nil {
				return fmt.Errorf("clear configured OS secrets: %w", purgeErr)
			}
		}
		if err := os.Remove(resolved); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove host settings: %w", err)
		}
		_ = os.Remove(filepath.Dir(resolved)) // succeeds only when already empty
		fmt.Fprintf(stdout, "cleared host settings: %s (OS secrets removed: %d)\n", resolved, removedSecrets)
		return nil
	default:
		return configUsageError()
	}
}

func configMaintenancePath(kind, configPath string) (string, error) {
	switch kind {
	case "config", "settings":
		resolved, err := appconfig.ResolvePath(configPath)
		if err != nil {
			return "", err
		}
		return filepath.Dir(resolved), nil
	case "data", "user-data", "appdata":
		paths, err := programmer.DefaultHostDataPaths()
		if err != nil {
			return "", err
		}
		return paths.DataDir, nil
	default:
		return "", errors.New("config directory kind must be config or data")
	}
}

var openDirectory = func(path string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("open directory %s: %w", path, err)
	}
	return nil
}

func configUsageError() error {
	return errors.New("usage: config path [config|data] | open [config|data] | clear --confirm | show|validate | config secrets status|set REF (--from-env NAME|--stdin)|clear REF")
}

func runConfigSecrets(args []string, stdin io.Reader, stdout io.Writer, store *appconfig.Store) error {
	if len(args) == 0 || strings.EqualFold(args[0], "status") || strings.EqualFold(args[0], "refs") {
		if len(args) > 1 {
			return configUsageError()
		}
		encoded, err := json.MarshalIndent(store.SecretsStatus(), "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(encoded))
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "set":
		if len(args) != 3 {
			return configUsageError()
		}
		reference := args[1]
		var value string
		switch {
		case strings.HasPrefix(args[2], "--from-env="):
			name := strings.TrimPrefix(args[2], "--from-env=")
			if err := secretstore.ValidateReference("env:" + name); err != nil {
				return fmt.Errorf("environment source: %w", err)
			}
			var present bool
			value, present = os.LookupEnv(name)
			if !present {
				return fmt.Errorf("environment variable %s is not set", name)
			}
		case args[2] == "--stdin":
			content, err := io.ReadAll(io.LimitReader(stdin, secretstore.MaxSecretBytes+3))
			if err != nil {
				return fmt.Errorf("read secret from stdin: %w", err)
			}
			value = strings.TrimSuffix(strings.TrimSuffix(string(content), "\n"), "\r")
		default:
			return configUsageError()
		}
		if err := store.SetSecret(reference, value); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "stored:", reference)
		return nil
	case "clear", "delete", "remove":
		if len(args) != 2 {
			return configUsageError()
		}
		if err := store.DeleteSecret(args[1]); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "cleared:", args[1])
		return nil
	default:
		return configUsageError()
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
}

func findProjectRoot() string {
	directory, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "PCController.ino")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return "."
		}
		directory = parent
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func printUsage(output io.Writer, configuredTitle ...string) {
	title := ""
	if len(configuredTitle) != 0 {
		title = configuredTitle[0]
	}
	usage := `◆ {{PRODUCT}}

Interactive control:
  controller                         launch the Charm TUI
	controller tui [--ipc-addr HOST:PORT] [--ipc-token-ref REF] [--sync-navigation=false] [--simple] [connection flags]
	controller web [--no-open] [--no-tray] [--no-auto] [connection flags]
  controller web export --output FILE.zip
  controller ports [connection flags]
  controller shell [connection flags]
  controller exec [connection flags] COMMAND...

Automation, monitoring and bridges:
  controller batch --file SCRIPT [connection flags]
  controller monitor [--interval 500ms] [--json] [connection flags]
  controller ipc serve [--listen 127.0.0.1:8787|--stdio] [connection flags]
  controller ipc call --method METHOD [--params JSON]
	controller ipc monitor [--addr HOST:PORT] [--token-ref REF] [--kind program] [--after latest]
  controller ws serve --file firmware.hex [flags]
  controller ws client --url ws://host:3000/firmware [programmer flags]

Device, firmware and recovery:
  controller reset [connection flags]
	controller eeprom inspect|export|import|restore [file-only backup flags]
	controller firmware inspect|identity|patch-identity [artifact flags]
	controller program flash HEX [PORT] [--method urclock|usbasp] [--app-device SELECTOR] [--allow-incomplete-backup] [--reinitialize-eeprom]
	controller program recover HEX [PORT]  fresh readback + durable restore; never rewrites flash
	controller program --operation DIAGNOSTIC [program flags]
	controller program --method compile --sketch PROJECT [--firmware-feature NAME ...|--no-firmware-features]
  controller boot probe|info|metadata|backup|read|write|verify|start [flags]
	controller toolchain check|update|bootstrap|lock|sync|profile|compile PROJECT [--firmware-feature NAME ...|--no-firmware-features]
	controller board initialize [--name NAME] [--uart auto|PORT|none] [--firmware HEX] [--firmware-feature NAME ...|--no-firmware-features] [--bootloader-only]
	controller board blank --confirm NAME [--uart auto|PORT|none]
	controller board name [get|set NAME|clear]
	controller driver usbasp status | ensure | install [--package DIR] | zadig [--latest] [--download-only] [--exe FILE]

Host configuration and integration:
	controller [--app-name NAME] [--tagline TEXT] COMMAND...
	controller app launch tui|webui [--mode ensure|launch|focus] [--target INSTANCE] [--page PAGE] [--peer NAME]
	controller config path [config|data] | open [config|data] | clear --confirm | show|validate
	controller config secrets status|set REF (--from-env NAME|--stdin)|clear REF
	controller network edge-enable|edge-disable|peer-add|peer-remove|probe|status
	controller --open-user-data | --open-config-dir | --clear-settings
	controller package inventory --directory DIR [--output FILE]
	controller install [--package DIR] [--expected-package-sha256 SHA256] [--desktop]
	controller repair [--package DIR] [--expected-package-sha256 SHA256] [--desktop]
	controller installation status
	controller uninstall [--purge-data [--preview-purge | --confirm-purge {{PURGE_CONFIRMATION}}]]
  controller desktop [install|ensure|uninstall|remove]
  controller uri {{SCHEME}}://ACTION
  controller version

Connection flags:
  --device SELECTOR  COM, friendly name, VID:PID, serial:, or instance:
  --port COM18       explicit port
  --vid 1A86         USB VID filter
  --pid 7523         USB PID filter
  --name CH340       name/product/manufacturer substring
  --baud 115200      UART rate

Remote TUI flags:
  --ipc-addr HOST:PORT    attach the full Charm TUI to an existing primary
  --ipc-token-ref REF     load its bearer token from the OS vault/environment
  --sync-navigation=false keep this full TUI's active page independent
  --simple                explicit minimal line-oriented IPC fallback

When another local primary already owns serial, the default is the full IPC
TUI. Simple mode is never selected implicitly.

Application UART and Urboot/AVRDUDE are mutually exclusive. Normal firmware
writes first verify a complete flash + EEPROM + metadata backup, then flash,
and finally authenticate application HELLO. Direct dependency upload is disabled.
Device auto-detection always requires a valid controller HELLO identity.`
	usage = strings.NewReplacer(
		"{{PRODUCT}}", productidentity.Title(title),
		"{{SCHEME}}", productidentity.ProtocolScheme,
		"{{PURGE_CONFIRMATION}}", installer.PurgeConfirmation,
	).Replace(usage)
	fmt.Fprintln(output, decorateUsage(usage, usageANSIEnabled(output)))
}

func configuredProductTitle(configPath string, override ...string) string {
	if len(override) != 0 && strings.TrimSpace(override[0]) != "" {
		return productidentity.Title(override[0])
	}
	if environment := strings.TrimSpace(os.Getenv("APP_NAME")); environment != "" {
		return productidentity.Title(environment)
	}
	resolved, err := appconfig.ResolvePath(configPath)
	if err == nil {
		if value, _, loadErr := appconfig.Load(resolved); loadErr == nil {
			return productidentity.Title(value.UI.AppTitle)
		}
	}
	return productidentity.Title("")
}

func usageANSIEnabled(output io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	file, ok := output.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func decorateUsage(usage string, color bool) string {
	if !color {
		return usage
	}
	var builder strings.Builder
	for index, line := range strings.Split(usage, "\n") {
		if index != 0 {
			builder.WriteByte('\n')
		}
		switch {
		case index == 0:
			fmt.Fprintf(&builder, "\x1b[1;36m%s\x1b[0m", line)
		case strings.HasSuffix(line, ":"):
			fmt.Fprintf(&builder, "\x1b[1;33m%s\x1b[0m", line)
		case strings.HasPrefix(line, "  "):
			trimmed := strings.TrimSpace(line)
			fields := strings.Fields(trimmed)
			if len(fields) == 0 {
				continue
			}
			commandEnd := strings.Index(trimmed, fields[0]) + len(fields[0])
			fmt.Fprintf(&builder, "  \x1b[1;32m%s\x1b[0m%s", trimmed[:commandEnd], trimmed[commandEnd:])
		case line != "":
			fmt.Fprintf(&builder, "\x1b[2m%s\x1b[0m", line)
		}
	}
	return builder.String()
}
