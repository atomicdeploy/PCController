package localapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/appcore"
	"pccontroller.local/desktop/internal/commands"
	"pccontroller.local/desktop/internal/config"
	"pccontroller.local/desktop/internal/deviceterminal"
	"pccontroller.local/desktop/internal/ipc"
)

func TestTerminalWebSocketRequiresSessionAndBridgesBytes(t *testing.T) {
	deviceListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer deviceListener.Close()
	devicePort := deviceListener.Addr().(*net.TCPAddr).Port
	deviceInput := make(chan []byte, 1)
	go func() {
		connection, acceptErr := deviceListener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = connection.Write([]byte{0xff, 0xfb, 0x01})
		_, _ = connection.Write([]byte("\x1b[32mController terminal ready\x1b[0m\r\n> "))
		buffer := make([]byte, len("status\r\n"))
		if _, readErr := io.ReadFull(connection, buffer); readErr == nil {
			deviceInput <- buffer
		}
		<-time.After(2 * time.Second)
	}()

	cfg := config.Default()
	cfg.Device.BaseURL = "http://127.0.0.1"
	cfg.Device.TelnetPort = devicePort
	cfg.Device.WebSocket = false
	cfg.Device.HTTPPoll = 0
	cfg.Device.RequestTimeout = config.Duration(2 * time.Second)
	core, err := appcore.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Stop()
	dispatcher, err := commands.New(core, filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	running := startBrowserAPI(t, core, dispatcher.Handler)
	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/ui/terminal"

	unauthorizedHeaders := http.Header{}
	unauthorizedHeaders.Set("Origin", running.origin)
	if connection, response, dialErr := websocket.DefaultDialer.Dial(
		socketURL,
		unauthorizedHeaders,
	); dialErr == nil {
		connection.Close()
		t.Fatal("terminal WebSocket connected without a browser session")
	} else if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized handshake response = %#v, err = %v", response, dialErr)
	}

	cookie, _ := mintBrowserSession(t, running)
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	connection, _, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatalf("dial terminal WebSocket: %v", err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(5 * time.Second))

	var initial terminalSocketFrame
	if err := connection.ReadJSON(&initial); err != nil {
		t.Fatalf("read initial terminal status: %v", err)
	}
	if initial.Type != "status" || initial.Status == nil ||
		initial.Status.State != deviceterminal.StateDisconnected {
		t.Fatalf("initial terminal frame = %#v", initial)
	}
	if err := connection.WriteJSON(terminalSocketCommand{Type: "connect"}); err != nil {
		t.Fatalf("send connect: %v", err)
	}

	connected := false
	var output []byte
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline) &&
		(!connected || !strings.Contains(string(output), "Controller terminal ready")); {
		messageType, payload, readErr := connection.ReadMessage()
		if readErr != nil {
			t.Fatalf("read terminal frame: %v", readErr)
		}
		switch messageType {
		case websocket.BinaryMessage:
			output = append(output, payload...)
		case websocket.TextMessage:
			var frame terminalSocketFrame
			if err := json.Unmarshal(payload, &frame); err != nil {
				t.Fatalf("decode terminal frame: %v", err)
			}
			if frame.Type == "error" {
				t.Fatalf("terminal error frame: %#v", frame)
			}
			if frame.Status != nil &&
				frame.Status.State == deviceterminal.StateConnected {
				connected = true
			}
		}
	}
	if !connected {
		t.Fatal("terminal WebSocket never reported connected")
	}
	if !strings.Contains(string(output), "Controller terminal ready") {
		t.Fatalf("terminal output = %q", output)
	}
	if strings.ContainsRune(string(output), rune(0xff)) {
		t.Fatalf("Telnet IAC leaked into browser output: %q", output)
	}

	if err := connection.WriteMessage(websocket.BinaryMessage, []byte("status\r\n")); err != nil {
		t.Fatalf("write terminal input: %v", err)
	}
	select {
	case got := <-deviceInput:
		if string(got) != "status\r\n" {
			t.Fatalf("device input = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("terminal input did not reach the device")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = running.server.Close(ctx)
}

func TestTerminalWebSocketExpiresWithBrowserSession(t *testing.T) {
	handler := func(ctx context.Context, request ipc.Request) (any, error) {
		switch request.Method {
		case "terminal.read":
			timer := time.NewTimer(25 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return deviceterminal.ReadResult{
					Snapshot: deviceterminal.Snapshot{
						State: deviceterminal.StateDisconnected,
					},
				}, nil
			}
		default:
			return deviceterminal.Snapshot{
				State: deviceterminal.StateDisconnected,
			}, nil
		}
	}
	running := startBrowserAPI(t, newTestEventSource(), handler)
	cookie, _ := mintBrowserSession(t, running)

	sessionKey := sha256.Sum256([]byte(cookie.Value))
	expires := time.Now().Add(350 * time.Millisecond)
	running.server.api.sessions.mu.Lock()
	session, exists := running.server.api.sessions.sessions[sessionKey]
	if !exists {
		running.server.api.sessions.mu.Unlock()
		t.Fatal("minted browser session is missing from the store")
	}
	session.expires = expires
	running.server.api.sessions.sessions[sessionKey] = session
	running.server.api.sessions.mu.Unlock()

	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/ui/terminal"
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	connection, _, err := websocket.DefaultDialer.Dial(socketURL, headers)
	if err != nil {
		t.Fatalf("dial terminal WebSocket: %v", err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(3 * time.Second))

	for {
		_, _, err = connection.ReadMessage()
		if err == nil {
			continue
		}
		var closeError *websocket.CloseError
		if !errors.As(err, &closeError) {
			t.Fatalf("terminal session expiry error = %T %v", err, err)
		}
		if closeError.Code != websocket.ClosePolicyViolation {
			t.Fatalf("terminal session close code = %d, want %d", closeError.Code, websocket.ClosePolicyViolation)
		}
		if !strings.Contains(closeError.Text, errTerminalSessionExpired.Error()) {
			t.Fatalf("terminal session close reason = %q", closeError.Text)
		}
		break
	}
}

func TestTerminalWebSocketCloseWaitsForSocketWorkers(t *testing.T) {
	readStarted := make(chan struct{})
	cancelSeen := make(chan struct{})
	releaseWorker := make(chan struct{})
	var releaseWorkerOnce sync.Once
	releaseTerminalWorker := func() {
		releaseWorkerOnce.Do(func() { close(releaseWorker) })
	}
	defer releaseTerminalWorker()
	var readStartedOnce sync.Once
	var cancelSeenOnce sync.Once
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

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		closeDone <- running.server.Close(ctx)
	}()
	select {
	case <-cancelSeen:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal poll worker did not receive cancellation")
	}
	select {
	case err := <-closeDone:
		t.Fatalf("server Close returned before terminal worker exited: %v", err)
	case <-time.After(75 * time.Millisecond):
	}

	releaseTerminalWorker()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("server Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server Close did not join terminal worker")
	}

	_ = connection.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("terminal WebSocket survived server Close")
	}
}

