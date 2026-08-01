package ipcjson

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostos"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/shell"
)

func TestListenRejectsNonLoopback(t *testing.T) {
	if listener, err := Listen("0.0.0.0:8787"); err == nil {
		listener.Close()
		t.Fatal("expected non-loopback address to be rejected")
	}
}

func TestAppPageRPCPublishesValidatedTUIAction(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	broker := hostui.NewActionBroker()
	service := Service{Client: client, AppAction: broker.Publish}
	params, _ := json.Marshal(map[string]string{"page": "events"})
	response := service.Dispatch(context.Background(), Request{
		Method: "controller.app.page", Params: params,
	})
	if response.Error != nil {
		t.Fatal(response.Error)
	}
	select {
	case action := <-broker.Events():
		if action.Kind != "app.page" || action.Value != "events" || action.Source != "ipc" {
			t.Fatalf("action=%#v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("app.page did not reach TUI broker")
	}
}

func TestOSRPCSurfacesAreAuditedAndDisabledByDefault(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			if err := change(&config); err != nil {
				return err
			}
			return config.Validate()
		},
	}
	status := service.Dispatch(context.Background(), Request{Method: "controller.os.status"})
	if status.Error != nil {
		t.Fatal(status.Error)
	}
	keyParams, _ := json.Marshal(hostos.VirtualKeyRequest{Key: "F13"})
	key := service.Dispatch(context.Background(), Request{
		Method: "controller.os.key", Params: keyParams,
	})
	if key.Error == nil || !strings.Contains(key.Error.Message, "disabled") {
		t.Fatalf("disabled virtual key RPC=%#v", key)
	}
	powerParams, _ := json.Marshal(hostos.PowerRequest{
		Action: "lock", Confirmation: "CONFIRM",
	})
	power := service.Dispatch(context.Background(), Request{
		Method: "controller.os.power", Params: powerParams,
	})
	if power.Error == nil || !strings.Contains(power.Error.Message, "disabled") {
		t.Fatalf("disabled power RPC=%#v", power)
	}
	policy := config.OSActions
	policy.VirtualKeys.Allowed = append(policy.VirtualKeys.Allowed, "F14")
	policyParams, _ := json.Marshal(policy)
	configured := service.Dispatch(context.Background(), Request{
		Method: "controller.os.configure", Params: policyParams,
	})
	if configured.Error != nil || len(config.OSActions.VirtualKeys.Allowed) != 6 {
		t.Fatalf("OS policy configure=%#v config=%#v", configured, config.OSActions)
	}
}

func TestHostMenuConfigRPCAndRESTUsePersistentHostConfig(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	service := &Service{
		Client: client, HostConfig: func() appconfig.Config { return config },
		UpdateHostConfig: func(change func(*appconfig.Config) error) error {
			candidate := config
			if err := change(&candidate); err != nil {
				return err
			}
			if err := candidate.Validate(); err != nil {
				return err
			}
			config = candidate
			return nil
		},
	}
	get := service.Dispatch(context.Background(), Request{Method: "controller.host_menu.config.get"})
	if get.Error != nil {
		t.Fatal(get.Error)
	}
	updated := config.HostMenus
	updated.Menus[0].Label = "MAIN"
	encoded, _ := json.Marshal(updated)
	set := service.Dispatch(context.Background(), Request{Method: "controller.host_menu.config.set", Params: encoded})
	if set.Error != nil || config.HostMenus.Menus[0].Label != "MAIN" {
		t.Fatalf("host-menu RPC set=%#v label=%q", set, config.HostMenus.Menus[0].Label)
	}

	server := httptest.NewServer(websocketMux(service))
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/host-menus")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"label":"MAIN"`) {
		t.Fatalf("host-menu REST GET status=%d body=%s", response.StatusCode, body)
	}
	updated.Menus[0].Content = "REST applied"
	payload, _ := json.Marshal(updated)
	request, _ := http.NewRequest(http.MethodPut, server.URL+"/api/v1/host-menus", strings.NewReader(string(payload)))
	request.Header.Set("Content-Type", "application/json")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || config.HostMenus.Menus[0].Content != "REST applied" {
		t.Fatalf("host-menu REST PUT status=%d body=%s content=%q", response.StatusCode, body, config.HostMenus.Menus[0].Content)
	}
}

