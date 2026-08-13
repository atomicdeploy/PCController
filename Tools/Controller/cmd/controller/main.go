package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	gort "runtime"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/artifacts"
	"pccontroller.local/controller/internal/consolewindow"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/envfile"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/installer"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/nativeshell"
	"pccontroller.local/controller/internal/netpolicy"
	"pccontroller.local/controller/internal/pcspeaker"
	"pccontroller.local/controller/internal/portowner"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/tui"
)

var (
	version    = productidentity.Version
	sourceHash = "unknown"
	buildTime  = "unknown"
)

func main() {
	if _, err := envfile.LoadProcess(); err != nil {
		fmt.Fprintln(os.Stderr, "environment:", err)
		os.Exit(1)
	}
	// A console can be created by conhost before Windows consults the named
	// executable icon resource. Explicitly apply the packaged product icon so
	// direct launches and inherited build/terminal consoles do not retain the
	// generic console-host icon. Pseudoconsole and resource-free developer
	// builds intentionally treat this as a best-effort no-op.
	nativeshell.ApplyConsoleIcon()
	if err := netpolicy.EnsureProcessLocalNetworkNoProxy(); err != nil {
		fmt.Fprintln(os.Stderr, "network proxy bypass policy:", err)
		os.Exit(1)
	}
	if installer.IsUninstallHelperInvocation(os.Args[1:]) {
		service, err := newInstallerService("")
		if err == nil {
			ctx, cancel := lifecycleCommandContext()
			err = installer.RunExternalUninstallHelper(ctx, os.Args[2], service)
			cancel()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "uninstall helper:", err)
			os.Exit(1)
		}
		return
	}
	if pcspeaker.IsHelperInvocation(os.Args[1:]) {
		if err := pcspeaker.RunHelperInvocation(context.Background(), os.Args[2:], os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "pc-speaker helper:", err)
			os.Exit(1)
		}
		return
	}
	if portowner.IsHelperInvocation(os.Args[1:]) {
		if err := portowner.RunHelperInvocation(context.Background(), os.Args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "serial-owner helper:", err)
			os.Exit(1)
		}
		return
	}
	if artifacts.IsSelfUpdateHelperInvocation(os.Args[1:]) {
		if err := artifacts.PrepareSelfUpdateHelperProcess(); err != nil {
			fmt.Fprintln(os.Stderr, "self-update helper:", err)
			os.Exit(1)
		}
		if err := artifacts.RunSelfUpdateHelper(context.Background(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "self-update helper:", err)
			os.Exit(1)
		}
		return
	}
	// A replacement must survive normal config/IPC/device startup long enough
	// to prove it is not an immediate crash loop before the helper commits it.
	artifacts.ScheduleSelfUpdateHealthAcknowledgement(10 * time.Second)
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cleanArgs, configPath, presentation, err := extractGlobalArguments(args)
	if err != nil {
		return err
	}
	args = cleanArgs
	if len(args) == 0 {
		store, openErr := appconfig.Open(configPath)
		if openErr != nil {
			return openErr
		}
		if overrideErr := store.SetPresentationOverrides(presentation.AppName, presentation.Tagline); overrideErr != nil {
			return overrideErr
		}
		runtimeConfig, runtimeErr := store.Runtime()
		if runtimeErr != nil {
			return runtimeErr
		}
		configurePrimaryIPC(runtimeConfig)
		return runTUI(nil, stdout, stderr, store)
	}
	switch strings.ToLower(args[0]) {
	case "version", "--version", "-version":
		fmt.Fprintf(
			stdout,
			"%s %s source-hash=%s built=%s\n",
			configuredProductTitle(configPath, presentation.AppName),
			version,
			sourceHash,
			buildTime,
		)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout, configuredProductTitle(configPath, presentation.AppName))
		return nil
	case "eeprom":
		// Offline EEPROM inspection/transfer is dispatched before config or
		// device setup and therefore cannot open a serial port.
		return runEEPROM(args[1:], stdout, stderr)
	case "firmware":
		// Artifact identity inspection and guarded patching are also entirely
		// offline and must not depend on host configuration or serial hardware.
		return runFirmwareArtifact(args[1:], stdout, stderr)
	case "driver":
		return runDriver(args[1:], stdout, stderr)
	case "package":
		return runPackageLifecycle(args[1:], stdout, stderr)
	case "install", "repair":
		return runInstallLifecycle(
			strings.ToLower(args[0]), args[1:], stdout, stderr,
			configuredProductTitle(configPath, presentation.AppName),
		)
	case "installation":
		return runInstallationStatus(args[1:], stdout, stderr)
	case "uninstall":
		return runUninstallLifecycle(
			args[1:], stdout, stderr, configPath,
			configuredProductTitle(configPath, presentation.AppName),
		)
	}
	if isConfigMaintenance(args) {
		return runConfigMaintenance(args, configPath, stdout)
	}
	if configIndependentProgramCompile(args) {
		config, configErr := offlineCompileConfig(configPath)
		if configErr != nil {
			return configErr
		}
		return runProgramWithConfig(
			args[1:], stdout, stderr, config,
		)
	}
	if configIndependentToolchainCompile(args) {
		config, configErr := offlineCompileConfig(configPath)
		if configErr != nil {
			return configErr
		}
		translated, translateErr := toolchainCLIArguments(args[1:])
		if translateErr != nil {
			return translateErr
		}
		return runProgramWithConfig(
			translated, stdout, stderr, config,
		)
	}
	store, err := appconfig.Open(configPath)
	if err != nil {
		return err
	}
	if err := store.SetPresentationOverrides(presentation.AppName, presentation.Tagline); err != nil {
		return err
	}
	runtimeConfig, runtimeErr := store.Runtime()
	if runtimeErr != nil {
		return runtimeErr
	}
	configurePrimaryIPC(runtimeConfig)
	switch strings.ToLower(args[0]) {
	case "tui":
		return runTUI(args[1:], stdout, stderr, store)
	case "web":
		return runWeb(args[1:], stdout, stderr, store)
	case "ports":
		return runPorts(args[1:], stdout, stderr, store)
	case "shell":
		return runShell(args[1:], stdout, stderr, store)
	case "exec":
		return runExec(args[1:], stdout, stderr, store)
	case "batch", "script":
		return runBatch(args[1:], stdout, stderr, store)
	case "monitor":
		return runMonitor(args[1:], stdout, stderr, store)
	case "ipc":
		return runIPC(args[1:], stdout, stderr, store)
	case "reset":
		return runReset(args[1:], stdout, stderr, store)
	case "program":
		return runProgram(args[1:], stdout, stderr, store)
	case "boot":
		return runBoot(args[1:], stdout, stderr, store)
	case "toolchain":
		return runToolchain(args[1:], stdout, stderr, store)
	case "board":
		return runBoard(args[1:], stdout, stderr, store)
	case "ws":
		return runWS(args[1:], stdout, stderr, store)
	case "config":
		return runConfig(args[1:], stdout, store)
	case "network":
		return runNetwork(args[1:], stdout, stderr, store)
	case "desktop":
		return runDesktop(args[1:], stdout, store)
	case "app":
		return runApp(args[1:], stdout, stderr, store)
	case "uri", "action":
		return runURIAction(args[1:], stdout, stderr, store)
	default:
		return fmt.Errorf("unknown command %q; use help", args[0])
	}
}

