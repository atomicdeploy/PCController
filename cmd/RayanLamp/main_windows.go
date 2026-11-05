//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"rayanlamp.local/app/internal/application"
	"rayanlamp.local/app/internal/buildinfo"
	"rayanlamp.local/app/internal/cli"
	"rayanlamp.local/app/internal/comserver"
	"rayanlamp.local/app/internal/headlessweb"
	"rayanlamp.local/app/internal/ipc"
	"rayanlamp.local/app/internal/selfinstall"
	"rayanlamp.local/app/internal/shellintegration"
	"rayanlamp.local/app/internal/terminal"
	"rayanlamp.local/app/internal/tui"
	"rayanlamp.local/app/internal/winfx"
	"rayanlamp.local/app/internal/wmipc"
)

type launchMode uint8

const (
	launchGUI launchMode = iota
	launchCLI
	launchTUI
	launchServe
)

func main() {
	os.Exit(run())
}

func run() int {
	mode, args := routeLaunch(os.Args[1:])
	switch mode {
	case launchCLI:
		prepareTerminalMode(false)
		return runCLI(args)
	case launchTUI:
		prepareTerminalMode(true)
		return runTUI(args)
	case launchServe:
		prepareTerminalMode(false)
		return runServe(args)
	default:
		prepareGUIConsole()
		return runGUI(args)
	}
}

func prepareTerminalMode(allocateWhenDetached bool) {
	prepareTerminalConsole(allocateWhenDetached)
	// This must run after attaching/allocating the console because the standard
	// handles can change during prepareTerminalConsole.
	_, _ = terminal.EnableVT100(os.Stdout, os.Stderr)
}

func routeLaunch(args []string) (launchMode, []string) {
	if len(args) == 0 {
		return launchGUI, args
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "cli", "ctl", "--cli", "--ctl", "/cli", "/ctl":
		return launchCLI, args[1:]
	case "tui", "--tui", "/tui":
		return launchTUI, args[1:]
	case "serve", "server", "--serve", "/serve":
		return launchServe, args[1:]
	case "gui", "--gui", "/gui":
		return launchGUI, args[1:]
	default:
		return launchGUI, args
	}
}

func runGUI(args []string) int {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Establish the process/taskbar identity before the shell creates a top
	// level window. This does not write registration and therefore preserves
	// portable-mode behavior.
	_ = shellintegration.SetAppUserModelID()
	_, _ = terminal.EnableVT100(os.Stdout, os.Stderr)
	allowColor := argumentsAllowColor(args)

	options, err := cli.Parse(args)
	if err != nil {
		reportError("Invalid command line", err, allowColor)
		return 2
	}
	allowColor = !options.NoColor
	if options.Help {
		showInformation("RayanLamp command line", unifiedUsage())
		return 0
	}
	if options.Version {
		showInformation(
			buildinfo.DisplayName+" "+buildinfo.Version,
			fmt.Sprintf(
				"Edition: %s\nCommit: %s\nBuilt: %s",
				buildinfo.Edition,
				buildinfo.Commit,
				buildinfo.Date,
			),
		)
		return 0
	}
	if options.Special != cli.SpecialNone {
		if err := handleSpecialMode(options); err != nil {
			if shellintegration.IsPartial(err) {
				reportSpecialError(
					"Windows integration was only partially completed",
					err,
					allowColor,
					options.Quiet,
				)
				// Installers can distinguish a useful partial registration
				// (normally URI installed but Jump Lists disabled by policy)
				// from a total registration failure.
				return 3
			}
			reportSpecialError(
				"Windows integration",
				err,
				allowColor,
				options.Quiet,
			)
			return 1
		}
		return 0
	}

	if handled, response, handoffErr := handoff(options); handled {
		if handoffErr != nil {
			reportError(
				"Unable to contact the running RayanLamp",
				handoffErr,
				allowColor,
			)
			return 1
		}
		if response != nil {
			if err := cli.PrintResponse(os.Stdout, *response, options); err != nil {
				reportError("Command failed", err, allowColor)
				return 1
			}
		}
		return 0
	}

	app, err := application.New(options)
	if errors.Is(err, application.ErrAlreadyRunning) {
		handled, response, retryErr := handoff(options)
		if handled && retryErr == nil {
			if response != nil {
				_ = cli.PrintResponse(os.Stdout, *response, options)
			}
			return 0
		}
		if retryErr != nil {
			err = retryErr
		}
	}
	if err != nil {
		reportError("RayanLamp could not start", err, allowColor)
		return 1
	}
	exitCode, err := app.Run(options)
	if err != nil {
		reportError("RayanLamp stopped unexpectedly", err, allowColor)
		return 1
	}
	return exitCode
}