func TestTerminalWebSocketCloseGatesAdmittedHandshake(t *testing.T) {
	handler := &recordedHandler{result: deviceterminal.ReadResult{
		Snapshot: deviceterminal.Snapshot{
			State: deviceterminal.StateDisconnected,
		},
	}}
	running := startBrowserAPI(t, newTestEventSource(), handler.Handle)
	cookie, _ := mintBrowserSession(t, running)

	handshakeAdmitted := make(chan struct{})
	releaseUpgrade := make(chan struct{})
	var releaseUpgradeOnce sync.Once
	resumeUpgrade := func() {
		releaseUpgradeOnce.Do(func() { close(releaseUpgrade) })
	}
	defer resumeUpgrade()
	var admittedOnce sync.Once
	running.server.api.terminalBeforeUpgrade = func() {
		admittedOnce.Do(func() { close(handshakeAdmitted) })
		<-releaseUpgrade
	}

	socketURL := "ws" + strings.TrimPrefix(running.origin, "http") + "/ui/terminal"
	headers := http.Header{}
	headers.Set("Origin", running.origin)
	headers.Set("Cookie", cookie.Name+"="+cookie.Value)
	type dialResult struct {
		connection *websocket.Conn
		response   *http.Response
		err        error
	}
	dialDone := make(chan dialResult, 1)
	go func() {
		dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
		connection, response, err := dialer.Dial(socketURL, headers)
		dialDone <- dialResult{
			connection: connection,
			response:   response,
			err:        err,
		}
	}()
	select {
	case <-handshakeAdmitted:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal handshake was not admitted")
	}

	closeDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		closeDone <- running.server.Close(ctx)
	}()
	waitForTerminalRegistryClosing(t, running.server.api.terminalSockets)
	select {
	case err := <-closeDone:
		t.Fatalf("server Close returned while admitted handshake was paused: %v", err)
	case <-time.After(75 * time.Millisecond):
	}

	resumeUpgrade()
	var result dialResult
	select {
	case result = <-dialDone:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal WebSocket dial did not finish")
	}
	if result.response != nil && result.response.Body != nil {
		defer result.response.Body.Close()
	}
	if result.connection != nil {
		defer result.connection.Close()
		_ = result.connection.SetReadDeadline(time.Now().Add(time.Second))
		if _, _, readErr := result.connection.ReadMessage(); readErr == nil {
			t.Fatal("admitted terminal WebSocket survived API close")
		}
	}

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("server Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server Close did not join admitted handshake")
	}
}

func waitForTerminalRegistryClosing(t *testing.T, registry *terminalSocketRegistry) {
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
	t.Fatal("terminal socket registry did not enter closing state")
}
