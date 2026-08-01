package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"pccontroller.local/controller/internal/programmer"
	"pccontroller.local/controller/internal/script"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/controller/internal/tui"
	"pccontroller.local/controller/internal/wsrelay"
)

var (
	version    = "development"
	sourceHash = "unknown"
	buildTime  = "unknown"
)

type connectionFlags struct {
	device           string
	port             string
	vid              string
	pid              string
	name             string
	baud             int
	startupWait      time.Duration
	requestTimeout   time.Duration
	helloAttempts    int
	resetOnReconnect bool
	overrides        map[string]bool
	preferred        ports.Identity
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	cleanArgs, configPath, err := extractConfigArgument(args)
	if err != nil {
		return err
	}
	args = cleanArgs
	if len(args) == 0 {
		store, openErr := appconfig.Open(configPath)
		if openErr != nil {
			return openErr
		}
		configurePrimaryIPC(store.Current())
		return runTUI(nil, stdout, stderr, store)
	}
	switch strings.ToLower(args[0]) {
	case "version", "--version", "-version":
		fmt.Fprintf(
			stdout,
			"pccontroller-tool %s source-hash=%s built=%s\n",
			version,
			sourceHash,
			buildTime,
		)
		return nil
	case "help", "--help", "-h":
		printUsage(stdout)
		return nil
	}
	store, err := appconfig.Open(configPath)
	if err != nil {
		return err
	}
	configurePrimaryIPC(store.Current())
	switch strings.ToLower(args[0]) {
	case "tui":
		return runTUI(args[1:], stdout, stderr, store)
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
	case "arduino":
		return runArduino(args[1:], stdout, stderr, store)
	case "ws":
		return runWS(args[1:], stdout, stderr, store)
	case "config":
		return runConfig(args[1:], stdout, store)
	case "desktop":
		return runDesktop(args[1:], stdout, store)
	case "uri", "action":
		return runURIAction(args[1:], stdout, stderr, store)
	default:
		return fmt.Errorf("unknown command %q; use help", args[0])
	}
}

func runDesktop(
	args []string,
	stdout io.Writer,
	store *appconfig.Store,
) error {
	if len(args) > 1 || (len(args) == 1 &&
		!strings.EqualFold(args[0], "install") &&
		!strings.EqualFold(args[0], "ensure")) {
		return errors.New("usage: desktop install")
	}
	status, err := hostui.EnsureDesktopIntegration(
		hostui.DesktopIntegrationOptions{
			AppID:       "DRSDavidSoft.PCController",
			DisplayName: store.Current().UI.AppTitle,
		},
	)
	encoded, _ := json.MarshalIndent(status, "", "  ")
	fmt.Fprintln(stdout, string(encoded))
	return err
}

func runURIAction(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	if len(args) != 1 {
		return errors.New("usage: pccontroller uri pccontroller://page/events")
	}
	action, err := hostui.ParseActionURI(args[0])
	if err != nil {
		return err
	}
	probeContext, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	havePrimary := primaryAvailable(probeContext)
	cancel()
	if havePrimary {
		if action.Kind == "command" {
			output, err := executeThroughPrimary(context.Background(), action.Value)
			if output != "" {
				fmt.Fprintln(stdout, output)
			}
			return err
		}
		return callPrimary(
			context.Background(), "controller.app.action", action, nil,
		)
	}
	// Opening a notification while the app is closed starts the normal TUI and
	// injects the validated action after it becomes the primary process.
	return runTUIWithInitialAction(nil, stdout, stderr, store, action)
}

