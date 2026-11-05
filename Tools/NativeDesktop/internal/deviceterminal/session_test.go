package deviceterminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerDetectsReadOnlyBannerAcrossReads(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	client, server := net.Pipe()
	defer server.Close()
	defer manager.Close()

	if _, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return client, nil },
	); err != nil {
		t.Fatal(err)
	}

	fragments := [][]byte{
		{telnetIAC},
		{telnetWILL, 1},
		[]byte("\x1b[32mShell is on "),
		[]byte("Serial\x1b[0m\r\n"),
	}
	for _, fragment := range fragments {
		if _, err := server.Write(fragment); err != nil {
			t.Fatal(err)
		}
	}

	eventually(t, func() bool {
		snapshot := manager.Snapshot()
		return snapshot.ReadOnly && snapshot.Sequence >= 2
	})
	result, err := manager.Read(context.Background(), 0, MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	output := decodeChunks(t, result.Chunks)
	if bytes.Contains(output, []byte{telnetIAC}) {
		t.Fatalf("terminal transcript retained Telnet negotiation: %q", output)
	}
	if !bytes.Contains(output, []byte("Shell is on Serial")) {
		t.Fatalf("terminal transcript = %q, want split read-only banner", output)
	}
}

func TestManagerBoundedScrollbackResetsStaleCursor(t *testing.T) {
	t.Parallel()

	manager := New(6)
	client, server := net.Pipe()
	defer server.Close()
	defer manager.Close()
	snapshot, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return client, nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	manager.appendOutput(snapshot.Session, client, []byte("one"), false)
	manager.appendOutput(snapshot.Session, client, []byte("two"), false)
	manager.appendOutput(snapshot.Session, client, []byte("three"), false)

	result, err := manager.Read(context.Background(), 1, MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reset {
		t.Fatalf("Read from stale cursor did not request a reset: %+v", result)
	}
	wantSequence := snapshot.Sequence + 3
	if result.Snapshot.OldestSequence != wantSequence ||
		len(result.Chunks) != 1 ||
		result.Chunks[0].Sequence != wantSequence {
		t.Fatalf("bounded result = %+v, want only sequence %d", result, wantSequence)
	}
	if got := string(decodeChunks(t, result.Chunks)); got != "three" {
		t.Fatalf("bounded transcript = %q, want %q", got, "three")
	}
	if manager.scrollbackBytes > manager.scrollbackLimit {
		t.Fatalf(
			"scrollback bytes = %d, limit = %d",
			manager.scrollbackBytes,
			manager.scrollbackLimit,
		)
	}
}

func TestManagerLongPollWakesOnStateOnlyDisconnect(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	client, server := net.Pipe()
	defer server.Close()
	defer manager.Close()
	snapshot, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return client, nil },
	)
	if err != nil {
		t.Fatal(err)
	}

	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observed := &doneObservedContext{
		Context:  base,
		observed: make(chan struct{}),
	}
	result := make(chan ReadResult, 1)
	readError := make(chan error, 1)
	started := time.Now()
	go func() {
		value, readErr := manager.Read(observed, snapshot.Sequence, 1024, time.Second)
		result <- value
		readError <- readErr
	}()

	select {
	case <-observed.observed:
	case <-time.After(time.Second):
		t.Fatal("long poll did not begin waiting")
	}
	manager.Disconnect()

	select {
	case got := <-result:
		if err := <-readError; err != nil {
			t.Fatal(err)
		}
		if got.Snapshot.State != StateDisconnected {
			t.Fatalf("long-poll snapshot state = %q, want disconnected", got.Snapshot.State)
		}
		if elapsed := time.Since(started); elapsed >= 750*time.Millisecond {
			t.Fatalf("state-only wake took %s; it appears to have waited for its timer", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll did not wake on disconnect")
	}
}

func TestManagerNewReaderLongPollWaitsPastResetMarker(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	client, server := net.Pipe()
	defer server.Close()
	defer manager.Close()
	if _, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return client, nil },
	); err != nil {
		t.Fatal(err)
	}

	base, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	observed := &doneObservedContext{
		Context:  base,
		observed: make(chan struct{}),
	}
	result := make(chan ReadResult, 1)
	readError := make(chan error, 1)
	go func() {
		value, err := manager.Read(observed, 0, MaxReadBytes, time.Second)
		result <- value
		readError <- err
	}()
	select {
	case <-observed.observed:
		// Reaching the context select proves the reset marker did not cause an
		// immediate empty long-poll response for a brand-new reader.
	case <-time.After(time.Second):
		t.Fatal("new reader did not enter long polling")
	}
	if _, err := server.Write([]byte("ready\r\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case value := <-result:
		if err := <-readError; err != nil {
			t.Fatal(err)
		}
		if got := string(decodeChunks(t, value.Chunks)); got != "ready\r\n" {
			t.Fatalf("long-poll transcript = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("new reader did not wake for device output")
	}
}

func TestManagerConcurrentConnectWriteAndDisconnectWithNetPipe(t *testing.T) {
	t.Parallel()

	manager := New(4096)
	var peersMu sync.Mutex
	var peers []net.Conn
	dial := func(context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		peersMu.Lock()
		peers = append(peers, server)
		peersMu.Unlock()
		go func() {
			_, _ = io.Copy(io.Discard, server)
			_ = server.Close()
		}()
		return client, nil
	}
	if _, err := manager.Connect(context.Background(), "rayanlamp.local", 23, dial); err != nil {
		t.Fatal(err)
	}

	const workers = 32
	start := make(chan struct{})
	errorsSeen := make(chan error, workers)
	var workersWG sync.WaitGroup
	for index := 0; index < workers; index++ {
		index := index
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			<-start
			switch index % 4 {
			case 0:
				_, err := manager.Connect(
					context.Background(),
					"rayanlamp.local",
					23,
					dial,
				)
				errorsSeen <- err
			case 1:
				manager.Disconnect()
				errorsSeen <- nil
			default:
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				_, err := manager.Write(ctx, []byte("state\r\n"))
				errorsSeen <- err
			}
		}()
	}
	close(start)

	done := make(chan struct{})
	go func() {
		workersWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent terminal operations deadlocked")
	}
	close(errorsSeen)
	for err := range errorsSeen {
		if err == nil ||
			errors.Is(err, ErrBusy) ||
			errors.Is(err, ErrNotConnected) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, net.ErrClosed) ||
			errors.Is(err, io.ErrClosedPipe) {
			continue
		}
		t.Fatalf("unexpected concurrent operation error: %v", err)
	}

	manager.Disconnect()
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	peersMu.Lock()
	peersCopy := append([]net.Conn(nil), peers...)
	peersMu.Unlock()
	for _, peer := range peersCopy {
		_ = peer.Close()
	}
	if got := manager.Snapshot().State; got != StateDisconnected {
		t.Fatalf("final state = %q, want disconnected", got)
	}
}

