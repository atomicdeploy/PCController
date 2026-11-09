//go:build windows && !no_native_gui

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/nativeshell"
	"pccontroller.local/controller/internal/productidentity"
	"pccontroller.local/controller/internal/tui"
	"pccontroller.local/desktop/desktop"
)

func runNativeGUI(args []string, stdout, stderr io.Writer, store *appconfig.Store) error {
	flags := flag.NewFlagSet("gui", flag.ContinueOnError)
	flags.SetOutput(stderr)
	address := flags.String("ipc-addr", "", "attach to an existing controller host; defaults to the configured local primary")
	tokenRef := flags.String("ipc-token-ref", "", "OS-vault/environment reference for a remote IPC token")
	noAuto := flags.Bool("no-auto", false, "do not automatically connect a newly started local core to hardware")
	preview := flags.Bool("preview", false, "render an explicitly offline UI preview without starting a core or opening any transport")
	previewPage := flags.String("preview-page", "", "offline capture page: outputs, preferences, menu, motion or a native activity key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("usage: controller gui [--ipc-addr HOST:PORT] [--ipc-token-ref REFERENCE] [--no-auto]")
	}
	if *previewPage != "" {
		if !*preview {
			return errors.New("--preview-page requires --preview")
		}
		switch *previewPage {
		case "sequence", "effect-library", "peripherals", "folders", "lcd", "melodies":
		case "outputs", "preferences", "menu", "motion", "rf", "programs", "buzzer", "strip", "display", "settings", "connection", "firmware", "events", "board", "menus", "terminal", "diagnostics", "host", "updates", "safety", "i2c", "network", "history", "host-tools", "local-device", "firmware-tools", "status-lighting", "integration-settings", "discovery-settings", "udp-settings", "os-policy", "pwm-settings", "data", "board-automations", "rf-bindings", "releases", "profiles", "status-policy", "peers", "hotkeys", "host-automations", "rf-transmit":
		default:
			return errors.New("unknown preview page")
		}
	}
	ctx, cancel := signalContext()
	defer cancel()
	if *preview {
		finishStartupPresentation(stdout)
		updates := make(chan desktop.Snapshot, 1)
		sample := desktop.Snapshot{Preview: true, HavePanel: true, Segments: [4]byte{0x3f, 0x06, 0x5b, 0x4f}, Brightness: 7, Endpoint: "Offline preview · no hardware connection"}
		for i := 0; i < 6; i++ {
			kind, name, key := "relay", fmt.Sprintf("Relay %d", i+5), fmt.Sprintf("relay.%d", i+5)
			if i >= 2 {
				kind = "mosfet"
				name = fmt.Sprintf("MOSFET %d", i-1)
				key = fmt.Sprintf("pwm.%d", i-2)
			}
			state := "off"
			if i%2 == 0 {
				state = "on"
			}
			ch := desktop.Channel{Key: key, Name: name, Kind: kind, Known: true, State: state, Percent: float64(i * 20), Order: i}
			if state == "off" {
				ch.Percent = 0
			}
			ch.Actions = []desktop.Action{{ID: key + ".on", Label: "On", Verb: "on"}, {ID: key + ".off", Label: "Off", Verb: "off"}}
			if kind == "mosfet" {
				ch.Actions = append(ch.Actions, desktop.Action{ID: key + ".decrease", Verb: "decrease"}, desktop.Action{ID: key + ".increase", Verb: "increase"})
			}
			sample.Channels = append(sample.Channels, ch)
		}
		sample.Seats = []desktop.Seat{{Key: "seat.a", Name: "Seat A", Side: 1, State: "stop", Known: true}, {Key: "seat.b", Name: "Seat B", Side: 2, State: "stop", Known: true}}
		updates <- sample
		pages := nativePages{preview: true}
		return desktop.Run(ctx, desktop.Options{Title: productidentity.Title(store.Current().UI.AppTitle) + " — Preview", PreviewPage: *previewPage, ConfigPath: filepath.Join(filepath.Dir(store.Path()), "native-preview.json"), Updates: updates, LoadPage: pages.load, SubmitPage: pages.submit, Log: func(format string, args ...any) { fmt.Fprintf(stderr, format+"\n", args...) }, Execute: func(context.Context, string) error { return errors.New("preview cannot control hardware") }})
	}
	configured := currentPrimaryEndpoint()
	auth := configured.AuthToken
	if strings.TrimSpace(*address) != "" && *address != configured.Listen {
		auth = ""
	}
	if *tokenRef != "" {
		var err error
		auth, err = store.ResolveSecret(*tokenRef)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(*address) == "" {
		ready := make(chan struct{}, 1)
		completed := make(chan error, 1)
		webArgs := []string{"--no-open", "--no-tray", "--no-tui"}
		if *noAuto {
			webArgs = append(webArgs, "--no-auto")
		}
		go func() {
			completed <- runWebWithContext(ctx, func() {
				select {
				case ready <- struct{}{}:
				default:
				}
			}, webArgs, stdout, stderr, store, hostui.AppAction{})
		}()
		select {
		case <-ready:
			coreStopped := make(chan struct{})
			go func() {
				defer close(coreStopped)
				monitorNativeCore(completed, cancel, stderr)
			}()
			defer func() { cancel(); <-coreStopped }()
		case err := <-completed:
			if err != nil {
				return err
			} // An existing primary was found; attach only.
		case <-ctx.Done():
			return ctx.Err()
		}
		configured = currentPrimaryEndpoint()
		*address = configured.Listen
		auth = configured.AuthToken
	}
	finishStartupPresentation(stdout)
	client := newRemoteTUIIPC(ctx, *address, auth)
	eventHistory := &nativeEventBuffer{}
	pageChanges := make(chan struct{}, 1)
	defer func() { cancel(); client.Close() }()
	updates := make(chan desktop.Snapshot, 128)
	messages := make(chan desktop.Message, 32)
	readbacks := make(chan control.Snapshot, 1)
	type channelCatalog struct {
		Preferences    *desktop.Preferences
		HostInstanceID string                        `json:"host_instance_id"`
		Generation     uint64                        `json:"connection_generation"`
		PWMRevision    string                        `json:"pwm_configuration_revision"`
		Controls       []appconfig.ControlDescriptor `json:"controls"`
		BoardProfile   struct {
			Identity   string `json:"board_identity"`
			Revision   string `json:"revision"`
			Attached   bool   `json:"attached"`
			Configured bool   `json:"configured"`
		} `json:"board_profile"`
	}
	catalogUpdates := make(chan channelCatalog, 1)
	refreshCatalog := make(chan struct{}, 1)
	go func() {
		var lastPreferences *desktop.Preferences
		for {
			var catalog channelCatalog
			catalog.Preferences = lastPreferences
			readCtx, done := context.WithTimeout(ctx, 5*time.Second)
			err := client.call(readCtx, "controller.peripherals.get", map[string]any{}, &catalog)
			if err == nil {
				var preferences nativeHostPreferences
				if preferenceErr := client.call(readCtx, "controller.ui.preferences.get", map[string]any{}, &preferences); preferenceErr == nil {
					catalog.Preferences = desktopPreferences(preferences)
					lastPreferences = catalog.Preferences
				}
			}
			done()
			if err == nil {
				select {
				case catalogUpdates <- catalog:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-refreshCatalog:
			case <-ctx.Done():
				return
			}
		}
	}()
	edges := make(chan controllerapi.Event, 128)
	client.observeState = func(event controllerapi.Event) {
		select {
		case edges <- event:
		case <-ctx.Done():
		}
	}
	initialCtx, initialCancel := context.WithTimeout(ctx, 5*time.Second)
	initial, err := client.Snapshot(initialCtx)
	initialCancel()
	if err != nil {
		return fmt.Errorf("connect native desktop to controller: %w", err)
	}
	updates <- desktopSnapshot(initial, *address, productidentity.Title(store.Current().UI.AppTitle))
	client.StartLive()
	go func() {
		state := initial
		var catalog channelCatalog
		publish := func() {
			snapshot := desktopSnapshot(state, *address, productidentity.Title(store.Current().UI.AppTitle))
			_, liveOwner := client.liveSourceMarker()
			if catalog.Generation == state.ConnectionGeneration && (liveOwner == "" || catalog.HostInstanceID == liveOwner) {
				snapshot.Channels = desktopChannels(state, catalog.Controls)
				snapshot.Seats = desktopSeats(state, catalog.Controls)
				snapshot.ProfileAttached, snapshot.ProfileConfigured = catalog.BoardProfile.Attached, catalog.BoardProfile.Configured
				snapshot.CatalogRevision = catalog.BoardProfile.Revision
				snapshot.ControlOwner = desktop.ControlOwner{HostInstanceID: catalog.HostInstanceID, Generation: catalog.Generation, BoardIdentity: catalog.BoardProfile.Identity, Revision: catalog.BoardProfile.Revision, PWMRevision: catalog.PWMRevision}
			}
			snapshot.Preferences = catalog.Preferences
			select {
			case updates <- snapshot:
			case <-ctx.Done():
			}
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-client.events:
				if !ok {
					return
				}
				eventHistory.append(event)
				if nativeDesktopReceivesMessage(event) {
					select {
					case messages <- desktop.Message{Text: event.Text, Source: event.Source}:
					case <-ctx.Done():
						return
					}
				}
				select {
				case pageChanges <- struct{}{}:
				default:
				}
				if event.Kind == "peripherals.changed" || event.Kind == "config" || strings.HasPrefix(event.Kind, "config.") || strings.HasPrefix(event.Kind, "ui.") {
					select {
					case refreshCatalog <- struct{}{}:
					default:
					}
				}
			case next := <-catalogUpdates:
				catalog = next
				publish()
			case next := <-readbacks:
				if next.ConnectionGeneration == state.ConnectionGeneration {
					state = mergeDesktopSnapshot(state, next)
					publish()
				}
			case event := <-edges:
				if event.Time.Before(state.FrontPanelUpdated) {
					continue
				}
				if event.Kind == "front_panel.segment" {
					segment, err := native.ParseSegmentState(event.Payload)
					if err != nil {
						continue
					}
					state.FrontPanel.RawSegments = segment.RawSegments
					state.FrontPanel.Brightness = segment.Brightness
					state.HaveFrontPanelSegments = true
					state.FrontPanelUpdated = event.Time
				} else if event.Kind == "front_panel.changed" {
					panel, err := native.ParseFrontPanel(event.Payload)
					if err != nil {
						continue
					}
					state.FrontPanel = panel
					state.HaveFrontPanel = true
					state.HaveFrontPanelSegments = true
					state.FrontPanelUpdated = event.Time
				} else {
					continue
				}
				publish()
			case update, ok := <-client.live:
				if !ok {
					return
				}
				if update.Snapshot != nil {
					next := *update.Snapshot
					if next.ConnectionGeneration != state.ConnectionGeneration {
						select {
						case refreshCatalog <- struct{}{}:
						default:
						}
					}
					if next.ConnectionGeneration == state.ConnectionGeneration && next.FrontPanelUpdated.Before(state.FrontPanelUpdated) {
						next.FrontPanel = state.FrontPanel
						next.FrontPanelUpdated = state.FrontPanelUpdated
						next.HaveFrontPanelSegments = state.HaveFrontPanelSegments
					}
					state = mergeDesktopSnapshot(state, next)
				}
				if update.HaveStatus {
					state.Status = update.Status
					state.HaveStatus = true
				}
				if update.PWMValues != nil && update.ConnectionGeneration == state.ConnectionGeneration && update.PWMRevision >= state.PWMRevision {
					state.PWMValues = update.PWMValues
					state.PWMRevision = update.PWMRevision
					state.PWMUpdated = update.PWMUpdated
				}
				if update.ConnectionChange {
					state = desktopTransportState(state, update)
					select {
					case refreshCatalog <- struct{}{}:
					default:
					}
				}
				publish()
			}
		}
	}()
	console := newNativeConsole(store.Path(), *address, auth)
	pages := nativePages{client: client, events: eventHistory, rfCache: &nativeRFCache{}}
	notifiers := map[string]hostui.Notifier{}
	for _, delivery := range []string{"auto", "toast", "balloon"} {
		notifiers[delivery] = hostui.NewNotifier(hostui.NotifierOptions{Delivery: delivery})
	}
	return desktop.Run(ctx, desktop.Options{
		MotionHold: func(ctx context.Context, phase string, lease desktop.MotionHoldLease, direction string) (desktop.MotionHoldLease, error) {
			if phase != "start" && phase != "renew" && phase != "release" {
				return desktop.MotionHoldLease{}, errors.New("invalid motion hold phase")
			}
			var params any = lease
			if phase == "start" {
				params = map[string]any{"id": lease.ID, "expires_at": lease.ExpiresAt, "side": lease.Side, "generation": lease.Generation, "direction": direction}
			}
			var result desktop.MotionHoldLease
			var target any = &result
			if phase == "release" {
				target = &map[string]bool{}
			}
			err := client.call(ctx, "controller.motion.hold."+phase, params, target)
			return result, err
		},
		Notify: func(ctx context.Context, title, message, delivery string) error {
			if delivery == "off" {
				return nil
			}
			notifier := notifiers[delivery]
			if notifier == nil {
				return errors.New("unknown desktop notification delivery")
			}
			// The desktop owns custom WAV cues. Do not also play an OS sound.
			return notifier.Notify(ctx, hostui.Notification{Title: title, Body: message, Silent: true})
		},
		Title:      productidentity.Title(store.Current().UI.AppTitle),
		ConfigPath: filepath.Join(filepath.Dir(store.Path()), "native-desktop.json"), Updates: updates, Messages: messages,
		Log:     func(format string, args ...any) { fmt.Fprintf(stderr, format+"\n", args...) },
		Execute: func(ctx context.Context, command string) error { _, err := client.Execute(ctx, command); return err },
		SetPWM: func(actionCtx context.Context, owner desktop.ControlOwner, key string, percent float64) error {
			if owner.HostInstanceID == "" || owner.Generation == 0 || owner.BoardIdentity == "" || owner.Revision == "" || owner.PWMRevision == "" {
				return errors.New("refresh the current board controls before changing PWM")
			}
			if !strings.HasPrefix(key, "pwm.") {
				return errors.New("not a PWM channel")
			}
			channel, err := strconv.Atoi(strings.TrimPrefix(key, "pwm."))
			if err != nil {
				return err
			}
			var reply any
			params := struct {
				desktop.ControlOwner
				Channel int     `json:"channel"`
				Percent float64 `json:"percent"`
			}{owner, channel, percent}
			if err = client.call(actionCtx, "controller.pwm.set", params, &reply); err != nil {
				return err
			}
			state, err := client.Snapshot(actionCtx)
			if err != nil {
				return fmt.Errorf("PWM acknowledged; readback failed: %w", err)
			}
			select {
			case readbacks <- state:
				return nil
			case <-actionCtx.Done():
				return actionCtx.Err()
			}
		},
		UpdateChannel: func(actionCtx context.Context, key, revision string, fields map[string]any) error {
			if revision == "" {
				return errors.New("refresh the board profile before editing channels")
			}
			params := map[string]any{"key": key, "expected_revision": revision}
			for k, v := range fields {
				switch k {
				case "name", "icon", "order", "hidden", "locked":
					params[k] = v
				default:
					return fmt.Errorf("unsupported channel field %q", k)
				}
			}
			var reply any
			if err := client.call(actionCtx, "controller.peripheral.presentation.update", params, &reply); err != nil {
				return err
			}
			var updated channelCatalog
			if err := client.call(actionCtx, "controller.peripherals.get", map[string]any{}, &updated); err != nil {
				return fmt.Errorf("channel saved; refresh failed: %w", err)
			}
			select {
			case catalogUpdates <- updated:
				return nil
			case <-actionCtx.Done():
				return actionCtx.Err()
			}
		},
		InvokeAction: func(actionCtx context.Context, owner desktop.ControlOwner, id string) error {
			if owner.HostInstanceID == "" || owner.Generation == 0 || owner.BoardIdentity == "" || owner.Revision == "" {
				return errors.New("refresh the current board controls before invoking an action")
			}
			var reply struct {
				Accepted bool `json:"accepted"`
			}
			params := struct {
				desktop.ControlOwner
				ActionID string `json:"action_id"`
			}{owner, id}
			if err := client.call(actionCtx, "controller.action.invoke", params, &reply); err != nil {
				return err
			}
			if !reply.Accepted {
				return errors.New("controller did not accept the channel action")
			}
			state, err := client.Snapshot(actionCtx)
			if err != nil {
				return fmt.Errorf("action acknowledged; readback failed: %w", err)
			}
			select {
			case readbacks <- state:
				return nil
			case <-actionCtx.Done():
				return actionCtx.Err()
			}
		},
		OpenPage: func(page string) error {
			target, err := browserURL(*address)
			if err != nil {
				return err
			}
			if page != "" {
				if alias, ok := map[string]string{"control": "controls", "rf": "device", "programs": "workbench", "firmware": "updates"}[page]; ok {
					page = alias
				}
				target, err = webURLForAppAction(target, hostui.AppAction{Kind: "app.page", Value: page})
				if err != nil {
					return err
				}
			}
			if *address == configured.Listen {
				return openAuthenticatedBrowser(target, store)
			}
			return openBrowser(target)
		},
		InvokeHostMenu: func(actionCtx context.Context, id string) error {
			if id == "exit" {
				var reply any
				return client.call(actionCtx, "controller.quit", nil, &reply)
			}
			if id == "reconnect" {
				_, err := client.Execute(actionCtx, "reconnect")
				return err
			}
			current, err := client.Snapshot(actionCtx)
			if err != nil {
				return err
			}
			if !current.Connected {
				return errors.New("controller disconnected before the page could open")
			}
			known := false
			for _, item := range desktopHostMenu(current) {
				known = known || (item.ID == id && id != "" && !item.Disabled && !item.Separator)
			}
			if !known {
				return fmt.Errorf("unknown host tray action %q", id)
			}
			target, err := browserURL(*address)
			if err != nil {
				return err
			}
			target, err = nativeWebPageURL(target, id)
			if err != nil {
				return err
			}
			if *address == configured.Listen {
				return openAuthenticatedBrowser(target, store)
			}
			return openBrowser(target)
		},
		ShowConsole:    console.toggle,
		ConsoleVisible: console.visible.Load,
		LoadPage:       pages.load, SubmitPage: pages.submit,
		PageChanges: pageChanges,
		PageVersion: eventHistory.version,
	})
}

// RemoteLiveUpdate.Connected describes the IPC stream, not the physical board.
// Only an authoritative snapshot may promote the board to connected. A lost
// stream still revokes the GUI's live controls immediately.
func desktopTransportState(state control.Snapshot, update tui.RemoteLiveUpdate) control.Snapshot {
	if update.ConnectionChange && !update.Connected {
		state.Connected = false
	}
	return state
}

func monitorNativeCore(completed <-chan error, cancel context.CancelFunc, stderr io.Writer) {
	err := <-completed
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "native desktop core:", err)
	}
	cancel()
}

