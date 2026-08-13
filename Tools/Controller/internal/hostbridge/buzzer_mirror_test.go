package hostbridge

import (
	"context"
	"encoding/json"
	"fmt"
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
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/shell"
)

func TestAuthenticatedPeerStateStreamMirrorsBuzzerOnceAndCannotLoop(t *testing.T) {
	for _, protocol := range []string{"jsonrpc", "socketio"} {
		t.Run(protocol, func(t *testing.T) {
			const token = "peer-state-test-token"
			subscribed := make(chan []string, 1)
			release := make(chan struct{})
			stopServer := make(chan struct{})
			serverErrors := make(chan error, 1)
			reportServerError := func(err error) {
				select {
				case serverErrors <- err:
				default:
				}
			}
			remote := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter,
				request *http.Request,
			) {
				if request.Header.Get("Authorization") != "Bearer "+token {
					reportServerError(fmt.Errorf("authorization=%q", request.Header.Get("Authorization")))
					http.Error(writer, "authentication required", http.StatusUnauthorized)
					return
				}
				connection, err := websocket.Accept(writer, request, nil)
				if err != nil {
					reportServerError(err)
					return
				}
				defer connection.CloseNow()
				var topics []string
				if protocol == "socketio" {
					err = serveSocketIOStateSubscription(request.Context(), connection, &topics)
				} else {
					err = serveJSONRPCStateSubscription(request.Context(), connection, &topics)
				}
				if err != nil {
					reportServerError(err)
					return
				}
				subscribed <- topics
				select {
				case <-release:
				case <-stopServer:
					return
				case <-request.Context().Done():
					return
				}
				event := controller.Event{
					ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board",
					Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"},
				}
				if protocol == "socketio" {
					encoded, _ := json.Marshal([]any{"controller.state", event})
					err = connection.Write(request.Context(), websocket.MessageText, append([]byte("42"), encoded...))
				} else {
					params, _ := json.Marshal(event)
					encoded, _ := json.Marshal(ipcjson.Request{
						JSONRPC: ipcjson.Version, Method: "controller.state", Params: params,
					})
					err = connection.Write(request.Context(), websocket.MessageText, encoded)
				}
				if err != nil {
					reportServerError(err)
					return
				}
				select {
				case <-stopServer:
				case <-request.Context().Done():
				}
			}))

			store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
			if err != nil {
				close(stopServer)
				remote.Close()
				t.Fatal(err)
			}
			_, err = store.Update(func(config *appconfig.Config) error {
				config.Integrations.Hotkeys = nil
				config.Integrations.Notifications.Enabled = false
				config.Integrations.BuzzerMirror.Enabled = true
				config.Integrations.BuzzerMirror.NativeEnabled = true
				config.Integrations.WebSocketClients = []appconfig.WebSocketClient{{
					Name: "edge", Enabled: true,
					URL:      strings.Replace(remote.URL, "http://", "ws://", 1),
					Protocol: protocol, AuthToken: token,
				}}
				return nil
			})
			if err != nil {
				close(stopServer)
				remote.Close()
				t.Fatal(err)
			}
			runtime := control.New(control.Options{})
			client := controller.AttachSharedRuntime(runtime, shell.New(8))
			ctx, cancel := context.WithCancel(context.Background())
			manager, err := Start(ctx, client, store, nil)
			if err != nil {
				cancel()
				close(stopServer)
				remote.Close()
				runtime.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				close(stopServer)
				cancel()
				manager.Close()
				remote.Close()
				runtime.Close()
			})

			played := make(chan buzzerMirrorJob, 2)
			manager.mu.Lock()
			manager.buzzerPlayer = func(_ context.Context, job buzzerMirrorJob) error {
				played <- job
				return nil
			}
			manager.mu.Unlock()
			select {
			case topics := <-subscribed:
				if strings.Join(topics, ",") != "events,state" {
					t.Fatalf("default subscription topics=%v", topics)
				}
			case err := <-serverErrors:
				t.Fatal(err)
			case <-time.After(3 * time.Second):
				t.Fatal("peer did not authenticate and subscribe")
			}
			after := runtime.LatestEventID()
			close(release)
			select {
			case job := <-played:
				if job.frequencyHz != 880 || job.durationMS != 125 {
					t.Fatalf("played job=%+v", job)
				}
			case err := <-serverErrors:
				t.Fatal(err)
			case <-time.After(3 * time.Second):
				t.Fatal("structured state-stream buzzer note was not mirrored")
			}
			waitContext, waitCancel := context.WithTimeout(ctx, time.Second)
			defer waitCancel()
			event, err := runtime.WaitEvent(waitContext, after, "buzzer.note")
			if err != nil {
				t.Fatal(err)
			}
			if event.Source != "bridge" || event.Metadata["bridge.ingress"] != "edge" ||
				event.Metadata["bridge.original_source"] != "board" {
				t.Fatalf("peer event provenance=%#v source=%q", event.Metadata, event.Source)
			}
			if bridgeEventForwardable(controller.Event{Kind: event.Kind, Metadata: event.Metadata}) {
				t.Fatal("ingressed state event could be forwarded into a bridge cycle")
			}
			select {
			case duplicate := <-played:
				t.Fatalf("one peer state event played more than once: %+v", duplicate)
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
}

