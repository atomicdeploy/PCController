package ipcjson

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pccontroller.local/controller/internal/appconfig"
)

func TestAlphaAuthorizationDisabledAcrossHTTPRawIPCAndWebSocket(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	service.AuthorizationDisabled = true
	alphaConfig := appconfig.Defaults()
	alphaConfig.IPC.AllowRemote = true
	alphaConfig.IPC.AllowedOrigins = []string{"allowed.example:*"}
	service.HostConfig = func() appconfig.Config { return alphaConfig }
	handler := websocketMux(context.Background(), service)

	uiRequest := httptest.NewRequest(http.MethodGet, "http://controller.example/api/ui-config", nil)
	uiResponse := httptest.NewRecorder()
	handler.ServeHTTP(uiResponse, uiRequest)
	if uiResponse.Code != http.StatusOK || !strings.Contains(uiResponse.Body.String(), `"auth_required":false`) {
		t.Fatalf("UI config status=%d body=%s", uiResponse.Code, uiResponse.Body.String())
	}
	for _, authRoute := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, ServerProofPath},
		{http.MethodPost, SessionTicketPath},
	} {
		request := httptest.NewRequest(authRoute.method, "http://controller.example"+authRoute.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "disabled in the immediate alpha") {
			t.Fatalf("dormant auth route %s status=%d body=%s", authRoute.path, response.Code, response.Body.String())
		}
	}

	httpRequest := httptest.NewRequest(http.MethodPost, "http://controller.example/api/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`))
	httpRequest.RemoteAddr = "198.51.100.10:45000"
	httpRequest.Header.Set("Content-Type", "application/json")
	httpResponse := httptest.NewRecorder()
	handler.ServeHTTP(httpResponse, httpRequest)
	if httpResponse.Code != http.StatusOK || httpResponse.Header().Get("X-PCController-Authentication") != "disabled-alpha" || !strings.Contains(httpResponse.Body.String(), `"ok":true`) {
		t.Fatalf("alpha HTTP status=%d headers=%v body=%s", httpResponse.Code, httpResponse.Header(), httpResponse.Body.String())
	}
	allowedOrigin := httptest.NewRequest(http.MethodPost, "http://controller.example/api/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":11,"method":"controller.ping"}`))
	allowedOrigin.RemoteAddr = "198.51.100.10:45001"
	allowedOrigin.Header.Set("Content-Type", "application/json")
	allowedOrigin.Header.Set("Origin", "http://allowed.example:4444")
	allowedResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedResponse, allowedOrigin)
	if allowedResponse.Code != http.StatusOK {
		t.Fatalf("allowed alpha browser origin status=%d body=%s", allowedResponse.Code, allowedResponse.Body.String())
	}
	forbiddenOrigin := httptest.NewRequest(http.MethodPost, "http://controller.example/api/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":12,"method":"controller.ping"}`))
	forbiddenOrigin.RemoteAddr = "198.51.100.10:45002"
	forbiddenOrigin.Header.Set("Origin", "https://forbidden.example")
	forbiddenResponse := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenResponse, forbiddenOrigin)
	if forbiddenResponse.Code != http.StatusForbidden {
		t.Fatalf("forbidden alpha browser origin status=%d body=%s", forbiddenResponse.Code, forbiddenResponse.Body.String())
	}
	urlSecret := httptest.NewRequest(http.MethodPost, "http://controller.example/api/rpc?access_token=must-not-travel", strings.NewReader(`{"jsonrpc":"2.0","id":13,"method":"controller.ping"}`))
	urlSecretResponse := httptest.NewRecorder()
	handler.ServeHTTP(urlSecretResponse, urlSecret)
	if urlSecretResponse.Code != http.StatusBadRequest || !strings.Contains(urlSecretResponse.Body.String(), "URL credentials are not accepted") {
		t.Fatalf("alpha URL secret status=%d body=%s", urlSecretResponse.Code, urlSecretResponse.Body.String())
	}

	var rawOutput bytes.Buffer
	if err := serveStreams(context.Background(), strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"controller.ping"}`+"\n"), &rawOutput, service, Access{Remote: true, Transport: "ipc"}); err != nil {
		t.Fatal(err)
	}
	var rawResponse Response
	if err := json.NewDecoder(&rawOutput).Decode(&rawResponse); err != nil || rawResponse.Error != nil {
		t.Fatalf("raw response=%s decode=%v", rawOutput.String(), err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ipc", nil)
	if err != nil {
		t.Fatalf("unauthenticated WebSocket response=%v err=%v", response, err)
	}
	defer connection.CloseNow()
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":3,"method":"controller.ping"}`)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(ctx)
	if err != nil || !strings.Contains(string(payload), `"ok":true`) {
		t.Fatalf("WebSocket payload=%s err=%v", payload, err)
	}

	socketIO, response, err := websocket.Dial(
		ctx,
		"ws"+strings.TrimPrefix(server.URL, "http")+"/socket.io/?EIO=4&transport=websocket",
		nil,
	)
	if err != nil {
		t.Fatalf("unauthenticated Socket.IO response=%v err=%v", response, err)
	}
	defer socketIO.CloseNow()
	_, packet, err := socketIO.Read(ctx)
	if err != nil || !strings.HasPrefix(string(packet), "0{") {
		t.Fatalf("Socket.IO open packet=%q err=%v", packet, err)
	}
	if err = socketIO.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
		t.Fatal(err)
	}
	_, packet, err = socketIO.Read(ctx)
	if err != nil || !strings.HasPrefix(string(packet), "40{") {
		t.Fatalf("Socket.IO connect packet=%q err=%v", packet, err)
	}
	if err = socketIO.Write(ctx, websocket.MessageText, []byte(
		`42["rpc",{"jsonrpc":"2.0","id":4,"method":"controller.ping"}]`,
	)); err != nil {
		t.Fatal(err)
	}
	_, packet, err = socketIO.Read(ctx)
	if err != nil || !strings.Contains(string(packet), `"rpc.response"`) ||
		!strings.Contains(string(packet), `"ok":true`) {
		t.Fatalf("credentialless Socket.IO RPC packet=%q err=%v", packet, err)
	}
}