func desktopSnapshot(state control.Snapshot, address, title string) desktop.Snapshot {
	trayState := desktopTrayState(state, title)
	return desktop.Snapshot{
		ObservedAt: state.StatusUpdated, Generation: state.ConnectionGeneration,
		HostMenu: desktopHostMenu(state),
		TrayIcon: string(nativeshell.StateIcon(trayState)), TrayTooltip: nativeshell.Tooltip(trayState),
		Busy:      state.EmergencyStop.Active || state.Programming.Active || state.Programming.RestorationPending,
		Connected: state.Connected, HavePanel: state.HaveFrontPanel || state.HaveFrontPanelSegments,
		HaveStatus: state.HaveStatus, CanNavigate: state.Connected && state.Hello.Capabilities&native.CapabilityMenuRemote != 0,
		HavePower:         state.HaveStatus && state.Status.INA219Available && state.Status.INA219ValuesValid(),
		HaveTemperature:   state.HaveStatus && state.Status.TLEDAvailable && state.Status.TLEDValueValid(),
		HaveTemperatureBT: state.HaveStatus && state.Status.TBTAvailable && state.Status.TBTValueValid(),
		BusMV:             state.Status.BusMV, PowerMW: state.Status.PowerMW, TemperatureBTC: float64(state.Status.TBTCenti) / 100,
		Segments: state.FrontPanel.RawSegments, Brightness: state.FrontPanel.Brightness, PressedKeys: state.FrontPanel.PressedKeys,
		LCDLine1: state.FrontPanel.LCDLine1, LCDLine2: state.FrontPanel.LCDLine2, Mode: strconv.Itoa(int(state.FrontPanel.ProgramMode)), Endpoint: address,
		SupplyMV: state.Status.SupplyMV, CurrentMA: state.Status.CurrentMA, TemperatureC: float64(state.Status.TLEDCenti) / 100,
	}
}