// offlineCompileConfig reads the selected persistent compile policy without
// creating a config file, resolving secrets, configuring IPC, or probing a
// device. A genuinely absent config retains the historic default-off profile;
// an existing invalid config fails visibly instead of silently compiling with
// different feature selections.
func offlineCompileConfig(path string) (appconfig.Config, error) {
	resolved, err := appconfig.ResolvePath(path)
	if err != nil {
		return appconfig.Config{}, err
	}
	config, _, err := appconfig.Load(resolved)
	if errors.Is(err, os.ErrNotExist) {
		return appconfig.Defaults(), nil
	}
	return config, err
}

func runDesktop(
	args []string,
	stdout io.Writer,
	store *appconfig.Store,
) error {
	if len(args) > 1 {
		return errors.New("usage: desktop install|ensure|uninstall|remove")
	}
	action := "ensure"
	if len(args) == 1 {
		action = strings.ToLower(strings.TrimSpace(args[0]))
	}
	options := hostui.DesktopIntegrationOptions{
		AppID:       productidentity.StableAppID,
		DisplayName: productidentity.Title(store.Current().UI.AppTitle),
	}
	var status any
	var integrationErr error
	switch action {
	case "install", "ensure":
		status, integrationErr = hostui.EnsureDesktopIntegration(options)
	case "uninstall", "remove":
		status, integrationErr = hostui.RemoveDesktopIntegration(options)
	default:
		return errors.New("usage: desktop install|ensure|uninstall|remove")
	}
	encoded, _ := json.MarshalIndent(status, "", "  ")
	fmt.Fprintln(stdout, string(encoded))
	return integrationErr
}