func runTUI(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	return runTUIWithInitialAction(args, stdout, stderr, store, hostui.AppAction{})
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
	noAuto := flags.Bool("no-auto", false, "start with automatic connection paused")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	probeContext, probeCancel := context.WithTimeout(
		context.Background(),
		400*time.Millisecond,
	)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		return runSecondaryConsole(os.Stdin, stdout, stderr)
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
	engine := control.NewCommandEngine(runtime, commandOptions(store, project))
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
	runtime.SetConnectionReadyHandler(func(_ ports.Info, hello native.Hello) {
		hostPanel.ConnectionReady()
		if hello.Capabilities&(native.CapabilityHostMenuOverlay|native.CapabilityHostFrontPanel) != 0 {
			syncHostMenuOverlay(runtime, hostMenus, hostPanel, nil)
		}
	})
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	primary, err := startPrimaryIPC(watchContext, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		_ = runtime.Close()
		return runSecondaryConsole(os.Stdin, stdout, stderr)
	}
	if err != nil {
		_ = runtime.Close()
		return err
	}
	defer primary.Close()
	if initial.Kind != "" {
		if err := primary.actions.Publish(initial); err != nil {
			return err
		}
	}
	go watchConfiguration(watchContext, store, runtime, connection)
	go func() {
		for value := range store.Subscribe(watchContext) {
			hostMenus.UpdateConfig(value.HostMenus)
		}
	}()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	program := tea.NewProgram(
		tui.NewApplicationWithOptions(runtime, engine, tui.Options{
			UIConfig: func() appconfig.UI { return store.Current().UI },
			SaveUI: func(value appconfig.UI) error {
				_, err := store.UpdateUI(value)
				return err
			},
			HostIntegrations: func() appconfig.Integrations {
				return store.Current().Integrations
			},
			SaveIntegrations: func(value appconfig.Integrations) error {
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
			AppActions: primary.AppActions(),
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
			Name: "mDNS + SSDP", Enabled: status.DiscoveryActive,
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

func newHostMenuManager(
	store *appconfig.Store,
	runtime *control.Runtime,
	engine *shell.Engine,
) *hostmenu.Manager {
	read := func(ctx context.Context, action string) (string, error) {
		config := store.Current()
		snapshot := runtime.Snapshot()
		switch strings.ToLower(strings.TrimSpace(action)) {
		case "host.status":
			return fmt.Sprintf("PC online · device %s", map[bool]string{true: "connected", false: "offline"}[snapshot.Connected]), nil
		case "device.status":
			if !snapshot.Connected {
				return "Offline", nil
			}
			return fmt.Sprintf("%s · uptime %s", snapshot.Port.Name, time.Duration(snapshot.Status.UptimeMS)*time.Millisecond), nil
		case "host.ip":
			return preferredHostAddress(), nil
		case "api.status":
			return fmt.Sprintf("IPC %s · WS %s", config.IPC.Listen, config.IPC.WebSocketPath), nil
		case "pc.ui.app_title":
			return config.UI.AppTitle, nil
		case "pc.ui.status_interval_ms":
			return strconv.Itoa(config.UI.StatusIntervalMS), nil
		case "pc.ui.mirror_prompt_to_lcd":
			return strconv.FormatBool(config.UI.MirrorPromptToLCD), nil
		case "pc.ui.lcd_service_enabled":
			return strconv.FormatBool(config.UI.LCDServiceEnabled), nil
		case "pc.connection.reset_on_reconnect":
			return strconv.FormatBool(config.Connection.ResetOnReconnect), nil
		case "os.brightness":
			result, err := hostos.DefaultExecutor.MonitorBrightness(ctx)
			if err != nil {
				runtime.PublishHostEvent("os.brightness", "host-menu brightness read unavailable: "+err.Error())
				return "", err
			}
			runtime.PublishHostEvent("os.brightness", fmt.Sprintf("host-menu brightness read %d%%", result.Status.Percent))
			return strconv.Itoa(result.Status.Percent), nil
		default:
			if line, ok := hostMenuShellAction(action); ok {
				return engine.Execute(ctx, line)
			}
			return "", fmt.Errorf("unknown host-menu read action %q", action)
		}
	}
	write := func(ctx context.Context, action, value string) (string, error) {
		rawAction := strings.TrimSpace(action)
		action = strings.ToLower(rawAction)
		if action == "os.brightness" {
			return engine.Execute(ctx, shell.Join([]string{"os", "brightness", "set", value}))
		}
		_, err := store.Update(func(config *appconfig.Config) error {
			switch action {
			case "pc.ui.app_title":
				config.UI.AppTitle = strings.TrimSpace(value)
			case "pc.ui.status_interval_ms":
				parsed, parseErr := strconv.Atoi(value)
				if parseErr != nil {
					return parseErr
				}
				config.UI.StatusIntervalMS = parsed
			case "pc.ui.mirror_prompt_to_lcd":
				parsed, parseErr := strconv.ParseBool(value)
				if parseErr != nil {
					return parseErr
				}
				config.UI.MirrorPromptToLCD = parsed
			case "pc.ui.lcd_service_enabled":
				parsed, parseErr := strconv.ParseBool(value)
				if parseErr != nil {
					return parseErr
				}
				config.UI.LCDServiceEnabled = parsed
			case "pc.connection.reset_on_reconnect":
				parsed, parseErr := strconv.ParseBool(value)
				if parseErr != nil {
					return parseErr
				}
				config.Connection.ResetOnReconnect = parsed
			default:
				return fmt.Errorf("not a built-in host setting")
			}
			return nil
		})
		if err == nil {
			return "Saved on PC", nil
		}
		if line, ok := hostMenuShellAction(rawAction); ok {
			quoted := shell.Join([]string{value})
			line = strings.ReplaceAll(line, "${value}", quoted)
			return engine.Execute(ctx, line)
		}
		return "", err
	}
	execute := func(ctx context.Context, action string) (string, error) {
		normalized := strings.ToLower(strings.TrimSpace(action))
		if strings.HasPrefix(normalized, "os.") {
			operation := strings.TrimPrefix(normalized, "os.")
			config := store.Current()
			return engine.Execute(ctx, shell.Join([]string{"os", operation, config.OSActions.Power.ConfirmationToken}))
		}
		if line, ok := hostMenuShellAction(action); ok {
			return engine.Execute(ctx, line)
		}
		return "", fmt.Errorf("unknown host-menu action %q", action)
	}
	saveConfig := func(value appconfig.HostMenuConfig) error {
		_, err := store.Update(func(config *appconfig.Config) error {
			config.HostMenus = value
			return nil
		})
		return err
	}
	return hostmenu.New(store.Current().HostMenus, hostmenu.Callbacks{
		Read: read, Write: write, Execute: execute, SaveConfig: saveConfig,
		Interaction: func(event hostmenu.InteractionEvent) {
			publishHostMenuInteraction(runtime, event)
		},
	})
}

func hostMenuShellAction(action string) (string, bool) {
	action = strings.TrimSpace(action)
	for _, prefix := range []string{"shell:", "command:"} {
		if strings.HasPrefix(strings.ToLower(action), prefix) {
			return strings.TrimSpace(action[len(prefix):]), true
		}
	}
	return "", false
}

func preferredHostAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "Unavailable"
	}
	for _, networkInterface := range interfaces {
		if networkInterface.Flags&net.FlagUp == 0 || networkInterface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, addressErr := networkInterface.Addrs()
		if addressErr != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, parseErr := net.ParseCIDR(address.String())
			if parseErr == nil && ip.To4() != nil && !ip.IsLoopback() {
				return ip.String()
			}
		}
	}
	return "Offline"
}

type hostFrontPanelBridge struct {
	runtime   *control.Runtime
	mu        sync.Mutex
	captured  bool
	animation *legacyPanelAnimator
}

func (bridge *hostFrontPanelBridge) Push(snapshot hostmenu.Snapshot) error {
	animation := bridge.animator()
	animation.Stop()
	if err := bridge.pushSnapshot(snapshot, true); err != nil {
		return err
	}
	animation.Start(snapshot)
	return nil
}

func (bridge *hostFrontPanelBridge) animator() *legacyPanelAnimator {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.animation == nil {
		bridge.animation = newLegacyPanelAnimator(func(snapshot hostmenu.Snapshot) error {
			return bridge.pushSnapshot(snapshot, false)
		})
	}
	return bridge.animation
}

func (bridge *hostFrontPanelBridge) pushSnapshot(snapshot hostmenu.Snapshot, renderLCD bool) error {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	live := bridge.runtime.Snapshot()
	if !live.Connected {
		bridge.captured = false
		return errors.New("device is offline")
	}
	if live.Hello.Capabilities&native.CapabilityHostFrontPanel == 0 {
		return errors.New("firmware does not advertise host front-panel capture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if !bridge.captured {
		// Capturing removes local menu control; explicitly stop both motion groups
		// and cancel RF learning first so no hidden operation survives the handoff.
		if err := bridge.runtime.Command(ctx, native.OpRFLearnCancel, nil); err != nil {
			return fmt.Errorf("cancel RF learning before panel capture: %w", err)
		}
		for side := byte(0); side < 2; side++ {
			payload, _ := native.RelaySidePayload(side, 0)
			if err := bridge.runtime.Command(ctx, native.OpRelaySide, payload); err != nil {
				return fmt.Errorf("stop motion side %d before panel capture: %w", side, err)
			}
		}
	}
	state := byte(0)
	if snapshot.GuardPending {
		state = 2
	}
	value, _ := strconv.ParseUint(snapshot.Value, 10, 12)
	payload, err := native.HostPanelPayload(
		snapshot.Panel.Segments, snapshot.Panel.LCDLine1, snapshot.Panel.LCDLine2,
		state, uint16(value),
	)
	if err != nil {
		return err
	}
	if err := bridge.runtime.Command(ctx, native.OpDisplayText, payload); err != nil {
		return err
	}
	if renderLCD && live.Hello.Capabilities&native.CapabilityI2CTransfer != 0 {
		if err := bridge.runtime.LCDPresenter().RenderPhysical(
			ctx, snapshot.Panel.LCDLine1, snapshot.Panel.LCDLine2,
		); err != nil {
			// The front-panel capture and TM1637 remain useful when no LCD
			// backpack is connected; expose physical-LCD failure separately.
			bridge.runtime.PublishHostEvent("lcd.error", "host-menu LCD: "+err.Error())
		}
	}
	bridge.captured = true
	return nil
}

func (bridge *hostFrontPanelBridge) Release() error {
	bridge.animator().Stop()
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if !bridge.captured {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := bridge.runtime.Command(ctx, native.OpDisplayText, native.HostPanelReleasePayload())
	if err == nil || !bridge.runtime.Snapshot().Connected {
		bridge.captured = false
	}
	return err
}

// ConnectionReady invalidates capture state from a previous USB/TCP session;
// the next Push performs the complete safe capture handshake again.
func (bridge *hostFrontPanelBridge) ConnectionReady() {
	bridge.animator().Stop()
	bridge.mu.Lock()
	bridge.captured = false
	bridge.mu.Unlock()
}

func publishHostMenuDefinitionChange(runtime *control.Runtime, change hostmenu.DefinitionChange) {
	runtime.PublishStructuredEvent(control.Event{
		Kind:   change.Kind,
		Text:   fmt.Sprintf("host-menu %s node=0x%02X fields=%s", change.MenuID, change.NodeID, strings.Join(change.Fields, ",")),
		Source: "host-config", Target: "host-menu", MessageType: "configuration",
		Metadata: map[string]string{
			"menu_id":    change.MenuID,
			"node_id":    fmt.Sprintf("0x%02X", change.NodeID),
			"builtin":    strconv.FormatBool(change.Builtin),
			"fields":     strings.Join(change.Fields, ","),
			"generation": strconv.Itoa(int(change.Generation)),
			"active":     strconv.FormatBool(change.Active),
		},
	})
}

func publishHostMenuInteraction(runtime *control.Runtime, event hostmenu.InteractionEvent) {
	runtime.PublishStructuredEvent(control.Event{
		Kind: event.Kind, Text: event.Reason, Source: "front-panel", Target: "host-menu",
		MessageType: "interaction",
		Metadata: map[string]string{
			"menu_id": event.MenuID, "item_id": event.ItemID,
			"key": strconv.Itoa(event.Key), "phase": event.Phase,
		},
	})
	opcode, payload, cue := hostMenuInteractionCue(event)
	if !cue || !runtime.Snapshot().Connected {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		defer cancel()
		if err := runtime.Command(ctx, opcode, payload); err != nil {
			runtime.PublishHostEvent("menu.audio.error", "disabled/error cue: "+err.Error())
		}
	}()
}

func hostMenuInteractionCue(event hostmenu.InteractionEvent) (byte, []byte, bool) {
	if event.Kind != "menu.action.denied" {
		return 0, nil, false
	}
	// The MCU-owned EEPROM silent flag remains authoritative; when enabled the
	// same command is accepted without sounding the buzzer.
	return native.OpBuzzer, native.BuzzerPayload(180, 80), true
}

type hostMenuPanelBridge interface {
	Push(hostmenu.Snapshot) error
	Release() error
}

// syncLegacyHostMenuOverlay is the single routing rule for watched menu
// definitions on cap19 firmware. Active edits refresh both display surfaces
// through Push, unrelated edits emit their normalized event only, and removal
// or hiding of the active page releases front-panel capture immediately.
func syncLegacyHostMenuOverlay(manager *hostmenu.Manager, legacy hostMenuPanelBridge, change *hostmenu.DefinitionChange) error {
	if legacy == nil {
		return nil
	}
	snapshot := manager.Snapshot()
	if change != nil && change.Active {
		// UpdateConfig already produced the exact post-edit definition preview.
		// Re-reading Manager.Snapshot here would render the selected item instead
		// and could leave the TM1637/LCD showing stale per-menu presentation.
		snapshot = change.Snapshot
	}
	if snapshot.Active && (change == nil || change.Active) {
		return legacy.Push(snapshot)
	}
	if change != nil && !snapshot.Active {
		return legacy.Release()
	}
	return nil
}

func syncHostMenuOverlay(runtime *control.Runtime, manager *hostmenu.Manager, legacy hostMenuPanelBridge, change *hostmenu.DefinitionChange) {
	live := runtime.Snapshot()
	if !live.Connected {
		return
	}
	if live.Hello.Capabilities&native.CapabilityHostMenuOverlay == 0 {
		if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
			if err := syncLegacyHostMenuOverlay(manager, legacy, change); err != nil {
				runtime.PublishHostEvent("menu.preview.error", err.Error())
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	directory, err := manager.Directory()
	if err == nil {
		err = runtime.ReplaceHostMenuDirectory(ctx, directory)
	}
	if err != nil {
		runtime.PublishHostEvent("menu.directory.error", err.Error())
		return
	}
	nodeID := native.HostMenuRoot
	if change != nil && change.Active {
		nodeID = change.NodeID
	} else if state, stateErr := runtime.HostMenuState(ctx); stateErr == nil {
		nodeID = state.ActiveID
	}
	if nodeID == native.HostMenuRoot {
		return
	}
	content, err := manager.Content(nodeID)
	if err == nil {
		err = runtime.PushHostMenuContent(ctx, content)
	}
	if err != nil {
		runtime.PublishHostEvent("menu.content.error", err.Error())
	}
}

func syncHostMenuRequest(runtime *control.Runtime, manager *hostmenu.Manager, request native.HostMenuContentRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	if request.Generation != manager.Generation() {
		directory, err := manager.Directory()
		if err != nil || runtime.ReplaceHostMenuDirectory(ctx, directory) != nil {
			runtime.PublishHostEvent("menu.directory.error", "cannot refresh generation for content request")
			return
		}
	}
	content, err := manager.Content(request.ID)
	if err == nil {
		err = runtime.PushHostMenuContent(ctx, content)
	}
	if err != nil {
		runtime.PublishStructuredEvent(control.Event{
			Kind: "menu.content.unavailable", Text: err.Error(), Source: "host", Target: "board",
			Metadata: map[string]string{"node_id": fmt.Sprintf("0x%02X", request.ID), "attempt": strconv.Itoa(int(request.Attempt))},
		})
	}
}

func runPorts(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("ports", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	if err := flags.Parse(args); err != nil {
		return err
	}
	list, err := ports.List()
	if err != nil {
		return err
	}
	list = ports.Candidates(list, connection.filter())
	if len(list) == 0 {
		fmt.Fprintln(stdout, "No matching serial ports.")
		return nil
	}
	for _, port := range list {
		fmt.Fprintln(stdout, port.Label())
	}
	return nil
}

func runShell(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("shell", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	noConnect := flags.Bool("no-connect", false, "start without auto-opening a device")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	probeContext, probeCancel := context.WithTimeout(
		context.Background(),
		400*time.Millisecond,
	)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		return runSecondaryConsole(os.Stdin, stdout, stderr)
	}
	if err := selectInteractiveDevice(
		connection,
		os.Stdin,
		stderr,
	); err != nil {
		return err
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go watchConfiguration(watchContext, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPC(watchContext, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return runSecondaryConsole(os.Stdin, stdout, stderr)
	}
	if err != nil {
		return err
	}
	defer primary.Close()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	if *noConnect {
		_ = runtime.Close()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := runtime.EnsureConnected(ctx)
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "auto-connect:", err)
		}
	}
	fmt.Fprintln(stdout, "PCController shell. Type help; Ctrl+Z then Enter exits on Windows.")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for {
		fmt.Fprint(stdout, "pc> ")
		if !scanner.Scan() {
			break
		}
		line := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(line, "exit") || strings.EqualFold(line, "quit") {
			break
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		output, err := engine.Execute(ctx, line)
		cancel()
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
		}
	}
	return scanner.Err()
}

func runExec(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	if flags.NArg() == 0 {
		return errors.New("exec requires a controller shell command")
	}
	commandText := joinControllerCommand(flags.Args())
	probeContext, probeCancel := context.WithTimeout(
		context.Background(),
		400*time.Millisecond,
	)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Minute,
		)
		defer cancel()
		output, err := executeThroughPrimary(ctx, commandText)
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return err
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	ownerContext, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	primary, err := startPrimaryIPC(ownerContext, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		ctx, cancel := context.WithTimeout(
			context.Background(),
			10*time.Minute,
		)
		defer cancel()
		output, routeErr := executeThroughPrimary(ctx, commandText)
		if output != "" {
			fmt.Fprintln(stdout, output)
		}
		return routeErr
	}
	if err != nil {
		return err
	}
	defer primary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := runtime.EnsureConnected(ctx); err != nil {
		cancel()
		return err
	}
	cancel()
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	output, err := engine.Execute(ctx, commandText)
	if output != "" {
		fmt.Fprintln(stdout, output)
	}
	return err
}

func runBatch(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("batch", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	fileName := flags.String("file", "-", "script file, or - for standard input")
	continueOnError := flags.Bool("continue", false, "continue after command errors")
	jsonOutput := flags.Bool("json", false, "emit one JSON result object per command")
	noConnect := flags.Bool("no-connect", false, "do not connect before running")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)

	var input io.Reader = os.Stdin
	var file *os.File
	var err error
	if *fileName != "-" {
		file, err = os.Open(*fileName)
		if err != nil {
			return err
		}
		defer file.Close()
		input = file
	}
	scriptOptions := script.Options{
		ContinueOnError: *continueOnError,
		OnResult: func(result script.Result) {
			if *jsonOutput {
				_ = json.NewEncoder(stdout).Encode(result)
				return
			}
			fmt.Fprintf(stdout, "[line %d] > %s\n", result.Line, result.Command)
			if result.Output != "" {
				fmt.Fprintln(stdout, result.Output)
			}
			if result.Error != "" {
				fmt.Fprintln(stderr, "error:", result.Error)
			}
		},
	}
	ctx, cancel := signalContext()
	defer cancel()
	probeContext, probeCancel := context.WithTimeout(ctx, 400*time.Millisecond)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		return script.Run(ctx, input, primaryExecutor{}, scriptOptions)
	}

	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	watchContext, stopWatching := context.WithCancel(context.Background())
	defer stopWatching()
	go watchConfiguration(watchContext, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPC(watchContext, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return script.Run(ctx, input, primaryExecutor{}, scriptOptions)
	}
	if err != nil {
		return err
	}
	defer primary.Close()
	go control.RunAutomations(watchContext, runtime, engine, store.Current)
	if !*noConnect {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = runtime.EnsureConnected(ctx)
		cancel()
		if err != nil {
			return err
		}
	} else {
		_ = runtime.Close()
	}
	return script.Run(ctx, input, engine, scriptOptions)
}

func runMonitor(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("monitor", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	period := flags.Duration(
		"interval",
		time.Duration(store.Current().UI.StatusIntervalMS)*time.Millisecond,
		"status refresh interval",
	)
	count := flags.Int("count", 0, "number of samples; zero runs until interrupted")
	jsonOutput := flags.Bool("json", false, "emit newline-delimited JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	connection.captureOverrides(flags)
	if *period < 100*time.Millisecond {
		return errors.New("monitor interval must be at least 100ms")
	}
	if *count < 0 {
		return errors.New("monitor count cannot be negative")
	}
	ctx, cancel := signalContext()
	defer cancel()
	probeContext, probeCancel := context.WithTimeout(ctx, 400*time.Millisecond)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		return runRemoteMonitor(
			ctx,
			*period,
			*count,
			*jsonOutput,
			stdout,
			stderr,
		)
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	go watchConfiguration(ctx, store, runtime, connection)
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	primary, err := startPrimaryIPC(ctx, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		return runRemoteMonitor(
			ctx,
			*period,
			*count,
			*jsonOutput,
			stdout,
			stderr,
		)
	}
	if err != nil {
		return err
	}
	defer primary.Close()
	go control.RunAutomations(ctx, runtime, engine, store.Current)
	if err := connectWithTimeout(ctx, runtime); err != nil {
		return err
	}
	ticker := time.NewTicker(*period)
	defer ticker.Stop()
	samples := 0
	refresh := func() error {
		requestContext, requestCancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		status, err := runtime.RefreshStatus(requestContext)
		requestCancel()
		if err != nil {
			return err
		}
		if *jsonOutput {
			record := struct {
				Type   string        `json:"type"`
				Time   time.Time     `json:"time"`
				Port   string        `json:"port"`
				Status native.Status `json:"status"`
			}{
				Type: "status", Time: time.Now(),
				Port: runtime.Snapshot().Port.Name, Status: status,
			}
			return json.NewEncoder(stdout).Encode(record)
		}
		fmt.Fprintf(
			stdout,
			"%s supply=%7.3fV bus=%7.3fV current=%6dmA power=%6dmW "+
				"tLED=%6.2fC tBT=%6.2fC keys=%X relays=%02X door=%t BT=%d "+
				"PWM=%d:%d reset=%d/0x%02X\n",
			time.Now().Format("15:04:05.000"),
			float64(status.SupplyMV)/1000,
			float64(status.BusMV)/1000,
			status.CurrentMA,
			status.PowerMW,
			float64(status.TLEDCenti)/100,
			float64(status.TBTCenti)/100,
			status.ActiveKeys,
			status.ActiveRelays,
			status.DoorOpen,
			status.BluetoothState,
			status.PWMChannel,
			status.PWMValue,
			status.ResetCount,
			status.ResetCause,
		)
		return nil
	}
	emitEvent := func(event control.Event) error {
		if event.Kind == "telemetry" {
			return nil
		}
		if *jsonOutput {
			var device *native.DeviceEvent
			gesture := event.Gesture
			source := event.Source
			if event.Frame.Opcode == native.OpEvent {
				if parsed, err := native.ParseDeviceEvent(event.Frame.Payload); err == nil {
					device = &parsed
					if parsed.Type == native.EventKey {
						gesture = control.NormalizeGesture(parsed.Gesture)
						source = map[byte]string{
							native.InputSourcePhysical: "physical",
							native.InputSourceRF:       "rf",
							native.InputSourceHost:     "host",
						}[parsed.Source]
					}
				}
			}
			record := struct {
				Type       string              `json:"type"`
				ID         uint64              `json:"id"`
				Time       time.Time           `json:"time"`
				Kind       string              `json:"kind"`
				Text       string              `json:"text"`
				Opcode     byte                `json:"opcode,omitempty"`
				Payload    []byte              `json:"payload,omitempty"`
				Lifecycle  string              `json:"lifecycle,omitempty"`
				Port       ports.Info          `json:"port,omitempty"`
				Reason     string              `json:"reason,omitempty"`
				State      string              `json:"state,omitempty"`
				Device     *native.DeviceEvent `json:"device,omitempty"`
				Gesture    string              `json:"gesture,omitempty"`
				Source     string              `json:"source,omitempty"`
				RFCode     uint32              `json:"rf_code,omitempty"`
				RFBits     byte                `json:"rf_bits,omitempty"`
				RFProtocol byte                `json:"rf_protocol,omitempty"`
				RFPulseUS  uint16              `json:"rf_pulse_us,omitempty"`
				ResetCause byte                `json:"reset_cause,omitempty"`
				ResetCount uint32              `json:"reset_count,omitempty"`
			}{
				Type: "event", ID: event.ID, Time: event.Time,
				Kind: event.Kind, Text: event.Text,
				Opcode: event.Frame.Opcode, Payload: event.Frame.Payload,
				Lifecycle: event.Lifecycle, Port: event.Port,
				Reason: event.Reason, State: event.State,
				Device: device, Gesture: gesture, Source: source,
				RFCode: event.RFCode, RFBits: event.RFBits,
				RFProtocol: event.RFProtocol, RFPulseUS: event.RFPulseUS,
				ResetCause: event.ResetCause, ResetCount: event.ResetCount,
			}
			return json.NewEncoder(stdout).Encode(record)
		}
		fmt.Fprintf(
			stdout,
			"%s EVENT %-10s %s\n",
			event.Time.Format("15:04:05.000"),
			event.Kind,
			event.Text,
		)
		return nil
	}
	if err := refresh(); err != nil {
		fmt.Fprintln(stderr, "monitor:", err)
	} else {
		samples++
	}
	for {
		if *count != 0 && samples >= *count {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case event := <-runtime.Events():
			if err := emitEvent(event); err != nil {
				return err
			}
		case <-ticker.C:
			if err := refresh(); err != nil {
				fmt.Fprintln(stderr, "monitor:", err)
				if !runtime.Snapshot().Connected {
					runtime.ResumeAuto()
					if reconnectErr := connectWithTimeout(ctx, runtime); reconnectErr != nil {
						continue
					}
				}
			} else {
				samples++
			}
		}
	}
}

func connectWithTimeout(ctx context.Context, runtime *control.Runtime) error {
	connectContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return runtime.EnsureConnected(connectContext)
}

func runIPC(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	if len(args) == 0 {
		return errors.New("usage: ipc serve|call")
	}
	switch strings.ToLower(args[0]) {
	case "serve":
		flags := flag.NewFlagSet("ipc serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		connection := addConnectionFlags(flags, store.Current().Connection)
		listen := flags.String(
			"listen",
			store.Current().IPC.Listen,
			"loopback TCP listen address for NDJSON and WebSocket",
		)
		websocketPath := flags.String(
			"ws-path",
			store.Current().IPC.WebSocketPath,
			"WebSocket endpoint path on the same IPC port",
		)
		stdio := flags.Bool("stdio", false, "serve newline-delimited JSON-RPC on stdin/stdout")
		noConnect := flags.Bool("no-connect", false, "start without connecting to the board")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		serverConfig := store.Current()
		serverConfig.IPC.Listen = *listen
		serverConfig.IPC.WebSocketPath = *websocketPath
		if err := serverConfig.Validate(); err != nil {
			return fmt.Errorf("IPC server configuration: %w", err)
		}
		connection.captureOverrides(flags)
		var listener net.Listener
		if *stdio {
			probeContext, probeCancel := context.WithTimeout(
				context.Background(),
				400*time.Millisecond,
			)
			havePrimary := primaryAvailable(probeContext)
			probeCancel()
			if havePrimary {
				return errPrimaryAlreadyRunning
			}
		} else {
			var err error
			listener, err = ipcjson.ListenWithRemote(
				*listen,
				serverConfig.IPC.AllowRemote,
			)
			if err != nil {
				probeContext, probeCancel := context.WithTimeout(
					context.Background(),
					400*time.Millisecond,
				)
				havePrimary := primaryAvailableAt(probeContext, *listen)
				probeCancel()
				if havePrimary {
					return errPrimaryAlreadyRunning
				}
				return err
			}
			defer listener.Close()
		}
		currentConfig := store.Current()
		client := controllerapi.New(apiOptions(currentConfig, connection))
		if err := client.ConfigureHistory(configuredHistoryOptions(store)); err != nil {
			return err
		}
		bindClientDevicePersistence(client, store)
		defer client.Shutdown()
		ctx, cancel := signalContext()
		defer cancel()
		go store.Watch(
			ctx,
			appconfig.DefaultWatchInterval,
			func(value appconfig.Config) {
				client.ApplyHostOptions(apiOptions(value, connection))
				if historyErr := client.ConfigureHistory(
					configuredHistoryOptions(store),
				); historyErr != nil {
					fmt.Fprintln(stderr, "history configuration rejected:", historyErr)
				}
				fmt.Fprintln(stderr, "configuration reloaded:", store.Path())
			},
			func(err error) {
				fmt.Fprintln(stderr, "configuration reload rejected:", err)
			},
		)
		if !*noConnect {
			connectContext, connectCancel := context.WithTimeout(ctx, 15*time.Second)
			err := client.Connect(connectContext)
			connectCancel()
			if err != nil {
				fmt.Fprintln(stderr, "initial auto-connect:", err)
			}
		}
		service := &ipcjson.Service{
			Client: client, WebSocketPath: *websocketPath,
			SocketIOPath:    serverConfig.IPC.SocketIOPath,
			AuthToken:       serverConfig.IPC.AuthToken,
			AllowedOrigins:  append([]string(nil), serverConfig.IPC.AllowedOrigins...),
			InboundWebhooks: serverConfig.Integrations.InboundWebhooksEnabled,
			Shutdown:        cancel,
			HostConfig:      store.Current,
			UpdateHostConfig: func(change func(*appconfig.Config) error) error {
				_, err := store.Update(change)
				return err
			},
		}
		if *stdio {
			return ipcjson.ServeStreams(ctx, os.Stdin, stdout, service)
		}
		fmt.Fprintf(
			stdout,
			"JSON-RPC IPC listening on %s (WebSocket ws://%s%s)\n",
			listener.Addr(),
			listener.Addr(),
			*websocketPath,
		)
		return ipcjson.Serve(ctx, listener, service)

	case "call":
		flags := flag.NewFlagSet("ipc call", flag.ContinueOnError)
		flags.SetOutput(stderr)
		address := flags.String("addr", store.Current().IPC.Listen, "IPC server address")
		token := flags.String("token", store.Current().IPC.AuthToken, "IPC bearer token")
		method := flags.String("method", "", "JSON-RPC method")
		params := flags.String("params", "{}", "JSON object with method parameters")
		timeout := flags.Duration("timeout", 15*time.Second, "call timeout")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *method == "" && flags.NArg() != 0 {
			*method = flags.Arg(0)
		}
		if *method == "" {
			return errors.New("ipc call requires --method")
		}
		rawParams := json.RawMessage(*params)
		if !json.Valid(rawParams) {
			return errors.New("--params must be valid JSON")
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		response, err := ipcjson.Call(ctx, *address, ipcjson.Request{
			Method: *method,
			Params: rawParams,
			Auth:   *token,
		})
		if err != nil {
			return err
		}
		formatted, err := json.MarshalIndent(response.Result, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(formatted))
		return nil
	default:
		return fmt.Errorf("unknown ipc command %q", args[0])
	}
}

func runReset(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("reset", flag.ContinueOnError)
	flags.SetOutput(stderr)
	connection := addConnectionFlags(flags, store.Current().Connection)
	duration := flags.Duration("pulse", 120*time.Millisecond, "DTR/RTS low pulse duration")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *duration < time.Millisecond {
		return errors.New("pulse duration must be at least 1ms")
	}
	probeContext, probeCancel := context.WithTimeout(
		context.Background(),
		400*time.Millisecond,
	)
	havePrimary := primaryAvailable(probeContext)
	probeCancel()
	if havePrimary {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := callPrimary(
			ctx,
			"controller.reset.lines",
			map[string]int{"pulse_ms": int(duration.Milliseconds())},
			nil,
		); err != nil {
			return err
		}
		fmt.Fprintln(
			stdout,
			"DTR/RTS reset complete over IPC; application HELLO reauthenticated.",
		)
		return nil
	}
	runtime := newRuntime(connection, store)
	bindRuntimeDevicePersistence(runtime, store)
	defer runtime.Close()
	engine := control.NewCommandEngine(runtime, commandOptions(store, findProjectRoot()))
	if err := hostmenu.RegisterCommands(engine, newHostMenuManager(store, runtime, engine)); err != nil {
		return err
	}
	ownerContext, stopOwner := context.WithCancel(context.Background())
	defer stopOwner()
	primary, err := startPrimaryIPC(ownerContext, runtime, engine, store)
	if errors.Is(err, errPrimaryAlreadyRunning) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if routeErr := callPrimary(
			ctx,
			"controller.reset.lines",
			map[string]int{"pulse_ms": int(duration.Milliseconds())},
			nil,
		); routeErr != nil {
			return routeErr
		}
		fmt.Fprintln(
			stdout,
			"DTR/RTS reset complete over IPC; application HELLO reauthenticated.",
		)
		return nil
	}
	if err != nil {
		return err
	}
	defer primary.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(ctx); err != nil {
		return err
	}
	if err := runtime.PulseResetFor(ctx, *duration); err != nil {
		return err
	}
	if err := runtime.Reconnect(ctx, "DTR/RTS reset pulse completed"); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "DTR/RTS reset complete; application HELLO reauthenticated.")
	return nil
}

func runProgram(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	var normalizeErr error
	args, normalizeErr = normalizeProgramCLIArgs(args)
	if normalizeErr != nil {
		return normalizeErr
	}
	config := store.Current()
	flags := flag.NewFlagSet("program", flag.ContinueOnError)
	flags.SetOutput(stderr)
	defaultMethod := config.Programming.Method
	if defaultMethod == "" {
		defaultMethod = "urclock"
	}
	method := flags.String("method", defaultMethod, "compile|arduino|urclock|usbasp|avrdude")
	operation := flags.String(
		"operation",
		string(programmer.OperationWriteFlash),
		"write-flash|read-flash|verify-flash|read-eeprom|write-eeprom|metadata|probe|start|core-info|burn-bootloader|backup",
	)
	device := flags.String(
		"device",
		envOr("PCCONTROLLER_DEVICE", ""),
		"COM ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
	)
	port := flags.String("port", envOr("PCCONTROLLER_PORT", config.Connection.Port), "serial port")
	hexPath := flags.String("hex", config.Paths.FirmwareHex, "Intel HEX file for avrdude workflows")
	sketch := flags.String("sketch", configuredProject(config, findProjectRoot()), "Arduino sketch directory")
	outputDir := flags.String("output-dir", "", "arduino-cli compile output directory")
	outputPath := flags.String("output", "", "output file for flash/EEPROM reads")
	fqbn := flags.String("fqbn", configuredFQBN(config), "Arduino FQBN")
	defaultProgrammer := config.Programming.Programmer
	if defaultProgrammer == "" {
		defaultProgrammer = "usbasp"
	}
	programmerName := flags.String("programmer", defaultProgrammer, "programmer ID (for example usbasp)")
	mcu := flags.String("mcu", "atmega328p", "avrdude MCU")
	baud := flags.Int("baud", 115200, "urclock baud rate")
	arduinoCLI := flags.String("arduino-cli", config.Programming.ArduinoCLI, "arduino-cli executable")
	avrdude := flags.String("avrdude", config.Programming.Avrdude, "avrdude executable")
	avrdudeConf := flags.String("avrdude-conf", config.Programming.AvrdudeConf, "avrdude.conf path")
	noVerify := flags.Bool("no-verify", false, "skip avrdude flash verification")
	allowUSBasp := flags.Bool(
		"usbasp-troubleshooting",
		false,
		"explicitly authorize hidden USBasp backup/flash troubleshooting",
	)
	allowIncompleteBackup := flags.Bool(
		"allow-incomplete-backup",
		false,
		"advanced override: flash even if the automatic full backup fails",
	)
	confirmEEPROM := flags.Bool(
		"confirm-eeprom-write",
		false,
		"explicitly authorize destructive EEPROM write",
	)
	dryRun := flags.Bool("dry-run", false, "print the resolved command without running it")
	appReconnect := flags.Bool(
		"app-reconnect",
		true,
		"authenticate application HELLO after the programmer releases the port",
	)
	if err := flags.Parse(args); err != nil {
		return err
	}
	explicitDevice := false
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "device" || value.Name == "port" {
			explicitDevice = true
		}
	})
	if strings.TrimSpace(*device) != "" {
		*port = *device
		explicitDevice = true
	}
	options := programmer.Options{
		Method:    programmer.Method(strings.ToLower(*method)),
		Operation: programmer.Operation(strings.ToLower(*operation)),
		Port:      *port, HexPath: *hexPath, SketchPath: *sketch,
		OutputDir: *outputDir, OutputPath: *outputPath,
		FQBN: *fqbn, Programmer: *programmerName,
		MCU: *mcu, BaudRate: *baud, ArduinoCLI: *arduinoCLI,
		Avrdude: *avrdude, AvrdudeConf: *avrdudeConf, NoVerify: *noVerify,
		ConfirmEEPROMWrite: *confirmEEPROM,
	}
	safeFlash := options.Operation == programmer.OperationWriteFlash &&
		options.Method != programmer.MethodCompile
	if safeFlash {
		switch options.Method {
		case programmer.MethodUrclock:
		case programmer.MethodUSBasp:
			if !*allowUSBasp {
				return errors.New("USBasp flash is hidden troubleshooting functionality; pass --usbasp-troubleshooting explicitly")
			}
			options.Port = ""
		case programmer.MethodArduino:
			return errors.New("direct arduino upload is disabled; compile to Intel HEX, then use program flash HEX [PORT]")
		default:
			return fmt.Errorf("guarded flash supports Urclock or explicitly authorized USBasp, got %q", options.Method)
		}
	}
	deviceOperation := options.Method != programmer.MethodCompile &&
		options.Operation != programmer.OperationCoreInfo
	if deviceOperation && !*dryRun {
		probeContext, probeCancel := context.WithTimeout(
			context.Background(),
			400*time.Millisecond,
		)
		havePrimary := primaryAvailable(probeContext)
		probeCancel()
		if havePrimary {
			ctx, cancel := signalContext()
			defer cancel()
			if explicitDevice {
				openContext, openCancel := context.WithTimeout(ctx, 15*time.Second)
				_, err := executeThroughPrimary(
					openContext,
					joinControllerCommand([]string{"open", *port}),
				)
				openCancel()
				if err != nil {
					return fmt.Errorf("select primary device: %w", err)
				}
			}
			remoteOptions := options
			remoteOptions.Port = ""
			words := programShellWords(remoteOptions)
			if safeFlash && *allowIncompleteBackup {
				words = append(words, "--allow-incomplete-backup")
			}
			output, err := executeThroughPrimary(
				ctx,
				joinControllerCommand(words),
			)
			if output != "" {
				fmt.Fprintln(stdout, output)
			}
			return err
		}
	}
	if operationNeedsSerialPort(options) {
		resolvedPort, err := resolveProgrammingPort(
			*port,
			config.Connection,
			os.Stdin,
			stderr,
		)
		if err != nil {
			return err
		}
		options.Port = resolvedPort
	}
	if options.Operation == programmer.OperationBackup {
		if err := programmer.ValidateBackup(options); err != nil {
			return err
		}
	}
	if (options.Operation == programmer.OperationBackup || safeFlash) &&
		!*dryRun &&
		options.Port != "" {
		hello, identityErr := readApplicationIdentityBeforeProgramming(
			options.Port,
			config.Connection,
		)
		if identityErr != nil {
			fmt.Fprintln(
				stderr,
				"backup: application identity unavailable; continuing with programmer metadata:",
				identityErr,
			)
		} else {
			options.ApplicationHash = hello.BuildHash
			options.ApplicationDate = hello.BuildDate
			options.ApplicationTime = hello.BuildTime
			options.ApplicationIdentitySchema = hello.IdentitySchema
			options.ApplicationPackedTimestamp = hello.BuildTimestamp
		}
	}
	if options.Method == programmer.MethodCompile {
		var err error
		options, _, err = programmer.PlanCompile(options)
		if err != nil {
			return err
		}
	}
	if safeFlash {
		if *dryRun {
			fmt.Fprintf(
				stdout,
				"dry-run: guarded %s flash %s; require verified flash + EEPROM + metadata backup before write\n",
				options.Method,
				options.HexPath,
			)
			if *allowIncompleteBackup {
				fmt.Fprintln(stdout, "dry-run WARNING: explicit incomplete-backup override enabled")
			}
			return nil
		}
		ctx, cancel := signalContext()
		defer cancel()
		return executeGuardedCLIFlash(
			ctx, options, config.Connection,
			*allowUSBasp, *allowIncompleteBackup, *appReconnect,
			stdout,
		)
	}
	var command programmer.Command
	var err error
	if options.Operation == programmer.OperationBackup {
		fmt.Fprintf(
			stdout,
			"backup flash + EEPROM + metadata under %s\n",
			options.OutputPath,
		)
	} else {
		command, err = programmer.Build(options)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, command.String())
	}
	if *dryRun {
		if options.Method == programmer.MethodUSBasp &&
			options.Operation == programmer.OperationWriteFlash {
			fmt.Fprintln(stdout, "dry-run: execution will first read hfuse and require EESAVE bit 3 = 0")
		}
		return nil
	}
	ctx, cancel := signalContext()
	defer cancel()
	programErr := programmer.Execute(ctx, options, stdout)
	if !*appReconnect || !deviceOperation || options.Port == "" {
		return programErr
	}
	reconnectErr := reconnectApplicationAfterProgramming(
		context.WithoutCancel(ctx),
		options.Port,
		config.Connection,
		stdout,
	)
	return errors.Join(programErr, reconnectErr)
}

func normalizeProgramCLIArgs(args []string) ([]string, error) {
	shortcut := 0
	for shortcut < len(args) && guardedFlashBooleanFlag(args[shortcut]) {
		shortcut++
	}
	if shortcut >= len(args) || !strings.EqualFold(args[shortcut], "flash") {
		return args, nil
	}
	if shortcut+1 >= len(args) {
		return nil, errors.New("usage: controller program flash HEX [PORT] [--usbasp-troubleshooting] [--allow-incomplete-backup]")
	}
	arguments := append([]string(nil), args[:shortcut]...)
	arguments = append(arguments, args[shortcut+1:]...)
	positionals := make([]string, 0, 2)
	flags := make([]string, 0, len(arguments))
	usbasp := false
	for _, argument := range arguments {
		if guardedFlashBooleanFlag(argument) {
			flags = append(flags, argument)
			usbasp = usbasp || strings.EqualFold(argument, "--usbasp-troubleshooting")
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return nil, fmt.Errorf("unknown guarded flash flag %q", argument)
		}
		positionals = append(positionals, argument)
	}
	if len(positionals) < 1 || len(positionals) > 2 {
		return nil, errors.New("usage: controller program flash HEX [PORT] [--usbasp-troubleshooting] [--allow-incomplete-backup]")
	}
	result := []string{
		"--operation", string(programmer.OperationWriteFlash),
		"--method", string(programmer.MethodUrclock),
		"--hex", positionals[0],
	}
	if usbasp {
		result = append(result, "--method", string(programmer.MethodUSBasp))
	}
	result = append(result, flags...)
	if len(positionals) == 2 {
		result = append(result, "--port", positionals[1])
	}
	return result, nil
}

func guardedFlashBooleanFlag(argument string) bool {
	lower := strings.ToLower(argument)
	if lower == "--usbasp-troubleshooting" || lower == "--allow-incomplete-backup" || lower == "--dry-run" {
		return true
	}
	return lower == "--app-reconnect" || strings.HasPrefix(lower, "--app-reconnect=")
}

func executeGuardedCLIFlash(
	ctx context.Context,
	options programmer.Options,
	connection appconfig.Connection,
	allowUSBasp, allowIncompleteBackup, appReconnect bool,
	output io.Writer,
) error {
	paths, err := programmer.DefaultHostDataPaths()
	if err != nil {
		return err
	}
	if err := programmer.EnsureHostDataPaths(paths); err != nil {
		return err
	}
	backup := options
	backup.Operation = programmer.OperationBackup
	backup.HexPath = ""
	backup.OutputPath = ""
	write := options
	write.Operation = programmer.OperationWriteFlash
	result, flashErr := programmer.AutomaticBackupThenFlash(
		ctx,
		programmer.AutomaticPreflashOptions{
			FirmwarePath: options.HexPath,
			Backup:       backup, DataPaths: paths,
			AllowUSBaspTroubleshooting:  allowUSBasp,
			AllowFlashWithoutFullBackup: allowIncompleteBackup,
		},
		programmer.CommandRunnerFunc(programmer.Run),
		func(flashContext context.Context, path string, writer io.Writer) error {
			write.HexPath = path
			return programmer.Execute(flashContext, write, writer)
		},
		output,
	)
	fmt.Fprintf(output, "firmware SHA-256: %s\n", result.FirmwareSHA256)
	if result.BackupReference != "" {
		fmt.Fprintf(output, "verified backup: %s\n", result.BackupReference)
	}
	if result.BackupManifest != "" {
		fmt.Fprintf(output, "backup manifest: %s\n", result.BackupManifest)
	}
	for _, warning := range result.Warnings {
		fmt.Fprintln(output, "WARNING:", warning)
	}
	if result.Flashed {
		fmt.Fprintln(output, "guarded firmware flash completed")
	}
	var reconnectErr error
	if appReconnect && options.Method == programmer.MethodUrclock && options.Port != "" {
		reconnectErr = reconnectApplicationAfterProgramming(
			context.WithoutCancel(ctx), options.Port, connection, output,
		)
	}
	return errors.Join(flashErr, reconnectErr)
}

func readApplicationIdentityBeforeProgramming(
	port string,
	connection appconfig.Connection,
) (native.Hello, error) {
	runtime := control.New(control.Options{
		Filter:         ports.Filter{Port: port},
		BaudRate:       connection.BaudRate,
		StartupWait:    time.Duration(connection.StartupWaitMS) * time.Millisecond,
		RequestTimeout: time.Duration(connection.RequestTimeoutMS) * time.Millisecond,
		HelloAttempts:  connection.HelloAttempts,
	})
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(ctx); err != nil {
		return native.Hello{}, err
	}
	return runtime.Snapshot().Hello, nil
}

func operationNeedsSerialPort(options programmer.Options) bool {
	if options.Method == programmer.MethodUrclock {
		return true
	}
	if options.Method == programmer.MethodArduino {
		return options.Operation == programmer.OperationWriteFlash
	}
	return options.Method == programmer.MethodAvrdude &&
		!strings.EqualFold(options.Programmer, "usbasp")
}

func resolveProgrammingPort(
	selector string,
	config appconfig.Connection,
	input io.Reader,
	output io.Writer,
) (string, error) {
	options := &connectionFlags{
		port: config.Port, vid: config.VID, pid: config.PID, name: config.Name,
		baud:      config.BaudRate,
		overrides: make(map[string]bool),
	}
	if config.LastDevice != nil {
		options.preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	if strings.TrimSpace(selector) != "" {
		options.device = selector
		options.overrides["device"] = true
	}
	if err := selectInteractiveDevice(options, input, output); err != nil {
		return "", err
	}
	filter := options.filter()
	port := filter.Port
	if port == "" {
		list, err := ports.List()
		if err != nil {
			return "", err
		}
		if selected, ok := ports.PreferredCandidate(
			ports.Candidates(list, filter),
			filter.Preferred,
		); ok {
			port = selected.Name
		}
	}
	if port == "" {
		return "", errors.New(
			"no unique serial device matched; use --device COM_ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
		)
	}
	return port, nil
}

func programShellWords(options programmer.Options) []string {
	if options.Operation == programmer.OperationWriteFlash &&
		(options.Method == programmer.MethodUrclock || options.Method == programmer.MethodUSBasp) {
		words := []string{"program", "flash", options.HexPath}
		if options.Port != "" && options.Method == programmer.MethodUrclock {
			words = append(words, options.Port)
		}
		if options.Method == programmer.MethodUSBasp {
			words = append(words, "--usbasp-troubleshooting")
		}
		return words
	}
	words := []string{"program"}
	if options.Operation != "" &&
		options.Operation != programmer.OperationWriteFlash {
		words = append(words, string(options.Operation))
	}
	words = append(words, string(options.Method))
	switch {
	case options.Operation == programmer.OperationBackup:
		words = append(words, options.OutputPath)
	case options.Method == programmer.MethodCompile ||
		options.Method == programmer.MethodArduino &&
			options.Operation == programmer.OperationWriteFlash:
		words = append(words, options.SketchPath)
	case options.Operation == programmer.OperationReadFlash ||
		options.Operation == programmer.OperationReadEEPROM:
		words = append(words, options.OutputPath)
	case options.Operation != programmer.OperationMetadata &&
		options.Operation != programmer.OperationProbe &&
		options.Operation != programmer.OperationStart &&
		options.Operation != programmer.OperationCoreInfo &&
		options.Operation != programmer.OperationBurnBoot:
		words = append(words, options.HexPath)
	}
	if options.Operation == programmer.OperationWriteEEPROM {
		words = append(words, "CONFIRM")
	}
	if options.Port != "" {
		words = append(words, options.Port)
	}
	return words
}

func runBoot(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	translated, err := bootCLIArguments(args)
	if err != nil {
		return err
	}
	return runProgram(translated, stdout, stderr, store)
}

func bootCLIArguments(args []string) ([]string, error) {
	const usage = "usage: controller boot probe|info|metadata|backup DIR|read FILE|write FILE|verify FILE|start [program flags]"
	if len(args) == 0 {
		return nil, errors.New(usage)
	}
	translated := []string{"--method", string(programmer.MethodUrclock)}
	action := strings.ToLower(args[0])
	switch action {
	case "probe":
		translated = append(translated, "--operation", string(programmer.OperationProbe))
		return append(translated, args[1:]...), nil
	case "info", "metadata":
		translated = append(translated, "--operation", string(programmer.OperationMetadata))
		return append(translated, args[1:]...), nil
	case "start":
		translated = append(translated, "--operation", string(programmer.OperationStart))
		return append(translated, args[1:]...), nil
	case "backup":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		translated = append(
			translated,
			"--operation", string(programmer.OperationBackup),
			"--output", args[1],
		)
		return append(translated, args[2:]...), nil
	case "read":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		translated = append(
			translated,
			"--operation", string(programmer.OperationReadFlash),
			"--output", args[1],
		)
		return append(translated, args[2:]...), nil
	case "write", "verify":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		operation := programmer.OperationWriteFlash
		if action == "verify" {
			operation = programmer.OperationVerifyFlash
		}
		translated = append(
			translated,
			"--operation", string(operation),
			"--hex", args[1],
		)
		return append(translated, args[2:]...), nil
	default:
		return nil, errors.New(usage)
	}
}

func runArduino(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	if len(args) != 0 && strings.EqualFold(args[0], "update") {
		return runArduinoUpdate(args[1:], stdout, stderr, store)
	}
	translated, err := arduinoCLIArguments(args)
	if err != nil {
		return err
	}
	return runProgram(translated, stdout, stderr, store)
}

func runArduinoUpdate(
	args []string,
	stdout, stderr io.Writer,
	store *appconfig.Store,
) error {
	flags := flag.NewFlagSet("arduino update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	arduinoCLI := flags.String(
		"arduino-cli",
		store.Current().Programming.ArduinoCLI,
		"arduino-cli executable",
	)
	directRetry := flags.Bool(
		"direct-retry",
		true,
		"retry a failed proxy attempt once without proxy variables",
	)
	dryRun := flags.Bool("dry-run", false, "print every update/install step without executing it")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller arduino update [--arduino-cli PATH] [--direct-retry=false] [--dry-run]")
	}
	ctx, cancel := signalContext()
	defer cancel()
	report, err := programmer.UpdateArduino(ctx, programmer.ArduinoUpdateOptions{
		ArduinoCLI: *arduinoCLI, DirectRetry: *directRetry, DryRun: *dryRun,
	}, stdout)
	if *dryRun {
		fmt.Fprintf(stdout, "\nArduino update plan complete: %d steps; no changes made.\n", len(report.Steps))
	} else {
		succeeded := 0
		for _, step := range report.Steps {
			if step.Succeeded {
				succeeded++
			}
		}
		fmt.Fprintf(stdout, "\nArduino update result: %d/%d steps succeeded.\n", succeeded, len(report.Steps))
	}
	return err
}

func arduinoCLIArguments(args []string) ([]string, error) {
	const usage = "usage: controller arduino update|compile SKETCH|core-info|burn-bootloader [flags]"
	if len(args) == 0 {
		return nil, errors.New(usage)
	}
	switch strings.ToLower(args[0]) {
	case "compile":
		if len(args) < 2 {
			return nil, errors.New(usage)
		}
		result := []string{
			"--method", string(programmer.MethodCompile),
			"--sketch", args[1],
		}
		return append(result, args[2:]...), nil
	case "upload":
		return nil, errors.New(
			"direct arduino upload is disabled; compile to Intel HEX, then use controller program flash HEX [PORT]",
		)
	case "core-info", "info":
		result := []string{
			"--method", string(programmer.MethodArduino),
			"--operation", string(programmer.OperationCoreInfo),
		}
		return append(result, args[1:]...), nil
	case "burn", "burn-bootloader":
		result := []string{
			"--method", string(programmer.MethodArduino),
			"--operation", string(programmer.OperationBurnBoot),
		}
		return append(result, args[1:]...), nil
	default:
		return nil, errors.New(usage)
	}
}

func reconnectApplicationAfterProgramming(
	ctx context.Context,
	port string,
	connection appconfig.Connection,
	output io.Writer,
) error {
	runtime := control.New(control.Options{
		Filter:         ports.Filter{Port: port},
		BaudRate:       connection.BaudRate,
		StartupWait:    time.Duration(connection.StartupWaitMS) * time.Millisecond,
		RequestTimeout: time.Duration(connection.RequestTimeoutMS) * time.Millisecond,
		HelloAttempts:  connection.HelloAttempts,
	})
	defer runtime.Close()
	reconnectContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := runtime.EnsureConnected(reconnectContext); err != nil {
		return fmt.Errorf(
			"programmer completed, but application HELLO reconnect failed: %w",
			err,
		)
	}
	snapshot := runtime.Snapshot()
	fmt.Fprintf(
		output,
		"Application mode restored and authenticated on %s: %s\n",
		snapshot.Port.Name,
		snapshot.Hello.Name,
	)
	return nil
}

func runWS(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	config := store.Current()
	if len(args) == 0 {
		return errors.New("usage: ws serve|client")
	}
	switch strings.ToLower(args[0]) {
	case "serve":
		flags := flag.NewFlagSet("ws serve", flag.ContinueOnError)
		flags.SetOutput(stderr)
		file := flags.String("file", config.Paths.FirmwareHex, "watched Intel HEX path")
		listen := flags.String("listen", "127.0.0.1:3000", "HTTP listen address")
		path := flags.String("path", "/firmware", "WebSocket endpoint path")
		poll := flags.Duration("poll", 500*time.Millisecond, "file polling interval")
		maxSize := flags.Int64("max-size", wsrelay.DefaultMaxSize, "maximum firmware bytes")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if *file == "" && flags.NArg() != 0 {
			*file = flags.Arg(0)
		}
		logger := log.New(stdout, "ws-server: ", log.LstdFlags)
		ctx, cancel := signalContext()
		defer cancel()
		return wsrelay.Serve(ctx, wsrelay.ServerOptions{
			Listen: *listen, Path: *path, FirmwarePath: *file,
			PollInterval: *poll, MaxSize: *maxSize, Logger: logger,
		})

	case "client":
		flags := flag.NewFlagSet("ws client", flag.ContinueOnError)
		flags.SetOutput(stderr)
		url := flags.String("url", envOr("PCCONTROLLER_WS_URL", "ws://127.0.0.1:3000/firmware"), "relay WebSocket URL")
		method := flags.String("method", "urclock", "urclock|usbasp")
		port := flags.String("port", envOr("PCCONTROLLER_PORT", config.Connection.Port), "serial port")
		programmerName := flags.String("programmer", "", "custom avrdude programmer")
		mcu := flags.String("mcu", "atmega328p", "avrdude MCU")
		baud := flags.Int("baud", 115200, "urclock baud rate")
		avrdude := flags.String("avrdude", config.Programming.Avrdude, "avrdude executable")
		avrdudeConf := flags.String("avrdude-conf", config.Programming.AvrdudeConf, "avrdude.conf path")
		allowUSBasp := flags.Bool("usbasp-troubleshooting", false, "explicitly allow hidden USBasp fallback")
		allowIncomplete := flags.Bool("allow-incomplete-backup", false, "explicitly allow flashing without a complete verified backup")
		reconnect := flags.Duration("reconnect", 2*time.Second, "reconnect delay")
		maxSize := flags.Int64("max-size", wsrelay.DefaultMaxSize, "maximum firmware bytes")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		flashMethod := programmer.Method(strings.ToLower(strings.TrimSpace(*method)))
		if flashMethod != programmer.MethodUrclock && flashMethod != programmer.MethodUSBasp {
			return fmt.Errorf("ws client method %q is unsupported; use urclock or explicit USBasp troubleshooting", *method)
		}
		if flashMethod == programmer.MethodUSBasp && !*allowUSBasp {
			return errors.New("USBasp is hidden troubleshooting only; pass --usbasp-troubleshooting explicitly")
		}
		logger := log.New(stdout, "ws-client: ", log.LstdFlags)
		ctx, cancel := signalContext()
		defer cancel()
		return wsrelay.RunClient(ctx, wsrelay.ClientOptions{
			URL: *url, ReconnectDelay: *reconnect, MaxSize: *maxSize,
			Logger: logger,
			OnFirmware: func(ctx context.Context, message wsrelay.FirmwareMessage) error {
				tempPath, cleanup, err := wsrelay.SaveTemp(message)
				if err != nil {
					return err
				}
				defer cleanup()
				probeContext, probeCancel := context.WithTimeout(
					ctx,
					400*time.Millisecond,
				)
				havePrimary := primaryAvailable(probeContext)
				probeCancel()
				if havePrimary {
					words := []string{"program", "flash", tempPath}
					if strings.TrimSpace(*port) != "" {
						words = append(words, *port)
					}
					if *allowUSBasp {
						words = append(words, "--usbasp-troubleshooting")
					}
					if *allowIncomplete {
						words = append(words, "--allow-incomplete-backup")
					}
					output, routeErr := executeThroughPrimary(
						ctx,
						joinControllerCommand(words),
					)
					if output != "" {
						logger.Print(output)
					}
					return routeErr
				}
				flashOptions := programmer.Options{
					Operation: programmer.OperationWriteFlash,
					Method:    flashMethod,
					Port:      *port, HexPath: tempPath, Programmer: *programmerName,
					MCU: *mcu, BaudRate: *baud, Avrdude: *avrdude,
					AvrdudeConf: *avrdudeConf,
				}
				command, err := programmer.Build(flashOptions)
				if err != nil {
					return err
				}
				logger.Print("guarded preflight: ", command.String())
				return executeGuardedCLIFlash(
					ctx, flashOptions, config.Connection,
					*allowUSBasp, *allowIncomplete, true, stdout,
				)
			},
		})
	default:
		return fmt.Errorf("unknown ws command %q", args[0])
	}
}

func addConnectionFlags(
	flags *flag.FlagSet,
	config appconfig.Connection,
) *connectionFlags {
	options := &connectionFlags{overrides: make(map[string]bool)}
	if config.LastDevice != nil {
		options.preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	flags.StringVar(
		&options.device,
		"device",
		envOr("PCCONTROLLER_DEVICE", ""),
		"COM ID, friendly name, VID:PID, serial:VALUE, or instance:VALUE",
	)
	flags.StringVar(&options.port, "port", envOr("PCCONTROLLER_PORT", config.Port), "explicit serial port or tcp://host:port")
	flags.StringVar(&options.vid, "vid", envOr("PCCONTROLLER_VID", config.VID), "USB VID filter")
	flags.StringVar(&options.pid, "pid", envOr("PCCONTROLLER_PID", config.PID), "USB PID filter")
	flags.StringVar(&options.name, "name", envOr("PCCONTROLLER_NAME", config.Name), "port/product/manufacturer substring")
	flags.IntVar(&options.baud, "baud", envInt("PCCONTROLLER_BAUD", config.BaudRate), "UART baud rate")
	flags.DurationVar(
		&options.startupWait,
		"startup-wait",
		time.Duration(config.StartupWaitMS)*time.Millisecond,
		"wait after opening before HELLO",
	)
	flags.DurationVar(
		&options.requestTimeout,
		"request-timeout",
		time.Duration(config.RequestTimeoutMS)*time.Millisecond,
		"timeout for each HELLO attempt",
	)
	flags.IntVar(
		&options.helloAttempts,
		"hello-attempts",
		config.HelloAttempts,
		"HELLO attempts while Urboot starts",
	)
	flags.BoolVar(
		&options.resetOnReconnect,
		"reset-on-reconnect",
		config.ResetOnReconnect,
		"pulse DTR once when a disconnected USB board reappears",
	)
	return options
}

func (options *connectionFlags) captureOverrides(flags *flag.FlagSet) {
	flags.Visit(func(value *flag.Flag) {
		options.overrides[value.Name] = true
	})
}

func (options connectionFlags) filter() ports.Filter {
	filter := ports.Filter{
		Port: options.port, VID: options.vid, PID: options.pid, Name: options.name,
		Preferred: options.preferred,
	}
	if options.device != "" {
		selector, _ := ports.ParseSelector(options.device)
		selector.Preferred = options.preferred
		filter = selector
	}
	if options.overrides["device"] || options.overrides["port"] ||
		options.overrides["vid"] || options.overrides["pid"] ||
		options.overrides["name"] {
		filter.Preferred = ports.Identity{}
	}
	return filter
}

func runtimeOptions(options *connectionFlags) control.Options {
	return control.Options{
		Filter:           options.filter(),
		BaudRate:         options.baud,
		StartupWait:      options.startupWait,
		RequestTimeout:   options.requestTimeout,
		HelloAttempts:    options.helloAttempts,
		ResetOnReconnect: options.resetOnReconnect,
	}
}

func newRuntime(
	connection *connectionFlags,
	store *appconfig.Store,
) *control.Runtime {
	runtime := control.New(runtimeOptions(connection))
	if err := configureRuntimeHistory(runtime, store); err != nil {
		runtime.PublishHostEvent("error", "history configuration: "+err.Error())
	}
	if err := configureRuntimeLCD(runtime, store.Current().UI); err != nil {
		runtime.PublishHostEvent("error", "LCD presentation configuration: "+err.Error())
	}
	return runtime
}

func configureRuntimeLCD(runtime *control.Runtime, ui appconfig.UI) error {
	return runtime.LCDPresenter().Configure(control.LCDPresentationOptions{
		Enabled:      ui.LCDServiceEnabled,
		Debounce:     time.Duration(ui.LCDPromptDebounceMS) * time.Millisecond,
		PriorityHold: time.Duration(ui.LCDPriorityHoldMS) * time.Millisecond,
	})
}

func configureRuntimeHistory(
	runtime *control.Runtime,
	store *appconfig.Store,
) error {
	return runtime.ConfigureHistory(configuredHistoryOptions(store))
}

func configuredHistoryOptions(store *appconfig.Store) control.HistoryOptions {
	config := store.Current()
	path := strings.TrimSpace(config.Paths.HistoryFile)
	if path == "" {
		path = filepath.Join(filepath.Dir(store.Path()), "timeline.jsonl")
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(store.Path()), path)
	}
	return control.HistoryOptions{
		Retention:      time.Duration(config.UI.HistoryHours) * time.Hour,
		SampleInterval: time.Duration(config.UI.HistorySampleMS) * time.Millisecond,
		TimelineLimit:  config.UI.EventLogLimit,
		TimelinePath:   path,
	}
}

func (options *connectionFlags) fromConfig(
	config appconfig.Connection,
) control.Options {
	port, vid, pid, name := config.Port, config.VID, config.PID, config.Name
	baud := config.BaudRate
	startupWait := time.Duration(config.StartupWaitMS) * time.Millisecond
	requestTimeout := time.Duration(config.RequestTimeoutMS) * time.Millisecond
	helloAttempts := config.HelloAttempts
	resetOnReconnect := config.ResetOnReconnect
	if options.overrides["port"] {
		port = options.port
	}
	if options.overrides["vid"] {
		vid = options.vid
	}
	if options.overrides["pid"] {
		pid = options.pid
	}
	if options.overrides["name"] {
		name = options.name
	}
	filter := ports.Filter{Port: port, VID: vid, PID: pid, Name: name}
	if config.LastDevice != nil {
		filter.Preferred = ports.Identity{
			Port: config.LastDevice.Port,
			VID:  config.LastDevice.VID, PID: config.LastDevice.PID,
			SerialNumber: config.LastDevice.SerialNumber,
			Name:         config.LastDevice.Name,
			InstanceID:   config.LastDevice.InstanceID,
		}
	}
	if options.overrides["device"] && options.device != "" {
		selector, _ := ports.ParseSelector(options.device)
		filter = selector
	}
	if options.overrides["device"] || options.overrides["port"] ||
		options.overrides["vid"] || options.overrides["pid"] ||
		options.overrides["name"] {
		filter.Preferred = ports.Identity{}
	}
	if options.overrides["baud"] {
		baud = options.baud
	}
	if options.overrides["startup-wait"] {
		startupWait = options.startupWait
	}
	if options.overrides["request-timeout"] {
		requestTimeout = options.requestTimeout
	}
	if options.overrides["hello-attempts"] {
		helloAttempts = options.helloAttempts
	}
	if options.overrides["reset-on-reconnect"] {
		resetOnReconnect = options.resetOnReconnect
	}
	return control.Options{
		Filter:   filter,
		BaudRate: baud, StartupWait: startupWait,
		RequestTimeout: requestTimeout, HelloAttempts: helloAttempts,
		ResetOnReconnect: resetOnReconnect,
	}
}

func commandOptions(store *appconfig.Store, fallbackProject string) control.CommandOptions {
	config := store.Current()
	options := control.CommandOptions{
		ProjectPath: configuredProject(config, fallbackProject),
		FQBN:        configuredFQBN(config),
		ArduinoCLI:  config.Programming.ArduinoCLI,
		Avrdude:     config.Programming.Avrdude,
		AvrdudeConf: config.Programming.AvrdudeConf,
		Programmer:  configuredProgrammer(config),
		HostConfig:  store.Current,
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			_, err := store.Update(change)
			return err
		},
		Macros: func() []appconfig.Macro {
			return store.Current().Macros
		},
	}
	options.Resolve = func() control.CommandOptions {
		current := store.Current()
		return control.CommandOptions{
			ProjectPath:      configuredProject(current, fallbackProject),
			FQBN:             configuredFQBN(current),
			ArduinoCLI:       current.Programming.ArduinoCLI,
			Avrdude:          current.Programming.Avrdude,
			AvrdudeConf:      current.Programming.AvrdudeConf,
			Programmer:       configuredProgrammer(current),
			HostConfig:       store.Current,
			UpdateHostConfig: options.UpdateHostConfig,
			Macros:           options.Macros,
		}
	}
	return options
}

func configuredProgrammer(config appconfig.Config) string {
	if config.Programming.Programmer != "" {
		return config.Programming.Programmer
	}
	return "usbasp"
}

func configuredProject(config appconfig.Config, fallback string) string {
	if config.Paths.Project != "" {
		return config.Paths.Project
	}
	return fallback
}

func configuredFQBN(config appconfig.Config) string {
	if config.Programming.FQBN != "" {
		return config.Programming.FQBN
	}
	return programmer.DefaultFQBN
}

func apiMacros(source []appconfig.Macro) []controllerapi.Macro {
	result := make([]controllerapi.Macro, len(source))
	for index, macro := range source {
		result[index] = controllerapi.Macro{
			ID: macro.ID, Name: macro.Name, Category: macro.Category,
			Color: macro.Color, Label: macro.Label, LCDMessage: macro.LCDMessage,
			TimingToleranceUS:   macro.TimingToleranceUS,
			KeepOutputsOnCancel: macro.KeepOutputsOnCancel,
			Steps:               make([]controllerapi.MacroStep, len(macro.Steps)),
		}
		for stepIndex, step := range macro.Steps {
			result[index].Steps[stepIndex] = controllerapi.MacroStep{
				AtUS: step.AtUS, AtMS: step.AtMS, Kind: step.Kind,
				Target: step.Target, Value: step.Value,
				DurationMS: step.DurationMS, FrequencyHz: step.FrequencyHz,
				Text: step.Text, Destination: step.Destination,
				Code: step.Code, Bits: step.Bits, Protocol: step.Protocol,
				PulseUS: step.PulseUS, Red: step.Red, Green: step.Green,
				Blue: step.Blue, Brightness: step.Brightness,
				Opcode: step.Opcode, PayloadHex: step.PayloadHex,
			}
		}
	}
	return result
}

func apiAutomations(source []appconfig.Automation) []controllerapi.Automation {
	result := make([]controllerapi.Automation, len(source))
	for index, automation := range source {
		result[index] = controllerapi.Automation{
			Name: automation.Name, Enabled: automation.Enabled,
			CooldownMS: automation.CooldownMS,
			Match: controllerapi.AutomationMatch{
				Kind: automation.Match.Kind, Lifecycle: automation.Match.Lifecycle,
				State: automation.Match.State, Contains: automation.Match.Contains,
				Key: automation.Match.Key, Gesture: automation.Match.Gesture,
				Source: automation.Match.Source, RFID: automation.Match.RFID,
				RFCode:     automation.Match.RFCode,
				RFProtocol: automation.Match.RFProtocol,
			},
			Actions: make([]controllerapi.AutomationAction, len(automation.Actions)),
		}
		for actionIndex, action := range automation.Actions {
			result[index].Actions[actionIndex] = controllerapi.AutomationAction{
				Type: action.Type, Command: action.Command, Macro: action.Macro,
				Executable: action.Executable,
				Args:       append([]string(nil), action.Args...),
				Script:     action.Script,
				Event:      action.Event,
				VirtualKey: action.VirtualKey,
				HoldMS:     action.HoldMS,
				Power:      action.Power,
				Confirm:    action.Confirm,
			}
			if action.RF != nil {
				result[index].Actions[actionIndex].RF = &controllerapi.RFTransmit{
					Code: action.RF.Code, Bits: action.RF.Bits,
					Protocol: action.RF.Protocol, PulseUS: action.RF.PulseUS,
					Repeats: action.RF.Repeats,
				}
			}
		}
	}
	return result
}

func apiOptions(
	config appconfig.Config,
	connection *connectionFlags,
) controllerapi.Options {
	resolved := connection.fromConfig(config.Connection)
	return controllerapi.Options{
		Port: resolved.Filter.Port, VID: resolved.Filter.VID,
		PID: resolved.Filter.PID, Name: resolved.Filter.Name,
		BaudRate: resolved.BaudRate, StartupWait: resolved.StartupWait,
		RequestTimeout:   resolved.RequestTimeout,
		HelloAttempts:    resolved.HelloAttempts,
		ResetOnReconnect: resolved.ResetOnReconnect,
		PreferredDevice:  publicPreferredDevice(config.Connection.LastDevice),
		ProjectPath:      configuredProject(config, findProjectRoot()),
		FQBN:             configuredFQBN(config), Macros: apiMacros(config.Macros),
		Melodies:         config.Melodies,
		StatusEffects:    config.StatusEffects,
		ArduinoCLI:       config.Programming.ArduinoCLI,
		Avrdude:          config.Programming.Avrdude,
		AvrdudeConf:      config.Programming.AvrdudeConf,
		Programmer:       configuredProgrammer(config),
		MotionDoorPolicy: config.Safety.MotionDoorPolicy,
		RF:               config.RF,
		OSActions:        config.OSActions,
		LCDPresentation: controllerapi.LCDPresentationOptions{
			Enabled:      config.UI.LCDServiceEnabled,
			Debounce:     time.Duration(config.UI.LCDPromptDebounceMS) * time.Millisecond,
			PriorityHold: time.Duration(config.UI.LCDPriorityHoldMS) * time.Millisecond,
		},
		Scripts:     config.Scripts,
		Automations: apiAutomations(config.Automations),
	}
}

func publicPreferredDevice(
	identity *appconfig.DeviceIdentity,
) *controllerapi.PortInfo {
	if identity == nil {
		return nil
	}
	return &controllerapi.PortInfo{
		Name: identity.Port, VID: identity.VID, PID: identity.PID,
		SerialNumber: identity.SerialNumber,
		FriendlyName: identity.Name,
		InstanceID:   identity.InstanceID,
	}
}

func watchConfiguration(
	ctx context.Context,
	store *appconfig.Store,
	runtime *control.Runtime,
	connection *connectionFlags,
) {
	store.Watch(
		ctx,
		appconfig.DefaultWatchInterval,
		func(value appconfig.Config) {
			runtime.ApplyOptions(connection.fromConfig(value.Connection))
			if err := configureRuntimeLCD(runtime, value.UI); err != nil {
				runtime.PublishHostEvent(
					"error",
					"LCD presentation configuration rejected: "+err.Error(),
				)
			}
			if err := configureRuntimeHistory(runtime, store); err != nil {
				runtime.PublishHostEvent(
					"error",
					"history configuration rejected: "+err.Error(),
				)
			}
			runtime.PublishHostEvent(
				"config",
				"reloaded "+store.Path()+" (PC-side settings only)",
			)
		},
		func(err error) {
			runtime.PublishHostEvent(
				"error",
				"configuration reload rejected; retaining last good values: "+err.Error(),
			)
		},
	)
}

func bindRuntimeDevicePersistence(
	runtime *control.Runtime,
	store *appconfig.Store,
) {
	runtime.SetDeviceObserver(func(info ports.Info, _ native.Hello) {
		_, err := store.RememberDevice(appconfig.DeviceIdentity{
			Port: info.Name, VID: info.VID, PID: info.PID,
			SerialNumber: info.SerialNumber,
			Name:         firstDeviceName(info.FriendlyName, info.Product),
			InstanceID:   info.InstanceID,
			LastSeen:     time.Now(),
		})
		if err != nil {
			runtime.PublishHostEvent(
				"error",
				"persist last successful device: "+err.Error(),
			)
		}
	})
}

func bindClientDevicePersistence(
	client *controllerapi.Client,
	store *appconfig.Store,
) {
	client.SetDeviceObserver(func(info controllerapi.PortInfo, _ controllerapi.Hello) {
		_, _ = store.RememberDevice(appconfig.DeviceIdentity{
			Port: info.Name, VID: info.VID, PID: info.PID,
			SerialNumber: info.SerialNumber,
			Name:         firstDeviceName(info.FriendlyName, info.Product),
			InstanceID:   info.InstanceID,
			LastSeen:     time.Now(),
		})
	})
}

func firstDeviceName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractConfigArgument(args []string) ([]string, string, error) {
	result := make([]string, 0, len(args))
	var path string
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--config" {
			if index+1 >= len(args) {
				return nil, "", errors.New("--config requires a JSON file path")
			}
			path = args[index+1]
			index++
			continue
		}
		if strings.HasPrefix(argument, "--config=") {
			path = strings.TrimPrefix(argument, "--config=")
			if path == "" {
				return nil, "", errors.New("--config requires a JSON file path")
			}
			continue
		}
		result = append(result, argument)
	}
	return result, path, nil
}

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

func printUsage(output io.Writer) {
	const usage = `◆ PCController Tool

Interactive control:
  controller                         launch the Charm TUI
  controller tui [connection flags]
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
  controller program flash HEX [PORT] [--usbasp-troubleshooting] [--allow-incomplete-backup]
  controller program [non-write diagnostic flags]
  controller boot probe|info|metadata|backup|read|write|verify|start [flags]
  controller arduino update|compile|core-info|burn-bootloader [flags]

Host configuration and integration:
  controller config [path|show|validate]
  controller desktop [install|ensure]
  controller uri pccontroller://ACTION
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
and finally authenticate application HELLO. Direct Arduino upload is disabled.
Device auto-detection always requires a valid PCController HELLO identity.`
	fmt.Fprintln(output, decorateUsage(usage, usageANSIEnabled(output)))
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
