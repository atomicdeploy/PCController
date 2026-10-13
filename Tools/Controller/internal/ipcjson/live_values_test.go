package ipcjson

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
	"testing"
	"time"
)

func TestStateBurstFlushesEachFinalValueWithoutNewEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	input := make(chan controller.Event, 6)
	output := make(chan controller.Event, 6)
	go streamLatestState(ctx, 0, 40*time.Millisecond, func(ctx context.Context, _ uint64) (controller.Event, error) {
		select {
		case value := <-input:
			return value, nil
		case <-ctx.Done():
			return controller.Event{}, ctx.Err()
		}
	}, func(value controller.Event) error { output <- value; return nil })
	for i, kind := range []string{"status_led.changed", "pwm.changed", "status_led.changed", "pwm.changed", "front_panel.segment", "buzzer.note"} {
		input <- controller.Event{ID: uint64(i + 1), Kind: kind}
	}
	seen := map[uint64]bool{}
	for len(seen) < 6 {
		select {
		case value := <-output:
			seen[value.ID] = true
		case <-ctx.Done():
			t.Fatalf("final domains missing: %v", seen)
		}
	}
}

func TestTwoWebSocketClientsReceiveCompleteCalibratedPWMAndReconnectBaseline(t *testing.T) {
	runtime := control.New(control.Options{})
	defer runtime.Close()
	client := controller.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.Integrations.Discovery = appconfig.Discovery{}
	config.IPC.UDP.Enabled = false
	service := &Service{Client: client, HostInstanceID: "fixture-owner", WebSocketPath: "/ipc", HostConfig: func() appconfig.Config { return config }}
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go Serve(ctx, listener, service)
	defer listener.Close()
	connect := func() (*websocket.Conn, *pwmValues) {
		socket, _, err := websocket.Dial(ctx, "ws://"+listener.Addr().String()+"/ipc", nil)
		if err != nil {
			t.Fatal(err)
		}
		body := []byte(`{"jsonrpc":"2.0","id":1,"method":"controller.subscribe","params":{"topics":["state"],"state_interval_ms":50}}`)
		if err := socket.Write(ctx, websocket.MessageText, body); err != nil {
			t.Fatal(err)
		}
		var baseline *pwmValues
		ack, snapshotSeen := false, false
		for !ack || !snapshotSeen {
			_, body, err := socket.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var frame struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(body, &frame) != nil {
				t.Fatal("invalid subscription frame")
			}
			if len(frame.ID) > 0 {
				ack = true
			}
			if frame.Method == "controller.snapshot" {
				var state controllerSnapshotEnvelope
				if json.Unmarshal(frame.Params, &state) != nil {
					t.Fatal("invalid baseline")
				}
				baseline = state.PWMValues
				snapshotSeen = true
			}
		}
		return socket, baseline
	}
	first, _ := connect()
	second, _ := connect()
	defer first.CloseNow()
	defer second.CloseNow()
	values := controller.PWMValues{Available: true, SelectedChannel: 2}
	values.Values[2] = 1234
	client.PublishPWMValues(values)
	read := func(socket *websocket.Conn, method string) *pwmValues {
		for {
			_, body, err := socket.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var message struct {
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if json.Unmarshal(body, &message) != nil || message.Method != method {
				continue
			}
			if method == "controller.snapshot" {
				var snapshot controllerSnapshotEnvelope
				if json.Unmarshal(message.Params, &snapshot) != nil {
					t.Fatal("invalid snapshot")
				}
				if snapshot.PWMValues != nil {
					return snapshot.PWMValues
				}
				continue
			}
			var value pwmValues
			if json.Unmarshal(message.Params, &value) != nil {
				t.Fatal("invalid values")
			}
			return &value
		}
	}
	for _, socket := range []*websocket.Conn{first, second} {
		value := read(socket, "controller.pwm.values")
		if value.HostInstanceID != "fixture-owner" || value.Revision != 1 || value.Values[2] != 1234 || len(value.Channels) != 16 || value.Channels[2].LogicalPercent <= 0 {
			t.Fatalf("incomplete push=%+v", value)
		}
	}
	third, value := connect()
	defer third.CloseNow()
	if value == nil || value.Revision != 1 || value.Values[2] != 1234 {
		t.Fatalf("reconnect baseline=%+v", value)
	}
}
