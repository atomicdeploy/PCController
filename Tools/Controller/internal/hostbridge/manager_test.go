package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/shell"
)

func TestWaitForIntegrationShutdownIsBounded(t *testing.T) {
	var completed sync.WaitGroup
	completed.Add(1)
	if waitForIntegrationShutdown(&completed, 15*time.Millisecond) {
		t.Fatal("unfinished integration workers were reported as stopped")
	}
	completed.Done()
	if !waitForIntegrationShutdown(&completed, time.Second) {
		t.Fatal("completed integration workers timed out")
	}
}

func TestEnabledTextMappingExecutesAllowlistedCommandOnly(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.TextMappings = []appconfig.TextMapping{{
			Name: "trusted-door", Enabled: true,
			Source: "ipc", Target: "host", Type: "door-command",
			Contains: "open", Command: "mark",
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	engine := shell.New(8)
	called := make(chan struct{}, 1)
	if err := engine.Register(shell.Command{
		Name: "mark", Usage: "mark", Summary: "test mapping",
		Run: func(context.Context, []string) (string, error) {
			called <- struct{}{}
			return "marked", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := controller.AttachIsolatedRuntime(runtime, engine)
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, client, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		manager.Close()
	}()
	_, err = client.SendTextMessage(ctx, controller.TextMessage{
		Source: "ipc", Target: "host", Type: "door-command",
		Text: "door open", Action: "this descriptive text is never executed",
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("enabled source/target/type text mapping did not execute")
	}
}

func TestConfiguredToastActionsBecomeActionableProtocolURIs(t *testing.T) {
	notification, err := configuredNotificationActions(
		hostui.Notification{Title: "Door", Body: "opened"},
		[]appconfig.NotificationAction{
			{ID: "events", Label: "Show timeline", Command: "app page events"},
			{ID: "stop", Label: "Stop outputs", Command: "relay off"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(notification.Actions) != 2 ||
		notification.Actions[0].Label != "Show timeline" ||
		notification.Actions[0].URI != "pccontroller://page/events" ||
		notification.Actions[1].URI != "pccontroller://command/relay%20off" {
		t.Fatalf("actions=%#v", notification.Actions)
	}
}

func TestRunningDoorWarningRequiresConnectedLiveDoorAndExplicitRunningState(t *testing.T) {
	snapshot := controller.Snapshot{
		Connected: true, HaveStatus: true,
		Status:       controller.Status{DoorOpen: true},
		ProgramState: controller.ProgramStateSnapshot{Mode: control.ProgramRunning},
	}
	if !runningDoorCondition(snapshot) {
		t.Fatal("connected Running state with an open door did not warn")
	}
	for name, mutate := range map[string]func(*controller.Snapshot){
		"idle":      func(value *controller.Snapshot) { value.ProgramState.Mode = control.ProgramIdle },
		"closed":    func(value *controller.Snapshot) { value.Status.DoorOpen = false },
		"offline":   func(value *controller.Snapshot) { value.Connected = false },
		"no-status": func(value *controller.Snapshot) { value.HaveStatus = false },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := snapshot
			mutate(&candidate)
			if runningDoorCondition(candidate) {
				t.Fatalf("%s state incorrectly warned: %#v", name, candidate)
			}
		})
	}
}

func TestPeerRPCSessionCorrelatesResponseAndPreservesCallerID(t *testing.T) {
	writes := make(chan ipcjson.Request, 1)
	session := newPeerRPCSession(func(value any) error {
		request, ok := value.(ipcjson.Request)
		if !ok {
			t.Fatalf("wire value=%T", value)
		}
		writes <- request
		return nil
	})
	defer session.Close(nil)
	type callResult struct {
		response ipcjson.Response
		err      error
	}
	result := make(chan callResult, 1)
	go func() {
		response, err := session.Call(context.Background(), ipcjson.Request{
			JSONRPC: ipcjson.Version, ID: json.RawMessage("77"),
			Method: "controller.snapshot",
		})
		result <- callResult{response: response, err: err}
	}()
	request := <-writes
	if string(request.ID) == "77" || request.Method != "controller.snapshot" {
		t.Fatalf("uncorrelated wire request=%#v", request)
	}
	if !session.Resolve(ipcjson.Response{
		JSONRPC: ipcjson.Version, ID: request.ID,
		Result: map[string]bool{"connected": true},
	}) {
		t.Fatal("wire response did not resolve")
	}
	select {
	case completed := <-result:
		if completed.err != nil || string(completed.response.ID) != "77" {
			t.Fatalf("call response=%#v err=%v", completed.response, completed.err)
		}
	case <-time.After(time.Second):
		t.Fatal("correlated bridge call timed out")
	}
}

func TestRelayTrackerAllowsCascadeAndRejectsCycle(t *testing.T) {
	trace := ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Limit: 8,
	}
	var first, second relayTracker
	one, err := first.advance(&trace)
	if err != nil || one.Hops != 1 {
		t.Fatalf("first hop=%#v err=%v", one, err)
	}
	two, err := second.advance(&one)
	if err != nil || two.Hops != 2 {
		t.Fatalf("second hop=%#v err=%v", two, err)
	}
	if _, err := first.advance(&two); err == nil ||
		!strings.Contains(err.Error(), "already crossed") {
		t.Fatalf("cycle was not rejected: %v", err)
	}
}

func TestRelayTrackerRejectsHopLimit(t *testing.T) {
	tracker := relayTracker{}
	trace := ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Hops: 2, Limit: 2,
	}
	if _, err := tracker.advance(&trace); err == nil ||
		!strings.Contains(err.Error(), "hop limit") {
		t.Fatalf("over-budget relay was not rejected: %v", err)
	}
}

func TestCascadedBridgeCallUsesExplicitAlphaAuthorizationMode(t *testing.T) {
	newManager := func(name string, policy appconfig.RemoteAccessPolicy) *Manager {
		store, err := appconfig.Open(filepath.Join(t.TempDir(), name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Update(func(config *appconfig.Config) error {
			config.IPC.AllowRemote = true
			config.IPC.AuthToken = "relay-test-token-0123456789abcdef"
			config.IPC.AllowedOrigins = []string{"localhost:*"}
			config.IPC.RemotePolicy = policy
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return &Manager{
			client: controller.AttachIsolatedRuntime(control.New(control.Options{}), shell.New(8)),
			store:  store, peers: make(map[string]*peerState),
		}
	}
	first := newManager("first", appconfig.DefaultRemoteAccessPolicy())
	middlePolicy := appconfig.DefaultRemoteAccessPolicy()
	middle := newManager("middle", middlePolicy)
	edgePolicy := appconfig.DefaultRemoteAccessPolicy()
	edgePolicy.Read = false
	edge := newManager("edge", edgePolicy)

	connect := func(source, target *Manager, name string) *peerRPCSession {
		var session *peerRPCSession
		session = newPeerRPCSession(func(value any) error {
			request, ok := value.(ipcjson.Request)
			if !ok {
				return fmt.Errorf("wire request type %T", value)
			}
			service := target.remotePeerService()
			response := service.DispatchRemote(
				context.Background(), request, "bridge",
			)
			if !session.Resolve(response) {
				return errors.New("wire response was not correlated")
			}
			return nil
		})
		source.peers[strings.ToLower(name)] = &peerState{name: name, session: session}
		return session
	}
	firstSession := connect(first, middle, "middle")
	middleSession := connect(middle, edge, "edge")
	defer firstSession.Close(nil)
	defer middleSession.Close(nil)

	nested, _ := json.Marshal(map[string]any{
		"peer": "edge",
		"request": map[string]any{
			"jsonrpc": ipcjson.Version, "id": 3, "method": "controller.snapshot",
		},
	})
	call := func() ipcjson.Response {
		response, err := first.CallBridge(context.Background(), "middle", ipcjson.Request{
			JSONRPC: ipcjson.Version, ID: json.RawMessage("1"),
			Method: "controller.bridge.call", Params: nested,
		})
		if err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := call()
	encoded, _ := json.Marshal(response)
	if response.Error != nil || !strings.Contains(string(encoded), `"connected":false`) {
		t.Fatalf("cascaded read response=%s", encoded)
	}
	for name, manager := range map[string]*Manager{"middle": middle, "edge": edge} {
		if service := manager.remotePeerService(); !service.AuthorizationDisabled {
			t.Fatalf("%s bridge service did not opt into alpha authorization mode", name)
		}
	}

	strictEdge := edge.remotePeerService()
	strictEdge.AuthorizationDisabled = false
	strictResponse := strictEdge.DispatchRemote(
		context.Background(), ipcjson.Request{Method: "controller.snapshot"}, "bridge",
	)
	if strictResponse.Error == nil || strictResponse.Error.Code != -32003 ||
		!strings.Contains(strictResponse.Error.Message, "read") {
		t.Fatalf("zero-value strict edge policy was not preserved: %#v", strictResponse)
	}
}

func TestRemoteNotificationPreservesRelayAndLegacyBridgeEventsStayLocal(t *testing.T) {
	manager := &Manager{}
	trace := ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Hops: 3, Limit: 8,
	}
	encoded, _ := json.Marshal(controller.Event{
		ID: 44, Kind: "message", Source: "websocket", Text: "relayed",
		Metadata: relayMetadata(trace, controller.Event{ID: 44, Kind: "door"}),
	})
	message, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", encoded,
	)
	if err != nil {
		t.Fatal(err)
	}
	want := trace
	want.Hops++
	got, present, err := relayTraceFromMetadata(message.Metadata)
	if err != nil || !present || got == nil || *got != want {
		t.Fatalf("preserved trace=%#v present=%t err=%v", got, present, err)
	}
	if message.Metadata[relayEventKindMetadata] != "door" ||
		message.Metadata[relayEventSourceMetadata] != "" {
		t.Fatalf("original event identity was not preserved: %#v", message.Metadata)
	}

	legacy, _ := json.Marshal(controller.Event{
		ID: 45, Kind: "message", Source: "bridge", Text: "legacy",
	})
	message, err = manager.remoteNotificationMessage(
		"websocket", "controller.event", legacy,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, present, _ := relayTraceFromMetadata(message.Metadata); present {
		t.Fatalf("legacy bridge event unexpectedly became repeatable: %#v", message)
	}
}

func TestRelayIngressCycleDiamondAndReplayRunLocalSideEffectsOnce(t *testing.T) {
	manager := &Manager{}
	incoming := ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Hops: 1, Limit: 8,
	}
	remote := controller.Event{ID: 61, Kind: "door", Source: "board", Text: "open"}
	remote.Metadata = relayMetadata(incoming, remote)
	raw, _ := json.Marshal(remote)
	message, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", raw,
	)
	if err != nil {
		t.Fatal(err)
	}
	local := controller.Event{
		ID: 62, Kind: "message", Source: message.Source,
		Text: message.Text, Metadata: message.Metadata,
	}

	localSideEffects := 0
	for _, delivery := range []string{"first", "cycle", "diamond", "replay"} {
		relay, process := manager.prepareRelayEvent(local)
		if process {
			localSideEffects++
		}
		if delivery == "first" {
			if !process || relay == nil || relay.Hops != 2 {
				t.Fatalf("first delivery process=%t relay=%#v", process, relay)
			}
		} else if process || relay != nil {
			t.Fatalf("%s duplicate was accepted: process=%t relay=%#v", delivery, process, relay)
		}
	}
	if localSideEffects != 1 {
		t.Fatalf("local side effects ran %d times, want 1", localSideEffects)
	}
	if _, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", raw,
	); err == nil || !strings.Contains(err.Error(), "already crossed") {
		t.Fatalf("upstream replay was not rejected before publication: %v", err)
	}
}

func TestRelayDuplicateCannotRepeatTextMappingSideEffect(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.TextMappings = []appconfig.TextMapping{{
			Name: "remote-door", Enabled: true,
			Source: "websocket", Target: "host", Type: "remote-event",
			Contains: "open", Command: "mark",
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	engine := shell.New(8)
	called := make(chan struct{}, 8)
	if err := engine.Register(shell.Command{
		Name: "mark", Usage: "mark", Summary: "relay side-effect test",
		Run: func(context.Context, []string) (string, error) {
			called <- struct{}{}
			return "marked", nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := controller.AttachIsolatedRuntime(control.New(control.Options{}), engine)
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, client, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		manager.Close()
	}()

	trace := ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Hops: 1, Limit: 8,
	}
	remote := controller.Event{ID: 81, Kind: "door", Source: "board", Text: "open"}
	remote.Metadata = relayMetadata(trace, remote)
	raw, _ := json.Marshal(remote)
	message, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", raw,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.SendTextMessage(ctx, message); err != nil {
		t.Fatal(err)
	}
	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("first accepted relay event did not run its mapping")
	}
	for duplicate := 0; duplicate < 3; duplicate++ {
		if _, err := client.SendTextMessage(ctx, message); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-called:
		t.Fatal("cycle/diamond/replay duplicate repeated the text-mapping side effect")
	case <-time.After(400 * time.Millisecond):
	}
}

func TestSensitiveRemoteEventKindsStayLocalAndPreserveIdentity(t *testing.T) {
	for _, kind := range []string{
		"security.remote.denied",
		"security.local.authorized",
		"bridge.call",
		"bridge.call.error",
		"integration.error",
		"controller.error",
		"transport.error",
	} {
		t.Run(kind, func(t *testing.T) {
			manager := &Manager{}
			trace := ipcjson.RelayTrace{
				ID: "00112233445566778899aabbccddeeff", Hops: 1, Limit: 8,
			}
			remote := controller.Event{ID: 71, Kind: kind, Source: "remote-host"}
			remote.Metadata = relayMetadata(trace, remote)
			raw, _ := json.Marshal(remote)
			message, err := manager.remoteNotificationMessage(
				"websocket", "controller.event", raw,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, present, err := relayTraceFromMetadata(message.Metadata); err != nil || present {
				t.Fatalf("sensitive event retained relay trace: present=%t err=%v metadata=%#v", present, err, message.Metadata)
			}
			if message.Metadata[relayEventKindMetadata] != kind ||
				message.Metadata[relayEventSourceMetadata] != "remote-host" {
				t.Fatalf("original kind/source lost: %#v", message.Metadata)
			}

			wrapped := controller.Event{
				Kind: "message", Source: "websocket", Metadata: message.Metadata,
			}
			if bridgeEventForwardable(wrapped) {
				t.Fatalf("sensitive wrapped event became forwardable: %#v", wrapped)
			}
			wrapped.Metadata = relayMetadata(trace, wrapped)
			if bridgeEventForwardable(wrapped) {
				t.Fatalf("forged trace made sensitive event forwardable: %#v", wrapped)
			}

			generic := controller.Event{
				ID: 72, Kind: "message", Source: "remote-host",
				Metadata: message.Metadata,
			}
			genericRaw, _ := json.Marshal(generic)
			genericMessage, err := manager.remoteNotificationMessage(
				"websocket", "controller.event", genericRaw,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, present, err := relayTraceFromMetadata(genericMessage.Metadata); err != nil || present {
				t.Fatalf("generic wrapper assigned a trace to sensitive kind: present=%t err=%v metadata=%#v", present, err, genericMessage.Metadata)
			}
		})
	}
}

func TestMalformedRemoteEventIsRejectedBeforePublication(t *testing.T) {
	manager := &Manager{}
	if _, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", json.RawMessage(`{"kind":`),
	); err == nil || !strings.Contains(err.Error(), "decode remote event") {
		t.Fatalf("malformed event was accepted: %v", err)
	}
	event := controller.Event{
		Kind: "door", Metadata: map[string]string{relayTraceMetadata: "short"},
	}
	raw, _ := json.Marshal(event)
	if _, err := manager.remoteNotificationMessage(
		"websocket", "controller.event", raw,
	); err == nil {
		t.Fatal("partial relay metadata was accepted")
	}
}

func TestTracedRemoteEventCanRepeatButUntracedCannot(t *testing.T) {
	event := controller.Event{Kind: "message", Source: "websocket"}
	if bridgeEventForwardable(event) {
		t.Fatal("untraced remote message was repeatable")
	}
	event.Metadata = relayMetadata(ipcjson.RelayTrace{
		ID: "00112233445566778899aabbccddeeff", Hops: 1, Limit: 8,
	}, event)
	if !bridgeEventForwardable(event) {
		t.Fatal("valid traced remote message was not repeatable")
	}
}

func TestOutboundBridgeCanCallRemoteJSONRPCService(t *testing.T) {
	const token = "bridge-test-token"
	forwarded := make(chan controller.TextMessage, 16)
	remote := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.Header.Get("Authorization") != "Bearer "+token {
			http.Error(writer, "authentication required", http.StatusUnauthorized)
			return
		}
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		for {
			messageType, payload, err := connection.Read(request.Context())
			if err != nil {
				return
			}
			if messageType != websocket.MessageText {
				continue
			}
			var rpcRequest ipcjson.Request
			if json.Unmarshal(payload, &rpcRequest) != nil {
				continue
			}
			if rpcRequest.Method == "controller.subscribe" {
				continue
			}
			if rpcRequest.Method == "controller.message.send" {
				var message controller.TextMessage
				if json.Unmarshal(rpcRequest.Params, &message) == nil {
					forwarded <- message
				}
			}
			encoded, _ := json.Marshal(ipcjson.Response{
				JSONRPC: ipcjson.Version, ID: rpcRequest.ID,
				Result: map[string]any{"remote": true, "method": rpcRequest.Method},
			})
			if connection.Write(
				request.Context(), websocket.MessageText, encoded,
			) != nil {
				return
			}
		}
	}))
	defer remote.Close()

	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Update(func(config *appconfig.Config) error {
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.WebSocketClients = []appconfig.WebSocketClient{{
			Name: "remote-lab", Enabled: true,
			URL:      strings.Replace(remote.URL, "http://", "ws://", 1),
			Protocol: "jsonrpc", AuthToken: token, Topics: []string{"events"},
			ForwardEvents: true,
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	client := controller.AttachIsolatedRuntime(runtime, shell.New(8))
	ctx, cancel := context.WithCancel(context.Background())
	manager, err := Start(ctx, client, store, nil)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer func() {
		cancel()
		manager.Close()
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		peers := manager.BridgePeers()
		if len(peers) == 1 && peers[0].Connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("bridge did not connect: %#v", peers)
		}
		time.Sleep(10 * time.Millisecond)
	}
	callContext, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	response, err := manager.CallBridge(callContext, "REMOTE-LAB", ipcjson.Request{
		JSONRPC: ipcjson.Version, ID: json.RawMessage("42"),
		Method: "controller.snapshot",
	})
	if err != nil || response.Error != nil || string(response.ID) != "42" {
		t.Fatalf("bridge response=%#v err=%v", response, err)
	}
	encoded, _ := json.Marshal(response.Result)
	if !strings.Contains(string(encoded), `"remote":true`) {
		t.Fatalf("bridge result=%s", encoded)
	}
	runtime.PublishHostEvent("door", "door opened")
	forwardDeadline := time.After(time.Second)
	for {
		select {
		case message := <-forwarded:
			if message.Metadata["event.kind"] == "door" {
				if message.Source != "bridge" || message.Type != "local-event" {
					t.Fatalf("forwarded event=%#v", message)
				}
				return
			}
		case <-forwardDeadline:
			t.Fatal("local event was not forwarded through correlated message RPC")
		}
	}
}

func TestOutboundWebhooksSupportConfiguredHTTPMethods(t *testing.T) {
	type observedRequest struct {
		method      string
		body        string
		kind        string
		testHeader  string
		contentType string
	}
	observed := make(chan observedRequest, 5)
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		payload, _ := io.ReadAll(request.Body)
		_ = request.Body.Close()
		observed <- observedRequest{
			method: request.Method, body: string(payload),
			kind:        request.URL.Query().Get("kind"),
			testHeader:  request.Header.Get("X-Wire-Test"),
			contentType: request.Header.Get("Content-Type"),
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := control.New(control.Options{})
	client := controller.AttachIsolatedRuntime(runtime, shell.New(8))
	manager := &Manager{ctx: context.Background(), client: client}
	event := controller.Event{ID: 7, Kind: "door", Text: "opened", Source: "board"}
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		webhook := appconfig.Webhook{
			Name: "test-" + method, URL: server.URL, Method: method,
			Headers: map[string]string{"X-Wire-Test": "multi-method"},
		}
		if method == http.MethodPatch {
			webhook.BodyTemplate = `{"event":"{{kind}}","origin":"{{source}}"}`
		}
		manager.sendWebhook(webhook, event)
		select {
		case request := <-observed:
			if request.method != method {
				t.Fatalf("method=%s want=%s", request.method, method)
			}
			if request.testHeader != "multi-method" {
				t.Fatalf("configured header was not delivered: %#v", request)
			}
			if method == http.MethodGet || method == http.MethodDelete {
				if request.kind != "door" || request.body != "" {
					t.Fatalf("query webhook=%#v", request)
				}
			} else if request.contentType != "application/json" {
				t.Fatalf("body webhook content type=%#v", request)
			} else if method == http.MethodPatch && request.body != `{"event":"door","origin":"board"}` {
				t.Fatalf("templated webhook=%#v", request)
			} else if method != http.MethodPatch && !strings.Contains(request.body, `"kind":"door"`) {
				t.Fatalf("body webhook=%#v", request)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s webhook was not delivered", method)
		}
	}
}
