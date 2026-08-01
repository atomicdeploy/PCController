// Package hostbridge runs opt-in host integrations around the one primary
// controller client. It never opens the serial port itself.
package hostbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/discovery"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/productidentity"
)

type Status struct {
	DiscoveryActive bool                            `json:"discovery_active"`
	HotkeysActive   int                             `json:"hotkeys_active"`
	KeyboardActive  int                             `json:"keyboard_control_active"`
	Notifications   bool                            `json:"notifications_active"`
	DoorWarning     bool                            `json:"door_open_running_warning"`
	StatusLEDState  string                          `json:"status_led_state,omitempty"`
	WebhooksActive  int                             `json:"webhooks_active"`
	WSClientsActive []string                        `json:"websocket_clients_active,omitempty"`
	LastError       string                          `json:"last_error,omitempty"`
	Desktop         hostui.DesktopIntegrationStatus `json:"desktop"`
}

type peerState struct {
	name          string
	protocol      string
	allowCommands bool
	forwardEvents bool
	cancel        context.CancelFunc
	events        chan controller.Event
	mu            sync.RWMutex
	session       *peerRPCSession
}

// PeerInfo is the non-secret, live state exposed by bridge list APIs.
type PeerInfo struct {
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Connected     bool   `json:"connected"`
	AllowCommands bool   `json:"allow_commands"`
	ForwardEvents bool   `json:"forward_events"`
}

type peerRPCSession struct {
	write   func(any) error
	nextID  uint64
	mu      sync.Mutex
	pending map[string]chan ipcjson.Response
	done    chan struct{}
	err     error
	once    sync.Once
}

func newPeerRPCSession(write func(any) error) *peerRPCSession {
	return &peerRPCSession{
		write: write, nextID: 1000,
		pending: make(map[string]chan ipcjson.Response), done: make(chan struct{}),
	}
}

func (session *peerRPCSession) Call(
	ctx context.Context,
	request ipcjson.Request,
) (ipcjson.Response, error) {
	session.mu.Lock()
	session.nextID++
	wireID := json.RawMessage(strconv.FormatUint(session.nextID, 10))
	originalID := append(json.RawMessage(nil), request.ID...)
	request.JSONRPC = ipcjson.Version
	request.ID = wireID
	request.Auth = ""
	key := string(wireID)
	responseChannel := make(chan ipcjson.Response, 1)
	select {
	case <-session.done:
		err := session.err
		session.mu.Unlock()
		if err == nil {
			err = errors.New("bridge session is closed")
		}
		return ipcjson.Response{}, err
	default:
		session.pending[key] = responseChannel
	}
	session.mu.Unlock()
	if err := session.write(request); err != nil {
		session.mu.Lock()
		delete(session.pending, key)
		session.mu.Unlock()
		return ipcjson.Response{}, err
	}
	select {
	case response := <-responseChannel:
		if len(originalID) != 0 {
			response.ID = originalID
		}
		return response, nil
	case <-session.done:
		session.mu.Lock()
		err := session.err
		session.mu.Unlock()
		if err == nil {
			err = errors.New("bridge session closed before response")
		}
		return ipcjson.Response{}, err
	case <-ctx.Done():
		session.mu.Lock()
		delete(session.pending, key)
		session.mu.Unlock()
		return ipcjson.Response{}, ctx.Err()
	}
}

func (session *peerRPCSession) Resolve(response ipcjson.Response) bool {
	key := string(response.ID)
	session.mu.Lock()
	channel, ok := session.pending[key]
	if ok {
		delete(session.pending, key)
	}
	session.mu.Unlock()
	if ok {
		channel <- response
	}
	return ok
}

func (session *peerRPCSession) Close(err error) {
	session.once.Do(func() {
		session.mu.Lock()
		session.err = err
		close(session.done)
		session.mu.Unlock()
	})
}

func (peer *peerState) attach(session *peerRPCSession) func() {
	peer.mu.Lock()
	peer.session = session
	peer.mu.Unlock()
	return func() {
		peer.mu.Lock()
		if peer.session == session {
			peer.session = nil
		}
		peer.mu.Unlock()
		session.Close(errors.New("bridge peer disconnected"))
	}
}

type Manager struct {
	client *controller.Client
	store  *appconfig.Store
	ctx    context.Context
	cancel context.CancelFunc

	mu                 sync.RWMutex
	closing            bool
	digest             [sha256.Size]byte
	advertiser         *discovery.Advertiser
	peers              map[string]*peerState
	status             Status
	webhookGate        chan struct{}
	wait               sync.WaitGroup
	actions            *hostui.ActionBroker
	hotkeys            hostui.HotkeyRegistrar
	keyboard           hostui.KeyboardRegistrar
	keyboardLatchMu    sync.Mutex
	keyboardLatches    map[string]keyboardLatch
	keyboardActuator   func(context.Context, keyboardOperation) error
	notifier           hostui.Notifier
	warningBeep        func() error
	runningDoorWarning bool
	statusLED          *statusLEDArbiter
}

