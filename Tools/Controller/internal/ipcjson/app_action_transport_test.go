package ipcjson

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func TestTypedAppActionPushAckOutcomeAcrossBrowserTransports(t *testing.T) {
	for _, transport := range []string{"websocket", "socket_io"} {
		t.Run(transport, func(t *testing.T) {
			runtime := control.New(control.Options{})
			defer runtime.Close()
			client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
			defer client.Shutdown()
			registry := hostui.NewInstanceRegistry()
			instanceID := "web:transport-" + transport
			if _, err := registry.Upsert(hostui.AppInstance{
				ID: instanceID, Surface: "webui", State: "active", LeaseSeconds: 45,
				Values: map[string]string{hostui.ActionCapabilitiesKey: hostui.WebActionCapabilities},
			}); err != nil {
				t.Fatal(err)
			}
			broker := hostui.NewActionBroker()
			coordinator := hostui.NewActionCoordinator(registry, broker.Publish)
			defer coordinator.Close()
			broker.SetObserver(func(action hostui.AppAction) {
				metadata := map[string]string{
					"target_instance": action.Target,
					"value":           action.Value,
					"operation_id":    action.OperationID,
				}
				for key, value := range action.Metadata {
					metadata[key] = value
				}
				runtime.PublishStructuredEvent(control.Event{
					Kind: action.Kind, Stream: control.EventStreamState,
					Source: action.Source, Target: "app.clients", Metadata: metadata,
				})
			})
			coordinator.SetObserver(func(change hostui.ActionOutcomeChange) {
				runtime.PublishStructuredEvent(control.Event{
					Kind: "app.action.outcome", Stream: control.EventStreamState,
					Source: "host", Target: "app.clients", Metadata: map[string]string{
						"operation_id": change.OperationID, "instance_id": change.InstanceID,
						"state": change.State,
					},
				})
			})
			service := &Service{
				Client: client, AuthorizationDisabled: true,
				WebSocketPath: "/ipc", SocketIOPath: "/socket.io/",
				AppAction: broker.Publish, AppActionSubmit: coordinator.Submit,
				AppActionAck: coordinator.Ack, AppActionOutcome: coordinator.Outcome,
				AppInstances: registry,
			}
			server := httptest.NewServer(websocketMux(context.Background(), service))
			defer server.Close()

			path := "/ipc"
			if transport == "socket_io" {
				path = "/socket.io/?EIO=4&transport=websocket"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			connection, response, err := websocket.Dial(
				ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, nil,
			)
			if err != nil {
				t.Fatalf("dial response=%v err=%v", response, err)
			}
			defer connection.CloseNow()

			write := func(value string) {
				t.Helper()
				if writeErr := connection.Write(ctx, websocket.MessageText, []byte(value)); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			read := func(stage string) []byte {
				t.Helper()
				_, value, readErr := connection.Read(ctx)
				if readErr != nil {
					t.Fatalf("%s: %v", stage, readErr)
				}
				return value
			}
			if transport == "socket_io" {
				if packet := string(read("Engine.IO open")); !strings.HasPrefix(packet, "0{") {
					t.Fatalf("Engine.IO open=%q", packet)
				}
				write("40")
				if packet := string(read("Socket.IO connect")); !strings.HasPrefix(packet, "40{") {
					t.Fatalf("Socket.IO connect=%q", packet)
				}
				write(`42["subscribe",{"topics":["state"]}]`)
				for {
					packet := string(read("Socket.IO subscribe"))
					if strings.HasPrefix(packet, "42") {
						name, _, decodeErr := decodeSocketIOEvent(packet[2:])
						if decodeErr == nil && name == "subscribed" {
							break
						}
					}
				}
			} else {
				write(`{"jsonrpc":"2.0","id":1,"method":"controller.subscribe","params":{"topics":["state"]}}`)
				for {
					var response Response
					if decodeErr := json.Unmarshal(read("WebSocket subscribe"), &response); decodeErr == nil && string(response.ID) == "1" {
						break
					}
				}
			}

			operation, err := coordinator.Submit(hostui.AppAction{
				Kind: "app.title", Value: "Transport proof", Target: instanceID,
				OperationID: "transport-" + transport,
			}, 2*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			var pushed control.Event
			for pushed.Kind != "app.title" {
				payload := read("pushed app action")
				if transport == "socket_io" {
					packet := string(payload)
					if !strings.HasPrefix(packet, "42") {
						continue
					}
					name, raw, decodeErr := decodeSocketIOEvent(packet[2:])
					if decodeErr != nil || name != "controller.state" {
						continue
					}
					if decodeErr = json.Unmarshal(raw, &pushed); decodeErr != nil {
						t.Fatal(decodeErr)
					}
				} else {
					var notification wsNotification
					if decodeErr := json.Unmarshal(payload, &notification); decodeErr != nil || notification.Method != "controller.state" {
						continue
					}
					raw, _ := json.Marshal(notification.Params)
					if decodeErr := json.Unmarshal(raw, &pushed); decodeErr != nil {
						t.Fatal(decodeErr)
					}
				}
			}
			if pushed.Metadata["operation_id"] != operation.OperationID ||
				pushed.Metadata[hostui.ActionDeliveryIDKey] == "" ||
				pushed.Metadata[hostui.ActionExpiresAtKey] == "" ||
				pushed.Metadata["target_instance"] != instanceID {
				t.Fatalf("pushed event=%#v", pushed)
			}
			ack := hostui.ActionAck{
				OperationID: operation.OperationID,
				DeliveryID:  pushed.Metadata[hostui.ActionDeliveryIDKey],
				InstanceID:  instanceID, State: hostui.ActionStateApplied,
			}
			request := map[string]any{
				"jsonrpc": "2.0", "id": 2, "method": "controller.app.action.ack", "params": ack,
			}
			encoded, _ := json.Marshal(request)
			ackResponseSeen := false
			outcomeEventSeen := false
			observeTerminalOutcome := func(event control.Event) {
				t.Helper()
				if event.Kind != "app.action.outcome" ||
					event.Metadata["operation_id"] != operation.OperationID {
					return
				}
				if event.Stream != control.EventStreamState ||
					event.Metadata["instance_id"] != instanceID ||
					event.Metadata["state"] != hostui.ActionStateApplied {
					t.Fatalf("terminal outcome event=%#v", event)
				}
				outcomeEventSeen = true
			}
			if transport == "socket_io" {
				write(`42["rpc",` + string(encoded) + `]`)
				for !ackResponseSeen || !outcomeEventSeen {
					packet := string(read("Socket.IO action ACK"))
					if !strings.HasPrefix(packet, "42") {
						continue
					}
					name, raw, decodeErr := decodeSocketIOEvent(packet[2:])
					if decodeErr != nil {
						continue
					}
					switch name {
					case "rpc.response":
						var response Response
						if json.Unmarshal(raw, &response) == nil && string(response.ID) == "2" {
							if response.Error != nil {
								t.Fatalf("ACK response=%#v", response)
							}
							ackResponseSeen = true
						}
					case "controller.state":
						var event control.Event
						if decodeErr = json.Unmarshal(raw, &event); decodeErr != nil {
							t.Fatal(decodeErr)
						}
						observeTerminalOutcome(event)
					}
				}
			} else {
				write(string(encoded))
				for !ackResponseSeen || !outcomeEventSeen {
					payload := read("WebSocket action ACK")
					var response Response
					if decodeErr := json.Unmarshal(payload, &response); decodeErr == nil && string(response.ID) == "2" {
						if response.Error != nil {
							t.Fatalf("ACK response=%#v", response)
						}
						ackResponseSeen = true
						continue
					}
					var notification wsNotification
					if decodeErr := json.Unmarshal(payload, &notification); decodeErr != nil || notification.Method != "controller.state" {
						continue
					}
					raw, _ := json.Marshal(notification.Params)
					var event control.Event
					if decodeErr := json.Unmarshal(raw, &event); decodeErr != nil {
						t.Fatal(decodeErr)
					}
					observeTerminalOutcome(event)
				}
			}
			outcome, err := coordinator.Outcome(operation.OperationID)
			if err != nil || outcome.State != hostui.ActionStateApplied ||
				len(outcome.Targets) != 1 || outcome.Targets[0].State != hostui.ActionStateApplied {
				t.Fatalf("outcome=%#v err=%v", outcome, err)
			}
		})
	}
}
