package ipcjson

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestHTTPControlPlaneRejectsCrossOriginBrowserRequests(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	handler := websocketMux(context.Background(), &Service{Client: client})

	request := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:8787/api/v1/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`),
	)
	request.Header.Set("Origin", "https://hostile.example")
	request.Header.Set("Content-Type", "text/plain")
	request.RemoteAddr = "127.0.0.1:49200"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), "origin") {
		t.Fatalf("cross-origin status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWebSocketRejectsUntrustedBrowserOriginWithoutToken(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	server := httptest.NewServer(websocketMux(context.Background(), &Service{Client: client}))
	defer server.Close()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"
	connection, response, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{"https://hostile.example"}},
	})
	if connection != nil {
		_ = connection.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("untrusted websocket err=%v response=%v", err, response)
	}
}

func TestHTTPJSONRPCRequiresJSONContentType(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	handler := websocketMux(context.Background(), &Service{Client: client})
	body := `{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`

	request := httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:8787/api/v1/rpc",
		strings.NewReader(body),
	)
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	request.Header.Set("Content-Type", "text/plain")
	request.RemoteAddr = "127.0.0.1:49201"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(
		http.MethodPost,
		"http://127.0.0.1:8787/api/v1/rpc",
		strings.NewReader(body),
	)
	request.Header.Set("Origin", "http://127.0.0.1:8787")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.RemoteAddr = "127.0.0.1:49202"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("application/json status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPControlPlaneAllowsConfiguredOriginAndNativeClient(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	handler := websocketMux(context.Background(), &Service{
		Client: client, AllowedOrigins: []string{"console.example:*"},
	})
	body := `{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`

	for _, origin := range []string{"https://console.example:9443", ""} {
		request := httptest.NewRequest(
			http.MethodPost,
			"http://127.0.0.1:8787/api/v1/rpc",
			strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "127.0.0.1:49203"
		if origin != "" {
			request.Header.Set("Origin", origin)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("origin=%q status=%d body=%s", origin, response.Code, response.Body.String())
		}
	}
}

func TestHTTPControlPlaneCORSUsesConfiguredOriginWithoutCredentials(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	handler := websocketMux(context.Background(), &Service{
		Client: client, AllowedOrigins: []string{"console.example:*"},
	})

	preflight := httptest.NewRequest(
		http.MethodOptions,
		"http://127.0.0.1:8787/api/v1/rpc",
		nil,
	)
	preflight.Header.Set("Origin", "https://console.example:9443")
	preflight.Header.Set("Access-Control-Request-Method", http.MethodPost)
	preflight.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	preflight.RemoteAddr = "127.0.0.1:49204"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preflight)
	if response.Code != http.StatusNoContent {
		t.Fatalf("preflight status=%d body=%s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example:9443" {
		t.Fatalf("Access-Control-Allow-Origin=%q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentialed CORS was enabled: %q", got)
	}
	if !strings.Contains(response.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatalf("Access-Control-Allow-Headers=%q", response.Header().Get("Access-Control-Allow-Headers"))
	}

	configRequest := httptest.NewRequest(
		http.MethodGet,
		"http://127.0.0.1:8787/api/v1/ui-config",
		nil,
	)
	configRequest.Header.Set("Origin", "https://console.example:9443")
	configRequest.RemoteAddr = "127.0.0.1:49205"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, configRequest)
	if response.Code != http.StatusOK || response.Header().Get("Access-Control-Allow-Origin") != "https://console.example:9443" {
		t.Fatalf("cross-origin config status=%d origin=%q body=%s", response.Code, response.Header().Get("Access-Control-Allow-Origin"), response.Body.String())
	}
}

func TestHTTPControlPlaneCORSRejectsUntrustedOriginAndHeaders(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	handler := websocketMux(context.Background(), &Service{
		Client: client, AllowedOrigins: []string{"console.example:*"},
	})

	for _, test := range []struct {
		name    string
		origin  string
		headers string
	}{
		{name: "origin", origin: "https://hostile.example", headers: "content-type"},
		{name: "header", origin: "https://console.example:9443", headers: "x-forwarded-user"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:8787/api/v1/rpc", nil)
			request.Header.Set("Origin", test.origin)
			request.Header.Set("Access-Control-Request-Method", http.MethodPost)
			request.Header.Set("Access-Control-Request-Headers", test.headers)
			request.RemoteAddr = "127.0.0.1:49206"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.name == "origin" && response.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("untrusted origin was reflected: %q", response.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}