func runURIAction(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: controller uri %s://page/events", productidentity.ProtocolScheme)
	}
	action, err := hostui.ParseActionURI(args[0])
	if err != nil {
		return err
	}
	if action.Kind == "app.quit" {
		probeContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		claim, existing, resolveErr := claimOrResolveHostInstance(probeContext, "web")
		cancel()
		if claim != nil {
			_ = claim.Close()
			return nil
		}
		if resolveErr != nil {
			return resolveErr
		}
		if existing == nil {
			return nil
		}
		return deliverExistingAppAction(context.Background(), action, stdout)
	}
	// Desktop/protocol activation is a graphical entry point. A live primary
	// receives the action through its authenticated IPC endpoint; a cold
	// activation starts the browser-first host and preserves the requested page.
	return runWebWithInitialAction(nil, stdout, stderr, store, action)
}

func runTUI(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	return runTUIWithInitialAction(args, stdout, stderr, store, hostui.AppAction{})
}

func preparePrimaryMode(surface string) (*hostInstanceClaim, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	claim, existing, err := claimOrResolveHostInstance(ctx, surface)
	return claim, existing != nil, err
}

func runWeb(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	return runWebWithInitialAction(args, stdout, stderr, store, hostui.AppAction{})
}

func runWebWithInitialAction(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
	initial hostui.AppAction,
) error {
	if len(args) != 0 && strings.EqualFold(args[0], "export") {
		if initial.Kind != "" {
			return errors.New("a desktop action cannot be combined with web export")
		}
		return runWebExport(args[1:], stdout, stderr)
	}
	flags := flag.NewFlagSet("web", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	buzzerOptions, err := addBuzzerRuntimeFlags(flags, store.Persistent().Integrations.BuzzerMirror)
	if err != nil {
		return err
	}
	noAuto := flags.Bool("no-auto", false, "start with automatic connection paused")
	noOpen := flags.Bool("no-open", false, "serve the web app without opening a browser")
	noTray := flags.Bool("no-tray", false, "serve the web app without a native system-tray menu")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller web [--no-open] [--no-tray] [--no-auto] [connection flags]")
	}
	connection.captureOverrides(flags)
	if err := buzzerOptions.captureOverrides(flags); err != nil {
		return err
	}
	if err := buzzerOptions.apply(store); err != nil {
		return err
	}
	claimContext, claimCancel := context.WithTimeout(context.Background(), 3*time.Second)
	claim, existing, err := claimOrResolveHostInstance(claimContext, "web")
	claimCancel()
	if err != nil {
		return err
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	appURL, err := browserURL(store.Current().IPC.Listen)
	if err != nil {
		return err
	}
	if existing != nil {
		appURL, err = browserURL(existing.Listen)
		if err != nil {
			return err
		}
		appURL, err = webURLForAppAction(appURL, initial)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, productidentity.ServiceName(store.Current().UI.AppTitle, "web app:"), appURL)
		if initial.Kind != "" {
			if err := deliverExistingAppAction(context.Background(), initial, stdout); err != nil {
				return err
			}
		}
		if *noOpen {
			return nil
		}
		connectionContext, connectionCancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		connected := primaryControllerConnected(connectionContext)
		connectionCancel()
		if !webBrowserAutoOpenAllowed(*noOpen, connected) {
			fmt.Fprintln(stdout, "controller offline; browser not opened")
			return nil
		}
		return openBrowser(appURL)
	}
	appURL, err = webURLForAppAction(appURL, initial)
	if err != nil {
		return err
	}

	ctx, cancel := signalContext()
	defer cancel()
	// An unpackaged Windows toast needs the per-user AppUserModelID and
	// Start-menu shortcut before Explorer can associate it with the packaged
	// application icon. Keep desktop-integration trouble non-fatal: the host
	// and board connection must remain usable even when a locked-down profile
	// prevents a notification registration write.
	if status, desktopErr := ensureWebDesktopIntegration(store); desktopErr != nil {
		fmt.Fprintln(stderr, "desktop notification identity:", desktopErr)
	} else if status.Supported && (!status.ProtocolReady || !status.ShortcutReady) {
		fmt.Fprintln(stderr, "desktop notification identity is incomplete")
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	if *noAuto {
		_ = runtime.Close()
	}
	project := findProjectRoot()
	outputs := control.NewOutputScheduler(runtime)
	commandConfiguration := commandOptions(store, project)
	commandConfiguration.Outputs = outputs
	engine := control.NewCommandEngine(runtime, commandConfiguration)
	hostMenus := newHostMenuManager(store, runtime, engine)
	if err := hostmenu.RegisterCommands(engine, hostMenus); err != nil {
		_ = runtime.Close()
		return err
	}
	hostPanel := &hostFrontPanelBridge{runtime: runtime}
	hostMenus.SetPanelChanged(func(_ hostmenu.Snapshot) {
		go syncHostMenuOverlay(runtime, hostMenus, hostPanel, nil)
	})
	hostMenus.SetDefinitionChanged(func(change hostmenu.DefinitionChange) {
		publishHostMenuDefinitionChange(runtime, change)
		go syncHostMenuOverlay(runtime, hostMenus, hostPanel, &change)
	})
	runtime.SetHostMenuRequestHandler(func(request native.HostMenuContentRequest) {
		syncHostMenuRequest(runtime, hostMenus, request)
	})
	programDataPaths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		_ = runtime.Close()
		return err
	}
	lifecycleOptions := control.ProgrammingLifecycleOptions{
		DataPaths: programDataPaths, Outputs: outputs, HostConfig: store.Current,
	}
	primaryReady := make(chan struct{})
	var primaryReadyOnce sync.Once
	var browserOpenOnce sync.Once
	openWhenConnected := func() {
		if *noOpen {
			return
		}
		go func() {
			select {
			case <-ctx.Done():
				return
			case <-primaryReady:
			}
			if !webBrowserAutoOpenAllowed(*noOpen, runtime.Snapshot().Connected) {
				return
			}
			browserOpenOnce.Do(func() {
				if openErr := openBrowser(appURL); openErr != nil {
					fmt.Fprintln(stderr, "open browser:", openErr)
				}
			})
		}()
	}
	runtime.SetConnectionReadyHandler(func(_ ports.Info, hello native.Hello) {
		recoveryContext, recoveryCancel := context.WithTimeout(context.Background(), 8*time.Second)
		var recoveryOutput bytes.Buffer
		recoveryErr := control.RecoverPendingProgrammingSessions(
			recoveryContext,
			runtime,
			lifecycleOptions,
			&recoveryOutput,
		)
		recoveryCancel()
		if text := strings.TrimSpace(recoveryOutput.String()); text != "" {
			runtime.PublishHostEvent("program.recovery", text)
		}
		if recoveryErr != nil {
			runtime.PublishHostEvent(
				"program.recovery.error",
				"pending MCU settings restore failed: "+recoveryErr.Error(),
			)
		}
		hostPanel.ConnectionReady()
		if native.SupportsHostMenuOverlay(hello) ||
			hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
			syncHostMenuOverlay(runtime, hostMenus, hostPanel, nil)
		}
		openWhenConnected()
	})

	primary, err := startPrimaryIPCClaimed(ctx, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		_ = runtime.Close()
		return errors.New("per-user host ownership changed before the web service started")
	}
	if err != nil {
		_ = runtime.Close()
		return err
	}
	claim = nil
	appURL, err = browserURL(currentPrimaryEndpoint().Listen)
	if err != nil {
		_ = primary.Close()
		_ = runtime.Close()
		return err
	}
	appURL, err = webURLForAppAction(appURL, initial)
	if err != nil {
		_ = primary.Close()
		_ = runtime.Close()
		return err
	}
	defer primary.Close()
	defer runtime.Close()
	if !*noAuto {
		// The Web host owns the same controller runtime as the TUI and CLI, so
		// it must initiate the first authenticated connection itself. Reconnect
		// also arms the bounded device watcher after an initial failure; the
		// HTTP UI remains available meanwhile and the browser-open gate below
		// continues to require a truthfully connected snapshot.
		connectContext, connectCancel := context.WithTimeout(ctx, 15*time.Second)
		connectErr := runtime.Reconnect(connectContext, "web host initial automatic connection")
		connectCancel()
		if connectErr != nil {
			runtime.PublishHostEvent("connection.auto.error", "initial Web connection: "+connectErr.Error())
			fmt.Fprintln(stderr, "auto-connect:", connectErr)
		}
	}
	if initial.Kind != "" {
		applyInitialWebAction(ctx, primary, runtime, engine, initial)
	}
	if !*noTray {
		nativeShell, shellErr := startNativeWebShell(ctx, cancel, appURL, runtime, store, primary)
		if shellErr != nil {
			fmt.Fprintln(stderr, "native web shell:", shellErr)
		} else {
			defer func() {
				if closeErr := nativeShell.Close(); closeErr != nil {
					fmt.Fprintln(stderr, "close native web shell:", closeErr)
				}
			}()
		}
	}
	primaryReadyOnce.Do(func() { close(primaryReady) })
	go watchConfiguration(ctx, store, runtime, connection)
	go func() {
		for value := range store.Subscribe(ctx) {
			updateHostMenuManager(hostMenus, value, runtime)
		}
	}()
	go control.RunAutomations(ctx, runtime, engine, store.Current)

	fmt.Fprintln(stdout, productidentity.ServiceName(store.Current().UI.AppTitle, "web app:"), appURL)
	if webBrowserAutoOpenAllowed(*noOpen, runtime.Snapshot().Connected) {
		openWhenConnected()
	} else if !*noOpen {
		fmt.Fprintln(stdout, "controller offline; browser will open after an authenticated connection")
	}
	select {
	case <-ctx.Done():
		return nil
	case <-primary.QuitRequested():
		return nil
	}
}