func serveJSONRPCStateSubscription(
	ctx context.Context,
	connection *websocket.Conn,
	topics *[]string,
) error {
	messageType, payload, err := connection.Read(ctx)
	if err != nil {
		return err
	}
	if messageType != websocket.MessageText {
		return fmt.Errorf("JSON-RPC subscription used WebSocket message type %d", messageType)
	}
	var request ipcjson.Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return err
	}
	if request.Method != "controller.subscribe" {
		return fmt.Errorf("JSON-RPC subscription method=%q", request.Method)
	}
	var params struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return err
	}
	*topics = params.Topics
	return nil
}

func serveSocketIOStateSubscription(
	ctx context.Context,
	connection *websocket.Conn,
	topics *[]string,
) error {
	if err := connection.Write(ctx, websocket.MessageText, []byte(
		`0{"sid":"state-test","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`,
	)); err != nil {
		return err
	}
	_, packet, err := connection.Read(ctx)
	if err != nil {
		return err
	}
	if string(packet) != "40" {
		return fmt.Errorf("Socket.IO connect packet=%q", packet)
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
		return err
	}
	_, packet, err = connection.Read(ctx)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(packet), "42") {
		return fmt.Errorf("Socket.IO subscription packet=%q", packet)
	}
	name, raw, err := decodeSocketIOPacket(string(packet[2:]))
	if err != nil {
		return err
	}
	if name != "subscribe" {
		return fmt.Errorf("Socket.IO subscription event=%q", name)
	}
	var params struct {
		Topics []string `json:"topics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return err
	}
	*topics = params.Topics
	return nil
}

func TestAuthenticatedPeerBuzzerEventStaysStructuredAndLoopSafe(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()
	raw, _ := json.Marshal(controller.Event{
		ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board",
		Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"},
	})
	if !manager.ingestPeerEvent("cafe-pc", raw) {
		t.Fatal("valid peer event was not accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := runtime.WaitEvent(ctx, after, "buzzer.note")
	if err != nil {
		t.Fatal(err)
	}
	if event.Metadata["bridge.ingress"] != "cafe-pc" ||
		event.Metadata["bridge.original_source"] != "board" ||
		event.Source != "bridge" {
		t.Fatalf("peer provenance=%#v source=%q", event.Metadata, event.Source)
	}
	if bridgeEventForwardable(controller.Event{Kind: event.Kind, Metadata: event.Metadata}) {
		t.Fatal("ingressed event could be forwarded into a bridge cycle")
	}
	config := appconfig.DefaultBuzzerMirror()
	config.Enabled, config.NativeEnabled = true, true
	if job, ok := buzzerMirrorJobFor(config, controller.Event{Kind: event.Kind, Metadata: event.Metadata}); !ok || job.frequencyHz != 880 || job.durationMS != 125 {
		t.Fatalf("mirrored job=%+v ok=%t", job, ok)
	}
}

func TestBuzzerMirrorJobRequiresOptInAndValidBoardNote(t *testing.T) {
	config := appconfig.DefaultBuzzerMirror()
	event := controller.Event{Kind: "buzzer.note", Metadata: map[string]string{
		"frequency_hz": "440", "duration_ms": "220", "muted": "false",
	}}
	if _, ok := buzzerMirrorJobFor(config, event); ok {
		t.Fatal("disabled mirror accepted a note")
	}
	config.Enabled = true
	config.NativeEnabled = true
	config.DriverDirectory = `C:\optional\winring0`
	job, ok := buzzerMirrorJobFor(config, event)
	if !ok || job.frequencyHz != 440 || job.durationMS != 220 {
		t.Fatalf("job=%+v ok=%t", job, ok)
	}
	event.Metadata["muted"] = "true"
	if _, ok := buzzerMirrorJobFor(config, event); !ok {
		t.Fatal("board-silent note did not reach the independently enabled host path")
	}
}

func TestNativeBuzzerFailuresAreStateTransitionsNotPerNoteLogSpam(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controller.AttachSharedRuntime(runtime, shell.New(1))
	manager := &Manager{client: client}
	after := runtime.LatestEventID()

	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	first := runtime.LatestEventID()
	if first != after+1 || manager.Status().BuzzerNativeState != "failed" {
		t.Fatalf("first failure id=%d status=%#v", first, manager.Status())
	}
	manager.recordNativeBuzzerResult(context.DeadlineExceeded)
	if runtime.LatestEventID() != first {
		t.Fatal("identical per-note failure emitted another activity event")
	}
	manager.recordNativeBuzzerResult(nil)
	if runtime.LatestEventID() != first+1 || manager.Status().BuzzerNativeState != "ready" {
		t.Fatalf("recovery id=%d status=%#v", runtime.LatestEventID(), manager.Status())
	}
}