func TestListenAllowsExplicitRemoteBind(t *testing.T) {
	listener, err := ListenWithRemote("0.0.0.0:0", true)
	if err != nil {
		t.Fatal(err)
	}
	_ = listener.Close()
}

func TestHTTPRESTAndAuthenticationShareIPCListener(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	const token = "0123456789abcdefghijklmn"
	go func() {
		done <- Serve(ctx, listener, &Service{
			Client: client, WebSocketPath: "/ipc", AuthToken: token,
			InboundWebhooks: true,
		})
	}()

	base := "http://" + listener.Addr().String()
	response, err := http.Get(base + "/api/v1/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d", response.StatusCode)
	}

	snapshotRequest, err := http.NewRequestWithContext(
		ctx, http.MethodGet, base+"/api/v1/snapshot", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRequest.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(snapshotRequest)
	if err != nil {
		t.Fatal(err)
	}
	snapshotBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(snapshotBody), `"uptime_ms":0`) ||
		strings.Contains(string(snapshotBody), `"UptimeMS"`) {
		t.Fatalf(
			"snapshot JSON status=%d body=%s err=%v",
			response.StatusCode, snapshotBody, readErr,
		)
	}

	osStatusRequest, err := http.NewRequestWithContext(
		ctx, http.MethodGet, base+"/api/v1/os/status", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	osStatusRequest.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(osStatusRequest)
	if err != nil {
		t.Fatal(err)
	}
	osStatusBody, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(osStatusBody), `"serial_discovery_source"`) ||
		!strings.Contains(string(osStatusBody), "Windows SetupAPI") {
		t.Fatalf("OS status=%d body=%s err=%v", response.StatusCode, osStatusBody, readErr)
	}

	for path, body := range map[string]string{
		"/api/v1/os/key":   `{"key":"F13"}`,
		"/api/v1/os/power": `{"action":"lock","confirmation":"CONFIRM"}`,
	} {
		osRequest, requestErr := http.NewRequestWithContext(
			ctx, http.MethodPost, base+path, strings.NewReader(body),
		)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		osRequest.Header.Set("Authorization", "Bearer "+token)
		osRequest.Header.Set("Content-Type", "application/json")
		response, requestErr = http.DefaultClient.Do(osRequest)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		deniedBody, responseErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if responseErr != nil || response.StatusCode != http.StatusForbidden ||
			!strings.Contains(string(deniedBody), "disabled") {
			t.Fatalf("disabled OS action %s status=%d body=%s err=%v", path, response.StatusCode, deniedBody, responseErr)
		}
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		base+"/api/v1/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK ||
		!strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("authenticated HTTP response status=%d body=%s err=%v", response.StatusCode, body, err)
	}

	rawResponse, err := Call(ctx, listener.Addr().String(), Request{
		Method: "controller.ping", Auth: token,
	})
	if err != nil || rawResponse.Error != nil {
		t.Fatalf("authenticated raw response=%#v err=%v", rawResponse, err)
	}
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete,
	} {
		request, err := http.NewRequestWithContext(
			ctx, method,
			base+"/api/v1/webhooks/inbound?text=door+event&type=automation",
			strings.NewReader(`{"value":1}`),
		)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-PCController-Token", token)
		request.Header.Set("X-Test-Event", "method-"+method)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("inbound webhook %s status=%d", method, response.StatusCode)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("authenticated IPC server did not stop")
	}
}