func TestAlphaAuthorizationBypassStillHonorsHotDisabledRemoteAccess(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	service.AuthorizationDisabled = true
	alphaConfig := appconfig.Defaults()
	alphaConfig.IPC.AllowRemote = true
	service.HostConfig = func() appconfig.Config { return alphaConfig }
	handler := websocketMux(context.Background(), service)

	// Model `network edge-disable` hot-applying AllowRemote=false while the old
	// LAN listener remains bound for the lifetime of the primary process.
	alphaConfig.IPC.AllowRemote = false
	request := httptest.NewRequest(http.MethodPost, "http://controller.example/api/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`))
	request.RemoteAddr = "198.51.100.10:45000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "remote network access is disabled") {
		t.Fatalf("hot-disabled alpha HTTP status=%d body=%s", response.Code, response.Body.String())
	}
	if err := service.authorizeCapability(Access{Remote: true, Transport: "ipc"}, "controller.ping", capabilityRead); err == nil || !strings.Contains(err.Error(), "remote network access is disabled") {
		t.Fatalf("hot-disabled raw/bridge capability error=%v", err)
	}

	websocketRequest := httptest.NewRequest(http.MethodGet, "http://controller.example/ipc", nil)
	websocketRequest.RemoteAddr = "198.51.100.10:45001"
	websocketRequest.Header.Set("Connection", "Upgrade")
	websocketRequest.Header.Set("Upgrade", "websocket")
	websocketResponse := httptest.NewRecorder()
	handler.ServeHTTP(websocketResponse, websocketRequest)
	if websocketResponse.Code != http.StatusForbidden || !strings.Contains(websocketResponse.Body.String(), "remote network access is disabled") {
		t.Fatalf("hot-disabled WebSocket status=%d body=%s", websocketResponse.Code, websocketResponse.Body.String())
	}
}

