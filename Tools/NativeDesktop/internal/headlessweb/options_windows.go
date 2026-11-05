//go:build windows

package headlessweb

import (
	"errors"
	"flag"
	"io"
	"strings"
)

// LaunchOptions is the command-line surface of `PCControllerDesktop.exe serve`.
type LaunchOptions struct {
	ConfigPath string
	Listen     string
	Open       bool
	Help       bool
	Version    bool
}

// ParseLaunchOptions parses only the explicit headless-dashboard mode. It does
// not accept ordinary GUI command flags, which prevents a misspelling from
// silently constructing the native shell.
func ParseLaunchOptions(args []string) (LaunchOptions, error) {
	options := LaunchOptions{Open: true}
	set := flag.NewFlagSet("PCControllerDesktop serve", flag.ContinueOnError)
	set.SetOutput(io.Discard)

	var noOpen bool
	set.StringVar(&options.ConfigPath, "config", "", "")
	set.StringVar(&options.Listen, "listen", "", "")
	set.BoolVar(&noOpen, "no-open", false, "")
	set.BoolVar(&options.Help, "help", false, "")
	set.BoolVar(&options.Help, "?", false, "")
	set.BoolVar(&options.Version, "version", false, "")
	if err := set.Parse(args); err != nil {
		return LaunchOptions{}, err
	}
	if set.NArg() != 0 {
		return LaunchOptions{}, errors.New(
			"unexpected serve argument " + quoteArgument(set.Arg(0)),
		)
	}
	options.ConfigPath = strings.TrimSpace(options.ConfigPath)
	options.Listen = strings.TrimSpace(options.Listen)
	options.Open = !noOpen
	return options, nil
}

func quoteArgument(value string) string {
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// Usage returns the complete dedicated server-mode help.
func Usage() string {
	return `PCControllerDesktop headless web dashboard

Usage:
  PCControllerDesktop.exe serve [--no-open] [--config PATH] [--listen HOST:PORT]

Options:
  --no-open             Serve without opening the default browser
  --config PATH         Use an alternate configuration file
  --listen HOST:PORT    Persist and use an explicit loopback listener
  --help, --version     Show help or product version

The serve mode creates no native window, tray icon, global hotkeys, or native
dialogs. It owns the ordinary per-user IPC pipe, the device connection, event
automation and audible cues, and the embedded loopback dashboard. Running
serve explicitly enables the local API in the selected configuration.`
}
