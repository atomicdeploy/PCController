//go:build windows

// Package cli implements the GUI-handoff and controller modes exposed by the
// unified RayanLamp.exe.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/shellintegration"
	"rayanlamp.local/app/internal/terminal"
)

type SpecialMode uint8

const (
	SpecialNone SpecialMode = iota
	SpecialCOMServer
	SpecialRegisterCOM
	SpecialUnregisterCOM
	SpecialRegisterShell
	SpecialUnregisterShell
	SpecialInstall
	SpecialUninstall
	SpecialInstallStatus
	SpecialCleanupHelper
)

type Options struct {
	Method             string
	Params             any
	ConfigPath         string
	JSON               bool
	Quiet              bool
	NoColor            bool
	Help               bool
	Version            bool
	StartMinimized     bool
	Special            SpecialMode
	Timeout            time.Duration
	InstallWithStartup bool
	InstallWithShell   bool
	InstallWithCOM     bool
	InstallLaunch      bool
	InstallForce       bool
	RemoveUserData     bool
	CleanupParentPID   uint32
	CleanupInstallPath string
}

func (o Options) HasCommand() bool { return o.Method != "" }

func (o Options) Request(id string) (ipc.Request, error) {
	if !o.HasCommand() {
		return ipc.Request{}, errors.New("no command selected")
	}
	return ipc.NewRequest(id, o.Method, o.Params)
}

