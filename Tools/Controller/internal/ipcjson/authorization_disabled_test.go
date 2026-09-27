package ipcjson

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestAuthorizationDisabledMustBeExplicitAcrossHTTPAndJSONRPC(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	handler := websocketMux(context.Background(), service)

	uiAuthRequired := func() bool {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "http://controller.example/api/ui-config", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("UI config status=%d body=%s", response.Code, response.Body.String())
		}
		var config struct {
			AuthRequired bool `json:"auth_required"`
		}
		if err := json.NewDecoder(response.Body).Decode(&config); err != nil {
			t.Fatal(err)
		}
		return config.AuthRequired
	}
	remoteHTTPRPC := func() *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(
			http.MethodPost,
			"http://controller.example/api/rpc",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`),
		)
		request.RemoteAddr = "198.51.100.10:45000"
		request.Header.Set("Authorization", "not-a-bearer-token")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	remoteRawRPC := func() Response {
		t.Helper()
		input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}` + "\n")
		var output bytes.Buffer
		if err := serveStreams(
			context.Background(), input, &output, service,
			Access{Remote: true, Transport: "ipc"},
		); err != nil {
			t.Fatal(err)
		}
		var response Response
		if err := json.NewDecoder(&output).Decode(&response); err != nil {
			t.Fatalf("decode raw JSON-RPC %q: %v", output.String(), err)
		}
		return response
	}

	if !uiAuthRequired() {
		t.Fatal("zero-value authorization mode hid the configured token")
	}
	strictHTTP := remoteHTTPRPC()
	if strictHTTP.Code != http.StatusUnauthorized ||
		!strings.Contains(strictHTTP.Body.String(), "Bearer") {
		t.Fatalf("strict HTTP status=%d body=%s", strictHTTP.Code, strictHTTP.Body.String())
	}
	strictRaw := remoteRawRPC()
	if strictRaw.Error == nil || strictRaw.Error.Code != -32001 {
		t.Fatalf("strict raw JSON-RPC response=%#v", strictRaw)
	}
	strictRemote := service.DispatchRemote(
		context.Background(), Request{Method: "controller.snapshot"}, "bridge",
	)
	if strictRemote.Error == nil || strictRemote.Error.Code != -32003 ||
		!strings.Contains(strictRemote.Error.Message, "remote network access") {
		t.Fatalf("strict remote capability response=%#v", strictRemote)
	}

	service.AuthorizationDisabled = true
	if uiAuthRequired() {
		t.Fatal("alpha authorization mode still advertised authentication")
	}
	alphaHTTP := remoteHTTPRPC()
	if alphaHTTP.Code != http.StatusOK ||
		alphaHTTP.Header().Get("X-PCController-Authentication") != "disabled" ||
		alphaHTTP.Header().Get("X-PCController-Principal") != "test-operator" ||
		!strings.Contains(alphaHTTP.Body.String(), `"ok":true`) {
		t.Fatalf("alpha HTTP status=%d headers=%v body=%s", alphaHTTP.Code, alphaHTTP.Header(), alphaHTTP.Body.String())
	}
	alphaRaw := remoteRawRPC()
	if alphaRaw.Error != nil {
		t.Fatalf("alpha raw JSON-RPC response=%#v", alphaRaw)
	}
	alphaRemote := service.DispatchRemote(
		context.Background(), Request{Method: "controller.snapshot"}, "bridge",
	)
	if alphaRemote.Error != nil {
		t.Fatalf("alpha remote capability response=%#v", alphaRemote)
	}
}

func TestAuthorizationDisabledCoversWebSocketAndSocketIO(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "strict"
		if enabled {
			name = "alpha-disabled"
		}
		t.Run(name, func(t *testing.T) {
			for _, transport := range []struct {
				name string
				path string
			}{
				{name: "websocket", path: "/ipc"},
				{name: "socket-io", path: "/socket.io/?EIO=4&transport=websocket"},
			} {
				t.Run(transport.name, func(t *testing.T) {
					service, client := testAuthenticatedService(t)
					defer client.Shutdown()
					service.AuthorizationDisabled = enabled
					server := httptest.NewServer(websocketMux(context.Background(), service))
					defer server.Close()

					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					connection, response, err := websocket.Dial(
						ctx,
						"ws"+strings.TrimPrefix(server.URL, "http")+transport.path,
						nil,
					)
					if !enabled {
						if connection != nil {
							_ = connection.CloseNow()
						}
						if response != nil && response.Body != nil {
							_, _ = io.Copy(io.Discard, response.Body)
							_ = response.Body.Close()
						}
						if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
							t.Fatalf("strict dial err=%v response=%v", err, response)
						}
						return
					}
					if err != nil {
						t.Fatalf("alpha dial response=%v err=%v", response, err)
					}
					defer connection.CloseNow()

					if transport.name == "websocket" {
						if err := connection.Write(ctx, websocket.MessageText, []byte(
							`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`,
						)); err != nil {
							t.Fatal(err)
						}
						_, payload, err := connection.Read(ctx)
						if err != nil || !strings.Contains(string(payload), `"ok":true`) {
							t.Fatalf("WebSocket payload=%s err=%v", payload, err)
						}
						return
					}

					_, packet, err := connection.Read(ctx)
					if err != nil || !strings.HasPrefix(string(packet), "0{") {
						t.Fatalf("Engine.IO open=%q err=%v", packet, err)
					}
					if err := connection.Write(ctx, websocket.MessageText, []byte("40")); err != nil {
						t.Fatal(err)
					}
					_, packet, err = connection.Read(ctx)
					if err != nil || !strings.HasPrefix(string(packet), "40{") {
						t.Fatalf("Socket.IO connect=%q err=%v", packet, err)
					}
					if err := connection.Write(ctx, websocket.MessageText, []byte(
						`42["rpc",{"jsonrpc":"2.0","id":1,"method":"controller.ping"}]`,
					)); err != nil {
						t.Fatal(err)
					}
					_, packet, err = connection.Read(ctx)
					if err != nil || !strings.Contains(string(packet), `"rpc.response"`) ||
						!strings.Contains(string(packet), `"ok":true`) {
						t.Fatalf("Socket.IO RPC=%q err=%v", packet, err)
					}
				})
			}
		})
	}
}
