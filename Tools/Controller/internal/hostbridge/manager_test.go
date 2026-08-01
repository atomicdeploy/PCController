package hostbridge

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	client := controller.AttachSharedRuntime(runtime, engine)
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
	client := controller.AttachSharedRuntime(runtime, shell.New(8))
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
		method string
		body   string
		kind   string
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
			kind: request.URL.Query().Get("kind"),
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(8))
	manager := &Manager{ctx: context.Background(), client: client}
	event := controller.Event{ID: 7, Kind: "door", Text: "opened", Source: "board"}
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		manager.sendWebhook(appconfig.Webhook{
			Name: "test-" + method, URL: server.URL, Method: method,
		}, event)
		select {
		case request := <-observed:
			if request.method != method {
				t.Fatalf("method=%s want=%s", request.method, method)
			}
			if method == http.MethodGet || method == http.MethodDelete {
				if request.kind != "door" || request.body != "" {
					t.Fatalf("query webhook=%#v", request)
				}
			} else if !strings.Contains(request.body, `"kind":"door"`) {
				t.Fatalf("body webhook=%#v", request)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s webhook was not delivered", method)
		}
	}
}
