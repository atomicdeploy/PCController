package localapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/ipc"
)

func TestEventWebSocketShutdownNotifiesBeforeAdmittedHandshakeJoins(t *testing.T) {
	running := startBrowserAPI(
		t,
		newTestEventSource(),
		(&recordedHandler{result: map[string]bool{"ok": true}}).Handle,
	)
	cookie, _ := mintBrowserSession(t, running)
	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/events"
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)

	active, _, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatalf("dial active event WebSocket: %v", err)
	}
	defer active.Close()
	_ = active.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := active.ReadMessage(); err != nil {
		t.Fatalf("read event hello: %v", err)
	}

	handshakeAdmitted := make(chan struct{})
	releaseUpgrade := make(chan struct{})
	var releaseOnce sync.Once
	resumeUpgrade := func() {
		releaseOnce.Do(func() { close(releaseUpgrade) })
	}
	defer resumeUpgrade()
	var admittedOnce sync.Once
	running.server.api.eventBeforeUpgrade = func() {
		admittedOnce.Do(func() { close(handshakeAdmitted) })
		<-releaseUpgrade
	}

	type dialResult struct {
		connection *websocket.Conn
		response   *http.Response
		err        error
	}
	dialDone := make(chan dialResult, 1)
	go func() {
		dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
		connection, response, dialErr := dialer.Dial(socketURL, headers)
		dialDone <- dialResult{
			connection: connection,
			response:   response,
			err:        dialErr,
		}
	}()
	select {
	case <-handshakeAdmitted:
	case <-time.After(2 * time.Second):
		t.Fatal("event handshake was not admitted")
	}

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		closeDone <- running.server.Close(ctx)
	}()
	waitForWebSocketRegistryClosing(t, running.server.api.eventSockets)

	assertGoingAway(t, active)
	select {
	case err := <-closeDone:
		t.Fatalf("server Close returned before admitted event handshake joined: %v", err)
	case <-time.After(75 * time.Millisecond):
	}

	resumeUpgrade()
	select {
	case result := <-dialDone:
		if result.response != nil && result.response.Body != nil {
			defer result.response.Body.Close()
		}
		if result.connection != nil {
			defer result.connection.Close()
			assertGoingAway(t, result.connection)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admitted event WebSocket dial did not finish")
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("server Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server Close did not join admitted event handshake")
	}
}

func TestServerCloseContextBoundsTerminalWebSocketJoin(t *testing.T) {
	readStarted := make(chan struct{})
	cancelSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var readStartedOnce sync.Once
	var cancelSeenOnce sync.Once
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseWorker) })
	}
	defer release()

	handler := func(ctx context.Context, request ipc.Request) (any, error) {
		if request.Method != "terminal.read" {
			return deviceterminal.Snapshot{
				State: deviceterminal.StateDisconnected,
			}, nil
		}
		readStartedOnce.Do(func() { close(readStarted) })
		<-ctx.Done()
		cancelSeenOnce.Do(func() { close(cancelSeen) })
		<-releaseWorker
		return nil, ctx.Err()
	}
	running := startBrowserAPI(t, newTestEventSource(), handler)
	cookie, _ := mintBrowserSession(t, running)
	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/ui/terminal"
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	connection, _, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatalf("dial terminal WebSocket: %v", err)
	}
	defer connection.Close()

	select {
	case <-readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal poll worker did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 125*time.Millisecond)
	defer cancel()
	err = running.server.Close(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("server Close error = %v, want context deadline", err)
	}
	assertGoingAway(t, connection)
	select {
	case <-cancelSeen:
	case <-time.After(time.Second):
		t.Fatal("terminal worker did not observe socket cancellation")
	}

	release()
	joinContext, joinCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer joinCancel()
	if err := running.server.api.terminalSockets.wait(joinContext); err != nil {
		t.Fatalf("join released terminal worker: %v", err)
	}
}

func TestWebSocketRegistryGatesAdmissionAndBoundsWait(t *testing.T) {
	registry := newWebSocketRegistry(1)
	release, ok := registry.begin()
	if !ok {
		t.Fatal("first WebSocket admission was rejected")
	}
	registry.shutdown()
	if _, ok := registry.begin(); ok {
		t.Fatal("WebSocket admitted after shutdown gate closed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := registry.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("registry wait error = %v, want context deadline", err)
	}

	release()
	joinContext, joinCancel := context.WithTimeout(context.Background(), time.Second)
	defer joinCancel()
	if err := registry.wait(joinContext); err != nil {
		t.Fatalf("registry did not join released worker: %v", err)
	}
}

func TestServerCloseIsConcurrentAndIdempotent(t *testing.T) {
	handler := &recordedHandler{result: map[string]string{"version": "test"}}
	server, err := New("127.0.0.1:0", testToken, handler.Handle)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}

	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for range callers {
		go func() {
			<-start
			results <- server.Close(ctx)
		}()
	}
	close(start)
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Close: %v", err)
		}
	}
	if err := server.Close(context.Background()); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func assertGoingAway(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := connection.ReadMessage(); err != nil {
			var closeError *websocket.CloseError
			if !errors.As(err, &closeError) {
				t.Fatalf("WebSocket close error = %T %v", err, err)
			}
			if closeError.Code != websocket.CloseGoingAway {
				t.Fatalf(
					"WebSocket close code = %d, want %d",
					closeError.Code,
					websocket.CloseGoingAway,
				)
			}
			if closeError.Text != webSocketShutdownReason {
				t.Fatalf(
					"WebSocket close reason = %q, want %q",
					closeError.Text,
					webSocketShutdownReason,
				)
			}
			return
		}
	}
}

func waitForWebSocketRegistryClosing(t *testing.T, registry *webSocketRegistry) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		registry.mu.Lock()
		closing := registry.closing
		registry.mu.Unlock()
		if closing {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("WebSocket registry did not enter closing state")
}