func webBrowserAutoOpenAllowed(noOpen, controllerConnected bool) bool {
	return !noOpen && controllerConnected
}

func primaryControllerConnected(ctx context.Context) bool {
	var snapshot control.Snapshot
	return callPrimary(
		ctx,
		"controller.snapshot",
		map[string]any{},
		&snapshot,
	) == nil && snapshot.Connected
}

func webURLForAppAction(value string, action hostui.AppAction) (string, error) {
	if action.Kind == "" || action.Kind != "app.page" {
		return value, nil
	}
	page := strings.ToLower(strings.TrimSpace(action.Value))
	switch page {
	case "dashboard", "controls", "workbench", "device", "data", "updates", "events", "settings":
	default:
		return "", fmt.Errorf("web page action %q is not recognized", action.Value)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("web app action requires an absolute HTTP(S) URL")
	}
	parsed.Fragment = "/" + page
	return parsed.String(), nil
}

func deliverExistingAppAction(
	parent context.Context,
	action hostui.AppAction,
	stdout io.Writer,
) error {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	switch action.Kind {
	case "command":
		output, err := executeThroughPrimary(ctx, action.Value)
		if output != "" && stdout != nil {
			fmt.Fprintln(stdout, output)
		}
		return err
	case "app.port.open":
		output, err := executeThroughPrimary(ctx, "open")
		if output != "" && stdout != nil {
			fmt.Fprintln(stdout, output)
		}
		return err
	case "app.port.close":
		output, err := executeThroughPrimary(ctx, "close")
		if output != "" && stdout != nil {
			fmt.Fprintln(stdout, output)
		}
		return err
	case "app.quit":
		return callPrimary(ctx, "controller.quit", map[string]any{}, nil)
	default:
		return callPrimary(ctx, "controller.app.action", action, nil)
	}
}