func desktopSeats(state control.Snapshot, controls []appconfig.ControlDescriptor) []desktop.Seat {
	var seats []desktop.Seat
	for _, item := range controls {
		if item.Kind != "seat" || item.Hidden {
			continue
		}
		side := byte(1)
		motion := state.Motion.Left
		if item.Key == "seat.b" {
			side = 2
			motion = state.Motion.Right
		} else if item.Key != "seat.a" {
			continue
		}
		seat := desktop.Seat{Key: item.Key, Name: item.Name, Side: side, State: motion.Applied, Known: state.HaveStatus, Locked: item.Locked}
		for _, action := range item.Actions {
			if action.Verb == "stop" {
				seat.StopAction = action.ID
			}
		}
		seats = append(seats, seat)
	}
	return seats
}

func desktopPreferences(value nativeHostPreferences) *desktop.Preferences {
	v := value.Settings
	return &desktop.Preferences{Revision: value.ETag, Theme: v.Appearance.Theme, Locale: v.Appearance.Locale, ReduceMotion: v.Appearance.ReduceMotion, ShowGraphs: v.ShowGraphs, ShowSupplyVoltage: v.ShowSupplyVoltage, ShowBusVoltage: v.ShowBusVoltage, ShowCurrent: v.ShowCurrent, ShowPower: v.ShowPower, ShowTemperatureLED: v.ShowTemperatureLED, ShowTemperatureBT: v.ShowTemperatureBT, VoltageDecimals: v.VoltageDecimals, CurrentDecimals: v.CurrentDecimals, PowerDecimals: v.PowerDecimals, TemperatureDecimals: v.TemperatureDecimals}
}

