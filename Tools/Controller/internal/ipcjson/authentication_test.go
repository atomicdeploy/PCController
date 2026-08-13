package ipcjson

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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
const testServerProofToken = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY"

func testAuthenticatedService(t *testing.T) (*Service, *controllerapi.Client) {
	t.Helper()
	runtime := control.New(control.Options{})
	client := controllerapi.AttachSharedRuntime(runtime, shell.New(8))
	config := appconfig.Defaults()
	config.IPC.AuthToken = testAccessToken
	config.IPC.RemotePrincipal = "test-operator"
	return &Service{Client: client, HostConfig: func() appconfig.Config { return config }}, client
}

type syntheticRemoteConn struct{ net.Conn }

func (connection syntheticRemoteConn) RemoteAddr() net.Addr {
	return stringAddress("198.51.100.10:45000")
}

type syntheticRemoteListener struct{ net.Listener }

func (listener syntheticRemoteListener) Accept() (net.Conn, error) {
	connection, err := listener.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return syntheticRemoteConn{Conn: connection}, nil
}

func startSyntheticRemoteServer(t *testing.T, service *Service) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(websocketMux(context.Background(), service))
	server.Listener = syntheticRemoteListener{Listener: server.Listener}
	server.Start()
	t.Cleanup(server.Close)
	return server
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