func TestAlphaRemoteSubscriptionsCloseWhenAllowRemoteIsHotDisabled(t *testing.T) {
	for _, transport := range []string{"websocket", "socket_io"} {
		t.Run(transport, func(t *testing.T) {
			service, client := testAuthenticatedService(t)
			defer client.Shutdown()
			service.AuthorizationDisabled = true
			config := appconfig.Defaults()
			config.IPC.AllowRemote = true
			service.HostConfig = func() appconfig.Config { return config }
			updates := make(chan appconfig.Config, 1)
			service.SubscribeHostConfig = func(context.Context) <-chan appconfig.Config {
				return updates
			}
			serverContext, stopServer := context.WithCancel(context.Background())
			defer stopServer()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				access := Access{
					Remote: true, Transport: transport, Principal: "alpha-peer",
					Authentication: "disabled-alpha", authenticated: true,
				}
				if transport == "socket_io" {
					serveSocketIO(serverContext, writer, request, service, access)
					return
				}
				serveWebSocket(serverContext, writer, request, service, access)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			path := "/ipc"
			if transport == "socket_io" {
				path = "/socket.io/?EIO=4&transport=websocket"
			}
			connection, response, err := websocket.Dial(
				ctx, "ws"+strings.TrimPrefix(server.URL, "http")+path, nil,
			)
			if err != nil {
				t.Fatalf("dial response=%v err=%v", response, err)
			}
			defer connection.CloseNow()
			if transport == "socket_io" {
				if _, _, err = connection.Read(ctx); err != nil {
					t.Fatal(err)
				}
				if err = connection.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
					t.Fatal(err)
				}
				if _, _, err = connection.Read(ctx); err != nil {
					t.Fatal(err)
				}
				if err = connection.Write(ctx, websocket.MessageText, []byte(`42["subscribe",{"topics":["events"]}]`)); err != nil {
					t.Fatal(err)
				}
			} else if err = connection.Write(ctx, websocket.MessageText, []byte(`{"jsonrpc":"2.0","id":8,"method":"controller.subscribe","params":{"topics":["events"]}}`)); err != nil {
				t.Fatal(err)
			}
			if _, _, err = connection.Read(ctx); err != nil {
				t.Fatalf("subscription acknowledgement: %v", err)
			}

			disabled := config
			disabled.IPC.AllowRemote = false
			updates <- disabled
			if _, _, err = connection.Read(ctx); err == nil {
				t.Fatal("remote subscription remained open after ipc.allow_remote was hot-disabled")
			}
		})
	}
}

func TestAlphaBridgeIngressCannotPivotThroughAnotherPeer(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	service.AuthorizationDisabled = true
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	service.HostConfig = func() appconfig.Config { return config }

	tests := []struct {
		name   string
		method string
		params string
	}{
		{"bridge call", "controller.bridge.call", `{"peer":"third","request":{"method":"controller.snapshot"}}`},
		{"peer host update", "controller.peer.update.host", `{"peer":"third","artifact_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
		{"discovery connect", "controller.discovery.connect", `{"endpoint":"third"}`},
		{"command bridge call", "controller.command.execute", `{"command":"bridge call third controller.snapshot"}`},
		{"command peer update", "controller.command.execute", `{"command":"peer-update host third aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
		{"app command bridge call", "controller.app.action", `{"kind":"command","value":"bridge call third controller.snapshot"}`},
		{"app command peer update", "controller.app.action", `{"kind":"command","value":"peer-update host third aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := service.DispatchRemote(context.Background(), Request{
				JSONRPC: Version, ID: json.RawMessage("1"), Method: test.method,
				Params: json.RawMessage(test.params),
			}, "bridge")
			if response.Error == nil || response.Error.Code != -32003 ||
				!strings.Contains(response.Error.Message, "may not pivot") {
				t.Fatalf("response=%#v", response)
			}
		})
	}
}
