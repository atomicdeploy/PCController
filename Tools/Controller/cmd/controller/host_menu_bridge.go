package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func newHostMenuManager(
	store *appconfig.Store,
	runtime *control.Runtime,
	engine *shell.Engine,
) *hostmenu.Manager {
	macroMenus := newHostMacroMenuActions(store, runtime, engine)
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
		case "host.date":
			return time.Now().Format("2006-01-02"), nil
		case "host.time":
			return time.Now().Format("15:04:05"), nil
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
			if output, handled, err := macroMenus.Read(action); handled {
				return output, err
			}
			if line, ok := hostMenuShellAction(action); ok {
				return engine.Execute(ctx, line)
			}
			return "", fmt.Errorf("unknown host-menu read action %q", action)
		}
	}
	write := func(ctx context.Context, action, value string) (string, error) {
		rawAction := strings.TrimSpace(action)
		action = strings.ToLower(rawAction)
		if output, handled, err := macroMenus.Write(action, value); handled {
			return output, err
		}
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
		if output, handled, err := macroMenus.Execute(ctx, action); handled {
			return output, err
		}
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
	manager := hostmenu.New(store.Current().HostMenus, hostmenu.Callbacks{
		Read: read, Write: write, Execute: execute, SaveConfig: saveConfig,
		Interaction: func(event hostmenu.InteractionEvent) {
			publishHostMenuInteraction(runtime, event)
		},
	})
	if err := macroMenus.Sync(manager, store.Current().Macros); err != nil {
		runtime.PublishHostEvent("menu.options.error", err.Error())
	}
	return manager
}

func updateHostMenuManager(
	manager *hostmenu.Manager,
	config appconfig.Config,
	runtime *control.Runtime,
) {
	manager.UpdateConfig(config.HostMenus)
	if err := manager.UpdateSelectOptions(macroHostMenuOptionsSource, macroHostMenuOptions(config.Macros)); err != nil {
		runtime.PublishHostEvent("menu.options.error", err.Error())
	}
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
	animation *panelFallbackAnimator
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

func (bridge *hostFrontPanelBridge) animator() *panelFallbackAnimator {
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.animation == nil {
		bridge.animation = newPanelFallbackAnimator(func(snapshot hostmenu.Snapshot) error {
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
		presenter := bridge.runtime.LCDPresenter()
		err := presenter.RenderPhysical(
			ctx, snapshot.Panel.LCDLine1, snapshot.Panel.LCDLine2,
		)
		// The front-panel capture and TM1637 remain useful when no LCD
		// backpack is connected; publish only physical-LCD state changes.
		presenter.ReportPhysicalError("host-menu LCD", err)
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

// syncFallbackHostMenuOverlay is the single routing rule for watched menu
// definitions on firmware with front-panel capture but no overlay directory.
// Active edits refresh both display surfaces
// through Push, unrelated edits emit their normalized event only, and removal
// or hiding of the active page releases front-panel capture immediately.
func syncFallbackHostMenuOverlay(manager *hostmenu.Manager, fallback hostMenuPanelBridge, change *hostmenu.DefinitionChange) error {
	if fallback == nil {
		return nil
	}
	snapshot := manager.Snapshot()
	if change != nil && change.Active {
		// UpdateConfig already produced the exact post-edit definition preview.
		// Re-reading Manager.Snapshot here would render the selected item instead
		// and could leave the TM1637/LCD showing stale per-menu presentation.
		snapshot = change.Snapshot
	}
	if !snapshot.Active {
		return fallback.Release()
	}
	if change == nil || change.Active {
		return fallback.Push(snapshot)
	}
	return nil
}

func syncHostMenuOverlay(runtime *control.Runtime, manager *hostmenu.Manager, fallback hostMenuPanelBridge, change *hostmenu.DefinitionChange) {
	live := runtime.Snapshot()
	if !live.Connected {
		return
	}
	if !native.SupportsHostMenuOverlay(live.Hello) {
		if live.Hello.Capabilities&native.CapabilityHostFrontPanel != 0 {
			if err := syncFallbackHostMenuOverlay(manager, fallback, change); err != nil {
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
