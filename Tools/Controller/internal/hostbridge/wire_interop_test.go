package hostbridge

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/shell"
)

const rawPeerWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

type rawPeerSocket struct {
	connection net.Conn
	reader     *bufio.Reader
}

func acceptRawPeerSocket(
	t *testing.T,
	listener net.Listener,
	path string,
	token string,
) *rawPeerSocket {
	t.Helper()
	if tcp, ok := listener.(*net.TCPListener); ok {
		_ = tcp.SetDeadline(time.Now().Add(3 * time.Second))
	}
	connection, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	request, err := http.ReadRequest(reader)
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	_ = request.Body.Close()
	if request.URL.Path != path || request.Header.Get("Authorization") != "Bearer "+token ||
		!strings.EqualFold(request.Header.Get("Upgrade"), "websocket") {
		_ = connection.Close()
		t.Fatalf("raw peer upgrade path=%q auth=%q headers=%v", request.URL.Path, request.Header.Get("Authorization"), request.Header)
	}
	key := request.Header.Get("Sec-WebSocket-Key")
	digest := sha1.Sum([]byte(key + rawPeerWebSocketGUID))
	accept := base64.StdEncoding.EncodeToString(digest[:])
	_, err = io.WriteString(connection,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Upgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Accept: "+accept+"\r\n\r\n",
	)
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	return &rawPeerSocket{connection: connection, reader: reader}
}

func (socket *rawPeerSocket) close() {
	_ = socket.connection.Close()
}

func (socket *rawPeerSocket) writeText(t *testing.T, value string) {
	t.Helper()
	if err := writeRawPeerFrame(socket.connection, 0x1, []byte(value)); err != nil {
		t.Fatal(err)
	}
}

func (socket *rawPeerSocket) readText(t *testing.T) string {
	t.Helper()
	_ = socket.connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		opcode, payload, masked, err := readRawPeerFrame(socket.reader)
		if err != nil {
			t.Fatal(err)
		}
		if !masked {
			t.Fatal("outbound WebSocket client sent an unmasked frame")
		}
		if opcode == 0x1 {
			return string(payload)
		}
		if opcode == 0x9 {
			if err := writeRawPeerFrame(socket.connection, 0xA, payload); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func writeRawPeerFrame(writer io.Writer, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	switch {
	case len(payload) <= 125:
		header = append(header, byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, 126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, 127)
		length := uint64(len(payload))
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(length>>shift))
		}
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(payload)
	return err
}

func readRawPeerFrame(reader *bufio.Reader) (byte, []byte, bool, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, false, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, false, err
	}
	if first&0x80 == 0 {
		return 0, nil, false, fmt.Errorf("fragmented peer frame")
	}
	length := uint64(second & 0x7F)
	switch length {
	case 126:
		var value [2]byte
		if _, err = io.ReadFull(reader, value[:]); err != nil {
			return 0, nil, false, err
		}
		length = uint64(binary.BigEndian.Uint16(value[:]))
	case 127:
		var value [8]byte
		if _, err = io.ReadFull(reader, value[:]); err != nil {
			return 0, nil, false, err
		}
		length = binary.BigEndian.Uint64(value[:])
	}
	if length > 1024*1024 {
		return 0, nil, false, fmt.Errorf("peer frame too large: %d", length)
	}
	masked := second&0x80 != 0
	var mask [4]byte
	if masked {
		if _, err = io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, false, err
		}
	}
	payload := make([]byte, int(length))
	if _, err = io.ReadFull(reader, payload); err != nil {
		return 0, nil, false, err
	}
	if masked {
		for index := range payload {
			payload[index] ^= mask[index%len(mask)]
		}
	}
	return first & 0x0F, payload, masked, nil
}

func rawPeerEvent(t *testing.T, packet string) (string, json.RawMessage) {
	t.Helper()
	if !strings.HasPrefix(packet, "42") {
		t.Fatalf("expected Socket.IO event packet, got %q", packet)
	}
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(packet[2:]), &values); err != nil || len(values) != 2 {
		t.Fatalf("decode Socket.IO event %q: %v", packet, err)
	}
	var name string
	if err := json.Unmarshal(values[0], &name); err != nil {
		t.Fatal(err)
	}
	return name, values[1]
}

