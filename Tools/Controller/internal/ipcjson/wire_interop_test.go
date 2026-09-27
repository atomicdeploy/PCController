package ipcjson_test

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/ipcjson"
	"pccontroller.local/controller/internal/native"
)

const rawWebSocketGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// rawWebSocket deliberately implements only the RFC 6455 pieces exercised by
// this test. It is independent from the WebSocket package used by production.
type rawWebSocket struct {
	connection net.Conn
	reader     *bufio.Reader
}

func dialRawWebSocket(
	t *testing.T,
	address string,
	path string,
	token string,
) *rawWebSocket {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	// RFC 6455 requires a base64-encoded nonce containing exactly 16 bytes.
	key := base64.StdEncoding.EncodeToString([]byte("raw-rfc6455-key!"))
	request := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + address + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n"
	if token != "" {
		request += "Authorization: Bearer " + token + "\r\n"
	}
	request += "\r\n"
	if _, err = io.WriteString(connection, request); err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		_ = connection.Close()
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		_ = connection.Close()
		t.Fatalf("WebSocket upgrade status=%d body=%s", response.StatusCode, body)
	}
	digest := sha1.Sum([]byte(key + rawWebSocketGUID))
	wantAccept := base64.StdEncoding.EncodeToString(digest[:])
	if got := response.Header.Get("Sec-WebSocket-Accept"); got != wantAccept {
		_ = response.Body.Close()
		_ = connection.Close()
		t.Fatalf("Sec-WebSocket-Accept=%q want=%q", got, wantAccept)
	}
	_ = response.Body.Close()
	return &rawWebSocket{connection: connection, reader: reader}
}

func rawWebSocketUpgradeStatus(
	t *testing.T,
	address string,
	path string,
) int {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	key := base64.StdEncoding.EncodeToString([]byte("unauthorized-key"))
	_, err = io.WriteString(connection,
		"GET "+path+" HTTP/1.1\r\nHost: "+address+"\r\n"+
			"Upgrade: websocket\r\nConnection: Upgrade\r\n"+
			"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: "+key+"\r\n\r\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func (socket *rawWebSocket) close() {
	_ = socket.connection.Close()
}

func (socket *rawWebSocket) writeText(value string) error {
	return writeRawWebSocketFrame(socket.connection, 0x1, []byte(value), true)
}

func (socket *rawWebSocket) readText(timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		_ = socket.connection.SetReadDeadline(deadline)
		opcode, payload, err := readRawWebSocketFrame(socket.reader)
		if err != nil {
			return "", err
		}
		switch opcode {
		case 0x1:
			return string(payload), nil
		case 0x8:
			return "", io.EOF
		case 0x9:
			if err := writeRawWebSocketFrame(socket.connection, 0xA, payload, true); err != nil {
				return "", err
			}
		}
	}
}

func writeRawWebSocketFrame(
	writer io.Writer,
	opcode byte,
	payload []byte,
	masked bool,
) error {
	header := []byte{0x80 | opcode}
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch {
	case len(payload) <= 125:
		header = append(header, maskBit|byte(len(payload)))
	case len(payload) <= 65535:
		header = append(header, maskBit|126, byte(len(payload)>>8), byte(len(payload)))
	default:
		header = append(header, maskBit|127)
		length := uint64(len(payload))
		for shift := 56; shift >= 0; shift -= 8 {
			header = append(header, byte(length>>shift))
		}
	}
	encoded := append([]byte(nil), payload...)
	if masked {
		mask := [4]byte{0x12, 0x34, 0x56, 0x78}
		header = append(header, mask[:]...)
		for index := range encoded {
			encoded[index] ^= mask[index%len(mask)]
		}
	}
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err := writer.Write(encoded)
	return err
}

func readRawWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	if first&0x80 == 0 {
		return 0, nil, fmt.Errorf("fragmented WebSocket frames are outside this test client")
	}
	length := uint64(second & 0x7F)
	switch length {
	case 126:
		var value [2]byte
		if _, err = io.ReadFull(reader, value[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(value[:]))
	case 127:
		var value [8]byte
		if _, err = io.ReadFull(reader, value[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(value[:])
	}
	if length > 1024*1024 {
		return 0, nil, fmt.Errorf("WebSocket test frame is too large: %d", length)
	}
	var mask [4]byte
	if second&0x80 != 0 {
		if _, err = io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if _, err = io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if second&0x80 != 0 {
		for index := range payload {
			payload[index] ^= mask[index%len(mask)]
		}
	}
	return first & 0x0F, payload, nil
}

func rawRPC(
	t *testing.T,
	socket *rawWebSocket,
	id int,
	method string,
	params any,
) map[string]json.RawMessage {
	t.Helper()
	request := map[string]any{
		"jsonrpc": ipcjson.Version,
		"id":      id,
		"method":  method,
	}
	if params != nil {
		request["params"] = params
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if err = socket.writeText(string(encoded)); err != nil {
		t.Fatal(err)
	}
	return readRawRPCResponse(t, socket, id, method)
}

func readRawRPCResponse(
	t *testing.T,
	socket *rawWebSocket,
	id int,
	label string,
) map[string]json.RawMessage {
	t.Helper()
	wantedID := fmt.Sprintf("%d", id)
	for {
		payload, err := socket.readText(3 * time.Second)
		if err != nil {
			t.Fatalf("read response for %s: %v", label, err)
		}
		var envelope map[string]json.RawMessage
		if json.Unmarshal([]byte(payload), &envelope) != nil || string(envelope["id"]) != wantedID {
			continue
		}
		if string(envelope["jsonrpc"]) != `"2.0"` {
			t.Fatalf("%s response is not JSON-RPC 2.0: %s", label, payload)
		}
		return envelope
	}
}

func readRawNotification(
	t *testing.T,
	socket *rawWebSocket,
	method string,
) json.RawMessage {
	t.Helper()
	for {
		payload, err := socket.readText(3 * time.Second)
		if err != nil {
			t.Fatalf("read %s notification: %v", method, err)
		}
		var envelope struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal([]byte(payload), &envelope) == nil && envelope.Method == method {
			if envelope.JSONRPC != ipcjson.Version {
				t.Fatalf("notification is not JSON-RPC 2.0: %s", payload)
			}
			return envelope.Params
		}
	}
}

func readRawSocketIOEvent(
	t *testing.T,
	socket *rawWebSocket,
	name string,
) json.RawMessage {
	t.Helper()
	for {
		packet, err := socket.readText(3 * time.Second)
		if err != nil {
			t.Fatalf("read Socket.IO event %s: %v", name, err)
		}
		if !strings.HasPrefix(packet, "42") {
			continue
		}
		var parts []json.RawMessage
		if json.Unmarshal([]byte(packet[2:]), &parts) != nil || len(parts) != 2 {
			continue
		}
		var actual string
		if json.Unmarshal(parts[0], &actual) == nil && actual == name {
			return parts[1]
		}
	}
}

func rawHTTPRequest(
	t *testing.T,
	address string,
	method string,
	path string,
	token string,
	body string,
) (int, []byte) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	request := method + " " + path + " HTTP/1.1\r\nHost: " + address + "\r\nConnection: close\r\n"
	if token != "" {
		request += "Authorization: Bearer " + token + "\r\n"
	}
	if body != "" {
		request += fmt.Sprintf("Content-Type: application/json\r\nContent-Length: %d\r\n", len(body))
	}
	request += "\r\n" + body
	if _, err = io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, payload
}

type rawBoardConnection struct {
	connection net.Conn
	writeMu    sync.Mutex
}

func (connection *rawBoardConnection) send(frame native.Frame) error {
	encoded, err := native.Encode(frame)
	if err != nil {
		return err
	}
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	_, err = connection.connection.Write(encoded)
	return err
}

type rawVirtualBoard struct {
	listener    net.Listener
	accepted    chan *rawBoardConnection
	statusCount atomic.Int32
	done        chan struct{}
	wait        sync.WaitGroup
}

func startRawVirtualBoard(t *testing.T) *rawVirtualBoard {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	board := &rawVirtualBoard{
		listener: listener,
		accepted: make(chan *rawBoardConnection, 4),
		done:     make(chan struct{}),
	}
	board.wait.Add(1)
	go board.acceptLoop()
	return board
}

func (board *rawVirtualBoard) endpoint() string {
	return "tcp://" + board.listener.Addr().String()
}

func (board *rawVirtualBoard) close() {
	close(board.done)
	_ = board.listener.Close()
	board.wait.Wait()
}

func (board *rawVirtualBoard) acceptLoop() {
	defer board.wait.Done()
	for {
		connection, err := board.listener.Accept()
		if err != nil {
			return
		}
		peer := &rawBoardConnection{connection: connection}
		select {
		case board.accepted <- peer:
		case <-board.done:
			_ = connection.Close()
			return
		}
		board.wait.Add(1)
		go func() {
			defer board.wait.Done()
			defer connection.Close()
			reader := bufio.NewReader(connection)
			for {
				encoded, err := reader.ReadBytes(0)
				if err != nil {
					return
				}
				request, err := native.Decode(encoded)
				if err != nil {
					return
				}
				response := native.Frame{Seq: request.Seq}
				switch request.Opcode {
				case native.OpHello:
					response.Opcode = native.OpHelloResp
					response.Payload = make([]byte, 14)
					response.Payload[0] = native.IdentitySchemaCompact
					response.Payload[1] = native.BoardKindPCController
					binary.LittleEndian.PutUint32(response.Payload[6:10], 0xA1B2C3D4)
				case native.OpGetStatus:
					count := board.statusCount.Add(1)
					response.Opcode = native.OpStatus
					response.Payload = make([]byte, native.StatusPayloadSize)
					binary.LittleEndian.PutUint32(response.Payload[0:4], uint32(count)*50)
					binary.LittleEndian.PutUint32(response.Payload[8:12], 12000)
				default:
					response.Opcode = native.OpACK
					response.Payload = []byte{request.Opcode, 0}
				}
				if peer.send(response) != nil {
					return
				}
			}
		}()
	}
}

func nextBoardConnection(t *testing.T, board *rawVirtualBoard) *rawBoardConnection {
	t.Helper()
	select {
	case connection := <-board.accepted:
		return connection
	case <-time.After(2 * time.Second):
		t.Fatal("virtual board did not accept the controller connection")
		return nil
	}
}

func TestIndependentRawClientsInteroperateWithAllVersionedSocketSurfaces(t *testing.T) {
	const token = "raw-wire-interoperability-token"
	board := startRawVirtualBoard(t)
	defer board.close()
	client := controller.New(controller.Options{
		RequestTimeout: 200 * time.Millisecond,
		HelloAttempts:  1,
	})
	defer client.Shutdown()
	shutdown := make(chan struct{}, 1)
	service := &ipcjson.Service{
		Client: client, AuthToken: token,
		HostInstanceID: "primary-wire-test",
		WebSocketPath:  "/ipc", SocketIOPath: "/socket.io/",
		Shutdown: func() { shutdown <- struct{}{} },
	}
	listener, err := ipcjson.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ipcjson.Serve(ctx, listener, service) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("IPC server shutdown: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("IPC server did not stop")
		}
	}()
	address := listener.Addr().String()

	if status := rawWebSocketUpgradeStatus(t, address, "/ipc"); status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated WebSocket upgrade status=%d", status)
	}
	status, body := rawHTTPRequest(t, address, http.MethodGet, "/api/snapshot", token, "")
	if status != http.StatusOK || !strings.Contains(string(body), `"connected":false`) ||
		!strings.Contains(string(body), `"host_instance_id":"primary-wire-test"`) {
		t.Fatalf("living REST snapshot status=%d body=%s", status, body)
	}
	status, _ = rawHTTPRequest(t, address, http.MethodGet, "/api/v1/snapshot", token, "")
	if status != http.StatusNotFound {
		t.Fatalf("versioned REST route status=%d", status)
	}
	rpcBody := `{"jsonrpc":"2.0","id":"rest-1","method":"controller.ping"}`
	status, body = rawHTTPRequest(t, address, http.MethodPost, "/api/rpc", token, rpcBody)
	if status != http.StatusOK || !strings.Contains(string(body), `"id":"rest-1"`) ||
		!strings.Contains(string(body), `"ok":true`) || strings.Contains(string(body), `api_version`) {
		t.Fatalf("REST JSON-RPC status=%d body=%s", status, body)
	}

	rawConnection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = rawConnection.SetDeadline(time.Now().Add(2 * time.Second))
	request := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":"ndjson-1","method":"controller.ping","auth":%q}`+"\n",
		token,
	)
	if _, err = io.WriteString(rawConnection, request); err != nil {
		t.Fatal(err)
	}
	var rawResponse ipcjson.Response
	if err = json.NewDecoder(rawConnection).Decode(&rawResponse); err != nil {
		t.Fatal(err)
	}
	_ = rawConnection.Close()
	if rawResponse.JSONRPC != ipcjson.Version || string(rawResponse.ID) != `"ndjson-1"` || rawResponse.Error != nil {
		t.Fatalf("NDJSON response=%#v", rawResponse)
	}

	standard := dialRawWebSocket(t, address, "/ipc", token)
	defer standard.close()
	ping := rawRPC(t, standard, 1, "controller.ping", nil)
	if !strings.Contains(string(ping["result"]), `"ok":true`) || strings.Contains(string(ping["result"]), `api_version`) {
		t.Fatalf("standard WebSocket ping=%s", ping["result"])
	}
	if err = standard.writeText(`{"jsonrpc":"1.0","id":100,"method":"controller.ping"}`); err != nil {
		t.Fatal(err)
	}
	wrongVersion := readRawRPCResponse(t, standard, 100, "wrong protocol version")
	if !strings.Contains(string(wrongVersion["error"]), `"code":-32600`) ||
		!strings.Contains(string(wrongVersion["error"]), `jsonrpc must`) {
		t.Fatalf("wrong JSON-RPC version response=%v", wrongVersion)
	}
	opened := rawRPC(t, standard, 2, "controller.open", map[string]any{
		"port": board.endpoint(),
	})
	if len(opened["error"]) != 0 || !strings.Contains(string(opened["result"]), `"connected":true`) {
		t.Fatalf("open response=%v", opened)
	}
	firstBoardConnection := nextBoardConnection(t, board)
	time.Sleep(130 * time.Millisecond)
	if count := board.statusCount.Load(); count != 0 {
		t.Fatalf("serial status polling started without a subscriber: %d", count)
	}

	subscribed := rawRPC(t, standard, 3, "controller.subscribe", map[string]any{
		"topics": []string{"status"}, "interval_ms": 50,
	})
	if !strings.Contains(string(subscribed["result"]), `"subscribed":true`) ||
		!strings.Contains(string(subscribed["result"]), `"instance_id":"primary-wire-test"`) {
		t.Fatalf("status subscribe response=%v", subscribed)
	}
	statusUpdate := readRawNotification(t, standard, "controller.status")
	if !strings.Contains(string(statusUpdate), `"bus_mv":12000`) || board.statusCount.Load() == 0 {
		t.Fatalf("status notification=%s count=%d", statusUpdate, board.statusCount.Load())
	}
	unsubscribed := rawRPC(t, standard, 4, "controller.unsubscribe", nil)
	if !strings.Contains(string(unsubscribed["result"]), `"subscribed":false`) {
		t.Fatalf("unsubscribe response=%v", unsubscribed)
	}
	time.Sleep(120 * time.Millisecond)
	countAfterCancel := board.statusCount.Load()
	time.Sleep(170 * time.Millisecond)
	if count := board.statusCount.Load(); count != countAfterCancel {
		t.Fatalf("status polling continued after unsubscribe: before=%d after=%d", countAfterCancel, count)
	}
	snapshot := rawRPC(t, standard, 5, "controller.snapshot", nil)
	if !strings.Contains(string(snapshot["result"]), `"connected":true`) ||
		!strings.Contains(string(snapshot["result"]), `"paused":false`) ||
		!strings.Contains(string(snapshot["result"]), `"host_instance_id":"primary-wire-test"`) {
		t.Fatalf("subscription cancellation changed serial lifecycle: %s", snapshot["result"])
	}

	_ = rawRPC(t, standard, 6, "controller.subscribe", map[string]any{
		"topics": []string{"events"},
	})
	if err := firstBoardConnection.send(native.Frame{
		Opcode: native.OpEvent, Payload: []byte{native.EventDoor, 1},
	}); err != nil {
		t.Fatal(err)
	}
	deviceEvent := readRawNotification(t, standard, "controller.event")
	if !strings.Contains(string(deviceEvent), `"kind":"door"`) ||
		!strings.Contains(string(deviceEvent), `"source":"board"`) {
		t.Fatalf("asynchronous board event=%s", deviceEvent)
	}

	reset := rawRPC(t, standard, 7, "controller.reset", map[string]any{"pulse_ms": 50})
	if len(reset["error"]) == 0 ||
		(!strings.Contains(string(reset["error"]), "control lines") &&
			!strings.Contains(string(reset["error"]), "does not support DTR/RTS")) {
		t.Fatalf("network reset did not return a correlated transport error: %v", reset)
	}
	message := rawRPC(t, standard, 8, "controller.message.send", map[string]any{
		"source": "client", "target": "host", "type": "actionable.notice",
		"text": "show diagnostics", "action": "app.page:events",
	})
	if len(message["error"]) != 0 ||
		!strings.Contains(string(message["result"]), `"source":"websocket"`) ||
		!strings.Contains(string(message["result"]), `"claimed_source":"client"`) ||
		!strings.Contains(string(message["result"]), `"action":"app.page:events"`) {
		t.Fatalf("typed actionable message=%v", message)
	}

	closed := rawRPC(t, standard, 9, "controller.close", nil)
	if !strings.Contains(string(closed["result"]), `"closed":true`) {
		t.Fatalf("close response=%v", closed)
	}
	snapshot = rawRPC(t, standard, 10, "controller.snapshot", nil)
	if !strings.Contains(string(snapshot["result"]), `"connected":false`) ||
		!strings.Contains(string(snapshot["result"]), `"paused":true`) {
		t.Fatalf("explicit close did not pause reconnect: %s", snapshot["result"])
	}
	reopened := rawRPC(t, standard, 11, "controller.open", map[string]any{
		"port": board.endpoint(),
	})
	if len(reopened["error"]) != 0 ||
		!strings.Contains(string(reopened["result"]), `"connected":true`) ||
		!strings.Contains(string(reopened["result"]), `"paused":false`) {
		t.Fatalf("explicit reopen did not resume lifecycle: %v", reopened)
	}
	_ = nextBoardConnection(t, board)

	socketIO := dialRawWebSocket(
		t,
		address,
		"/socket.io/?EIO=4&transport=websocket",
		token,
	)
	defer socketIO.close()
	engineOpen, err := socketIO.readText(2 * time.Second)
	if err != nil || !strings.HasPrefix(engineOpen, "0{") ||
		!strings.Contains(engineOpen, `"pingInterval":25000`) {
		t.Fatalf("Engine.IO open=%q err=%v", engineOpen, err)
	}
	if err = socketIO.writeText("2"); err != nil {
		t.Fatal(err)
	}
	if pong, err := socketIO.readText(time.Second); err != nil || pong != "3" {
		t.Fatalf("Engine.IO pong=%q err=%v", pong, err)
	}
	if err = socketIO.writeText("40"); err != nil {
		t.Fatal(err)
	}
	if connected, err := socketIO.readText(time.Second); err != nil || !strings.HasPrefix(connected, "40") {
		t.Fatalf("Socket.IO connect=%q err=%v", connected, err)
	}
	if err = socketIO.writeText(`42["rpc",{"jsonrpc":"2.0","id":201,"method":"controller.ping"}]`); err != nil {
		t.Fatal(err)
	}
	socketResponse := readRawSocketIOEvent(t, socketIO, "rpc.response")
	if !strings.Contains(string(socketResponse), `"id":201`) ||
		!strings.Contains(string(socketResponse), `"ok":true`) || strings.Contains(string(socketResponse), `api_version`) {
		t.Fatalf("Socket.IO RPC response=%s", socketResponse)
	}
	if err = socketIO.writeText(`42["message",{"source":"client","target":"host","type":"notice","text":"raw Socket.IO"}]`); err != nil {
		t.Fatal(err)
	}
	accepted := readRawSocketIOEvent(t, socketIO, "message.accepted")
	if !strings.Contains(string(accepted), `"source":"socket_io"`) ||
		!strings.Contains(string(accepted), `"claimed_source":"client"`) {
		t.Fatalf("Socket.IO typed message=%s", accepted)
	}

	quit := rawRPC(t, standard, 12, "controller.quit", nil)
	if !strings.Contains(string(quit["result"]), `"accepted":true`) {
		t.Fatalf("quit response=%v", quit)
	}
	select {
	case <-shutdown:
	case <-time.After(time.Second):
		t.Fatal("quit callback was not invoked")
	}
}