// Parse accepts idiomatic GNU options as well as Windows-style /on, /off,
// /toggle, /get, /show, and similar switches.
func Parse(args []string) (Options, error) {
	var options Options
	options.Timeout = 12 * time.Second

	normalized, err := normalizeCOMActivationArgs(normalizeWindowsArgs(args))
	if err != nil {
		return Options{}, err
	}
	normalized = coalesceNotifyArgs(normalized)
	set := flag.NewFlagSet(buildinfo.Name, flag.ContinueOnError)
	set.SetOutput(io.Discard)

	var (
		on, off, toggle, getState, show, hide bool
		audio                                 bool
		reconnect, settings, diagnostics, web bool
		quit, help, version                   bool
		jsonOutput, quiet, noColor            bool
		startMinimized                        bool
		registerCOM, unregisterCOM, comServer bool
		registerShell, unregisterShell        bool
		install, uninstall, installStatus     bool
		cleanupHelper                         bool
		installWithStartup, installWithShell  bool
		installWithCOM, installLaunch         bool
		installForce, removeUserData          bool
		cleanupParentPID                      uint64
		cleanupInstallPath                    string
		notifyMessage, notifyTitle            string
		audioCue                              string
		audioCount                            int
		diagnosticEndpoint                    string
		uriActivation                         string
		uriRequested                          bool
		timeout                               time.Duration
		configPath                            string
	)
	set.BoolVar(&on, "on", false, "")
	set.BoolVar(&off, "off", false, "")
	set.BoolVar(&toggle, "toggle", false, "")
	set.BoolVar(&getState, "get", false, "")
	set.BoolVar(&getState, "status", false, "")
	set.BoolVar(&show, "show", false, "")
	set.BoolVar(&hide, "hide", false, "")
	set.BoolVar(&audio, "audio", false, "")
	set.BoolVar(&audio, "sound", false, "")
	set.BoolVar(&reconnect, "reconnect", false, "")
	set.BoolVar(&settings, "settings", false, "")
	set.BoolVar(&diagnostics, "diagnostics", false, "")
	set.BoolVar(&web, "web", false, "")
	set.BoolVar(&quit, "quit", false, "")
	set.BoolVar(&help, "help", false, "")
	set.BoolVar(&help, "?", false, "")
	set.BoolVar(&version, "version", false, "")
	set.BoolVar(&jsonOutput, "json", false, "")
	set.BoolVar(&quiet, "quiet", false, "")
	set.BoolVar(&noColor, "no-color", false, "")
	set.BoolVar(&startMinimized, "background", false, "")
	set.BoolVar(&startMinimized, "start-minimized", false, "")
	set.BoolVar(&registerCOM, "register-com", false, "")
	set.BoolVar(&unregisterCOM, "unregister-com", false, "")
	set.BoolVar(&comServer, "com-server", false, "")
	set.BoolVar(&registerShell, "register-shell", false, "")
	set.BoolVar(&unregisterShell, "unregister-shell", false, "")
	set.BoolVar(&install, "install", false, "")
	set.BoolVar(&uninstall, "uninstall", false, "")
	set.BoolVar(&installStatus, "install-status", false, "")
	set.BoolVar(&cleanupHelper, "cleanup-helper", false, "")
	set.BoolVar(&installWithStartup, "with-startup", false, "")
	set.BoolVar(&installWithShell, "with-shell", false, "")
	set.BoolVar(&installWithCOM, "with-com", false, "")
	set.BoolVar(&installLaunch, "launch", false, "")
	set.BoolVar(&installForce, "force", false, "")
	set.BoolVar(&removeUserData, "remove-user-data", false, "")
	set.Uint64Var(&cleanupParentPID, "parent-pid", 0, "")
	set.StringVar(&cleanupInstallPath, "install-path", "", "")
	set.StringVar(&notifyMessage, "notify", "", "")
	set.StringVar(&notifyTitle, "title", "", "")
	set.StringVar(&audioCue, "cue", "", "")
	set.IntVar(&audioCount, "count", 0, "")
	set.StringVar(&diagnosticEndpoint, "endpoint", "", "")
	set.Func("uri", "", func(value string) error {
		uriRequested = true
		uriActivation = value
		return nil
	})
	set.StringVar(&configPath, "config", "", "")
	set.DurationVar(&timeout, "timeout", options.Timeout, "")

	if err := set.Parse(normalized); err != nil {
		return Options{}, fmt.Errorf("%w\n\n%s", err, Usage())
	}
	if set.NArg() != 0 {
		// `--notify hello world` remains convenient without requiring the user
		// to quote every word: remaining operands extend the message.
		if notifyMessage != "" {
			notifyMessage = strings.TrimSpace(notifyMessage + " " + strings.Join(set.Args(), " "))
		} else {
			return Options{}, fmt.Errorf("unexpected argument %q", set.Arg(0))
		}
	}
	options.ConfigPath = configPath
	options.JSON = jsonOutput
	options.Quiet = quiet
	options.NoColor = noColor
	options.Help = help
	options.Version = version
	options.StartMinimized = startMinimized
	options.Timeout = timeout
	options.InstallWithStartup = installWithStartup
	options.InstallWithShell = installWithShell
	options.InstallWithCOM = installWithCOM
	options.InstallLaunch = installLaunch
	options.InstallForce = installForce
	options.RemoveUserData = removeUserData
	if timeout < 100*time.Millisecond || timeout > 10*time.Minute {
		return Options{}, errors.New("timeout must be between 100ms and 10m")
	}
	if !audio && (audioCue != "" || audioCount != 0) {
		return Options{}, errors.New("--cue and --count require --audio")
	}
	if audio {
		audioCue = strings.ToLower(strings.TrimSpace(audioCue))
		if audioCue == "" {
			audioCue = "message"
		}
		switch audioCue {
		case "on", "off", "message", "error":
		default:
			return Options{}, errors.New(
				"--cue must be on, off, message, or error",
			)
		}
		if audioCount == 0 {
			audioCount = 1
		}
		if audioCount < 1 || audioCount > 10 {
			return Options{}, errors.New("--count must be between 1 and 10")
		}
	}

	specialCount := boolCount(
		registerCOM,
		unregisterCOM,
		comServer,
		registerShell,
		unregisterShell,
		install,
		uninstall,
		installStatus,
		cleanupHelper,
	)
	if specialCount > 1 {
		return Options{}, errors.New("choose only one special operation")
	}
	switch {
	case registerCOM:
		options.Special = SpecialRegisterCOM
	case unregisterCOM:
		options.Special = SpecialUnregisterCOM
	case comServer:
		options.Special = SpecialCOMServer
	case registerShell:
		options.Special = SpecialRegisterShell
	case unregisterShell:
		options.Special = SpecialUnregisterShell
	case install:
		options.Special = SpecialInstall
	case uninstall:
		options.Special = SpecialUninstall
	case installStatus:
		options.Special = SpecialInstallStatus
	case cleanupHelper:
		options.Special = SpecialCleanupHelper
	}

	hasInstallModifier := installWithStartup ||
		installWithShell ||
		installWithCOM ||
		installLaunch ||
		installForce
	if hasInstallModifier && options.Special != SpecialInstall {
		return Options{}, errors.New(
			"--with-startup, --with-shell, --with-com, --launch, and --force require --install",
		)
	}
	if removeUserData &&
		options.Special != SpecialUninstall &&
		options.Special != SpecialCleanupHelper {
		return Options{}, errors.New(
			"--remove-user-data requires --uninstall",
		)
	}
	hasCleanupArgument := cleanupParentPID != 0 ||
		strings.TrimSpace(cleanupInstallPath) != ""
	if hasCleanupArgument && options.Special != SpecialCleanupHelper {
		return Options{}, errors.New(
			"--parent-pid and --install-path are private cleanup-helper options",
		)
	}
	if options.Special == SpecialCleanupHelper {
		if cleanupParentPID == 0 ||
			cleanupParentPID > uint64(^uint32(0)) ||
			strings.TrimSpace(cleanupInstallPath) == "" {
			return Options{}, errors.New(
				"--cleanup-helper requires a valid --parent-pid and --install-path",
			)
		}
		options.CleanupParentPID = uint32(cleanupParentPID)
		options.CleanupInstallPath = cleanupInstallPath
	}

	type selectedCommand struct {
		active bool
		method string
		params any
	}
	commands := []selectedCommand{
		{on, "lamp.on", nil},
		{off, "lamp.off", nil},
		{toggle, "lamp.toggle", nil},
		{getState, "status", nil},
		{show, "window.show", nil},
		{hide, "window.hide", nil},
		{reconnect, "reconnect", nil},
		{settings, "settings.show", nil},
		{diagnostics, "diagnostics.show", nil},
		{web, "web.show", nil},
		{audio, "audio.play", map[string]any{
			"cue": audioCue, "count": audioCount,
		}},
		{quit, "quit", nil},
		{diagnosticEndpoint != "", "diagnostics.fetch", map[string]string{"endpoint": diagnosticEndpoint}},
		{notifyMessage != "", "notifications.show", map[string]string{
			"title": notifyTitle, "message": notifyMessage,
		}},
	}
	for _, command := range commands {
		if !command.active {
			continue
		}
		if options.Method != "" {
			return Options{}, errors.New("choose only one command")
		}
		options.Method = command.method
		options.Params = command.params
	}
	if uriRequested {
		method, err := shellintegration.ParseURI(uriActivation)
		if err != nil {
			return Options{}, err
		}
		if options.Method != "" {
			return Options{}, errors.New("choose only one command")
		}
		options.Method = method
		options.Params = nil
	}
	if options.Special != SpecialNone && options.HasCommand() {
		return Options{}, errors.New("special operations cannot be combined with a command")
	}
	return options, nil
}