func TestSocketIOEngineV4WebSocketAdapter(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, &Service{
			Client: client, WebSocketPath: "/ipc", SocketIOPath: "/socket.io/",
		})
	}()
	clientContext, stop := context.WithTimeout(ctx, 4*time.Second)
	defer stop()
	connection, _, err := websocket.Dial(
		clientContext,
		"ws://"+listener.Addr().String()+"/socket.io/?EIO=4&transport=websocket",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	readPacket := func() string {
		_, data, readErr := connection.Read(clientContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		return string(data)
	}
	writePacket := func(packet string) {
		if err := connection.Write(clientContext, websocket.MessageText, []byte(packet)); err != nil {
			t.Fatal(err)
		}
	}
	if packet := readPacket(); !strings.HasPrefix(packet, "0{") {
		t.Fatalf("Engine.IO open=%q", packet)
	}
	writePacket("40")
	if packet := readPacket(); !strings.HasPrefix(packet, "40{") {
		t.Fatalf("Socket.IO connect=%q", packet)
	}
	writePacket(`42["subscribe",{"topics":["events"]}]`)
	for {
		packet := readPacket()
		if strings.HasPrefix(packet, "42") {
			name, _, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "subscribed" {
				break
			}
		}
	}
	runtime.PublishHostEvent("door", "door opened")
	for {
		packet := readPacket()
		if strings.HasPrefix(packet, "42") {
			name, _, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "controller.event" {
				break
			}
		}
	}
	writePacket(`42["rpc",{"jsonrpc":"2.0","id":7,"method":"controller.ping"}]`)
	for {
		packet := readPacket()
		if strings.HasPrefix(packet, "42") {
			name, raw, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "rpc.response" {
				if !strings.Contains(string(raw), `"ok":true`) {
					t.Fatalf("rpc response payload=%s", raw)
				}
				break
			}
		}
	}
	writePacket(`42["rpc",{"jsonrpc":"2.0","id":8,"method":"controller.snapshot"}]`)
	for {
		packet := readPacket()
		if strings.HasPrefix(packet, "42") {
			name, raw, err := decodeSocketIOEvent(packet[2:])
			if err == nil && name == "rpc.response" &&
				strings.Contains(string(raw), `"id":8`) {
				if !strings.Contains(string(raw), `"uptime_ms":0`) ||
					strings.Contains(string(raw), `"UptimeMS"`) {
					t.Fatalf("Socket.IO snapshot payload=%s", raw)
				}
				break
			}
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Socket.IO server did not stop")
	}
}

func TestRawJSONRPCAndWebSocketShareOneIPCListener(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, listener, &Service{
			Client: client, WebSocketPath: "/ipc",
		})
	}()

	websocketContext, stopWebSocket := context.WithTimeout(ctx, 3*time.Second)
	defer stopWebSocket()
	connection, _, err := websocket.Dial(
		websocketContext,
		"ws://"+listener.Addr().String()+"/ipc",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	writeRPC := func(value any) {
		encoded, encodeErr := json.Marshal(value)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if writeErr := connection.Write(
			websocketContext,
			websocket.MessageText,
			encoded,
		); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	writeRPC(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "controller.subscribe",
		"params": map[string]any{"topics": []string{"events"}},
	})

	// The legacy NDJSON client remains usable while the WebSocket is active on
	// the exact same TCP address.
	rawContext, stopRaw := context.WithTimeout(ctx, time.Second)
	response, err := Call(rawContext, listener.Addr().String(), Request{
		Method: "controller.ping",
	})
	stopRaw()
	if err != nil || response.Error != nil {
		t.Fatalf("raw IPC ping response=%#v err=%v", response, err)
	}

	// Consume the subscription acknowledgement, then prove an event is pushed
	// without status polling or a second connection.
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var message map[string]any
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message["id"] != nil {
			break
		}
	}
	runtime.PublishHostEvent("door", "door opened")
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var message struct {
			Method string `json:"method"`
			Params struct {
				Kind string `json:"kind"`
			} `json:"params"`
		}
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Method == "controller.event" && message.Params.Kind == "door" {
			break
		}
	}

	writeRPC(map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "controller.snapshot",
	})
	for {
		_, data, readErr := connection.Read(websocketContext)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var envelope struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if string(envelope.ID) != "2" {
			continue
		}
		if !strings.Contains(string(data), `"uptime_ms":0`) ||
			strings.Contains(string(data), `"UptimeMS"`) {
			t.Fatalf("WebSocket snapshot payload=%s", data)
		}
		break
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("multiplexed IPC server did not stop")
	}
}

func TestCallRoundTripParseError(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		var request Request
		_ = json.NewDecoder(connection).Decode(&request)
		_ = json.NewEncoder(connection).Encode(Response{
			JSONRPC: Version,
			ID:      request.ID,
			Result:  map[string]bool{"ok": true},
		})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := Call(ctx, listener.Addr().String(), Request{
		Method: "controller.ping",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Result == nil {
		t.Fatal("missing result")
	}
}