func startRawPeerManager(
	t *testing.T,
	listener net.Listener,
	protocol string,
	token string,
) (*Manager, *controller.Client, *control.Runtime, context.CancelFunc) {
	t.Helper()
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	path := "/peer"
	if protocol == "socketio" {
		path = "/socket.io/"
	}
	_, err = store.Update(func(config *appconfig.Config) error {
		config.IPC.AllowRemote = true
		config.IPC.AuthToken = token
		config.Integrations.Hotkeys = nil
		config.Integrations.Notifications.Enabled = false
		config.Integrations.WebSocketClients = []appconfig.WebSocketClient{{
			Name: "raw-peer", Enabled: true,
			URL:      "ws://" + listener.Addr().String() + path,
			Protocol: protocol, AuthToken: token, Topics: []string{"events", "state", "status"},
			ForwardEvents: true, AllowCommands: true,
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
	return manager, client, runtime, cancel
}

func TestOutboundWebSocketClientsInteroperateWithRawRFC6455Servers(t *testing.T) {
	t.Run("jsonrpc", func(t *testing.T) {
		const token = "raw-jsonrpc-peer-token-0123456789"
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		manager, client, runtime, cancel := startRawPeerManager(t, listener, "jsonrpc", token)
		defer func() {
			cancel()
			manager.Close()
			_ = runtime.Close()
		}()
		peer := acceptRawPeerSocket(t, listener, "/peer", token)
		defer peer.close()

		var subscription ipcjson.Request
		if packet := peer.readText(t); json.Unmarshal([]byte(packet), &subscription) != nil ||
			subscription.JSONRPC != ipcjson.Version || subscription.Method != "controller.subscribe" ||
			!strings.Contains(string(subscription.Params), `"state"`) {
			t.Fatalf("JSON-RPC subscription=%s", packet)
		}
		cursor := client.LatestEventID()
		stateEvent := controller.Event{
			ID: 71, Kind: "buzzer.note", Stream: "state", Source: "board",
			Metadata: map[string]string{"frequency_hz": "880", "duration_ms": "125"},
		}
		stateParams, _ := json.Marshal(stateEvent)
		notification, _ := json.Marshal(ipcjson.Request{
			JSONRPC: ipcjson.Version, Method: "controller.event", Params: stateParams,
		})
		peer.writeText(t, string(notification))
		assertSinglePeerBuzzerEvent(t, client, cursor, "raw-peer")

		result := make(chan ipcjson.Response, 1)
		errors := make(chan error, 1)
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			response, err := manager.CallBridge(ctx, "raw-peer", ipcjson.Request{
				JSONRPC: ipcjson.Version, ID: json.RawMessage(`"caller-17"`),
				Method: "controller.snapshot",
			})
			result <- response
			errors <- err
		}()
		var request ipcjson.Request
		var packet string
		for {
			packet = peer.readText(t)
			if err := json.Unmarshal([]byte(packet), &request); err != nil {
				t.Fatalf("decode correlated JSON-RPC request=%s err=%v", packet, err)
			}
			if request.Method == "controller.message.send" {
				encoded, _ := json.Marshal(ipcjson.Response{
					JSONRPC: ipcjson.Version, ID: request.ID,
					Result: map[string]bool{"accepted": true},
				})
				peer.writeText(t, string(encoded))
				continue
			}
			break
		}
		if request.JSONRPC != ipcjson.Version || request.Method != "controller.snapshot" {
			t.Fatalf("correlated JSON-RPC request=%s", packet)
		}
		encoded, _ := json.Marshal(ipcjson.Response{
			JSONRPC: ipcjson.Version, ID: request.ID,
			Result: map[string]any{"raw_server": true},
		})
		peer.writeText(t, string(encoded))
		response := <-result
		if err := <-errors; err != nil || response.Error != nil ||
			string(response.ID) != `"caller-17"` ||
			!strings.Contains(fmt.Sprint(response.Result), "raw_server:true") {
			t.Fatalf("bridge response=%#v err=%v", response, err)
		}

		runtime.PublishHostEvent("door", "door opened")
		for {
			packet = peer.readText(t)
			if err := json.Unmarshal([]byte(packet), &request); err != nil ||
				request.Method != "controller.message.send" {
				t.Fatalf("forwarded JSON-RPC message=%s err=%v", packet, err)
			}
			encoded, _ = json.Marshal(ipcjson.Response{
				JSONRPC: ipcjson.Version, ID: request.ID,
				Result: map[string]bool{"accepted": true},
			})
			peer.writeText(t, string(encoded))
			if strings.Contains(string(request.Params), `"event.kind":"door"`) {
				break
			}
		}

		peerRequest := ipcjson.Request{
			JSONRPC: ipcjson.Version, ID: json.RawMessage("99"),
			Method: "controller.ping",
		}
		encoded, _ = json.Marshal(peerRequest)
		peer.writeText(t, string(encoded))
		packet = peer.readText(t)
		var peerResponse ipcjson.Response
		if err := json.Unmarshal([]byte(packet), &peerResponse); err != nil ||
			string(peerResponse.ID) != "99" || peerResponse.Error != nil {
			t.Fatalf("raw peer inbound RPC response=%s err=%v", packet, err)
		}
	})

	t.Run("socketio", func(t *testing.T) {
		const token = "raw-socketio-peer-token-0123456789"
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		manager, client, runtime, cancel := startRawPeerManager(t, listener, "socketio", token)
		defer func() {
			cancel()
			manager.Close()
			_ = runtime.Close()
		}()
		peer := acceptRawPeerSocket(t, listener, "/socket.io/", token)
		defer peer.close()
		peer.writeText(t, `0{"sid":"raw-peer","upgrades":[],"pingInterval":25000,"pingTimeout":20000,"maxPayload":1048576}`)
		if packet := peer.readText(t); packet != "40" {
			t.Fatalf("Socket.IO client connect=%q", packet)
		}
		peer.writeText(t, `40{"sid":"raw-peer"}`)
		name, payload := rawPeerEvent(t, peer.readText(t))
		if name != "subscribe" || !strings.Contains(string(payload), `"events"`) ||
			!strings.Contains(string(payload), `"state"`) {
			t.Fatalf("Socket.IO subscription name=%q payload=%s", name, payload)
		}
		cursor := client.LatestEventID()
		statePacket, _ := json.Marshal([]any{"controller.event", controller.Event{
			ID: 72, Kind: "buzzer.note", Stream: "state", Source: "board",
			Metadata: map[string]string{"frequency_hz": "660", "duration_ms": "90"},
		}})
		peer.writeText(t, "42"+string(statePacket))
		assertSinglePeerBuzzerEvent(t, client, cursor, "raw-peer")

		result := make(chan ipcjson.Response, 1)
		errors := make(chan error, 1)
		go func() {
			ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
			defer stop()
			response, err := manager.CallBridge(ctx, "raw-peer", ipcjson.Request{
				JSONRPC: ipcjson.Version, ID: json.RawMessage(`"caller-23"`),
				Method: "controller.snapshot",
			})
			result <- response
			errors <- err
		}()
		for {
			name, payload = rawPeerEvent(t, peer.readText(t))
			if name == "rpc" {
				break
			}
		}
		if name != "rpc" {
			t.Fatalf("Socket.IO bridge call event=%q payload=%s", name, payload)
		}
		var request ipcjson.Request
		if err := json.Unmarshal(payload, &request); err != nil ||
			request.JSONRPC != ipcjson.Version || request.Method != "controller.snapshot" {
			t.Fatalf("Socket.IO bridge request=%s err=%v", payload, err)
		}
		responsePacket, _ := json.Marshal([]any{"rpc.response", ipcjson.Response{
			JSONRPC: ipcjson.Version, ID: request.ID,
			Result: map[string]any{"raw_socketio": true},
		}})
		peer.writeText(t, "42"+string(responsePacket))
		response := <-result
		if err := <-errors; err != nil || response.Error != nil ||
			string(response.ID) != `"caller-23"` ||
			!strings.Contains(fmt.Sprint(response.Result), "raw_socketio:true") {
			t.Fatalf("Socket.IO bridge response=%#v err=%v", response, err)
		}

		runtime.PublishHostEvent("door", "door opened")
		var forwarded controller.TextMessage
		for {
			name, payload = rawPeerEvent(t, peer.readText(t))
			if name != "message" || json.Unmarshal(payload, &forwarded) != nil {
				continue
			}
			if strings.Contains(forwarded.Text, `"kind":"door"`) {
				break
			}
		}
		if forwarded.Type != "local-event" {
			t.Fatalf("Socket.IO forwarded event name=%q payload=%s", name, payload)
		}

		cursor = client.LatestEventID()
		messagePacket, _ := json.Marshal([]any{"message", controller.TextMessage{
			Source: "client", Target: "host", Type: "actionable.notice",
			Text: "open events", Action: "app.page:events",
		}})
		peer.writeText(t, "42"+string(messagePacket))
		messageContext, stopMessage := context.WithTimeout(context.Background(), 2*time.Second)
		defer stopMessage()
		event, err := client.NextEvent(messageContext, cursor, "message")
		if err != nil || event.Source != "bridge" || event.Action != "app.page:events" ||
			event.Metadata["claimed_source"] != "client" {
			t.Fatalf("Socket.IO inbound message event=%#v err=%v", event, err)
		}

		peer.writeText(t, "2")
		for attempts := 0; ; attempts++ {
			packet := peer.readText(t)
			if packet == "3" {
				break
			}
			// Host events are asynchronous Socket.IO messages and may already be
			// queued when the Engine.IO ping is sent. They do not violate pong
			// semantics, but a bounded loop still catches a missing response.
			if attempts >= 15 || !strings.HasPrefix(packet, "42") {
				t.Fatalf("Socket.IO Engine.IO pong not received; last packet=%q", packet)
			}
		}
	})
}

func assertSinglePeerBuzzerEvent(
	t *testing.T,
	client *controller.Client,
	afterID uint64,
	peerName string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	event, err := client.NextEvent(ctx, afterID, "buzzer.note")
	cancel()
	if err != nil || event.Stream != "state" || event.Source != "bridge" ||
		event.Metadata["bridge.ingress"] != peerName {
		t.Fatalf("peer buzzer event=%#v err=%v", event, err)
	}
	duplicateContext, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer stop()
	if duplicate, duplicateErr := client.NextEvent(duplicateContext, event.ID, "buzzer.note"); duplicateErr == nil {
		t.Fatalf("peer buzzer event was delivered more than once: %#v", duplicate)
	}
	if bridgeEventForwardable(event) {
		t.Fatal("ingressed buzzer event remained eligible for bridge forwarding")
	}
}