func applyInitialWebAction(
	parent context.Context,
	primary *primaryIPC,
	runtime *control.Runtime,
	engine *shell.Engine,
	action hostui.AppAction,
) {
	if primary == nil || runtime == nil || engine == nil {
		return
	}
	if action.Kind == "app.page" {
		if err := primary.actions.Publish(action); err != nil {
			runtime.PublishHostEvent("host.activation.error", err.Error())
		}
		return
	}
	command := ""
	switch action.Kind {
	case "command":
		command = strings.TrimSpace(action.Value)
	case "app.port.open":
		command = "open"
	case "app.port.close":
		command = "close"
	default:
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		defer cancel()
		if action.Kind == "command" {
			connectContext, connectCancel := context.WithTimeout(ctx, 15*time.Second)
			connectErr := runtime.EnsureConnected(connectContext)
			connectCancel()
			if connectErr != nil {
				runtime.PublishHostEvent("host.activation.error", "cold command connection: "+connectErr.Error())
				return
			}
		}
		output, err := engine.Execute(ctx, command)
		if err != nil {
			runtime.PublishHostEvent("host.activation.error", err.Error())
			return
		}
		runtime.PublishHostEvent("host.activation", firstDeviceName(output, "desktop action completed"))
	}()
}

func browserURL(address string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", fmt.Errorf("build web app URL from ipc.listen: %w", err)
	}
	host = strings.Trim(host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	value := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/"}
	return value.String(), nil
}

func openBrowser(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("browser URL must be an absolute HTTP(S) URL")
	}
	var command *exec.Cmd
	switch gort.GOOS {
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", value)
	case "darwin":
		command = exec.Command("open", value)
	default:
		command = exec.Command("xdg-open", value)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}