func TestServerProofAuthenticatesExactListenerBeforeBearerUse(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	config := service.HostConfig()
	config.IPC.AuthToken = testServerProofToken
	service.HostConfig = func() appconfig.Config { return config }
	service.HostInstanceID = "edge-instance"
	server := httptest.NewServer(websocketMux(context.Background(), service))
	defer server.Close()

	nonce := strings.Repeat("A", 43)
	request, err := http.NewRequest(http.MethodGet, server.URL+ServerProofPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-PCController-Nonce", nonce)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var proof ServerProof
	if err := json.NewDecoder(response.Body).Decode(&proof); err != nil {
		t.Fatal(err)
	}
	listenerAddress := strings.TrimPrefix(server.URL, "http://")
	if response.StatusCode != http.StatusOK || proof.Nonce != nonce || proof.Audience != listenerAddress ||
		proof.InstanceID != "edge-instance" || !VerifyServerProof(testServerProofToken, proof) || VerifyServerProof("wrong-token", proof) {
		t.Fatalf("server proof status=%d value=%#v", response.StatusCode, proof)
	}
}

func TestServerProofRejectsLowEntropyCompatibilityToken(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	request := httptest.NewRequest(http.MethodGet, "http://controller.test"+ServerProofPath, nil)
	request.Header.Set("X-PCController-Nonce", strings.Repeat("A", 43))
	request = request.WithContext(context.WithValue(request.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.0.2.5"), Port: 8787}))
	response := httptest.NewRecorder()
	serveServerProof(response, request, service)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "random base64url") {
		t.Fatalf("weak-token proof status=%d body=%s", response.Code, response.Body.String())
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

func TestAuthorizationDisabledAllowsCredentiallessRemoteRESTJSONRPCWebSocketAndSocketIO(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	config := service.HostConfig()
	config.IPC.AllowRemote = true
	config.IPC.RemotePolicy = appconfig.RemoteAccessPolicy{}
	service.HostConfig = func() appconfig.Config { return config }
	service.AuthorizationDisabled = true
	server := startSyntheticRemoteServer(t, service)

	response, err := http.Get(server.URL + "/api/snapshot")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK ||
		response.Header.Get("X-PCController-Authentication") != "disabled" ||
		response.Header.Get("X-PCController-Principal") != "test-operator" {
		t.Fatalf("credentialless REST status=%d headers=%v", response.StatusCode, response.Header)
	}
	uiResponse, err := http.Get(server.URL + "/api/ui-config")
	if err != nil {
		t.Fatal(err)
	}
	defer uiResponse.Body.Close()
	var uiConfig struct {
		AuthRequired bool `json:"auth_required"`
	}
	if err := json.NewDecoder(uiResponse.Body).Decode(&uiConfig); err != nil || uiConfig.AuthRequired {
		t.Fatalf("alpha UI auth contract=%+v err=%v", uiConfig, err)
	}
	for _, authRoute := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, ServerProofPath},
		{http.MethodPost, SessionTicketPath},
	} {
		request, err := http.NewRequest(authRoute.method, server.URL+authRoute.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusConflict {
			t.Fatalf("dormant auth route %s status=%d", authRoute.path, response.StatusCode)
		}
	}
	urlSecretResponse, err := http.Get(server.URL + "/api/snapshot?access_token=must-not-travel")
	if err != nil {
		t.Fatal(err)
	}
	defer urlSecretResponse.Body.Close()
	if urlSecretResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("alpha URL secret status=%d", urlSecretResponse.StatusCode)
	}

	rpcRequest, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/ipc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.peer.update.host","params":{"peer":"edge","artifact_sha256":"`+strings.Repeat("a", 64)+`","authorized":true,"idempotency_key":"intent:alpha-http"}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	rpcRequest.Header.Set("Content-Type", "application/json")
	rpcResponse, err := http.DefaultClient.Do(rpcRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcResponse.Body.Close()
	var rpcEnvelope Response
	if err := json.NewDecoder(rpcResponse.Body).Decode(&rpcEnvelope); err != nil {
		t.Fatal(err)
	}
	if rpcResponse.StatusCode != http.StatusBadRequest || rpcEnvelope.Error == nil ||
		rpcEnvelope.Error.Code == -32001 || rpcEnvelope.Error.Code == -32003 ||
		!strings.Contains(rpcEnvelope.Error.Message, "artifact service") {
		t.Fatalf("credentialless HTTP JSON-RPC status=%d response=%#v", rpcResponse.StatusCode, rpcEnvelope)
	}

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ipc"
	connection, dialResponse, err := websocket.Dial(context.Background(), websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{server.URL}},
	})
	if err != nil {
		t.Fatalf("credentialless WebSocket response=%v err=%v", dialResponse, err)
	}
	if err = connection.Write(context.Background(), websocket.MessageText, []byte(
		`{"jsonrpc":"2.0","id":2,"method":"controller.peer.update.host","params":{"peer":"edge","artifact_sha256":"`+strings.Repeat("b", 64)+`","authorized":true,"idempotency_key":"intent:alpha-websocket"}}`,
	)); err != nil {
		t.Fatal(err)
	}
	_, payload, err := connection.Read(context.Background())
	_ = connection.CloseNow()
	if err != nil || !strings.Contains(string(payload), "artifact service") ||
		strings.Contains(string(payload), "authentication required") ||
		strings.Contains(string(payload), "remote capability") {
		t.Fatalf("credentialless WebSocket payload=%s err=%v", payload, err)
	}

	socketURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/socket.io/?EIO=4&transport=websocket"
	socket, dialResponse, err := websocket.Dial(context.Background(), socketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{server.URL}},
	})
	if err != nil {
		t.Fatalf("credentialless Socket.IO response=%v err=%v", dialResponse, err)
	}
	defer socket.CloseNow()
	if _, _, err = socket.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = socket.Write(context.Background(), websocket.MessageText, []byte("40")); err != nil {
		t.Fatal(err)
	}
	if _, _, err = socket.Read(context.Background()); err != nil {
		t.Fatal(err)
	}
	packet := `42["rpc",{"jsonrpc":"2.0","id":3,"method":"controller.peer.update.host","params":{"peer":"edge","artifact_sha256":"` + strings.Repeat("c", 64) + `","authorized":true,"idempotency_key":"intent:alpha-socketio"}}]`
	if err = socket.Write(context.Background(), websocket.MessageText, []byte(packet)); err != nil {
		t.Fatal(err)
	}
	_, payload, err = socket.Read(context.Background())
	if err != nil || !strings.Contains(string(payload), `"rpc.response"`) ||
		!strings.Contains(string(payload), "artifact service") ||
		strings.Contains(string(payload), "authentication required") ||
		strings.Contains(string(payload), "remote capability") {
		t.Fatalf("credentialless Socket.IO payload=%s err=%v", payload, err)
	}
}