func Start(
	parent context.Context,
	client *controller.Client,
	store *appconfig.Store,
	actions *hostui.ActionBroker,
) (*Manager, error) {
	if client == nil || store == nil {
		return nil, fmt.Errorf("host bridge requires controller client and configuration store")
	}
	ctx, cancel := context.WithCancel(parent)
	manager := &Manager{
		client: client, store: store, ctx: ctx, cancel: cancel,
		peers: make(map[string]*peerState), webhookGate: make(chan struct{}, 8),
		keyboardLatches: make(map[string]keyboardLatch),
		actions:         actions,
		hotkeys:         hostui.NewHotkeyRegistrar(),
		notifier:        hostui.NewNotifier(hostui.NotifierOptions{AppID: productidentity.StableAppID}),
		warningBeep:     hostui.WarningBeep,
	}
	manager.statusLED = newStatusLEDArbiter(
		ctx,
		client,
		func(state string) {
			manager.mu.Lock()
			manager.status.StatusLEDState = state
			manager.mu.Unlock()
			client.EmitHostEvent("status-led.state", state)
		},
		func(err error) { manager.recordError("status LED: " + err.Error()) },
	)
	client.SetBeforeDisconnectHook(func(reason string) {
		_ = manager.ReleaseKeyboard("port-close: " + reason)
		requestContext, requestCancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		_ = manager.statusLED.PrepareDisconnect(requestContext)
		requestCancel()
	})
	if err := manager.reconcile(store.Current()); err != nil {
		client.SetBeforeDisconnectHook(nil)
		cancel()
		return nil, err
	}
	if store.Current().Integrations.Notifications.Enabled {
		desktop, desktopErr := hostui.EnsureDesktopIntegration(
			hostui.DesktopIntegrationOptions{
				AppID:       productidentity.StableAppID,
				DisplayName: productidentity.Title(store.Current().UI.AppTitle),
			},
		)
		manager.mu.Lock()
		manager.status.Desktop = desktop
		if desktopErr != nil {
			manager.status.LastError = "desktop integration: " + desktopErr.Error()
		}
		manager.mu.Unlock()
	}
	// Capture the cursor before starting the consumer. Otherwise a message
	// published immediately after Start returns can land between the goroutine
	// launch and its first LatestEventID call and be skipped forever.
	afterID := client.LatestEventID()
	manager.wait.Add(3)
	go manager.reconcileLoop()
	go manager.eventLoop(afterID)
	go func() {
		defer manager.wait.Done()
		manager.statusLED.Run()
	}()
	return manager, nil
}

func (manager *Manager) Close() {
	if manager == nil {
		return
	}
	manager.mu.Lock()
	if manager.closing {
		manager.mu.Unlock()
		manager.wait.Wait()
		return
	}
	manager.closing = true
	advertiser, hotkeys, keyboard, peers :=
		manager.advertiser, manager.hotkeys, manager.keyboard, manager.peers
	manager.advertiser, manager.hotkeys, manager.keyboard = nil, nil, nil
	manager.peers = make(map[string]*peerState)
	manager.mu.Unlock()
	// Release held motion/output actions while the runtime and callback context
	// are still available, then stop every background integration.
	if keyboard != nil {
		_ = keyboard.Stop("shutdown")
	}
	ctx, releaseCancel := context.WithTimeout(manager.ctx, 5*time.Second)
	_ = manager.releaseKeyboardLatches(ctx, "shutdown")
	releaseCancel()
	statusContext, statusCancel := context.WithTimeout(manager.ctx, 400*time.Millisecond)
	_ = manager.statusLED.PrepareDisconnect(statusContext)
	statusCancel()
	manager.client.SetBeforeDisconnectHook(nil)
	manager.cancel()
	if advertiser != nil {
		advertiser.Close()
	}
	if hotkeys != nil {
		_ = hotkeys.Stop()
	}
	for _, peer := range peers {
		peer.cancel()
	}
	manager.wait.Wait()
}

func (manager *Manager) Status() Status {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	result := manager.status
	result.WSClientsActive = append([]string(nil), result.WSClientsActive...)
	return result
}

