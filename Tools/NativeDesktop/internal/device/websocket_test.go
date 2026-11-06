package device

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"pccontroller.local/desktop/compatibility"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/model"
)

type countingResolver struct {
	mu    sync.Mutex
	calls int
	ips   []net.IPAddr
}

func (r *countingResolver) LookupIPAddr(_ context.Context, _ string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	return append([]net.IPAddr(nil), r.ips...), nil
}

func (r *countingResolver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func TestSharedLocalResolutionAndWebSocketReconnect(t *testing.T) {
	t.Parallel()

	var connections atomic.Int32
	var gets atomic.Int32
	var pings atomic.Int32
	var hostsMu sync.Mutex
	var hosts []string

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostsMu.Lock()
		hosts = append(hosts, r.Host)
		hostsMu.Unlock()

		if r.URL.Path == compatibility.StateEndpoint("get") {
			fmt.Fprint(w, "OFF")
			return
		}
		if r.URL.Path != "/ws" {
			http.NotFound(w, r)
			return
		}

		connectionNumber := connections.Add(1)
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		connection.SetPingHandler(func(payload string) error {
			pings.Add(1)
			return connection.WriteControl(
				websocket.PongMessage,
				[]byte(payload),
				time.Now().Add(100*time.Millisecond),
			)
		})

		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return
		}
		if messageType == websocket.TextMessage && string(payload) == "get" {
			gets.Add(1)
		}

		if connectionNumber == 1 {
			_ = connection.WriteMessage(websocket.TextMessage, []byte("ON"))
			_ = connection.WriteMessage(websocket.TextMessage, []byte("unrelated:event"))
			_ = connection.WriteMessage(websocket.TextMessage, []byte("msg:4,Hello+again"))
			_ = connection.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "test reconnect"),
				time.Now().Add(100*time.Millisecond),
			)
			return
		}

		_ = connection.WriteMessage(websocket.TextMessage, []byte("OFF"))
		for {
			messageType, payload, err = connection.ReadMessage()
			if err != nil {
				return
			}
			if messageType == websocket.TextMessage && string(payload) == "get" {
				gets.Add(1)
			}
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &countingResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}
	baseURL := "http://pccontroller-desktop.local:" + port
	client, err := New(
		baseURL,
		WithResolver(resolver),
		WithReconnectPolicy(ReconnectPolicy{
			Initial:    10 * time.Millisecond,
			Maximum:    25 * time.Millisecond,
			Multiplier: 2,
			Jitter:     0,
			ResetAfter: time.Second,
		}),
		WithKeepAlive(10*time.Millisecond, 80*time.Millisecond, 100*time.Millisecond),
	)
	if err != nil {
		t.Fatal(err)
	}

	if state, err := client.Get(context.Background()); err != nil || state != model.StateOff {
		t.Fatalf("HTTP Get = (%v, %v), want off", state, err)
	}
	if got := resolver.count(); got != 1 {
		t.Fatalf("resolver calls after HTTP = %d, want 1", got)
	}

	events := make(chan model.Event, 8)
	statuses := make(chan ConnectionStatus, 16)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	result := make(chan error, 1)
	go func() {
		result <- client.Listen(ctx, Handlers{
			OnEvent:      func(event model.Event) { events <- event },
			OnConnection: func(status ConnectionStatus) { statuses <- status },
		})
	}()

	var received []model.Event
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for len(received) < 3 || pings.Load() < 1 || connections.Load() < 2 {
		select {
		case event := <-events:
			received = append(received, event)
		case <-poll.C:
		case <-deadline.C:
			cancel()
			t.Fatalf(
				"timed out: events=%#v connections=%d gets=%d pings=%d resolver=%d",
				received,
				connections.Load(),
				gets.Load(),
				pings.Load(),
				resolver.count(),
			)
		}
	}
	cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Listen result = %v, want context.Canceled", err)
	}
	if got := connections.Load(); got != 2 {
		t.Fatalf("connections = %d, want exactly 2", got)
	}
	if got := gets.Load(); got != 2 {
		t.Fatalf("initial get commands = %d, want exactly one per connection", got)
	}
	if got := resolver.count(); got != 2 {
		t.Fatalf("resolver calls = %d, want initial resolution plus one after failed connection", got)
	}

	if received[0].Snapshot.State != model.StateOn ||
		received[1].Kind != model.EventMessage || received[1].Message != "Hello again" ||
		received[2].Snapshot.State != model.StateOff {
		t.Fatalf("events = %#v", received)
	}

	var online, waiting bool
