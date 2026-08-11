package ipcjson

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

const testAccessToken = "browser-session-ticket-test-token"

func testAuthenticatedService(t *testing.T) (*Service, *controllerapi.Client) {
	t.Helper()
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AuthToken = testAccessToken
	config.IPC.RemotePrincipal = "test-operator"
	return &Service{Client: client, HostConfig: func() appconfig.Config { return config }}, client
}

func issueBrowserTicket(
	t *testing.T,
	serverURL string,
	origin string,
	transport string,
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
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("ticket status=%d", response.StatusCode)
	}
	if response.Header.Get("Cache-Control") != "no-store" ||
		response.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("ticket cache/referrer headers=%v", response.Header)
	}
	var ticket sessionTicketResponse
	if err = json.NewDecoder(response.Body).Decode(&ticket); err != nil {
		t.Fatal(err)
	}
	return ticket
}

func TestBrowserSessionTicketAuthenticatesCleanURLOnce(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	ticket := issueBrowserTicket(t, server.URL, server.URL, "websocket")
	if ticket.Principal != "test-operator" || ticket.Protocol != browserWebSocketProtocol ||
		len(ticket.Ticket) != 64 || ticket.ExpiresInMS != sessionTicketLifetime.Milliseconds() {
		t.Fatalf("ticket=%#v", ticket)
	}
	digest := sha256.Sum256([]byte(ticket.Ticket))
	service.sessionMu.Lock()
	_, digestStored := service.sessionTickets[digest]
	cacheSnapshot := fmt.Sprint(service.sessionTickets)
	service.sessionMu.Unlock()
	if !digestStored || strings.Contains(cacheSnapshot, ticket.Ticket) {
		t.Fatalf("ticket cache retained raw credential or omitted digest: %s", cacheSnapshot)
	}
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"
	dial := func(origin string) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{origin}},
			Subprotocols: []string{
				browserWebSocketProtocol,
				browserTicketPrefix + ticket.Ticket,
			},
		})
	}
	connection, response, err := dial(server.URL)
	if err != nil {
		t.Fatalf("ticket WebSocket dial response=%v err=%v", response, err)
	}
	if connection.Subprotocol() != browserWebSocketProtocol {
		t.Fatalf("selected subprotocol=%q", connection.Subprotocol())
	}
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`)
	if err = connection.Write(context.Background(), websocket.MessageText, request); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(context.Background())
	if err != nil || !strings.Contains(string(payload), `"ok":true`) {
		t.Fatalf("ping payload=%s err=%v", payload, err)
	}
	_ = connection.CloseNow()

	replayed, replayResponse, replayErr := dial(server.URL)
	if replayed != nil {
		_ = replayed.CloseNow()
	}
	if replayErr == nil || replayResponse == nil || replayResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replayed ticket err=%v response=%v", replayErr, replayResponse)
	}
}

func TestBrowserSessionTicketHasOneConcurrentWinner(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	ticket := issueBrowserTicket(t, server.URL, server.URL, "websocket")
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"
	type result struct {
		accepted bool
		status   int
		err      error
	}
	const contenders = 16
	start := make(chan struct{})
	results := make(chan result, contenders)
	for range contenders {
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			connection, response, err := websocket.Dial(ctx, websocketURL, &websocket.DialOptions{
				HTTPHeader: http.Header{"Origin": []string{server.URL}},
				Subprotocols: []string{
					browserWebSocketProtocol,
					browserTicketPrefix + ticket.Ticket,
				},
			})
			if connection != nil {
				_ = connection.CloseNow()
			}
			status := 0
			if response != nil {
				status = response.StatusCode
			}
			results <- result{accepted: err == nil, status: status, err: err}
		}()
	}
	close(start)
	accepted, rejected := 0, 0
	for range contenders {
		result := <-results
		if result.accepted {
			accepted++
			continue
		}
		if result.status != http.StatusUnauthorized {
			t.Errorf("losing ticket request status=%d err=%v", result.status, result.err)
			continue
		}
		rejected++
	}
	if accepted != 1 || rejected != contenders-1 {
		t.Fatalf("accepted=%d rejected=%d", accepted, rejected)
	}
}

func TestBrowserSessionTicketAuthenticatesSocketIO(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	ticket := issueBrowserTicket(t, server.URL, server.URL, "socket_io")
	url := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/socket.io/?EIO=4&transport=websocket"
	connection, response, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{server.URL}},
		Subprotocols: []string{
			browserWebSocketProtocol,
			browserTicketPrefix + ticket.Ticket,
		},
	})
	if err != nil {
		t.Fatalf("Socket.IO ticket dial response=%v err=%v", response, err)
	}
	defer connection.CloseNow()
	if connection.Subprotocol() != browserWebSocketProtocol {
		t.Fatalf("selected Socket.IO subprotocol=%q", connection.Subprotocol())
	}

	_, packet, err := connection.Read(context.Background())
	if err != nil || !strings.HasPrefix(string(packet), "0{") {
		t.Fatalf("Engine.IO open packet=%q err=%v", packet, err)
	}
	if err = connection.Write(context.Background(), websocket.MessageText, []byte("40")); err != nil {
		t.Fatal(err)
	}
	_, packet, err = connection.Read(context.Background())
	if err != nil || !strings.HasPrefix(string(packet), "40{") {
		t.Fatalf("Socket.IO connect packet=%q err=%v", packet, err)
	}
	if err = connection.Write(
		context.Background(),
		websocket.MessageText,
		[]byte(`42["subscribe",{"topics":["events"]}]`),
	); err != nil {
		t.Fatal(err)
	}
	_, packet, err = connection.Read(context.Background())
	if err != nil || !strings.Contains(string(packet), `"subscribed"`) ||
		!strings.Contains(string(packet), `"principal":"test-operator"`) {
		t.Fatalf("Socket.IO subscription packet=%q err=%v", packet, err)
	}
}

func TestBrowserSessionTicketIsOriginTransportAndExpiryBound(t *testing.T) {
	service, client := testAuthenticatedService(t)
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
			ticket := issueBrowserTicket(t, server.URL, server.URL, test.transport)
			now = now.Add(test.advance)
			url := "ws" + strings.TrimPrefix(server.URL, "http") + test.path
			connection, response, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
				HTTPHeader:   http.Header{"Origin": []string{test.origin}},
				Subprotocols: []string{browserWebSocketProtocol, browserTicketPrefix + ticket.Ticket},
			})
			if connection != nil {
				_ = connection.CloseNow()
			}
			if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("bound ticket err=%v response=%v", err, response)
			}
			now = now.Add(-test.advance)
		})
	}

	ticket := issueBrowserTicket(t, server.URL, server.URL, "websocket")
	request := httptest.NewRequest(http.MethodGet, server.URL+"/ipc", nil)
	request.RemoteAddr = "127.0.0.2:45000"
	request.Header.Set("Origin", server.URL)
	request.Header.Set("Sec-WebSocket-Protocol", strings.Join([]string{
		browserWebSocketProtocol,
		browserTicketPrefix + ticket.Ticket,
	}, ", "))
	if _, accepted := service.consumeSessionTicket(
		request,
		accessFromAddress(stringAddress(request.RemoteAddr), "websocket"),
		"websocket",
	); accepted {
		t.Fatal("ticket issued to 127.0.0.1 was accepted from a different peer")
	}
}

func TestDurableURLCredentialsAndPreAuthSocketFramesAreRejected(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	for _, path := range []string{
		"/ipc?access_token=" + testAccessToken,
		"/ipc?ticket=" + strings.Repeat("a", 64),
		"/socket.io/?EIO=4&transport=websocket&access_token=" + testAccessToken,
		"/socket.io/?EIO=4&transport=websocket&ticket=" + strings.Repeat("a", 64),
	} {
		url := "ws" + strings.TrimPrefix(server.URL, "http") + path
		connection, response, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
			HTTPHeader: http.Header{"Origin": []string{server.URL}},
		})
		if connection != nil {
			_ = connection.CloseNow()
		}
		if err == nil || response == nil || response.StatusCode != http.StatusBadRequest {
			t.Fatalf("URL credential path=%s err=%v response=%v", path, err, response)
		}
	}

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket"
	connection, response, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{server.URL}},
	})
	if connection != nil {
		_ = connection.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated Socket.IO emitted a session err=%v response=%v", err, response)
	}
}

func TestAuthorizationDisabledAllowsHTTPWithoutCredentialsAndSkipsRemotePolicy(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	service.AuthorizationDisabled = true
	handler := websocketMux(context.Background(), service)

	request := httptest.NewRequest(http.MethodGet, "http://controller.example/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45000"
	request.Header.Set("Authorization", "not-a-bearer-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-PCController-Authentication") != "disabled" ||
		response.Header().Get("X-PCController-Principal") != "test-operator" {
		t.Fatalf("disabled auth status=%d headers=%v", response.Code, response.Header())
	}

	uiRequest := httptest.NewRequest(http.MethodGet, "http://controller.example/api/ui-config", nil)
	ui := httptest.NewRecorder()
	handler.ServeHTTP(ui, uiRequest)
	var config struct {
		AuthRequired bool `json:"auth_required"`
	}
	if err := json.NewDecoder(ui.Body).Decode(&config); err != nil || config.AuthRequired {
		t.Fatalf("disabled auth config=%+v err=%v", config, err)
	}
}

func TestMissingOriginPolicyAndCredentialAmbiguityAreFailClosed(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	config := service.HostConfig()
	config.IPC.AllowRemote = true
	config.IPC.AllowedOrigins = []string{"console.example:*"}
	service.HostConfig = func() appconfig.Config { return config }
	handler := websocketMux(context.Background(), service)

	request := httptest.NewRequest(http.MethodGet, "http://controller.example/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "without Origin") {
		t.Fatalf("missing-Origin status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://controller.example/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45001"
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	request.Header.Set("X-PCController-Token", "different-token")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "conflicting") {
		t.Fatalf("ambiguous credentials status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://controller.example/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45002"
	request.Header.Set("Authorization", "Bearer "+testAccessToken)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("X-PCController-Principal") != "test-operator" {
		t.Fatalf("native remote status=%d principal=%q body=%s", response.Code, response.Header().Get("X-PCController-Principal"), response.Body.String())
	}
}

func TestCapabilityAuditIncludesStablePrincipalAndAuthentication(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	config := service.HostConfig()
	config.IPC.AllowRemote = true
	config.IPC.RemotePolicy.Messages = true
	service.HostConfig = func() appconfig.Config { return config }

	tests := []struct {
		access Access
		kind   string
	}{
		{Access{Transport: "rest", Principal: "test-operator", Authentication: "bearer", authenticated: true}, "security.local.authorized"},
		{Access{Transport: "websocket", Principal: "test-operator", Authentication: "session-ticket", authenticated: true}, "security.local.authorized"},
		{Access{Transport: "socket_io", Principal: "test-operator", Authentication: "session-ticket", authenticated: true}, "security.local.authorized"},
		{Access{Transport: "ipc", Principal: "local-operator", Authentication: "local-transport", authenticated: true}, "security.local.authorized"},
		{Access{Remote: true, Transport: "bridge", Principal: "bridge-peer", Authentication: "bridge-session", authenticated: true}, "security.remote.authorized"},
	}
	var afterID uint64
	for _, test := range tests {
		if err := service.authorizeCapability(test.access, "POST /api/messages", capabilityMessages); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		event, err := client.NextEvent(ctx, afterID, test.kind)
		cancel()
		if err != nil || !strings.Contains(event.Text, "principal="+test.access.Principal) ||
			!strings.Contains(event.Text, "transport="+test.access.Transport) ||
			!strings.Contains(event.Text, "authentication="+test.access.Authentication) ||
			!strings.Contains(event.Text, "capability=messages") {
			t.Fatalf("access=%#v audit event=%#v err=%v", test.access, event, err)
		}
		afterID = event.ID
	}
}
