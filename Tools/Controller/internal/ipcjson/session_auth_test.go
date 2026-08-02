package ipcjson

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

const testSessionCredential = "browser-session-test-credential"

func sessionTestService(t *testing.T) (*Service, *controllerapi.Client) {
	t.Helper()
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AuthToken = testSessionCredential
	return &Service{
		Client: client,
		HostConfig: func() appconfig.Config {
			return config
		},
	}, client
}

func issueSessionTicketForTest(
	t *testing.T,
	serverURL, origin, transport string,
) sessionTicketResponse {
	t.Helper()
	request, err := http.NewRequest(
		http.MethodPost,
		serverURL+SessionTicketPath,
		strings.NewReader(`{"transport":"`+transport+`"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testSessionCredential)
	request.Header.Set("Content-Type", "application/json")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("ticket status=%d", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" ||
		response.Header.Get("Pragma") != "no-cache" ||
		response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("ticket security headers=%v", response.Header)
	}
	var ticket sessionTicketResponse
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	return ticket
}

func dialBrowserTicket(
	serverURL, path, origin string,
	ticket sessionTicketResponse,
) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(
		context.Background(),
		"ws"+strings.TrimPrefix(serverURL, "http")+path,
		&websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{origin}},
			Subprotocols: []string{
				ticket.Protocol,
				browserTicketPrefix + ticket.Ticket,
			},
		},
	)
}

func TestBrowserSessionTicketUsesCleanURLAndIsSingleUse(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	ticket := issueSessionTicketForTest(t, server.URL, server.URL, "websocket")
	if ticket.Protocol != browserWebSocketProtocol || len(ticket.Ticket) != 64 ||
		ticket.ExpiresInMS != sessionTicketLifetime.Milliseconds() ||
		ticket.Principal != "controller-operator" || ticket.CorrelationID == "" {
		t.Fatalf("ticket=%#v", ticket)
	}
	connection, response, err := dialBrowserTicket(server.URL, "/ipc", server.URL, ticket)
	if err != nil {
		t.Fatalf("ticket dial response=%v err=%v", response, err)
	}
	defer connection.CloseNow()
	if connection.Subprotocol() != browserWebSocketProtocol {
		t.Fatalf("selected protocol=%q", connection.Subprotocol())
	}
	if response != nil {
		if response.Request != nil && response.Request.URL.RawQuery != "" {
			t.Fatalf("credential entered upgrade URL: %s", response.Request.URL)
		}
		negotiated := response.Header.Get("Sec-WebSocket-Protocol")
		if negotiated != browserWebSocketProtocol || strings.Contains(negotiated, ticket.Ticket) {
			t.Fatalf("ticket was echoed in upgrade response: %q", negotiated)
		}
	}
	if snapshot := fmt.Sprint(service.sessionTickets); strings.Contains(snapshot, ticket.Ticket) {
		t.Fatalf("raw ticket retained in server memory: %s", snapshot)
	}

	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`)
	if err = connection.Write(context.Background(), websocket.MessageText, request); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(context.Background())
	if err != nil || !strings.Contains(string(payload), `"ok":true`) {
		t.Fatalf("ping payload=%s err=%v", payload, err)
	}

	replayed, replayResponse, replayErr := dialBrowserTicket(server.URL, "/ipc", server.URL, ticket)
	if replayed != nil {
		_ = replayed.CloseNow()
	}
	if replayErr == nil || replayResponse == nil || replayResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed ticket err=%v response=%v", replayErr, replayResponse)
	}
}