func runCLI(args []string) int {
	allowColor := argumentsAllowColor(args)
	if len(args) == 0 {
		args = []string{"--status"}
	}
	options, err := cli.Parse(args)
	if err != nil {
		printTerminalError(err, allowColor)
		return 2
	}
	allowColor = !options.NoColor
	if options.Help {
		_, _ = fmt.Fprintln(os.Stdout, unifiedUsage())
		return 0
	}
	if options.Version {
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%s %s %s (%s, %s)\n",
			buildinfo.Name,
			buildinfo.Version,
			buildinfo.Edition,
			buildinfo.Commit,
			buildinfo.Date,
		)
		return 0
	}
	if options.Special != cli.SpecialNone {
		printTerminalError(
			errors.New("special operations cannot be combined with the cli launch mode"),
			allowColor,
		)
		return 2
	}
	if !options.HasCommand() {
		options.Method = "status"
	}
	ctx, cancel := context.WithTimeout(context.Background(), options.Timeout)
	defer cancel()
	response, err := cli.Call(ctx, options)
	if err != nil {
		printTerminalError(
			fmt.Errorf("RayanLamp primary instance is unavailable: %w", err),
			allowColor,
		)
		return 1
	}
	if err := cli.PrintResponse(os.Stdout, response, options); err != nil {
		printTerminalError(err, allowColor)
		return 1
	}
	return 0
}

func runTUI(args []string) int {
	options, err := tui.ParseLaunchOptions(args)
	if err != nil {
		printTerminalError(err, true)
		return 2
	}
	if options.Help {
		_, _ = fmt.Fprintln(os.Stdout, tui.Usage())
		return 0
	}
	if options.Version {
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%s TUI %s %s (%s, %s)\n",
			buildinfo.Name,
			buildinfo.Version,
			buildinfo.Edition,
			buildinfo.Commit,
			buildinfo.Date,
		)
		return 0
	}

	if err := validateTUITerminal(os.Stdin, os.Stdout); err != nil {
		printTerminalError(err, true)
		return 1
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	transport, err := tui.OpenTransport(ctx, options.Transport)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		printTerminalError(err, true)
		return 1
	}
	defer transport.Close()
	if err := tui.Run(ctx, transport, options); err != nil {
		if errors.Is(err, context.Canceled) {
			return 0
		}
		printTerminalError(err, true)
		return 1
	}
	return 0
}

func validateTUITerminal(stdin, stdout *os.File) error {
	if terminal.IsTerminal(stdin) && terminal.IsTerminal(stdout) {
		return nil
	}
	return errors.New(
		"TUI requires an interactive terminal on stdin and stdout; " +
			"use `RayanLamp cli` for redirected or scripted output",
	)
}

func runServe(args []string) int {
	options, err := headlessweb.ParseLaunchOptions(args)
	if err != nil {
		printTerminalError(
			fmt.Errorf("%w\n\n%s", err, headlessweb.Usage()),
			true,
		)
		return 2
	}
	if options.Help {
		_, _ = fmt.Fprintln(os.Stdout, headlessweb.Usage())
		return 0
	}
	if options.Version {
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%s headless dashboard %s %s (%s, %s)\n",
			buildinfo.Name,
			buildinfo.Version,
			buildinfo.Edition,
			buildinfo.Commit,
			buildinfo.Date,
		)
		return 0
	}

	host, err := headlessweb.New(headlessweb.Options{
		ConfigPath: options.ConfigPath,
		Listen:     options.Listen,
		Logger: func(format string, args ...any) {
			_, _ = fmt.Fprintf(
				os.Stderr,
				"RayanLamp serve: "+format+"\n",
				args...,
			)
		},
	})
	if err != nil {
		if headlessweb.IsPrimaryBusy(err) {
			return handoffServeToPrimary(options.Open)
		}
		printTerminalError(err, true)
		return 1
	}

	if options.Open {
		if err := host.OpenDashboard(); err != nil {
			_ = host.Close()
			printTerminalError(err, true)
			return 1
		}
	}
	if browserURL, urlErr := host.BrowserURL(); urlErr == nil {
		icon := terminal.Symbol(os.Stdout, "🌐", "")
		if icon != "" {
			icon += " "
		}
		_, _ = fmt.Fprintf(
			os.Stdout,
			"%sRayanLamp headless dashboard: %s\n",
			icon,
			browserURL,
		)
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	if err := host.Run(ctx); err != nil {
		printTerminalError(err, true)
		return 1
	}
	return 0
}