func desktopTrayState(state control.Snapshot, title string) nativeshell.State {
	return nativeshell.State{Title: title, Connected: state.Connected, Paused: state.Paused, Port: state.Port.Name, ConnectionState: state.ConnectionState}
}

func desktopHostMenu(state control.Snapshot) []desktop.MenuItem {
	var items []desktop.MenuItem
	for _, item := range nativeshell.BuildMenu(desktopTrayState(state, "")) {
		id, _ := nativeshell.PageForCommand(item.Command)
		if item.Command == nativeshell.CommandReconnect {
			id = "reconnect"
		}
		if item.Command == nativeshell.CommandExit {
			id = "exit"
		}
		items = append(items, desktop.MenuItem{ID: id, Label: item.Label, Disabled: !item.Enabled, Separator: item.Kind == nativeshell.ItemSeparator, Default: item.Default})
	}
	return items
}

func mergeDesktopSnapshot(previous, next control.Snapshot) control.Snapshot {
	if previous.ConnectionGeneration != next.ConnectionGeneration {
		return next
	}
	if next.FrontPanelUpdated.Before(previous.FrontPanelUpdated) {
		next.FrontPanel = previous.FrontPanel
		next.FrontPanelUpdated = previous.FrontPanelUpdated
		next.HaveFrontPanel = previous.HaveFrontPanel
		next.HaveFrontPanelSegments = previous.HaveFrontPanelSegments
	}
	if next.StatusUpdated.Before(previous.StatusUpdated) {
		next.Status = previous.Status
		next.StatusUpdated = previous.StatusUpdated
		next.HaveStatus = previous.HaveStatus
	}
	if next.PWMRevision < previous.PWMRevision {
		next.PWMValues = previous.PWMValues
		next.PWMRevision = previous.PWMRevision
		next.PWMUpdated = previous.PWMUpdated
	}
	return next
}