func TestBrowserSessionTicketHasOneConcurrentWinner(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	ticket := issueSessionTicketForTest(t, server.URL, server.URL, "websocket")

	const contenders = 12
	start := make(chan struct{})
	var wait sync.WaitGroup
	var accepted atomic.Int32
	var rejected atomic.Int32
	wait.Add(contenders)
	for range contenders {
		go func() {
			defer wait.Done()
			<-start
			connection, response, err := dialBrowserTicket(server.URL, "/ipc", server.URL, ticket)
			if err == nil {
				accepted.Add(1)
				_ = connection.CloseNow()
				return
			}
			if response != nil && response.StatusCode == http.StatusUnauthorized {
				rejected.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()
	if accepted.Load() != 1 || rejected.Load() != contenders-1 {
		t.Fatalf("accepted=%d rejected=%d", accepted.Load(), rejected.Load())
	}
}

func TestBrowserSessionTicketIsOriginTransportAndExpiryBound(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	service.sessionClock = func() time.Time { return now }
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	for _, test := range []struct {
		name      string
		transport string
		path      string
		origin    string
		advance   time.Duration
	}{
		{name: "origin", transport: "websocket", path: "/ipc", origin: "http://localhost:9999"},
		{name: "transport", transport: "socket_io", path: "/ipc", origin: server.URL},
		{name: "expiry", transport: "websocket", path: "/ipc", origin: server.URL, advance: sessionTicketLifetime},
	} {
		t.Run(test.name, func(t *testing.T) {
			ticket := issueSessionTicketForTest(t, server.URL, server.URL, test.transport)
			now = now.Add(test.advance)
			connection, response, err := dialBrowserTicket(server.URL, test.path, test.origin, ticket)
			if connection != nil {
				_ = connection.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("bound ticket err=%v response=%v", err, response)
			}
			now = now.Add(-test.advance)
		})
	}
}

func TestSocketIOAcceptsItsOwnBrowserTicket(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	ticket := issueSessionTicketForTest(t, server.URL, server.URL, "socket_io")
	connection, response, err := dialBrowserTicket(
		server.URL, "/socket.io/?EIO=4&transport=websocket", server.URL, ticket,
	)
	if err != nil {
		t.Fatalf("Socket.IO ticket response=%v err=%v", response, err)
	}
	defer connection.CloseNow()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, packet, err := connection.Read(ctx)
	if err != nil || !strings.HasPrefix(string(packet), "0{") {
		t.Fatalf("Socket.IO open=%q err=%v", packet, err)
	}
}

func TestWebSocketURLCredentialsAreRejectedBeforeUpgrade(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	for _, path := range []string{
		"/ipc?access_token=" + testSessionCredential,
		"/ipc?ticket=" + strings.Repeat("a", 64),
		"/socket.io/?EIO=4&transport=websocket&access_token=" + testSessionCredential,
	} {
		connection, response, err := websocket.Dial(
			context.Background(),
			"ws"+strings.TrimPrefix(server.URL, "http")+path,
			&websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{server.URL}}},
		)
		if connection != nil {
			_ = connection.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("path=%s err=%v response=%v", path, err, response)
		}
	}
}

func TestMissingOriginPolicyPreservesAuthenticatedNativeClients(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"

	connection, response, err := websocket.Dial(
		context.Background(), websocketURL,
		&websocket.DialOptions{HTTPHeader: http.Header{
			"Authorization": []string{"Bearer " + testSessionCredential},
		}},
	)
	if err != nil {
		t.Fatalf("native header-auth response=%v err=%v", response, err)
	}
	_ = connection.CloseNow()

	ticket := issueSessionTicketForTest(t, server.URL, server.URL, "websocket")
	connection, response, err = websocket.Dial(
		context.Background(), websocketURL,
		&websocket.DialOptions{Subprotocols: []string{
			browserWebSocketProtocol, browserTicketPrefix + ticket.Ticket,
		}},
	)
	if connection != nil {
		_ = connection.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("originless browser ticket response=%v err=%v", response, err)
	}
}

func TestSessionTicketIssuanceRequiresAllowedOriginAndUnambiguousCredential(t *testing.T) {
	service, client := sessionTestService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	for _, test := range []struct {
		name       string
		origin     string
		headerAuth string
		headerKey  string
		want       int
	}{
		{name: "missing origin", headerAuth: testSessionCredential, want: http.StatusForbidden},
		{name: "hostile origin", origin: "https://hostile.example", headerAuth: testSessionCredential, want: http.StatusForbidden},
		{name: "conflicting headers", origin: server.URL, headerAuth: testSessionCredential, headerKey: "different", want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(
				http.MethodPost, server.URL+SessionTicketPath,
				strings.NewReader(`{"transport":"websocket"}`),
			)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.headerAuth != "" {
				request.Header.Set("Authorization", "Bearer "+test.headerAuth)
			}
			if test.headerKey != "" {
				request.Header.Set("X-PCController-Token", test.headerKey)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.want {
				t.Fatalf("status=%d want=%d", response.StatusCode, test.want)
			}
		})
	}
}

func TestRemoteAuthorizationAuditIsStructuredAndCorrelated(t *testing.T) {
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	defer client.Shutdown()
	config := appconfig.Defaults()
	config.IPC.AllowRemote = true
	config.IPC.AuthToken = testSessionCredential
	config.IPC.AllowedOrigins = []string{"console.example:*"}
	config.IPC.RemotePolicy.Messages = true
	service := &Service{Client: client, HostConfig: func() appconfig.Config { return config }}
	access := Access{
		Remote: true, Transport: "rest", Principal: "controller-operator",
		Origin: "https://console.example:9443", CorrelationID: "request-42",
		Authentication: "bearer", authenticated: true,
	}
	if err := service.authorizeCapability(
		access, "POST /api/v1/messages", capabilityMessages,
	); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	event, err := client.NextEvent(ctx, 0, "security.remote.authorized")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"principal": "controller-operator", "transport": "rest",
		"origin": "https://console.example:9443", "capability": capabilityMessages,
		"decision": "authorized", "correlation_id": "request-42",
		"authentication": "bearer", "operation": "POST /api/v1/messages",
	}
	for key, value := range want {
		if event.Metadata[key] != value {
			t.Errorf("metadata[%q]=%q want %q", key, event.Metadata[key], value)
		}
	}
	if event.Source != "security" || event.Action != "authorized" ||
		!strings.Contains(event.Text, "correlation=request-42") {
		t.Fatalf("audit event=%#v", event)
	}
}
