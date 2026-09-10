package hostbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestPeerDefaultStateSubscriptionIngestsBuzzerWithoutThirdHop(t *testing.T) {
	for _, protocol := range []string{"jsonrpc", "socketio"} {
		t.Run(protocol, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errors := make(chan error, 1)
			stop := make(chan struct{})
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, err := websocket.Accept(w, r, nil)
				if err != nil {
					errors <- err
					return
				}
				defer connection.CloseNow()
				read := func() ([]byte, error) { _, raw, err := connection.Read(ctx); return raw, err }
				write := func(raw []byte) error { return connection.Write(ctx, websocket.MessageText, raw) }
				var params json.RawMessage
				if protocol == "socketio" {
					if err = write([]byte(`0{"sid":"state","upgrades":[],"pingInterval":25000,"pingTimeout":20000}`)); err != nil {
						errors <- err
						return
					}
					var raw []byte
					if raw, err = read(); err != nil || string(raw) != "40" {
						errors <- fmt.Errorf("connect %s: %v", raw, err)
						return
					}
					if err = write([]byte("40")); err != nil {
						errors <- err
						return
					}
					if raw, err = read(); err != nil {
						errors <- err
						return
					}
					if !strings.HasPrefix(string(raw), "42") {
						errors <- fmt.Errorf("subscription %s", raw)
						return
					}
					var name string
					name, params, err = decodeSocketIOPacket(string(raw[2:]))
					if err != nil || name != "subscribe" {
						errors <- fmt.Errorf("subscribe %s: %v", name, err)
						return
					}
				} else {
					raw, readErr := read()
					if readErr != nil {
						errors <- readErr
						return
					}
					var request ipcjson.Request
					if err = json.Unmarshal(raw, &request); err != nil || request.Method != "controller.subscribe" {
						errors <- fmt.Errorf("subscribe %s: %v", raw, err)
						return
					}
					params = request.Params
				}
				var subscription struct {
					Topics []string `json:"topics"`
				}
				if err = json.Unmarshal(params, &subscription); err != nil || strings.Join(subscription.Topics, ",") != "events,state" {
					errors <- fmt.Errorf("topics %s: %v", params, err)
					return
				}
				event := controller.Event{ID: 41, Kind: "buzzer.note", Stream: "state", Source: "board", Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"}}
				var raw []byte
				if protocol == "socketio" {
					raw, _ = json.Marshal([]any{"controller.state", event})
					raw = append([]byte("42"), raw...)
				} else {
					raw, _ = json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "controller.state", "params": event})
				}
				if err = write(raw); err != nil {
					errors <- err
					return
				}
				select {
				case <-stop:
				case <-ctx.Done():
				}
			}))
			defer remote.Close()
			defer close(stop)
			runtime := control.New(control.Options{})
			defer runtime.Close()
			client := controller.AttachSharedRuntime(runtime, shell.New(8))
			manager := &Manager{client: client}
			peer := &peerState{name: "test-peer", events: make(chan controller.Event, 4)}
			config := appconfig.WebSocketClient{Name: "test-peer", URL: strings.Replace(remote.URL, "http:", "ws:", 1), Protocol: protocol}
			finished := make(chan error, 1)
			go func() { finished <- manager.webSocketPeerSession(ctx, peer, config) }()
			event, err := runtime.WaitEvent(ctx, runtime.LatestEventID(), "buzzer.note")
			if err != nil {
				select {
				case cause := <-errors:
					t.Fatal(cause)
				default:
					t.Fatal(err)
				}
			}
			if event.Source != "bridge" || event.Metadata["bridge.ingress"] != "test-peer" {
				t.Fatalf("lost peer provenance: %#v", event)
			}
			if bridgeEventForwardable(controller.Event{Kind: event.Kind, Metadata: event.Metadata}) {
				t.Fatal("state can escape to third peer")
			}
			mirror := appconfig.DefaultBuzzerMirror()
			mirror.Enabled = true
			mirror.NativeEnabled = true
			if job, ok := buzzerMirrorJobFor(mirror, controller.Event{Kind: event.Kind, Metadata: event.Metadata}); !ok || job.frequencyHz != 880 || job.durationMS != 125 {
				t.Fatalf("not a playable structured note: %#v", job)
			}
			cancel()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("peer session did not stop")
			}
		})
	}
}