func runTUIWithInitialAction(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
	initial hostui.AppAction,
) error {
	flags := flag.NewFlagSet("tui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	buzzerOptions, err := addBuzzerRuntimeFlags(flags, store.Persistent().Integrations.BuzzerMirror)
	if err != nil {
		return err
	}
	consoleOptions, err := addTUIConsoleFlags(flags, store.Current().UI.TUIConsole)
	if err != nil {
		return err
	}
	noAuto := flags.Bool("no-auto", false, "start with automatic connection paused")
	ipcAddress := flags.String("ipc-addr", "", "attach the full TUI to an existing controller IPC host:port")
	ipcToken := flags.String("ipc-token", "", "bearer token for --ipc-addr (prefer --ipc-token-ref)")
	ipcTokenReference := flags.String("ipc-token-ref", "", "resolve the remote IPC bearer token from an OS-vault or environment reference")
	simpleMode := flags.Bool("simple", false, "use the minimal line-oriented IPC fallback instead of the full TUI")
	syncNavigation := flags.Bool("sync-navigation", true, "synchronize this full TUI's active page with other TUI instances")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	if err := buzzerOptions.captureOverrides(flags); err != nil {
		return err
	}
	if err := buzzerOptions.apply(store); err != nil {
		return err
	}
	if err := consoleOptions.captureOverrides(flags); err != nil {
		return err
	}
	ipcTokenWasSet := false
	flags.Visit(func(value *flag.Flag) {
		ipcTokenWasSet = ipcTokenWasSet || value.Name == "ipc-token"
	})
	if strings.TrimSpace(*ipcTokenReference) != "" {
		if ipcTokenWasSet {
			return errors.New("--ipc-token and --ipc-token-ref are mutually exclusive")
		}
		resolved, resolveErr := store.ResolveSecret(*ipcTokenReference)
		if resolveErr != nil {
			return fmt.Errorf("resolve remote IPC bearer token: %w", resolveErr)
		}
		*ipcToken = resolved
	}
	if err := applyTUIConsole(
		consoleOptions.resolve(store.Current().UI.TUIConsole), stderr,
		consoleOptions.haveRuntimeFlag(),
	); err != nil {
		return fmt.Errorf("apply local TUI console settings: %w", err)
	}
	if address := strings.TrimSpace(*ipcAddress); address != "" {
		auth := *ipcToken
		configured := currentPrimaryEndpoint()
		if auth == "" && strings.EqualFold(address, strings.TrimSpace(configured.Listen)) {
			auth = configured.AuthToken
		}
		if *simpleMode {
			return runSecondaryConsoleAt(
				os.Stdin, stdout, stderr, store.Current().UI.AppTitle,
				address, auth,
			)
		}
		return runRemoteTUI(address, auth, stdout, store, consoleOptions, *syncNavigation)
	}
	claim, havePrimary, err := preparePrimaryMode("tui")
	if err != nil {
		return err
	}
	if havePrimary {
		if initial.Kind != "" {
			return deliverExistingAppAction(context.Background(), initial, stdout)
		}
		if *simpleMode {
			return runSecondaryConsole(os.Stdin, stdout, stderr, store.Current().UI.AppTitle)
		}
		configured := currentPrimaryEndpoint()
		return runRemoteTUI(configured.Listen, configured.AuthToken, stdout, store, consoleOptions, *syncNavigation)
	}
	defer func() {
		if claim != nil {
			_ = claim.Close()
		}
	}()
	if *simpleMode {
		return errors.New("--simple requires an existing IPC primary; start without --simple to own the local runtime")
	}
	if err := selectInteractiveDevice(
		connection,
		os.Stdin,
		stderr,
	); err != nil {
		return err
	}

	runtime := newRuntime(connection, store)
	rfReplace := control.NewRFReplaceService(runtime)
	bindRuntimeDevicePersistence(runtime, store)
	if *noAuto {
		_ = runtime.Close()
	}
	project := findProjectRoot()
	outputs := control.NewOutputScheduler(runtime)
	commandConfiguration := commandOptions(store, project)
	commandConfiguration.Outputs = outputs
	engine := control.NewCommandEngine(runtime, commandConfiguration)
	hostMenus := newHostMenuManager(store, runtime, engine)
	if err := hostmenu.RegisterCommands(engine, hostMenus); err != nil {
		_ = runtime.Close()
		return err
	}
	hostPanel := &hostFrontPanelBridge{runtime: runtime}
	hostMenus.SetDefinitionChanged(func(change hostmenu.DefinitionChange) {
		publishHostMenuDefinitionChange(runtime, change)
		go syncHostMenuOverlay(runtime, hostMenus, hostPanel, &change)
	})
	runtime.SetHostMenuRequestHandler(func(request native.HostMenuContentRequest) {
		syncHostMenuRequest(runtime, hostMenus, request)
	})
	programDataPaths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		_ = runtime.Close()
		return err
	}
	lifecycleOptions := control.ProgrammingLifecycleOptions{
		DataPaths: programDataPaths, Outputs: outputs, HostConfig: store.Current,
	}
	runtime.SetConnectionReadyHandler(func(_ ports.Info, hello native.Hello) {
		recoveryContext, recoveryCancel := context.WithTimeout(context.Background(), 8*time.Second)
		var recoveryOutput bytes.Buffer
		recoveryErr := control.RecoverPendingProgrammingSessions(
			recoveryContext,
			runtime,
			lifecycleOptions,
			&recoveryOutput,
		)
		recoveryCancel()
		if text := strings.TrimSpace(recoveryOutput.String()); text != "" {
			runtime.PublishHostEvent("program.recovery", text)
		}
		if recoveryErr != nil {
			runtime.PublishHostEvent(
				"program.recovery.error",
				"pending MCU settings restore failed: "+recoveryErr.Error(),
			)
		}
		hostPanel.ConnectionReady()
		if native.SupportsHostMenuOverlay(hello) || hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
			syncHostMenuOverlay(runtime, hostMenus, hostPanel, nil)
		}
	})
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	primary, err := startPrimaryIPCClaimed(watchContext, runtime, engine, store, claim)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		_ = runtime.Close()
		configured := currentPrimaryEndpoint()
		return runRemoteTUI(configured.Listen, configured.AuthToken, stdout, store, consoleOptions, *syncNavigation)
	}
	if err != nil {
		_ = runtime.Close()
		return err
	}
	claim = nil
	defer primary.Close()
	appActions := primary.AppActions()
	navigationReporter, err := hostui.NewNavigationReporter(*syncNavigation, "")
	if err != nil {
		return fmt.Errorf("create TUI instance identity: %w", err)
	}
	tuiInstanceID := navigationReporter.InstanceID()
	processStartedAt := time.Time{}
	if primary.instanceClaim != nil {
		processStartedAt = primary.instanceClaim.startedAt
	}
	tuiSelf := hostui.CurrentProcessSelf(processStartedAt)
	defer primary.instances.Remove(tuiInstanceID)
	if initial.Kind != "" {
		if err := primary.actions.Publish(initial); err != nil {
			return err
		}
	}
	go watchConfiguration(watchContext, store, runtime, connection)
	go func() {
		for value := range store.Subscribe(watchContext) {
			updateHostMenuManager(hostMenus, value, runtime)
		}
	}()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	navigationCommits := make(chan string, 1)
	queueNavigationCommit := func(page string) {
		page = strings.ToLower(strings.TrimSpace(page))
		if page == "" {
			return
		}
		select {
		case navigationCommits <- page:
			return
		default:
		}
		// Keep only the latest local intent while a prior coordinator call is in
		// flight. Bubble Tea's update loop never waits for IPC or fan-out.
		select {
		case <-navigationCommits:
		default:
		}
		select {
		case navigationCommits <- page:
		default:
		}
	}
	go func() {
		for {
			select {
			case page := <-navigationCommits:
				for {
					select {
					case latest := <-navigationCommits:
						page = latest
					default:
						goto commit
					}
				}
			commit:
				_, commitErr := primary.navigationCommand(hostui.NavigationCommand{
					Group: hostui.DefaultNavigationGroup, Source: tuiInstanceID,
					Page: page, OperationID: navigationReporter.NextOperationID(),
				})
				if commitErr != nil {
					runtime.PublishHostEvent(
						"app.navigation.commit.rejected",
						"navigation change was not synchronized: "+commitErr.Error(),
					)
				}
			case <-watchContext.Done():
				return
			}
		}
	}()
	program := tea.NewProgram(
		tui.NewApplicationWithOptions(runtime, engine, tui.Options{
			UIConfig: func() appconfig.UI { return store.Current().UI },
			SaveUI: func(value appconfig.UI) error {
				_, err := store.UpdateUI(value)
				return err
			},
			ApplyTUIConsole: func(value appconfig.TUIConsole) error {
				settings := consoleOptions.resolve(value)
				result, err := consolewindow.Apply(settings)
				if err != nil {
					return err
				}
				if !result.Applied && settings.Enabled {
					return errors.New(result.Reason)
				}
				return nil
			},
			HostIntegrations: func() appconfig.Integrations {
				return store.Persistent().Integrations
			},
			BuzzerRuntime: func() appconfig.BuzzerRuntimeStatus {
				return primary.IntegrationStatus().BuzzerRuntime
			},
			SaveIntegrations: func(value appconfig.Integrations) error {
				if value.Discovery != store.Persistent().Integrations.Discovery {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					var persisted appconfig.Discovery
					err := callPrimary(ctx, "controller.discovery.config.set", value.Discovery, &persisted)
					cancel()
					if err != nil {
						return fmt.Errorf("save discovery settings through primary: %w", err)
					}
					value.Discovery = persisted
				}
				_, err := store.Update(func(config *appconfig.Config) error {
					config.Integrations = value
					return nil
				})
				return err
			},
			RFConfig: func() appconfig.RFConfig { return store.Current().RF },
			SaveRF: func(value appconfig.RFConfig) error {
				_, err := store.Update(func(config *appconfig.Config) error {
					config.RF = value
					return nil
				})
				return err
			},
			RFFetch:          rfReplace.Fetch,
			RFApplyOrder:     rfReplace.Replace,
			RFReplaceSupport: rfReplace.Support,
			RFProbeReplace:   rfReplace.Probe,
			HostMenus:        hostMenus,
			PushHostPanel:    hostPanel.Push,
			ReleaseHostPanel: hostPanel.Release,
			WelcomeMelody: func(ctx context.Context) error {
				name := strings.TrimSpace(store.Current().UI.WelcomeMelody)
				_, err := engine.Execute(ctx, shell.Join([]string{"melody", "wait", name}))
				return err
			},
			Integrations: func() hostui.IntegrationStatus {
				return primaryHostUIStatus(primary, store.Current())
			},
			NetworkDiscovery: func(ctx context.Context) ([]discovery.Instance, error) {
				var instances []discovery.Instance
				err := callPrimary(ctx, "controller.discovery.scan", map[string]any{
					"timeout_ms": 3000,
					"mdns":       true, "dns_sd": true, "ssdp": true, "upnp": true,
					"ws_discovery": true, "broadcast": true, "netbios": true,
				}, &instances)
				return instances, err
			},
			OpenNetwork:        openBrowser,
			AppActions:         appActions,
			InstanceID:         tuiInstanceID,
			NavigationSync:     *syncNavigation,
			NavigationGroup:    hostui.DefaultNavigationGroup,
			SetNavigationSync:  navigationReporter.SetFollow,
			NavigationIdentity: navigationReporter.Identity,
			WriteOSC: func(payload string) error {
				return hostui.WriteOSC(stdout, payload)
			},
			ReportTerminal: func(page, title string) error {
				ui := store.Current().UI
				values := navigationReporter.NextValues()
				values["color_mode"] = ui.Appearance.Theme
				values["locale"] = ui.Appearance.Locale
				values["terminal_title"] = title
				values["terminal_osc"] = "enabled"
				values["terminal_progress"] = "osc-9-4"
				_, err := primary.instances.Upsert(hostui.AppInstance{
					ID: tuiInstanceID, Surface: "tui", Page: page, State: "active",
					Self: &tuiSelf, Values: values,
				})
				return err
			},
			CommitNavigation: queueNavigationCommit,
		}),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	go func() {
		select {
		case <-primary.QuitRequested():
			program.Quit()
		case <-watchContext.Done():
		}
	}()
	_, err = program.Run()
	_ = hostui.WriteOSC(stdout, "9;4;0;0")
	_ = primary.Close()
	_ = runtime.Close()
	return err
}