func TestAuthorizationDisabledPreservesRemoteListenerOriginAndNoChainBoundaries(t *testing.T) {
	service, client := testAuthenticatedService(t)
	defer client.Shutdown()
	config := service.HostConfig()
	config.IPC.AllowRemote = false
	config.IPC.AllowedOrigins = []string{"controller.example:*"}
	service.HostConfig = func() appconfig.Config { return config }
	service.AuthorizationDisabled = true
	handler := websocketMux(context.Background(), service)

	request := httptest.NewRequest(
		http.MethodPost,
		"http://controller.example/api/rpc",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"controller.ping"}`),
	)
	request.RemoteAddr = "198.51.100.10:45000"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "remote network access is disabled") {
		t.Fatalf("allow_remote bypassed: status=%d body=%s", response.Code, response.Body.String())
	}

	config.IPC.AllowRemote = true
	request = httptest.NewRequest(http.MethodGet, "http://controller.example/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45001"
	request.Header.Set("Origin", "http://evil.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin is not allowed") {
		t.Fatalf("Origin bypassed: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://rebind.attacker.invalid:8787/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45002"
	request.Host = "rebind.attacker.invalid:8787"
	request.Header.Set("Origin", "http://rebind.attacker.invalid:8787")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin is not allowed") {
		t.Fatalf("DNS-rebinding Origin bypassed: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://rebind.attacker.invalid:8787/api/snapshot", nil)
	request.RemoteAddr = "127.0.0.1:45002"
	request.Host = "rebind.attacker.invalid:8787"
	request.Header.Set("Origin", "http://rebind.attacker.invalid:8787")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "origin is not allowed") {
		t.Fatalf("loopback DNS-rebinding Origin bypassed: status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "http://controller.example:8787/api/snapshot", nil)
	request.RemoteAddr = "198.51.100.10:45003"
	request.Header.Set("Origin", "http://controller.example:8787")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("explicit exact-host Origin rejected: status=%d body=%s", response.Code, response.Body.String())
	}

	nested, _ := json.Marshal(map[string]any{
		"peer":    "third",
		"request": Request{JSONRPC: Version, Method: "controller.snapshot"},
	})
	bridgeResponse := service.DispatchRemote(context.Background(), Request{
		JSONRPC: Version, Method: "controller.bridge.call", Params: nested,
	}, "bridge")
	if bridgeResponse.Error == nil || !strings.Contains(bridgeResponse.Error.Message, "recursive bridge calls") {
		t.Fatalf("bridge no-chain bypassed: %#v", bridgeResponse)
	}
	for _, wrapped := range []Request{
		{JSONRPC: Version, Method: "controller.command.execute", Params: json.RawMessage(`{"command":"bridge call third controller.snapshot"}`)},
		{JSONRPC: Version, Method: "controller.app.action", Params: json.RawMessage(`{"kind":"command","value":"bridge call third controller.snapshot"}`)},
	} {
		bridgeResponse = service.DispatchRemote(context.Background(), wrapped, "bridge")
		if bridgeResponse.Error == nil || !strings.Contains(bridgeResponse.Error.Message, "recursive bridge calls") {
			t.Fatalf("wrapped bridge no-chain bypassed for %s: %#v", wrapped.Method, bridgeResponse)
		}
	}
	peerUpdate, _ := json.Marshal(peerHostUpdateRequest{
		Peer: "third", ArtifactSHA256: strings.Repeat("d", 64), Authorized: true,
		IdempotencyKey: "intent:alpha-no-chain",
	})
	bridgeResponse = service.DispatchRemote(context.Background(), Request{
		JSONRPC: Version, Method: "controller.peer.update.host", Params: peerUpdate,
	}, "bridge")
	if bridgeResponse.Error == nil || !strings.Contains(bridgeResponse.Error.Message, "may not be chained") {
		t.Fatalf("peer-update no-chain bypassed: %#v", bridgeResponse)
	}
	peerCommand := "peer-update host third " + strings.Repeat("d", 64) + " intent:alpha-no-chain"
	for _, wrapped := range []Request{
		{JSONRPC: Version, Method: "controller.command.execute", Params: json.RawMessage(`{"command":` + strconv.Quote(peerCommand) + `}`)},
		{JSONRPC: Version, Method: "controller.app.action", Params: json.RawMessage(`{"kind":"command","value":` + strconv.Quote(peerCommand) + `}`)},
	} {
		bridgeResponse = service.DispatchRemote(context.Background(), wrapped, "bridge")
		if bridgeResponse.Error == nil || !strings.Contains(bridgeResponse.Error.Message, "may not be chained") {
			t.Fatalf("wrapped peer-update no-chain bypassed for %s: %#v", wrapped.Method, bridgeResponse)
		}
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