func TestManagerDisconnectAndCloseCancelInFlightDial(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		stop      func(*Manager)
		wantError error
	}{
		{
			name: "disconnect",
			stop: func(manager *Manager) {
				manager.Disconnect()
			},
			wantError: context.Canceled,
		},
		{
			name: "close",
			stop: func(manager *Manager) {
				_ = manager.Close()
			},
			wantError: ErrClosed,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := New(1024)
			started := make(chan struct{})
			connectResult := make(chan error, 1)
			go func() {
				_, err := manager.Connect(
					context.Background(),
					"rayanlamp.local",
					23,
					func(ctx context.Context) (net.Conn, error) {
						close(started)
						<-ctx.Done()
						return nil, ctx.Err()
					},
				)
				connectResult <- err
			}()

			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("dial did not start")
			}
			if _, err := manager.Connect(
				context.Background(),
				"second-rayanlamp.local",
				23,
				func(context.Context) (net.Conn, error) {
					t.Fatal("busy Connect invoked its dialer")
					return nil, nil
				},
			); !errors.Is(err, ErrBusy) {
				t.Fatalf("concurrent Connect error = %v, want ErrBusy", err)
			}
			stopped := make(chan struct{})
			go func() {
				test.stop(manager)
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-time.After(500 * time.Millisecond):
				t.Fatal("lifecycle operation waited for a canceled dial")
			}
			select {
			case err := <-connectResult:
				if !errors.Is(err, test.wantError) {
					t.Fatalf("Connect error = %v, want %v", err, test.wantError)
				}
			case <-time.After(time.Second):
				t.Fatal("Connect did not return after lifecycle cancellation")
			}
			if test.name != "close" {
				if err := manager.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestManagerStaleDialCannotReplaceNewConnection(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	defer manager.Close()

	staleClient, staleServer := net.Pipe()
	defer staleServer.Close()
	staleStarted := make(chan struct{})
	releaseStale := make(chan struct{})
	staleResult := make(chan error, 1)
	go func() {
		_, err := manager.Connect(
			context.Background(),
			"old-rayanlamp.local",
			23,
			func(context.Context) (net.Conn, error) {
				close(staleStarted)
				<-releaseStale
				return staleClient, nil
			},
		)
		staleResult <- err
	}()
	select {
	case <-staleStarted:
	case <-time.After(time.Second):
		t.Fatal("stale dial did not start")
	}

	disconnected := make(chan struct{})
	go func() {
		manager.Disconnect()
		close(disconnected)
	}()
	select {
	case <-disconnected:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Disconnect waited for a dialer that ignored cancellation")
	}

	currentClient, currentServer := net.Pipe()
	defer currentServer.Close()
	connected, err := manager.Connect(
		context.Background(),
		"new-rayanlamp.local",
		2323,
		func(context.Context) (net.Conn, error) { return currentClient, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if connected.Host != "new-rayanlamp.local" || connected.Port != 2323 {
		t.Fatalf("new connection snapshot = %+v", connected)
	}

	close(releaseStale)
	select {
	case err := <-staleResult:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stale Connect error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("stale Connect did not finish")
	}
	staleClosed := make(chan error, 1)
	go func() {
		_, readErr := staleServer.Read(make([]byte, 1))
		staleClosed <- readErr
	}()
	select {
	case err := <-staleClosed:
		if err == nil {
			t.Fatal("stale dial connection was not closed")
		}
	case <-time.After(time.Second):
		t.Fatal("stale dial connection remained open")
	}

	received := make(chan string, 1)
	go func() {
		buffer := make([]byte, len("state\r\n"))
		_, readErr := io.ReadFull(currentServer, buffer)
		if readErr != nil {
			received <- ""
			return
		}
		received <- string(buffer)
	}()
	if _, err := manager.Write(context.Background(), []byte("state\r\n")); err != nil {
		t.Fatalf("write to current connection: %v", err)
	}
	select {
	case value := <-received:
		if value != "state\r\n" {
			t.Fatalf("current connection received %q", value)
		}
	case <-time.After(time.Second):
		t.Fatal("new connection did not receive terminal input")
	}
}

func TestManagerSessionResetAdvancesCursorAndDisconnectPurgesTranscript(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	defer manager.Close()
	firstClient, firstServer := net.Pipe()
	defer firstServer.Close()
	first, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return firstClient, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := firstServer.Write([]byte("old-session-secret\r\n")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return manager.Snapshot().Sequence > first.Sequence
	})
	initial, err := manager.Read(context.Background(), 0, MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !initial.Reset || len(initial.Chunks) < 2 || !initial.Chunks[0].Reset {
		t.Fatalf("initial session result lacks cursor reset marker: %+v", initial)
	}
	oldCursor := initial.Chunks[len(initial.Chunks)-1].Sequence
	if got := string(decodeChunks(t, initial.Chunks)); !strings.Contains(got, "old-session-secret") {
		t.Fatalf("initial transcript = %q", got)
	}

	secondClient, secondServer := net.Pipe()
	defer secondServer.Close()
	second, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		2323,
		func(context.Context) (net.Conn, error) { return secondClient, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence <= oldCursor {
		t.Fatalf("session reset sequence = %d, old cursor = %d", second.Sequence, oldCursor)
	}
	if _, err := secondServer.Write([]byte("new-session\r\n")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return manager.Snapshot().Sequence > second.Sequence
	})
	replacement, err := manager.Read(
		context.Background(),
		oldCursor,
		MaxReadBytes,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !replacement.Reset ||
		len(replacement.Chunks) < 2 ||
		!replacement.Chunks[0].Reset ||
		replacement.Chunks[0].Sequence != second.Sequence {
		t.Fatalf("active reader did not receive one advancing reset: %+v", replacement)
	}
	replacementText := string(decodeChunks(t, replacement.Chunks))
	if replacementText != "new-session\r\n" ||
		strings.Contains(replacementText, "old-session-secret") {
		t.Fatalf("replacement transcript = %q", replacementText)
	}
	newCursor := replacement.Chunks[len(replacement.Chunks)-1].Sequence
	acknowledged, err := manager.Read(
		context.Background(),
		newCursor,
		MaxReadBytes,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if acknowledged.Reset || len(acknowledged.Chunks) != 0 {
		t.Fatalf("acknowledged reset repeated: %+v", acknowledged)
	}

	disconnected := manager.Disconnect()
	purged, err := manager.Read(
		context.Background(),
		newCursor,
		MaxReadBytes,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !purged.Reset ||
		len(purged.Chunks) != 1 ||
		!purged.Chunks[0].Reset ||
		purged.Chunks[0].Sequence != disconnected.Sequence {
		t.Fatalf("disconnect purge result = %+v", purged)
	}
	if data := decodeChunks(t, purged.Chunks); len(data) != 0 {
		t.Fatalf("disconnect retained transcript bytes %q", data)
	}
	fromStart, err := manager.Read(context.Background(), 0, MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(decodeChunks(t, fromStart.Chunks), []byte("old-session-secret")) ||
		bytes.Contains(decodeChunks(t, fromStart.Chunks), []byte("new-session")) {
		t.Fatalf("disconnected transcript was not purged: %+v", fromStart)
	}
}

func TestManagerFutureCursorReturnsResetAndCurrentTranscript(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	defer manager.Close()
	empty, err := manager.Read(context.Background(), ^uint64(0), MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Reset || len(empty.Chunks) != 0 || empty.Snapshot.Sequence != 0 {
		t.Fatalf("empty future-cursor result = %+v", empty)
	}

	client, server := net.Pipe()
	defer server.Close()
	connected, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return client, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.Write([]byte("current\r\n")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return manager.Snapshot().Sequence > connected.Sequence
	})
	result, err := manager.Read(context.Background(), ^uint64(0), MaxReadBytes, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reset || len(result.Chunks) < 2 || !result.Chunks[0].Reset {
		t.Fatalf("future cursor did not receive replacement chunks: %+v", result)
	}
	if got := string(decodeChunks(t, result.Chunks)); got != "current\r\n" {
		t.Fatalf("future-cursor transcript = %q", got)
	}
}

func TestManagerConcurrentCloseCallersWaitForReaderShutdown(t *testing.T) {
	t.Parallel()

	manager := New(1024)
	connection := newControlledTerminalConn()
	if _, err := manager.Connect(
		context.Background(),
		"rayanlamp.local",
		23,
		func(context.Context) (net.Conn, error) { return connection, nil },
	); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connection.readStarted:
	case <-time.After(time.Second):
		t.Fatal("terminal reader did not start")
	}

	first := make(chan error, 1)
	go func() { first <- manager.Close() }()
	select {
	case <-connection.closed:
	case <-time.After(time.Second):
		t.Fatal("first Close did not close the connection")
	}
	second := make(chan error, 1)
	go func() { second <- manager.Close() }()
	for name, result := range map[string]<-chan error{
		"first":  first,
		"second": second,
	} {
		select {
		case err := <-result:
			t.Fatalf("%s Close returned before reader shutdown: %v", name, err)
		case <-time.After(20 * time.Millisecond):
		}
	}

	close(connection.releaseRead)
	for name, result := range map[string]<-chan error{
		"first":  first,
		"second": second,
	} {
		select {
		case err := <-result:
			if err != nil {
				t.Fatalf("%s Close: %v", name, err)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s Close did not finish", name)
		}
	}
}

type controlledTerminalConn struct {
	readStarted     chan struct{}
	closed          chan struct{}
	releaseRead     chan struct{}
	readStartedOnce sync.Once
	closeOnce       sync.Once
}

func newControlledTerminalConn() *controlledTerminalConn {
	return &controlledTerminalConn{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
		releaseRead: make(chan struct{}),
	}
}

func (connection *controlledTerminalConn) Read([]byte) (int, error) {
	connection.readStartedOnce.Do(func() { close(connection.readStarted) })
	<-connection.closed
	<-connection.releaseRead
	return 0, net.ErrClosed
}

func (connection *controlledTerminalConn) Write(data []byte) (int, error) {
	select {
	case <-connection.closed:
		return 0, net.ErrClosed
	default:
		return len(data), nil
	}
}

func (connection *controlledTerminalConn) Close() error {
	connection.closeOnce.Do(func() { close(connection.closed) })
	return nil
}

func (*controlledTerminalConn) LocalAddr() net.Addr              { return nil }
func (*controlledTerminalConn) RemoteAddr() net.Addr             { return nil }
func (*controlledTerminalConn) SetDeadline(time.Time) error      { return nil }
func (*controlledTerminalConn) SetReadDeadline(time.Time) error  { return nil }
func (*controlledTerminalConn) SetWriteDeadline(time.Time) error { return nil }

type doneObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func decodeChunks(t *testing.T, chunks []Chunk) []byte {
	t.Helper()
	var decoded []byte
	for _, chunk := range chunks {
		value, err := base64.StdEncoding.DecodeString(chunk.DataBase64)
		if err != nil {
			t.Fatalf("decode chunk %d: %v", chunk.Sequence, err)
		}
		decoded = append(decoded, value...)
	}
	return decoded
}

func eventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}