func desktopChannels(state control.Snapshot, catalog []appconfig.ControlDescriptor) []desktop.Channel {
	var result []desktop.Channel
	catalog = append([]appconfig.ControlDescriptor(nil), catalog...)
	sort.SliceStable(catalog, func(i, j int) bool {
		if catalog[i].Kind == catalog[j].Kind {
			if catalog[i].Order == catalog[j].Order {
				return catalog[i].Key < catalog[j].Key
			}
			return catalog[i].Order < catalog[j].Order
		}
		return catalog[i].Kind > catalog[j].Kind
	})
	ranks := map[string]int{}
	for _, control := range catalog {
		rank := ranks[control.Kind]
		ranks[control.Kind]++
		// Motion requires a bounded hold/release contract; don't substitute
		// a latched motion command for a momentary physical button.
		if control.Hidden || control.Kind != "relay" && control.Kind != "mosfet" {
			continue
		}
		channel := desktop.Channel{Key: control.Key, Name: control.Name, Kind: control.Kind, Locked: control.Locked, Icon: control.Icon, Order: rank}
		parts := strings.Split(control.Key, ".")
		if len(parts) != 2 {
			continue
		}
		index, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		if control.Kind == "relay" && index >= 1 && index <= 8 && state.HaveStatus {
			channel.Known = true
			channel.State = "off"
			if state.Status.ActiveRelays&(1<<uint(index-1)) != 0 {
				channel.State = "on"
			}
		}
		if control.Kind == "mosfet" && index >= 0 && index < 16 && state.PWMValues != nil && state.PWMValues.Available {
			channel.Known = true
			channel.State = "off"
			raw := state.PWMValues.Values[index]
			if raw > 0 {
				channel.State = "on"
			}
			channel.Percent = appconfig.PWMPercentFromRaw(raw, appconfig.PWMChannelConfig{Curve: control.Curve, Gamma: control.Gamma})
		}
		for _, action := range control.Actions {
			channel.Actions = append(channel.Actions, desktop.Action{ID: action.ID, Label: action.Name, Verb: action.Verb})
		}
		result = append(result, channel)
	}
	return result
}
