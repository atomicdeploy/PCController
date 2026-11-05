package localapi

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const webSocketShutdownReason = "server shutting down"

// webSocketRegistry owns the complete lifecycle of one class of WebSocket
// connection, including the interval between admission and HTTP hijacking.
// begin and shutdown share a mutex so shutdown cannot miss a handshake which
// has been admitted but has not attached its connection yet.
type webSocketRegistry struct {
	mu      sync.Mutex
	limit   int
	closing bool
	clients int
	sockets map[*websocket.Conn]struct{}
	workers sync.WaitGroup

	waitOnce sync.Once
	done     chan struct{}
}

func newWebSocketRegistry(limit int) *webSocketRegistry {
	return &webSocketRegistry{
		limit:   limit,
		sockets: make(map[*websocket.Conn]struct{}),
		done:    make(chan struct{}),
	}
}

func (r *webSocketRegistry) begin() (func(), bool) {
	if r == nil {
		return func() {}, false
	}
	r.mu.Lock()
	if r.closing || (r.limit > 0 && r.clients >= r.limit) {
		r.mu.Unlock()
		return func() {}, false
	}
	r.clients++
	// shutdown sets closing while holding this same mutex before any Wait can
	// begin. No Add can therefore race the Wait started by wait.
	r.workers.Add(1)
	r.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.mu.Lock()
			r.clients--
			r.mu.Unlock()
			r.workers.Done()
		})
	}, true
}

func (r *webSocketRegistry) attach(connection *websocket.Conn) bool {
	if r == nil || connection == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return false
	}
	r.sockets[connection] = struct{}{}
	return true
}

func (r *webSocketRegistry) detach(connection *websocket.Conn) {
	if r == nil || connection == nil {
		return
	}
	r.mu.Lock()
	delete(r.sockets, connection)
	r.mu.Unlock()
}

// shutdown closes the admission gate synchronously, then starts a close-frame
// writer for every attached connection. Writers run independently so one
// unresponsive peer cannot delay the close notification for every other peer.
func (r *webSocketRegistry) shutdown() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return
	}
	r.closing = true
	sockets := make([]*websocket.Conn, 0, len(r.sockets))
	for connection := range r.sockets {
		sockets = append(sockets, connection)
	}
	r.mu.Unlock()

	for _, connection := range sockets {
		go closeWebSocketGoingAway(connection)
	}
}

// wait joins admitted handlers but never outlives ctx. shutdown is called
// defensively so callers cannot introduce a WaitGroup Add/Wait race.
func (r *webSocketRegistry) wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.shutdown()
	r.waitOnce.Do(func() {
		go func() {
			r.workers.Wait()
			close(r.done)
		}()
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func closeWebSocketGoingAway(connection *websocket.Conn) {
	if connection == nil {
		return
	}
	_ = connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(
			websocket.CloseGoingAway,
			webSocketShutdownReason,
		),
		time.Now().Add(webSocketWriteTimeout),
	)
	// The control frame is best-effort. Closing the transport is still
	// necessary to unblock readers and make handler joins deterministic.
	_ = connection.Close()
}