// COM appends the conventional -Embedding marker when it launches a
// LocalServer32 command. It is not a user-facing mode and is accepted only
// alongside our private COM-server switch.
func normalizeCOMActivationArgs(args []string) ([]string, error) {
	hasCOMServer := false
	embeddingCount := 0
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "--com-server":
			hasCOMServer = true
		case "-embedding", "/embedding":
			embeddingCount++
		}
	}
	if embeddingCount == 0 {
		return args, nil
	}
	if !hasCOMServer {
		return nil, errors.New("-Embedding is valid only for COM activation")
	}
	if embeddingCount > 1 {
		return nil, errors.New("-Embedding may be specified only once")
	}

	normalized := make([]string, 0, len(args)-1)
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "-embedding", "/embedding":
			continue
		default:
			normalized = append(normalized, arg)
		}
	}
	return normalized, nil
}

func normalizeWindowsArgs(args []string) []string {
	normalized := make([]string, len(args))
	for index, arg := range args {
		lower := strings.ToLower(strings.TrimSpace(arg))
		switch lower {
		case "/on", "/off", "/toggle", "/get", "/status", "/show", "/hide",
			"/audio", "/sound", "/cue", "/count",
			"/reconnect", "/settings", "/diagnostics", "/web", "/quit", "/version",
			"/help", "/?", "/json", "/quiet", "/background",
			"/register-com", "/unregister-com", "/com-server",
			"/register-shell", "/unregister-shell", "/install",
			"/uninstall", "/install-status", "/cleanup-helper",
			"/with-startup", "/with-shell", "/with-com", "/launch",
			"/force", "/remove-user-data", "/parent-pid",
			"/install-path", "/uri":
			normalized[index] = "--" + strings.TrimPrefix(lower, "/")
		default:
			normalized[index] = arg
		}
	}
	return normalized
}