func handoffServeToPrimary(openBrowser bool) int {
	if !openBrowser {
		printTerminalError(
			errors.New(
				"another RayanLamp primary already owns the coordinator; "+
					"headless serve was not started",
			),
			true,
		)
		return 1
	}
	request, err := ipc.NewRequest(
		fmt.Sprintf("serve-handoff-%d", time.Now().UnixNano()),
		"web.show",
		nil,
	)
	if err != nil {
		printTerminalError(err, true)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if _, err := proxyToPrimary(ctx, request); err != nil {
		printTerminalError(
			fmt.Errorf("running RayanLamp primary cannot open a web dashboard: %w", err),
			true,
		)
		return 1
	}
	return 0
}

func unifiedUsage() string {
	return `RayanLamp

Usage:
  RayanLamp.exe                         Open or hand over to the GUI
  RayanLamp.exe /on | /off | /toggle   Control through the primary instance
  RayanLamp.exe cli [command options]  Controller-only named-pipe client
  RayanLamp.exe tui [TUI options]      Interactive terminal interface
  RayanLamp.exe serve [serve options]  GUI-free web dashboard primary

Examples:
  RayanLamp.exe /on
  RayanLamp.exe cli --status --json
  RayanLamp.exe cli --endpoint /info --json
  RayanLamp.exe tui --mode auto
  RayanLamp.exe tui --mode standalone
  RayanLamp.exe serve --no-open

GUI and command options:
  --on, --off, --toggle, --get          Lamp control and state
  --show, --hide, --settings            Window control
  --web                                  Open the embedded web dashboard
  --reconnect                           Reconnect to the lamp
  --endpoint /info                      Fetch an allow-listed diagnostic
  --notify "message" [--title "title"] Show a local notification
  --audio [--cue CUE] [--count N]      Play on/off/message/error SFX
  --quit                                Exit the primary instance
  --json, --quiet, --no-color           Command output controls
  --timeout 12s                         IPC timeout
  --config PATH                         Alternate configuration file
  --register-com, --unregister-com      Per-user COM Automation registration
  --register-shell, --unregister-shell  Per-user URI and Jump List integration
  --install [install options]            Per-user file install (integrations opt in)
  --uninstall [--remove-user-data]       Remove owned integration and program files
  --install-status [--json]              Report portable/installed status
  --uri URI                             Handle a rayan-lamp:// activation
  --help, --version                     Help or version information

Use "RayanLamp.exe tui --help" for terminal-interface transport, appearance,
keyboard, and mouse options, or "RayanLamp.exe serve --help" for the headless
dashboard host. Windows-style /on, /off, /toggle, /get, /show, /web, /audio,
and /? aliases remain supported.`
}

func printTerminalError(err error, allowColor bool) {
	if err == nil {
		return
	}
	prefix := terminal.StyledIf(
		os.Stderr,
		allowColor,
		terminal.BoldRed,
		"error:",
	)
	if icon := terminal.Symbol(os.Stderr, "❌", ""); icon != "" {
		prefix = icon + " " + prefix
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s %v\n", prefix, err)
}

func argumentsAllowColor(args []string) bool {
	allowed := true
	for _, argument := range args {
		argument = strings.ToLower(strings.TrimSpace(argument))
		if argument == "--no-color" {
			allowed = false
			continue
		}
		const prefix = "--no-color="
		if strings.HasPrefix(argument, prefix) {
			disabled, err := strconv.ParseBool(strings.TrimPrefix(argument, prefix))
			if err != nil {
				// Invalid syntax will become a parse error. Keep that error
				// plain because the caller explicitly targeted no-color.
				allowed = false
			} else {
				allowed = !disabled
			}
		}
	}
	return allowed
}

func handleSpecialMode(options cli.Options) error {
	switch options.Special {
	case cli.SpecialRegisterCOM:
		if err := comserver.RegisterCurrentExecutable(); err != nil {
			return err
		}
		if !options.Quiet {
			showInformation(
				"COM Automation registered",
				"ProgID: "+comserver.ProgID+"\n\nRegistration is per-user and requires no administrator access.",
			)
		}
		return nil
	case cli.SpecialUnregisterCOM:
		if err := comserver.Unregister(); err != nil {
			return err
		}
		if !options.Quiet {
			showInformation(
				"COM Automation unregistered",
				"The per-user "+comserver.ProgID+" registration was removed.",
			)
		}
		return nil
	case cli.SpecialCOMServer:
		if err := ensurePrimaryForCOM(); err != nil {
			return err
		}
		return comserver.Run(context.Background(), proxyToPrimary)
	case cli.SpecialRegisterShell:
		if err := shellintegration.RegisterCurrentShell(); err != nil {
			return err
		}
		if !options.Quiet {
			showInformation(
				"Rayan Lamp shell integration registered",
				"The rayan-lamp:// protocol and Jump List tasks are registered for this user.\n\nRegistration requires no administrator access.",
			)
		}
		return nil
	case cli.SpecialUnregisterShell:
		if err := shellintegration.UnregisterCurrentShell(); err != nil {
			return err
		}
		if !options.Quiet {
			showInformation(
				"Rayan Lamp shell integration unregistered",
				"This executable's per-user URI, Jump List, and owned legacy shell metadata were removed.",
			)
		}
		return nil
	case cli.SpecialInstall:
		ctx, cancel := context.WithTimeout(
			context.Background(),
			options.Timeout,
		)
		defer cancel()
		result, err := selfinstall.Install(ctx, selfinstall.InstallOptions{
			WithStartup: options.InstallWithStartup,
			WithShell:   options.InstallWithShell,
			WithCOM:     options.InstallWithCOM,
			Launch:      options.InstallLaunch,
			Force:       options.InstallForce,
		})
		if err != nil {
			return err
		}
		if shellintegration.IsPartial(result.Warning) {
			return result.Warning
		}
		if !options.Quiet {
			content := "Installed executable:\n" + result.Executable
			switch {
			case result.Updated:
				content += "\n\nThe existing installation was updated safely."
			case result.Installed:
				content += "\n\nThe per-user installation is ready."
			default:
				content += "\n\nThe existing installation was configured."
			}
			if result.Warning != nil {
				content += "\n\nNote: " + result.Warning.Error()
			}
			showInformation("Rayan Lamp installed", content)
		}
		return nil
	case cli.SpecialUninstall:
		directory, _, err := selfinstall.InstallPath()
		if err != nil {
			return err
		}
		if !options.Quiet {
			content := "RayanLamp will remove its exact per-user installation and owned Windows integrations:\n\n" +
				directory +
				"\n\nConfiguration and logs will be preserved."
			if options.RemoveUserData {
				content = "RayanLamp will remove its exact per-user installation, owned Windows integrations, configuration, logs, API token, and toast cache:\n\n" +
					directory +
					"\n\nThis user-data removal cannot be undone."
			}
			confirmed, confirmErr := winfx.Confirm(
				0,
				"Uninstall Rayan Lamp",
				"Remove Rayan Lamp for this user?",
				content,
			)
			if confirmErr != nil {
				return confirmErr
			}
			if !confirmed {
				return nil
			}
		}
		ctx, cancel := context.WithTimeout(
			context.Background(),
			options.Timeout,
		)
		defer cancel()
		result, err := selfinstall.Uninstall(
			ctx,
			selfinstall.UninstallOptions{
				RemoveUserData: options.RemoveUserData,
			},
		)
		if err != nil {
			return err
		}
		if !options.Quiet {
			content := "Owned integrations were cleaned. Program-file removal will finish after this process exits."
			if options.RemoveUserData {
				content += "\n\nConfiguration and logs are also queued for removal."
			}
			if result.Warning != nil {
				content += "\n\nPreserved: " + result.Warning.Error()
			}
			showInformation("Rayan Lamp uninstall ready", content)
		}
		return nil
	case cli.SpecialInstallStatus:
		status, err := selfinstall.CurrentStatus()
		if err != nil {
			return err
		}
		if options.Quiet {
			return nil
		}
		if options.JSON {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(status)
		}
		state := "Portable"
		switch {
		case status.CurrentIsInstalled && status.Installed:
			state = "Running from the per-user installation"
		case status.Installed:
			state = "A per-user installation is present"
		case status.Incomplete:
			state = "The per-user installation is incomplete"
		}
		showInformation(
			"Rayan Lamp installation status",
			state+"\n\nCurrent executable:\n"+status.CurrentExecutable+
				"\n\nInstall location:\n"+status.InstalledExecutable,
		)
		return nil
	case cli.SpecialCleanupHelper:
		return selfinstall.RunCleanupHelper(
			context.Background(),
			selfinstall.CleanupOptions{
				ParentPID:      options.CleanupParentPID,
				InstallPath:    options.CleanupInstallPath,
				RemoveUserData: options.RemoveUserData,
			},
		)
	default:
		return errors.New("unknown special operation")
	}
}

func ensurePrimaryForCOM() error {
	probe := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
		defer cancel()
		return ipc.IsRunning(ctx)
	}
	if probe() {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate RayanLamp executable: %w", err)
	}
	command := exec.Command(executable, "/background")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start primary RayanLamp instance: %w", err)
	}
	_ = command.Process.Release()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if probe() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("primary RayanLamp instance did not become ready")
}