drain:
	for {
		select {
		case status := <-statuses:
			online = online || status.Phase == ConnectionOnline
			waiting = waiting || status.Phase == ConnectionWaiting
		default:
			break drain
		}
	}
	if !online || !waiting {
		t.Fatalf("lifecycle statuses missing: online=%v waiting=%v", online, waiting)
	}

	expectedHost := net.JoinHostPort("pccontroller-desktop.local", port)
	hostsMu.Lock()
	recordedHosts := append([]string(nil), hosts...)
	hostsMu.Unlock()
	for _, host := range recordedHosts {
		if !strings.EqualFold(host, expectedHost) {
			t.Fatalf("request Host = %q, want original host %q (all=%v)", host, expectedHost, recordedHosts)
		}
	}
}

func TestLocalResolutionPreservesTLSHostAndSNI(t *testing.T) {
	t.Parallel()

	var observedMu sync.Mutex
	var observedHosts []string
	var observedSNI []string
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedMu.Lock()
		observedHosts = append(observedHosts, r.Host)
		observedMu.Unlock()

		switch r.URL.Path {
		case compatibility.StateEndpoint("get"):
			fmt.Fprint(w, "ON")
		case "/ws":
			connection, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer connection.Close()
			_, payload, err := connection.ReadMessage()
			if err != nil || string(payload) != "get" {
				return
			}
			_ = connection.WriteMessage(websocket.TextMessage, []byte("ON"))
			for {
				if _, _, err := connection.ReadMessage(); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	server.TLS = &tls.Config{
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			observedMu.Lock()
			observedSNI = append(observedSNI, hello.ServerName)
			observedMu.Unlock()
			return nil, nil
		},
	}
	server.StartTLS()
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(serverURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	resolver := &countingResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Test certificate.
	dialer := *websocket.DefaultDialer
	dialer.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Test certificate.
	client, err := New(
		"https://pccontroller-desktop.local:"+port,
		WithResolver(resolver),
		WithHTTPClient(&http.Client{Transport: transport, Timeout: time.Second}),
		WithWebSocketDialer(&dialer),
	)
	if err != nil {
		t.Fatal(err)
	}

	if state, err := client.Get(context.Background()); err != nil || state != model.StateOn {
		t.Fatalf("HTTPS Get = (%v, %v), want on", state, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	event := make(chan model.Event, 1)
	result := make(chan error, 1)
	go func() {
		result <- client.Listen(ctx, Handlers{
			OnEvent: func(value model.Event) { event <- value },
		})
	}()
	select {
	case received := <-event:
		if received.Snapshot.State != model.StateOn {
			t.Fatalf("WebSocket event = %#v, want on", received)
		}
		cancel()
	case <-ctx.Done():
		t.Fatal("timed out waiting for secure WebSocket event")
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Listen result = %v, want context.Canceled", err)
	}

	if got := resolver.count(); got != 1 {
		t.Fatalf("resolver calls = %d, want one shared HTTP/WS resolution", got)
	}
	expectedHost := net.JoinHostPort("pccontroller-desktop.local", port)
	observedMu.Lock()
	hostsCopy := append([]string(nil), observedHosts...)
	sniCopy := append([]string(nil), observedSNI...)
	observedMu.Unlock()
	if len(hostsCopy) != 2 || len(sniCopy) != 2 {
		t.Fatalf("hosts=%v SNI=%v, want two HTTP handshakes", hostsCopy, sniCopy)
	}
	for _, host := range hostsCopy {
		if !strings.EqualFold(host, expectedHost) {
			t.Fatalf("Host = %q, want %q", host, expectedHost)
		}
	}
	for _, name := range sniCopy {
		if !strings.EqualFold(name, "pccontroller-desktop.local") {
			t.Fatalf("TLS ServerName = %q, want pccontroller-desktop.local", name)
		}
	}
}

func TestReconnectDelayIsBoundedExponential(t *testing.T) {
	t.Parallel()

	client, err := New(
		"http://127.0.0.1:"+strconv.Itoa(1),
		WithReconnectPolicy(ReconnectPolicy{
			Initial:    10 * time.Millisecond,
			Maximum:    25 * time.Millisecond,
			Multiplier: 2,
			Jitter:     0,
			ResetAfter: time.Second,
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{
		10 * time.Millisecond,
		20 * time.Millisecond,
		25 * time.Millisecond,
		25 * time.Millisecond,
	}
	for index, expected := range want {
		if got := client.reconnectDelay(index + 1); got != expected {
			t.Fatalf("reconnectDelay(%d) = %s, want %s", index+1, got, expected)
		}
	}
}
