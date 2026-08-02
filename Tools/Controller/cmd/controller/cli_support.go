package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/productidentity"
)

func runConfig(args []string, stdout io.Writer, store *appconfig.Store) error {
	action := "show"
	if len(args) > 1 {
		return errors.New("usage: config path|show|validate")
	}
	if len(args) == 1 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "path":
		fmt.Fprintln(stdout, store.Path())
	case "show":
		encoded, err := json.MarshalIndent(store.Current(), "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(encoded))
	case "validate":
		if _, _, err := appconfig.Load(store.Path()); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "valid:", store.Path())
	default:
		return errors.New("usage: config path|show|validate")
	}
	return nil
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
  controller tui [connection flags]
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
  controller ws serve --file firmware.hex [flags]
  controller ws client --url ws://host:3000/firmware [programmer flags]

Device, firmware and recovery:
  controller reset [connection flags]
	controller eeprom inspect|export|import|restore [file-only backup flags]
	controller firmware inspect|identity|patch-identity [artifact flags]
	controller program flash HEX [PORT] [--method urclock|usbasp] [--app-device SELECTOR] [--allow-incomplete-backup] [--reinitialize-eeprom]
	controller program recover HEX [PORT]  fresh readback + durable restore; never rewrites flash
	controller program --operation DIAGNOSTIC [program flags]
  controller boot probe|info|metadata|backup|read|write|verify|start [flags]
  controller toolchain check|update|bootstrap|lock|sync|profile|compile|core-info|install-bootloader [flags]

Host configuration and integration:
  controller config [path|show|validate]
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

Application UART and Urboot/AVRDUDE are mutually exclusive. Normal firmware
writes first verify a complete flash + EEPROM + metadata backup, then flash,
and finally authenticate application HELLO. Direct dependency upload is disabled.
Device auto-detection always requires a valid controller HELLO identity.`
	usage = strings.NewReplacer(
		"{{PRODUCT}}", productidentity.Title(title),
		"{{SCHEME}}", productidentity.ProtocolScheme,
	).Replace(usage)
	fmt.Fprintln(output, decorateUsage(usage, usageANSIEnabled(output)))
}

func configuredProductTitle(configPath string) string {
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