func proxyToPrimary(ctx context.Context, request ipc.Request) (any, error) {
	response, err := ipc.Call(ctx, request)
	if err != nil {
		return nil, err
	}
	if !response.OK {
		if response.Error == nil {
			return nil, errors.New("RayanLamp IPC command failed")
		}
		return nil, remoteCommandError{
			code: response.Error.Code, message: response.Error.Message,
		}
	}
	if len(response.Result) == 0 || string(response.Result) == "null" {
		return nil, nil
	}
	var result any
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil, fmt.Errorf("decode RayanLamp IPC result: %w", err)
	}
	return result, nil
}

type remoteCommandError struct {
	code    string
	message string
}

func (e remoteCommandError) Error() string { return e.message }
func (e remoteCommandError) Code() string  { return e.code }

func handoff(options cli.Options) (handled bool, response *ipc.Response, err error) {
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	hwnd := wmipc.FindPrimary()
	probeCtx, probeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	pipeRunning := ipc.IsRunning(probeCtx)
	probeCancel()
	if hwnd == 0 && !pipeRunning {
		return false, nil, nil
	}

	if !options.HasCommand() {
		options.Method = "window.show"
	}
	request, err := options.Request(fmt.Sprintf("handoff-%d", time.Now().UnixNano()))
	if err != nil {
		return true, nil, err
	}

	if queryNeedsResponse(options.Method) && pipeRunning {
		reply, callErr := ipc.Call(ctx, request)
		return true, &reply, callErr
	}
	if hwnd != 0 {
		if sendErr := wmipc.Send(ctx, hwnd, request); sendErr == nil {
			return true, nil, nil
		} else if !pipeRunning {
			return true, nil, sendErr
		}
	}
	reply, callErr := ipc.Call(ctx, request)
	return true, &reply, callErr
}

func queryNeedsResponse(method string) bool {
	switch method {
	case "status", "state", "lamp.get", "get", "version",
		"diagnostics.fetch", "config.get":
		return true
	default:
		return false
	}
}

func showInformation(instruction, content string) {
	_, _ = winfx.TaskDialog(winfx.TaskDialogOptions{
		Title:           buildinfo.DisplayName,
		MainInstruction: instruction,
		Content:         content,
		Icon:            winfx.DialogIconInformation,
		Buttons:         winfx.DialogButtonOK,
		AllowCancel:     true,
	})
}

func reportError(instruction string, err error, allowColor bool) {
	if err == nil {
		return
	}
	printTerminalError(err, allowColor)
	_, _ = winfx.TaskDialog(winfx.TaskDialogOptions{
		Title:           buildinfo.DisplayName,
		MainInstruction: instruction,
		Content:         err.Error(),
		Icon:            winfx.DialogIconError,
		Buttons:         winfx.DialogButtonOK,
		AllowCancel:     true,
	})
}

func reportSpecialError(
	instruction string,
	err error,
	allowColor,
	quiet bool,
) {
	if err == nil {
		return
	}
	if quiet {
		printTerminalError(err, allowColor)
		return
	}
	reportError(instruction, err, allowColor)
}