func primaryHostUIStatus(
	primary *primaryIPC,
	config appconfig.Config,
) hostui.IntegrationStatus {
	status := primary.IntegrationStatus()
	notificationStatus := hostui.NotificationStatus{}
	if notifier := primary.Notifier(); notifier != nil {
		notificationStatus = notifier.Status()
	}
	state := func(enabled bool) string {
		if enabled {
			return "running"
		}
		return "disabled"
	}
	return hostui.IntegrationStatus{
		Hotkeys:       primary.HotkeyStatus(),
		Keyboard:      primary.KeyboardStatus(),
		Notifications: notificationStatus,
		Messaging: hostui.ServiceStatus{
			Name: "JSON-RPC / REST / WebSocket", Enabled: true,
			State: "running", Endpoint: config.IPC.Listen + config.IPC.WebSocketPath,
			LastError: status.LastError,
		},
		Discovery: hostui.ServiceStatus{
			Name: "DNS-SD + SSDP/UPnP + WS-Discovery + broadcast + NetBIOS", Enabled: status.DiscoveryActive,
			State: state(status.DiscoveryActive),
		},
		Webhooks: hostui.ServiceStatus{
			Name: "HTTP webhooks", Enabled: status.WebhooksActive > 0 || config.Integrations.InboundWebhooksEnabled,
			State:  state(status.WebhooksActive > 0 || config.Integrations.InboundWebhooksEnabled),
			Detail: fmt.Sprintf("%d outbound", status.WebhooksActive),
		},
		SocketIO: hostui.ServiceStatus{
			Name: "Socket.IO v4 WS subset", Enabled: true,
			State: "running", Endpoint: config.IPC.Listen + config.IPC.SocketIOPath,
			Detail: "WebSocket transport; no polling, rooms, or binary attachments",
		},
	}
}