// BridgePeers returns configured peers and their current connection state
// without exposing authentication tokens or endpoint credentials.
func (manager *Manager) BridgePeers() []PeerInfo {
	manager.mu.RLock()
	peers := make([]*peerState, 0, len(manager.peers))
	for _, peer := range manager.peers {
		peers = append(peers, peer)
	}
	manager.mu.RUnlock()
	result := make([]PeerInfo, 0, len(peers))
	for _, peer := range peers {
		peer.mu.RLock()
		result = append(result, PeerInfo{
			Name: peer.name, Protocol: peer.protocol,
			Connected: peer.session != nil, AllowCommands: peer.allowCommands,
			ForwardEvents: peer.forwardEvents,
		})
		peer.mu.RUnlock()
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	return result
}

// CallBridge invokes one versioned JSON-RPC method through an already
// authenticated outbound peer connection. The remote host applies its own
// remote capability policy before touching its serial owner.
func (manager *Manager) CallBridge(
	ctx context.Context,
	name string,
	request ipcjson.Request,
) (ipcjson.Response, error) {
	manager.mu.RLock()
	peer := manager.peers[strings.ToLower(strings.TrimSpace(name))]
	manager.mu.RUnlock()
	if peer == nil {
		return ipcjson.Response{}, fmt.Errorf("bridge peer %q is not configured", name)
	}
	peer.mu.RLock()
	session := peer.session
	peer.mu.RUnlock()
	if session == nil {
		return ipcjson.Response{}, fmt.Errorf("bridge peer %q is not connected", peer.name)
	}
	response, err := session.Call(ctx, request)
	if err != nil {
		manager.client.EmitHostEvent(
			"bridge.call.error",
			fmt.Sprintf("%s %s: %v", peer.name, request.Method, err),
		)
		return ipcjson.Response{}, err
	}
	manager.client.EmitHostEvent(
		"bridge.call",
		fmt.Sprintf("%s %s completed", peer.name, request.Method),
	)
	return response, nil
}

// BridgeCommand exposes the same correlated bridge calls to the interactive
// shell/TUI console without creating another network or serial implementation.
func (manager *Manager) BridgeCommand(
	ctx context.Context,
	args []string,
) (string, error) {
	if len(args) == 1 && strings.EqualFold(args[0], "list") {
		encoded, err := json.MarshalIndent(manager.BridgePeers(), "", "  ")
		return string(encoded), err
	}
	if len(args) < 3 || len(args) > 4 || !strings.EqualFold(args[0], "call") {
		return "", errors.New("usage: bridge list | bridge call PEER METHOD [PARAMS_JSON]")
	}
	params := json.RawMessage("{}")
	if len(args) == 4 {
		if !json.Valid([]byte(args[3])) {
			return "", errors.New("bridge call params must be one valid JSON value")
		}
		params = json.RawMessage(args[3])
	}
	response, err := manager.CallBridge(ctx, args[1], ipcjson.Request{
		JSONRPC: ipcjson.Version, ID: json.RawMessage("1"),
		Method: args[2], Params: params,
	})
	if err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(response, "", "  ")
	return string(encoded), err
}

func (manager *Manager) Notifier() hostui.Notifier { return manager.notifier }

func (manager *Manager) HotkeyStatus() hostui.HotkeyStatus {
	manager.mu.RLock()
	registrar := manager.hotkeys
	manager.mu.RUnlock()
	if registrar == nil {
		return hostui.HotkeyStatus{}
	}
	return registrar.Status()
}

func (manager *Manager) KeyboardStatus() hostui.KeyboardStatus {
	manager.mu.RLock()
	registrar := manager.keyboard
	manager.mu.RUnlock()
	if registrar == nil {
		return hostui.KeyboardStatus{}
	}
	return registrar.Status()
}

// ReleaseKeyboard synchronously converts every held key into its configured
// release action before a caller closes the serial runtime.
func (manager *Manager) ReleaseKeyboard(reason string) error {
	manager.mu.RLock()
	registrar := manager.keyboard
	manager.mu.RUnlock()
	var releaseErr error
	if registrar != nil {
		releaseErr = registrar.ReleaseAll(reason)
	}
	ctx, cancel := context.WithTimeout(manager.ctx, 5*time.Second)
	defer cancel()
	return errors.Join(
		releaseErr,
		manager.releaseKeyboardLatches(ctx, reason),
	)
}

func (manager *Manager) NotificationStatus() hostui.NotificationStatus {
	if manager.notifier == nil {
		return hostui.NotificationStatus{}
	}
	return manager.notifier.Status()
}

func (manager *Manager) reconcileLoop() {
	defer manager.wait.Done()
	updates := manager.store.Subscribe(manager.ctx)
	for config := range updates {
		if err := manager.reconcile(config); err != nil {
			manager.recordError("integration reload: " + err.Error())
		}
	}
}

func integrationDigest(config appconfig.Config) [sha256.Size]byte {
	encoded, _ := json.Marshal(struct {
		IPC          appconfig.IPC
		Integrations appconfig.Integrations
	}{config.IPC, config.Integrations})
	return sha256.Sum256(encoded)
}

func (manager *Manager) reconcile(config appconfig.Config) error {
	manager.client.ConfigureRFPresentation(config.RF)
	digest := integrationDigest(config)
	manager.mu.RLock()
	closing := manager.closing
	unchanged := digest == manager.digest
	manager.mu.RUnlock()
	if closing {
		return nil
	}
	if unchanged {
		return nil
	}
	manager.mu.Lock()
	oldAdvertiser, oldHotkeys, oldKeyboard, oldPeers :=
		manager.advertiser, manager.hotkeys, manager.keyboard, manager.peers
	desktopStatus := manager.status.Desktop
	statusLEDState := manager.status.StatusLEDState
	doorWarning := manager.runningDoorWarning
	manager.advertiser, manager.hotkeys, manager.keyboard = nil, nil, nil
	manager.peers = make(map[string]*peerState)
	manager.mu.Unlock()
	if oldAdvertiser != nil {
		oldAdvertiser.Close()
	}
	if oldHotkeys != nil {
		_ = oldHotkeys.Stop()
	}
	if oldKeyboard != nil {
		_ = oldKeyboard.Stop("config-reload")
	}
	for _, peer := range oldPeers {
		peer.cancel()
	}

	status := Status{
		Notifications:  config.Integrations.Notifications.Enabled,
		Desktop:        desktopStatus,
		StatusLEDState: statusLEDState,
		DoorWarning:    doorWarning,
	}
	hotkeys := hostui.NewHotkeyRegistrar()
	var hotkeyBindings []hostui.HotkeyBinding
	for _, hotkey := range config.Integrations.Hotkeys {
		if hotkey.Enabled {
			hotkeyBindings = append(hotkeyBindings, hostui.HotkeyBinding{
				Name: hotkey.Name, Accelerator: hotkey.Chord, Command: hotkey.Command,
			})
		}
	}
	if len(hotkeyBindings) != 0 {
		if err := hotkeys.Start(manager.ctx, hotkeyBindings, manager.handleHotkey); err != nil {
			status.LastError = "hotkeys: " + err.Error()
		} else {
			status.HotkeysActive = len(hotkeyBindings)
		}
	}
	keyboard := hostui.NewKeyboardRegistrar()
	if config.Integrations.Keyboard.Enabled {
		bindings, configured := keyboardBindings(config.Integrations.Keyboard)
		if err := keyboard.Start(
			manager.ctx,
			bindings,
			func(event hostui.KeyboardEvent) error {
				return manager.handleKeyboard(configured, event)
			},
		); err != nil {
			status.LastError = "keyboard control: " + err.Error()
		} else {
			status.KeyboardActive = len(bindings)
		}
	}
	for _, webhook := range config.Integrations.OutboundWebhooks {
		if webhook.Enabled {
			status.WebhooksActive++
		}
	}

	var advertiser *discovery.Advertiser
	discoveryConfig := config.Integrations.Discovery
	if discoveryConfig.MDNSEnabled || discoveryConfig.SSDPEnabled {
		_, rawPort, err := net.SplitHostPort(config.IPC.Listen)
		if err != nil {
			return err
		}
		port, err := strconv.Atoi(rawPort)
		if err != nil {
			return err
		}
		name := strings.TrimSpace(discoveryConfig.InstanceName)
		if name == "" {
			name = config.UI.AppTitle
		}
		created, err := discovery.Advertise(
			manager.ctx, name, port,
			discoveryConfig.MDNSEnabled, discoveryConfig.SSDPEnabled,
			[]string{
				"ws=" + config.IPC.WebSocketPath,
				"socketio=" + config.IPC.SocketIOPath,
				"auth=required",
			},
		)
		if err != nil {
			_ = hotkeys.Stop()
			_ = keyboard.Stop("integration-start-failed")
			return err
		}
		advertiser = created
		status.DiscoveryActive = true
	}
	peers := make(map[string]*peerState)
	for _, peerConfig := range config.Integrations.WebSocketClients {
		if !peerConfig.Enabled {
			continue
		}
		peerContext, cancel := context.WithCancel(manager.ctx)
		peer := &peerState{
			name: peerConfig.Name, protocol: firstProtocol(peerConfig.Protocol),
			allowCommands: peerConfig.AllowCommands,
			forwardEvents: peerConfig.ForwardEvents,
			cancel:        cancel, events: make(chan controller.Event, 128),
		}
		peers[strings.ToLower(peerConfig.Name)] = peer
		status.WSClientsActive = append(
			status.WSClientsActive,
			peerConfig.Name,
		)
		manager.wait.Add(1)
		go manager.runWebSocketPeer(peerContext, peer, peerConfig)
	}
	manager.mu.Lock()
	if manager.closing {
		manager.mu.Unlock()
		if advertiser != nil {
			advertiser.Close()
		}
		_ = hotkeys.Stop()
		_ = keyboard.Stop("shutdown")
		for _, peer := range peers {
			peer.cancel()
		}
		return nil
	}
	manager.hotkeys = hotkeys
	manager.keyboard = keyboard
	manager.advertiser = advertiser
	manager.peers = peers
	manager.status = status
	manager.digest = digest
	manager.mu.Unlock()
	manager.statusLED.Observe(
		config.Integrations.StatusLED,
		manager.client.Snapshot(),
		controller.Event{Kind: "config"},
	)
	return nil
}

func (manager *Manager) eventLoop(afterID uint64) {
	defer manager.wait.Done()
	for manager.ctx.Err() == nil {
		event, err := manager.client.NextEvent(manager.ctx, afterID, "")
		if err != nil {
			return
		}
		afterID = event.ID
		if event.Kind == "telemetry" {
			snapshot := manager.client.Snapshot()
			if snapshot.HaveStatus {
				manager.observeKeyboardStatus(snapshot.Status, time.Now())
			}
		}
		if event.Kind == "connection" &&
			(event.State == "disconnected" || event.State == "connected") {
			manager.mu.RLock()
			keyboard := manager.keyboard
			manager.mu.RUnlock()
			if keyboard != nil {
				reason := "disconnect"
				if event.State == "connected" {
					reason = "reconnect-fail-safe"
				}
				if releaseErr := manager.ReleaseKeyboard(reason); releaseErr != nil {
					manager.recordError("keyboard " + reason + ": " + releaseErr.Error())
				}
			}
		}
		config := manager.store.Current()
		manager.observeRunningDoor(config)
		manager.statusLED.Observe(
			config.Integrations.StatusLED,
			manager.client.Snapshot(),
			event,
		)
		manager.dispatchWebhooks(config, event)
		manager.dispatchTextMappings(config, event)
		manager.dispatchNotification(config, event)
		if bridgeEventForwardable(event) {
			manager.mu.RLock()
			for _, peer := range manager.peers {
				select {
				case peer.events <- event:
				default:
				}
			}
			manager.mu.RUnlock()
		}
	}
}

func bridgeEventForwardable(event controller.Event) bool {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	if kind == "integration.error" || strings.HasPrefix(kind, "bridge.") ||
		strings.HasPrefix(kind, "security.remote.") {
		return false
	}
	return kind != "message" ||
		(!strings.EqualFold(event.Source, "bridge") &&
			!strings.EqualFold(event.Source, "websocket"))
}

// observeRunningDoor combines the explicit PC-owned Running state with the
// live reed input. The door never changes ProgramState; it only raises/clears
// this host warning and its configurable desktop sound/toast presentation.
func (manager *Manager) observeRunningDoor(config appconfig.Config) {
	snapshot := manager.client.Snapshot()
	active := runningDoorCondition(snapshot)
	manager.mu.Lock()
	if active == manager.runningDoorWarning {
		manager.mu.Unlock()
		return
	}
	manager.runningDoorWarning = active
	manager.status.DoorWarning = active
	beep := manager.warningBeep
	manager.mu.Unlock()
	if active {
		manager.client.EmitHostEvent(
			"warning.door-open-running",
			"Enclosure door opened while program state is Running",
		)
		if config.Integrations.Notifications.DoorRunningBeep && beep != nil {
			if err := beep(); err != nil {
				manager.recordError("door-running warning sound: " + err.Error())
			}
		}
		return
	}
	manager.client.EmitHostEvent(
		"warning.door-open-running.cleared",
		"Door-running warning cleared",
	)
}

func runningDoorCondition(snapshot controller.Snapshot) bool {
	return snapshot.Connected && snapshot.HaveStatus && snapshot.Status.DoorOpen &&
		strings.EqualFold(string(snapshot.ProgramState.Mode), "Running")
}

func (manager *Manager) handleHotkey(event hostui.HotkeyEvent) {
	action, err := hostui.ParseAction(event.Binding.Command, "hotkey:"+event.Binding.Name)
	if err != nil {
		manager.recordError(err.Error())
		return
	}
	if action.Kind != "command" {
		if manager.actions == nil {
			manager.recordError("hotkey app action routing is unavailable")
			return
		}
		if err := manager.actions.Publish(action); err != nil {
			manager.recordError(err.Error())
		}
		return
	}
	ctx, cancel := context.WithTimeout(manager.ctx, 30*time.Second)
	defer cancel()
	output, err := manager.client.Execute(ctx, action.Value)
	if err != nil {
		manager.recordError("hotkey " + event.Binding.Name + ": " + err.Error())
		return
	}
	manager.client.EmitHostEvent("hotkey", event.Binding.Name+": "+output)
}

func (manager *Manager) dispatchNotification(
	config appconfig.Config,
	event controller.Event,
) {
	if !config.Integrations.Notifications.Enabled || manager.notifier == nil ||
		strings.HasPrefix(event.Kind, "notification.") {
		return
	}
	if event.Kind == "warning.door-open-running" &&
		!config.Integrations.Notifications.DoorRunningToast {
		return
	}
	matched := false
	for _, kind := range config.Integrations.Notifications.ImportantKinds {
		if eventKindMatches(kind, event.Kind) {
			matched = true
			break
		}
	}
	if !matched {
		return
	}
	notification, ok := hostui.NotificationForImportantEvent(hostui.ImportantEvent{
		Kind: event.Kind, Message: event.Text, AppTitle: config.UI.AppTitle,
	})
	if !ok {
		return
	}
	if len(config.Integrations.Notifications.Actions) != 0 {
		configured, err := configuredNotificationActions(
			notification,
			config.Integrations.Notifications.Actions,
		)
		if err != nil {
			manager.recordError("notification actions: " + err.Error())
			return
		}
		notification = configured
	}
	go func() {
		ctx, cancel := context.WithTimeout(manager.ctx, 10*time.Second)
		defer cancel()
		if err := manager.notifier.Notify(ctx, notification); err != nil {
			manager.recordError("notification: " + err.Error())
		}
	}()
}

func configuredNotificationActions(
	notification hostui.Notification,
	actions []appconfig.NotificationAction,
) (hostui.Notification, error) {
	notification.Actions = make([]hostui.NotificationAction, 0, len(actions))
	for _, configured := range actions {
		action, err := hostui.ParseAction(configured.Command, "notification:"+configured.ID)
		if err != nil {
			return hostui.Notification{}, err
		}
		uri, err := hostui.ActionURI(action)
		if err != nil {
			return hostui.Notification{}, err
		}
		notification.Actions = append(notification.Actions, hostui.NotificationAction{
			Label: configured.Label, URI: uri,
		})
	}
	return notification, nil
}

func (manager *Manager) dispatchWebhooks(
	config appconfig.Config,
	event controller.Event,
) {
	if strings.HasPrefix(event.Kind, "webhook.") {
		return
	}
	for _, webhook := range config.Integrations.OutboundWebhooks {
		if !webhook.Enabled || !eventKindMatches(webhook.EventKind, event.Kind) {
			continue
		}
		select {
		case manager.webhookGate <- struct{}{}:
			manager.wait.Add(1)
			go func(webhook appconfig.Webhook) {
				defer manager.wait.Done()
				defer func() { <-manager.webhookGate }()
				manager.sendWebhook(webhook, event)
			}(webhook)
		default:
			manager.recordError("outbound webhook queue saturated")
		}
	}
}

func eventKindMatches(pattern, kind string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	kind = strings.ToLower(strings.TrimSpace(kind))
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(kind, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == kind
}

func (manager *Manager) sendWebhook(
	config appconfig.Webhook,
	event controller.Event,
) {
	method := strings.ToUpper(strings.TrimSpace(config.Method))
	target := config.URL
	var body io.Reader
	if method == http.MethodGet || method == http.MethodDelete {
		parsed, err := url.Parse(target)
		if err != nil {
			manager.recordError(config.Name + ": " + err.Error())
			return
		}
		query := parsed.Query()
		query.Set("kind", event.Kind)
		query.Set("text", event.Text)
		query.Set("id", strconv.FormatUint(event.ID, 10))
		parsed.RawQuery = query.Encode()
		target = parsed.String()
	} else {
		var encoded []byte
		if strings.TrimSpace(config.BodyTemplate) == "" {
			encoded, _ = json.Marshal(event)
		} else {
			template := strings.NewReplacer(
				"{{id}}", strconv.FormatUint(event.ID, 10),
				"{{kind}}", event.Kind,
				"{{text}}", event.Text,
				"{{source}}", event.Source,
			).Replace(config.BodyTemplate)
			encoded = []byte(template)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(manager.ctx, method, target, body)
	if err != nil {
		manager.recordError(config.Name + ": " + err.Error())
		return
	}
	for key, value := range config.Headers {
		request.Header.Set(key, value)
	}
	if body != nil && request.Header.Get("Content-Type") == "" {
		request.Header.Set("Content-Type", "application/json")
	}
	timeout := time.Duration(config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	response, err := client.Do(request)
	if err != nil {
		manager.recordError(config.Name + ": " + err.Error())
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
	_ = response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		manager.recordError(fmt.Sprintf("%s: HTTP %d", config.Name, response.StatusCode))
		return
	}
	manager.client.EmitHostEvent("webhook.sent", fmt.Sprintf(
		"%s delivered event %d (%s)", config.Name, event.ID, event.Kind,
	))
}

func (manager *Manager) dispatchTextMappings(
	config appconfig.Config,
	event controller.Event,
) {
	if event.Kind != "message" {
		return
	}
	for _, mapping := range config.Integrations.TextMappings {
		if !mapping.Enabled ||
			!optionalEqual(mapping.Source, event.Source) ||
			!optionalEqual(mapping.Target, event.Target) ||
			!optionalEqual(mapping.Type, event.MessageType) ||
			(mapping.Contains != "" && !strings.Contains(
				strings.ToLower(event.Text), strings.ToLower(mapping.Contains),
			)) {
			continue
		}
		go func(mapping appconfig.TextMapping) {
			ctx, cancel := context.WithTimeout(manager.ctx, 30*time.Second)
			defer cancel()
			output, err := manager.client.Execute(ctx, mapping.Command)
			if err != nil {
				manager.recordError(mapping.Name + ": " + err.Error())
				return
			}
			manager.client.EmitHostEvent("message.action", mapping.Name+": "+output)
		}(mapping)
	}
}

func optionalEqual(expected, actual string) bool {
	return strings.TrimSpace(expected) == "" ||
		strings.EqualFold(strings.TrimSpace(expected), strings.TrimSpace(actual))
}

func (manager *Manager) recordError(message string) {
	manager.setLastError(message)
	manager.client.EmitHostEvent("integration.error", message)
}

func (manager *Manager) setLastError(message string) {
	manager.mu.Lock()
	manager.status.LastError = message
	manager.mu.Unlock()
}

func (manager *Manager) runWebSocketPeer(
	ctx context.Context,
	peer *peerState,
	config appconfig.WebSocketClient,
) {
	defer manager.wait.Done()
	backoff := time.Second
	for ctx.Err() == nil {
		err := manager.webSocketPeerSession(ctx, peer, config)
		if ctx.Err() != nil {
			return
		}
		manager.recordError("WebSocket " + config.Name + ": " + err.Error())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (manager *Manager) webSocketPeerSession(
	ctx context.Context,
	peer *peerState,
	config appconfig.WebSocketClient,
) error {
	if strings.EqualFold(strings.TrimSpace(config.Protocol), "socketio") {
		return manager.socketIOPeerSession(ctx, peer, config)
	}
	header := http.Header{}
	if config.AuthToken != "" {
		header.Set("Authorization", "Bearer "+config.AuthToken)
	}
	connection, _, err := websocket.Dial(ctx, config.URL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(1024 * 1024)
	var writeMu sync.Mutex
	writeJSON := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return connection.Write(writeContext, websocket.MessageText, encoded)
	}
	rpcSession := newPeerRPCSession(writeJSON)
	detach := peer.attach(rpcSession)
	defer detach()
	topics := append([]string(nil), config.Topics...)
	if len(topics) == 0 {
		topics = []string{"events"}
	}
	if err := writeJSON(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "controller.subscribe",
		"params": map[string]any{"topics": topics},
	}); err != nil {
		return err
	}
	sessionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	writeErrors := make(chan error, 1)
	if config.ForwardEvents {
		go func() {
			for {
				select {
				case <-sessionContext.Done():
					return
				case event := <-peer.events:
					encoded, _ := json.Marshal(event)
					text := string(encoded)
					if len(text) > 4096 {
						text = text[:4096]
					}
					params, _ := json.Marshal(controller.TextMessage{
						Source: "bridge", Target: "host", Type: "local-event",
						Text: text, Metadata: map[string]string{
							"event.id":   strconv.FormatUint(event.ID, 10),
							"event.kind": event.Kind,
						},
					})
					response, err := rpcSession.Call(sessionContext, ipcjson.Request{
						JSONRPC: ipcjson.Version, Method: "controller.message.send",
						Params: params,
					})
					if err == nil && response.Error != nil {
						manager.setLastError(
							"WebSocket " + config.Name + " event rejected: " + response.Error.Error(),
						)
						continue
					}
					if err != nil {
						select {
						case writeErrors <- err:
						default:
						}
						return
					}
				}
			}
		}()
	}
	for {
		select {
		case err := <-writeErrors:
			return err
		default:
		}
		messageType, data, err := connection.Read(sessionContext)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}
		var responseEnvelope struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Result json.RawMessage   `json:"result"`
			Error  *ipcjson.RPCError `json:"error"`
		}
		if json.Unmarshal(data, &responseEnvelope) == nil &&
			len(responseEnvelope.ID) != 0 && responseEnvelope.Method == "" &&
			(len(responseEnvelope.Result) != 0 || responseEnvelope.Error != nil) {
			var response ipcjson.Response
			if json.Unmarshal(data, &response) == nil {
				_ = rpcSession.Resolve(response)
			}
			continue
		}
		var request ipcjson.Request
		if err := json.Unmarshal(data, &request); err != nil {
			continue
		}
		if request.Method == "controller.event" || request.Method == "controller.status" {
			_, _ = manager.client.SendTextMessage(ctx, controller.TextMessage{
				Source: "websocket", Target: "host", Type: "remote-event",
				Text: string(request.Params),
			})
			continue
		}
		if len(request.ID) == 0 {
			continue
		}
		if !config.AllowCommands {
			_ = writeJSON(ipcjson.Response{
				JSONRPC: "2.0", ID: request.ID,
				Error: &ipcjson.RPCError{Code: -32003, Message: "peer commands are disabled"},
			})
			continue
		}
		service := manager.remotePeerService()
		if err := writeJSON(service.DispatchRemote(ctx, request, "bridge")); err != nil {
			return err
		}
	}
}

func (manager *Manager) socketIOPeerSession(
	ctx context.Context,
	peer *peerState,
	config appconfig.WebSocketClient,
) error {
	target, err := url.Parse(config.URL)
	if err != nil {
		return err
	}
	query := target.Query()
	query.Set("EIO", "4")
	query.Set("transport", "websocket")
	target.RawQuery = query.Encode()
	header := http.Header{}
	if config.AuthToken != "" {
		header.Set("Authorization", "Bearer "+config.AuthToken)
	}
	connection, _, err := websocket.Dial(ctx, target.String(), &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		return err
	}
	defer connection.CloseNow()
	connection.SetReadLimit(1024 * 1024)
	var writeMu sync.Mutex
	writePacket := func(value string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return connection.Write(writeContext, websocket.MessageText, []byte(value))
	}
	writeEvent := func(name string, payload any) error {
		encoded, err := json.Marshal([]any{name, payload})
		if err != nil {
			return err
		}
		return writePacket("42" + string(encoded))
	}
	_, opened, err := connection.Read(ctx)
	if err != nil || !strings.HasPrefix(string(opened), "0{") {
		return fmt.Errorf("Socket.IO peer did not send Engine.IO open packet: %w", err)
	}
	if err := writePacket("40"); err != nil {
		return err
	}
	for {
		_, connected, err := connection.Read(ctx)
		if err != nil {
			return err
		}
		packet := string(connected)
		if packet == "2" {
			_ = writePacket("3")
			continue
		}
		if strings.HasPrefix(packet, "40") {
			break
		}
	}
	rpcSession := newPeerRPCSession(func(value any) error {
		return writeEvent("rpc", value)
	})
	detach := peer.attach(rpcSession)
	defer detach()
	topics := append([]string(nil), config.Topics...)
	if len(topics) == 0 {
		topics = []string{"events"}
	}
	if err := writeEvent("subscribe", map[string]any{"topics": topics}); err != nil {
		return err
	}
	sessionContext, cancel := context.WithCancel(ctx)
	defer cancel()
	writeErrors := make(chan error, 1)
	if config.ForwardEvents {
		go func() {
			for {
				select {
				case <-sessionContext.Done():
					return
				case event := <-peer.events:
					encoded, _ := json.Marshal(event)
					err := writeEvent("message", controller.TextMessage{
						Source: "bridge", Target: "host", Type: "local-event",
						Text: string(encoded),
					})
					if err != nil {
						select {
						case writeErrors <- err:
						default:
						}
						return
					}
				}
			}
		}()
	}
	for {
		select {
		case err := <-writeErrors:
			return err
		default:
		}
		messageType, data, err := connection.Read(sessionContext)
		if err != nil {
			return err
		}
		if messageType != websocket.MessageText {
			continue
		}
		packet := string(data)
		if packet == "2" {
			if err := writePacket("3"); err != nil {
				return err
			}
			continue
		}
		if !strings.HasPrefix(packet, "42") {
			continue
		}
		name, raw, err := decodeSocketIOPacket(packet[2:])
		if err != nil {
			continue
		}
		switch name {
		case "rpc.response":
			var response ipcjson.Response
			if json.Unmarshal(raw, &response) == nil {
				_ = rpcSession.Resolve(response)
			}
		case "controller.event", "controller.status", "message.accepted":
			_, _ = manager.client.SendTextMessage(ctx, controller.TextMessage{
				Source: "websocket", Target: "host", Type: "remote-event",
				Text: string(raw),
			})
		case "message", "controller.message":
			var message controller.TextMessage
			if json.Unmarshal(raw, &message) == nil {
				if message.Metadata == nil {
					message.Metadata = make(map[string]string)
				}
				if message.Source != "" && !strings.EqualFold(message.Source, "bridge") {
					if _, exists := message.Metadata["claimed_source"]; exists || len(message.Metadata) < 64 {
						message.Metadata["claimed_source"] = message.Source
					}
				}
				message.Source = "bridge"
				_, _ = manager.client.SendTextMessage(ctx, message)
			}
		case "command":
			if !config.AllowCommands {
				_ = writeEvent("error", map[string]string{"error": "peer commands are disabled"})
				continue
			}
			var command struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(raw, &command) != nil {
				continue
			}
			encoded, _ := json.Marshal(command)
			service := manager.remotePeerService()
			response := service.DispatchRemote(ctx, ipcjson.Request{
				JSONRPC: ipcjson.Version, ID: json.RawMessage("1"),
				Method: "controller.execute", Params: encoded,
			}, "bridge")
			_ = writeEvent("command.response", response)
		case "rpc":
			if !config.AllowCommands {
				_ = writeEvent("error", map[string]string{"error": "peer RPC is disabled"})
				continue
			}
			var request ipcjson.Request
			if json.Unmarshal(raw, &request) != nil {
				continue
			}
			service := manager.remotePeerService()
			_ = writeEvent("rpc.response", service.DispatchRemote(ctx, request, "bridge"))
		}
	}
}

func decodeSocketIOPacket(value string) (string, json.RawMessage, error) {
	var parts []json.RawMessage
	if err := json.Unmarshal([]byte(value), &parts); err != nil || len(parts) != 2 {
		return "", nil, fmt.Errorf("invalid Socket.IO event packet")
	}
	var name string
	if err := json.Unmarshal(parts[0], &name); err != nil {
		return "", nil, err
	}
	return name, parts[1], nil
}

func (manager *Manager) remotePeerService() ipcjson.Service {
	return ipcjson.Service{
		Client:     manager.client,
		HostConfig: manager.store.Current,
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			_, err := manager.store.Update(change)
			return err
		},
	}
}

func firstProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "jsonrpc"
	}
	return value
}
