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
}