func coalesceNotifyArgs(args []string) []string {
	for index, arg := range args {
		if arg != "--notify" || index+1 >= len(args) {
			continue
		}
		end := index + 2
		for end < len(args) && !strings.HasPrefix(args[end], "-") {
			end++
		}
		if end == index+2 {
			return args
		}
		joined := strings.Join(args[index+1:end], " ")
		result := append([]string{}, args[:index+1]...)
		result = append(result, joined)
		result = append(result, args[end:]...)
		return result
	}
	return args
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func Usage() string {
	return `RayanLamp command line

Usage:
  RayanLamp.exe                         Open or hand over to the primary app
  RayanLamp.exe /on | /off | /toggle   Control through the running instance
  RayanLamp.exe cli --status [--json] Query the primary instance

Commands:
  --on, --off, --toggle, --get          Lamp control and state
  --show, --hide, --settings            Window control
  --web                                  Open the embedded web dashboard
  --reconnect                           Reconnect to the lamp
  --endpoint /info                      Fetch an allow-listed diagnostic
  --notify "message" [--title "title"] Show a local notification
  --audio [--cue CUE] [--count N]      Play on/off/message/error SFX
  --quit                                Exit the primary instance

Options:
  --json              Machine-readable output
  --timeout 12s       IPC timeout
  --config PATH       Alternate configuration file
  --register-com      Register optional COM Automation for this user
  --unregister-com    Remove this user's COM Automation registration
  --register-shell    Register optional URI and Jump List integration
  --unregister-shell  Remove this executable's owned shell integration
  --install           Install this EXE for the current user (file only by default)
    --with-startup    Opt in to Start with Windows
    --with-shell      Opt in to URI and Jump List integration
    --with-com        Opt in to COM Automation
    --launch          Launch the installed application when finished
    --force           Safely update an existing per-user installation
  --uninstall         Remove the per-user installation and owned integrations
    --remove-user-data  Also remove configuration and logs
  --install-status    Report portable/installed status (supports --json)
  --uri URI           Handle a validated rayan-lamp:// activation
  --help, --version   Help or version information

Windows-style /on, /off, /toggle, /get, /show and /? aliases are supported.`
}

// Call sends one request to the primary instance through the authenticated
// per-user named pipe and returns its decoded response.
func Call(ctx context.Context, options Options) (ipc.Response, error) {
	request, err := options.Request(fmt.Sprintf("cli-%d", time.Now().UnixNano()))
	if err != nil {
		return ipc.Response{}, err
	}
	return ipc.Call(ctx, request)
}

func PrintResponse(out io.Writer, response ipc.Response, options Options) error {
	if out == nil || options.Quiet {
		return nil
	}
	if !response.OK {
		if response.Error == nil {
			return errors.New("command failed")
		}
		return fmt.Errorf("%s: %s", response.Error.Code, response.Error.Message)
	}
	if options.JSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if len(response.Result) == 0 {
			return encoder.Encode(map[string]bool{"ok": true})
		}
		var value any
		if err := json.Unmarshal(response.Result, &value); err != nil {
			return err
		}
		return encoder.Encode(map[string]any{"ok": true, "result": value})
	}

	prefix := terminal.StyledIf(
		out,
		!options.NoColor,
		terminal.BoldGreen,
		"OK",
	)
	if icon := terminal.Symbol(out, "✅", ""); icon != "" {
		prefix = icon + " " + prefix
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		_, err := fmt.Fprintln(out, prefix)
		return err
	}
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, response.Result, "", "  "); err != nil {
		_, writeErr := fmt.Fprintf(out, "%s %s\n", prefix, response.Result)
		return writeErr
	}
	_, err := fmt.Fprintf(out, "%s %s\n", prefix, formatted.String())
	return err
}
