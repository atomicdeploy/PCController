//go:build !no_mqtt

package mqttbridge

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

func TestMQTTPacketBound(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	go func() {
		_, _ = server.Write([]byte{0xd0, 0, 0x30, 0xff, 0xff, 0x7f})
	}()
	wire := &packetConn{Conn: client}
	header := make([]byte, 2)
	if _, err := io.ReadFull(wire, header); err != nil || header[0] != 0xd0 || header[1] != 0 {
		t.Fatalf("valid packet: %x, %v", header, err)
	}
	if _, err := wire.Read(header); err == nil {
		t.Fatal("oversized packet accepted")
	}
}

func TestMQTTRealBroker(t *testing.T) {
	address := os.Getenv("PCCONTROLLER_TEST_MQTT_BROKER")
	if address == "" {
		t.Skip("set PCCONTROLLER_TEST_MQTT_BROKER to an isolated broker")
	}
	config := Config{Enabled: true, AllowCommands: true, Broker: address, CAFile: os.Getenv("PCCONTROLLER_TEST_MQTT_CA"), Username: os.Getenv("PCCONTROLLER_TEST_MQTT_USERNAME"), Password: os.Getenv("PCCONTROLLER_TEST_MQTT_PASSWORD"), Topic: "pccontroller/test-" + rand.Text(), TelemetrySeconds: 30}
	endpoint, err := config.URL()
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(endpoint.Hostname()); ip == nil || !ip.IsLoopback() {
		t.Fatal("broker fixture must use literal loopback")
	}
	tlsConfig, err := brokerTLS(endpoint.Hostname(), config.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	if config.CAFile != "" {
		wrong := tlsConfig.Clone()
		wrong.ServerName = "incorrect-fixture-name.invalid"
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", endpoint.Host, wrong)
		if err == nil {
			conn.Close()
			t.Fatal("TLS accepted the wrong broker hostname")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	observed := make(chan deliveryWithTopic, 64)
	options := mqtt.NewClientOptions().AddBroker(endpoint.String()).SetClientID("obs" + rand.Text()[:16]).
		SetProtocolVersion(4).SetCleanSession(true).SetAutoReconnect(false).SetTLSConfig(tlsConfig).
		SetUsername(config.Username).SetPassword(config.Password)
	client := mqtt.NewClient(options)
	check := func(token mqtt.Token) {
		t.Helper()
		if err := waitToken(ctx, token, 3*time.Second); err != nil {
			t.Fatal(err)
		}
	}
	check(client.Connect())
	defer client.Disconnect(250)
	check(client.Subscribe(config.Topic+"/#", 1, func(_ mqtt.Client, m mqtt.Message) {
		select {
		case observed <- deliveryWithTopic{m.Topic(), append([]byte(nil), m.Payload()...)}:
		default:
		}
	}))
	// A retained command stored before subscription must not dispatch.
	check(client.Publish(config.Topic+"/command", 1, true, `{"command":"status","request_id":"retained"}`))
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	var writes atomic.Int32
	identity := Identity{"fixture-board", "fixture-boot"}
	go func() {
		defer close(done)
		Run(run, config, Hooks{
			Identity: func() Identity { return identity },
			Observe:  func() (Identity, any) { return identity, map[string]any{"connected": true} },
			Execute: func(_ context.Context, _ Command) Receipt {
				writes.Add(1)
				return Receipt{Accepted: true, Code: "accepted"}
			},
			Status: func(s Status) {
				if s.Error != "" {
					t.Log(s.Error)
				}
			},
		})
	}()
	defer func() { stop(); <-done }()
	await := func(suffix string, matches func([]byte) bool) []byte {
		t.Helper()
		for {
			select {
			case m := <-observed:
				if m.topic == config.Topic+"/"+suffix && matches(m.data) {
					return m.data
				}
			case <-ctx.Done():
				t.Fatalf("missing %s publication", suffix)
			}
		}
	}
	var status struct {
		BoardID string `json:"board_id"`
		BootID  string `json:"boot_id"`
	}
	state := await("status", func(b []byte) bool {
		return json.Unmarshal(b, &status) == nil && status.BoardID != "" && status.BootID != ""
	})
	t.Logf("received status (%d bytes)", len(state))
	await("availability", func(b []byte) bool { return string(b) == "online" })
	receipt := func(id, code string) {
		t.Helper()
		await("event", func(b []byte) bool {
			var value Receipt
			return json.Unmarshal(b, &value) == nil && value.RequestID == id && value.Code == code
		})
	}
	receipt("retained", "replayed_delivery")
	check(client.Publish(config.Topic+"/command", 1, true, []byte{}))
	check(client.Publish(config.Topic+"/command", 1, false, `{"command":"status","request_id":"query"}`))
	receipt("query", "accepted")
	payload, err := json.Marshal(Command{Command: "rpc", Method: "controller.pwm.set", RequestID: "write1", BoardID: status.BoardID, BootID: status.BootID, ExpiresAt: time.Now().Add(30 * time.Second).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	check(client.Publish(config.Topic+"/command", 1, false, payload))
	receipt("write1", "accepted")
	check(client.Publish(config.Topic+"/command", 1, false, payload))
	receipt("write1", "duplicate_request")
	if writes.Load() != 1 {
		t.Fatalf("expected one dispatch, got %d", writes.Load())
	}
	// Eviction triggers the broker's Will; a new transport session must
	// recover without allowing the same ID to execute again.
	shadowOptions := mqtt.NewClientOptions().AddBroker(endpoint.String()).SetProtocolVersion(4).
		SetClientID(clientID(config.Topic)).SetCleanSession(true).SetAutoReconnect(false).
		SetTLSConfig(tlsConfig).SetUsername(config.Username).SetPassword(config.Password)
	shadow := mqtt.NewClient(shadowOptions)
	check(shadow.Connect())
	await("availability", func(b []byte) bool { return string(b) == "offline" })
	shadow.Disconnect(250)
	await("availability", func(b []byte) bool { return string(b) == "online" })
	check(client.Publish(config.Topic+"/command", 1, false, payload))
	receipt("write1", "duplicate_request")
	if writes.Load() != 1 {
		t.Fatal("command replayed after broker reconnect")
	}
	stop()
	await("availability", func(b []byte) bool { return string(b) == "offline" })
}

type deliveryWithTopic struct {
	topic string
	data  []byte
}
