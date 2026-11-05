package deviceterminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

const readBufferBytes = 8 << 10

type storedChunk struct {
	sequence uint64
	data     []byte
	at       time.Time
	reset    bool
}

// Manager owns at most one TCP connection. Every consumer receives output by
// its own monotonic cursor, so a TUI and multiple browser tabs can observe the
// same session without stealing bytes from one another.
type Manager struct {
	writeMu sync.Mutex
	mu      sync.Mutex

	scrollbackLimit int
	scrollbackBytes int
	chunks          []storedChunk
	notify          chan struct{}

	state        State
	host         string
	port         int
	session      uint64
	sequence     uint64
	readOnly     bool
	connectedAt  *time.Time
	lastActivity *time.Time
	lastError    string

	conn              net.Conn
	connDone          chan struct{}
	activeConnections map[net.Conn]chan struct{}
	pendingDials      map[uint64]context.CancelFunc
	generation        uint64
	closed            bool
	closeDone         chan struct{}
}

func New(scrollbackBytes int) *Manager {
	if scrollbackBytes <= 0 {
		scrollbackBytes = DefaultScrollbackBytes
	}
	return &Manager{
		scrollbackLimit:   scrollbackBytes,
		notify:            make(chan struct{}),
		state:             StateDisconnected,
		port:              DefaultPort,
		activeConnections: make(map[net.Conn]chan struct{}),
		pendingDials:      make(map[uint64]context.CancelFunc),
		closeDone:         make(chan struct{}),
	}
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{State: StateDisconnected, Port: DefaultPort}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Snapshot {
	oldest := uint64(0)
	if len(m.chunks) > 0 {
		oldest = m.chunks[0].sequence
	}
	return Snapshot{
		State:          m.state,
		Host:           m.host,
		Port:           m.port,
		Session:        m.session,
		Sequence:       m.sequence,
		OldestSequence: oldest,
		ReadOnly:       m.readOnly,
		ConnectedAt:    cloneTime(m.connectedAt),
		LastActivity:   cloneTime(m.lastActivity),
		LastError:      m.lastError,
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (m *Manager) Connect(
	ctx context.Context,
	host string,
	port int,
	dial DialFunc,
) (Snapshot, error) {
	if m == nil {
		return Snapshot{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	host = strings.TrimSpace(host)
	if host == "" || port < 1 || port > 65535 || dial == nil {
		return m.Snapshot(), errors.New("invalid device terminal target")
	}

	m.mu.Lock()
	if m.closed {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, ErrClosed
	}
	if m.state == StateConnected && strings.EqualFold(m.host, host) && m.port == port {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, nil
	}
	if m.state == StateConnecting {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, ErrBusy
	}
	dialContext, cancelDial := context.WithCancel(ctx)
	old := m.conn
	oldDone := m.connDone
	m.conn = nil
	m.connDone = nil
	m.session++
	m.generation++
	generation := m.generation
	m.pendingDials[generation] = cancelDial
	// A manual connection starts a fresh, in-memory transcript. Keeping bytes
	// from an earlier device session would blur command boundaries and retain
	// potentially sensitive shell output longer than expected.
	m.resetTranscriptLocked(time.Now().UTC())
	m.state = StateConnecting
	m.host = host
	m.port = port
	m.readOnly = false
	m.connectedAt = nil
	m.lastActivity = nil
	m.lastError = ""
	m.signalLocked()
	m.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	waitForConnection(oldDone)

	connection, err := dial(dialContext)
	cancelDial()
	if err == nil && connection == nil {
		err = errors.New("device terminal dial returned a nil connection")
	}
	if err != nil {
		m.mu.Lock()
		delete(m.pendingDials, generation)
		current := !m.closed && m.generation == generation
		if current {
			m.state = StateDisconnected
			m.lastError = compactNetworkError(err)
			m.signalLocked()
		}
		snapshot := m.snapshotLocked()
		closed := m.closed
		m.mu.Unlock()
		if closed {
			return snapshot, ErrClosed
		}
		if !current {
			return snapshot, context.Canceled
		}
		return snapshot, fmt.Errorf("connect device terminal: %w", err)
	}

	now := time.Now().UTC()
	m.mu.Lock()
	delete(m.pendingDials, generation)
	if m.closed || m.generation != generation {
		snapshot := m.snapshotLocked()
		closed := m.closed
		m.mu.Unlock()
		_ = connection.Close()
		if closed {
			return snapshot, ErrClosed
		}
		return snapshot, context.Canceled
	}
	done := make(chan struct{})
	m.conn = connection
	m.connDone = done
	m.activeConnections[connection] = done
	m.state = StateConnected
	m.connectedAt = &now
	m.lastActivity = &now
	m.lastError = ""
	m.signalLocked()
	snapshot := m.snapshotLocked()
	go m.readLoop(generation, connection, done)
	m.mu.Unlock()
	return snapshot, nil
}

func (m *Manager) Disconnect() Snapshot {
	if m == nil {
		return Snapshot{State: StateDisconnected, Port: DefaultPort}
	}

	m.mu.Lock()
	if m.closed {
		done := m.closeDone
		m.mu.Unlock()
		<-done
		return m.Snapshot()
	}
	cancels := m.takePendingDialsLocked()
	connections := make([]net.Conn, 0, len(m.activeConnections))
	doneChannels := make([]chan struct{}, 0, len(m.activeConnections))
	for connection, done := range m.activeConnections {
		connections = append(connections, connection)
		doneChannels = append(doneChannels, done)
	}
	wasActive := m.state != StateDisconnected ||
		len(connections) > 0 ||
		len(cancels) > 0 ||
		m.transcriptHasDataLocked()
	m.conn = nil
	m.connDone = nil
	m.generation++
	m.state = StateDisconnected
	m.readOnly = false
	m.connectedAt = nil
	m.lastActivity = nil
	m.lastError = ""
	if wasActive {
		m.resetTranscriptLocked(time.Now().UTC())
	}
	m.signalLocked()
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	cancelDials(cancels)
	for _, connection := range connections {
		_ = connection.Close()
	}
	for _, done := range doneChannels {
		waitForConnection(done)
	}
	return snapshot
}

func (m *Manager) Write(ctx context.Context, data []byte) (Snapshot, error) {
	if m == nil {
		return Snapshot{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(data) == 0 {
		return m.Snapshot(), nil
	}
	if len(data) > MaxInputBytes {
		return m.Snapshot(), ErrInputTooLarge
	}

	m.mu.Lock()
	if m.closed {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, ErrClosed
	}
	connection := m.conn
	generation := m.generation
	if m.state != StateConnected || connection == nil {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, ErrNotConnected
	}
	if m.readOnly {
		snapshot := m.snapshotLocked()
		m.mu.Unlock()
		return snapshot, ErrReadOnly
	}
	m.mu.Unlock()

	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	if value, ok := ctx.Deadline(); ok {
		deadline = value
	}
	if err := connection.SetWriteDeadline(deadline); err != nil {
		return m.failConnection(generation, connection, err)
	}
	defer connection.SetWriteDeadline(time.Time{})
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return m.Snapshot(), err
		}
		written, err := connection.Write(data)
		if err != nil {
			return m.failConnection(generation, connection, err)
		}
		if written <= 0 {
			return m.failConnection(generation, connection, io.ErrShortWrite)
		}
		data = data[written:]
	}
	now := time.Now().UTC()
	m.mu.Lock()
	if m.generation == generation && m.conn == connection {
		m.lastActivity = &now
		m.signalLocked()
	}
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	return snapshot, nil
}

func (m *Manager) failConnection(
	generation uint64,
	connection net.Conn,
	err error,
) (Snapshot, error) {
	m.mu.Lock()
	if m.generation == generation && m.conn == connection {
		m.conn = nil
		m.state = StateDisconnected
		m.connectedAt = nil
		m.lastError = compactNetworkError(err)
		m.signalLocked()
	}
	snapshot := m.snapshotLocked()
	m.mu.Unlock()
	_ = connection.Close()
	return snapshot, err
}

func (m *Manager) Read(
	ctx context.Context,
	after uint64,
	limit int,
	wait time.Duration,
) (ReadResult, error) {
	if m == nil {
		return ReadResult{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = DefaultReadBytes
	}
	if limit > MaxReadBytes {
		limit = MaxReadBytes
	}
	if wait < 0 {
		wait = 0
	}
	if wait > MaxLongPoll {
		wait = MaxLongPoll
	}

	deadline := time.Time{}
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		m.mu.Lock()
		if m.closed {
			result := m.readLocked(after, limit)
			m.mu.Unlock()
			return result, ErrClosed
		}
		result := m.readLocked(after, limit)
		if readResultReady(result, after) || wait == 0 {
			m.mu.Unlock()
			return result, nil
		}
		notify := m.notify
		m.mu.Unlock()

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return m.currentRead(after, limit), nil
		}
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ReadResult{}, ctx.Err()
		case <-notify:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			// A state-only change should return promptly even if there are no
			// bytes. This makes disconnect/connect status immediately visible.
			return m.currentRead(after, limit), nil
		case <-timer.C:
			return m.currentRead(after, limit), nil
		}
	}
}

func readResultReady(result ReadResult, after uint64) bool {
	// A reset marker is a real cursor item for an established reader. A brand
	// new reader at cursor zero has nothing to clear, so a marker by itself
	// must not defeat long polling before device output arrives.
	if result.Reset && after != 0 {
		return true
	}
	for _, chunk := range result.Chunks {
		if !chunk.Reset {
			return true
		}
	}
	return false
}

func (m *Manager) currentRead(after uint64, limit int) ReadResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readLocked(after, limit)
}

func (m *Manager) readLocked(after uint64, limit int) ReadResult {
	result := ReadResult{Snapshot: m.snapshotLocked()}
	if after > m.sequence {
		result.Reset = true
		after = 0
	}
	if len(m.chunks) == 0 {
		return result
	}
	oldest := m.chunks[0].sequence
	if after != 0 && oldest > 1 && after < oldest-1 {
		result.Reset = true
		after = oldest - 1
	}
	used := 0
	for _, stored := range m.chunks {
		if stored.sequence <= after {
			continue
		}
		if used > 0 && used+len(stored.data) > limit {
			break
		}
		if stored.reset {
			result.Reset = true
		}
		// Chunks are cursor-atomic. Returning a partial first chunk and then
		// advancing its sequence would silently lose the remainder for that
		// reader, so one bounded read-buffer chunk may exceed a tiny requested
		// limit.
		data := stored.data
		result.Chunks = append(result.Chunks, Chunk{
			Sequence:   stored.sequence,
			DataBase64: base64.StdEncoding.EncodeToString(data),
			At:         stored.at,
			Reset:      stored.reset,
		})
		used += len(data)
		if used >= limit {
			break
		}
	}
	return result
}

func (m *Manager) readLoop(
	generation uint64,
	connection net.Conn,
	done chan struct{},
) {
	defer func() {
		close(done)
		m.mu.Lock()
		delete(m.activeConnections, connection)
		if m.connDone == done {
			m.connDone = nil
		}
		m.mu.Unlock()
	}()
	var decoder telnetDecoder
	var filter terminalFilter
	var bannerTail []byte
	buffer := make([]byte, readBufferBytes)
	for {
		count, err := connection.Read(buffer)
		if count > 0 {
			payload := decoder.Feed(buffer[:count])
			payload = filter.Feed(payload)
			if len(payload) > 0 {
				lower := bytes.ToLower(append(append([]byte(nil), bannerTail...), payload...))
				if len(lower) > 256 {
					lower = lower[len(lower)-256:]
				}
				bannerTail = append(bannerTail[:0], lower...)
				readOnly := bytes.Contains(lower, []byte("shell is on serial"))
				m.appendOutput(generation, connection, payload, readOnly)
			}
		}
		if err != nil {
			m.mu.Lock()
			if m.generation == generation && m.conn == connection {
				m.conn = nil
				// Keep connDone until the deferred cleanup closes it so a
				// concurrent Disconnect or Close can wait for this reader.
				m.state = StateDisconnected
				m.connectedAt = nil
				if !errors.Is(err, net.ErrClosed) {
					if errors.Is(err, io.EOF) {
						m.lastError = "device closed the terminal connection"
					} else {
						m.lastError = compactNetworkError(err)
					}
				}
				m.signalLocked()
			}
			m.mu.Unlock()
			_ = connection.Close()
			return
		}
	}
}

func (m *Manager) appendOutput(
	generation uint64,
	connection net.Conn,
	payload []byte,
	readOnly bool,
) {
	now := time.Now().UTC()
	payloadCopy := append([]byte(nil), payload...)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != generation || m.conn != connection {
		return
	}
	m.sequence++
	m.chunks = append(m.chunks, storedChunk{
		sequence: m.sequence,
		data:     payloadCopy,
		at:       now,
	})
	m.scrollbackBytes += len(payloadCopy)
	for m.scrollbackBytes > m.scrollbackLimit && len(m.chunks) > 1 {
		m.scrollbackBytes -= len(m.chunks[0].data)
		copy(m.chunks, m.chunks[1:])
		m.chunks = m.chunks[:len(m.chunks)-1]
	}
	m.lastActivity = &now
	if readOnly {
		m.readOnly = true
	}
	m.signalLocked()
}

func (m *Manager) resetTranscriptLocked(at time.Time) {
	if m.sequence == ^uint64(0) {
		// Wrapping to a low sequence makes every extant non-zero cursor a
		// future cursor; readLocked will return Reset and recover it.
		m.sequence = 0
	}
	m.sequence++
	m.chunks = []storedChunk{{
		sequence: m.sequence,
		at:       at,
		reset:    true,
	}}
	m.scrollbackBytes = 0
}

func (m *Manager) transcriptHasDataLocked() bool {
	for _, chunk := range m.chunks {
		if !chunk.reset && len(chunk.data) > 0 {
			return true
		}
	}
	return false
}

func (m *Manager) takePendingDialsLocked() []context.CancelFunc {
	cancels := make([]context.CancelFunc, 0, len(m.pendingDials))
	for generation, cancel := range m.pendingDials {
		cancels = append(cancels, cancel)
		delete(m.pendingDials, generation)
	}
	return cancels
}

func cancelDials(cancels []context.CancelFunc) {
	for _, cancel := range cancels {
		cancel()
	}
}

func waitForConnection(done <-chan struct{}) {
	if done != nil {
		<-done
	}
}

func (m *Manager) signalLocked() {
	close(m.notify)
	m.notify = make(chan struct{})
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if m.closed {
		done := m.closeDone
		m.mu.Unlock()
		<-done
		return nil
	}
	m.closed = true
	cancels := m.takePendingDialsLocked()
	connections := make([]net.Conn, 0, len(m.activeConnections))
	doneChannels := make([]chan struct{}, 0, len(m.activeConnections))
	for connection, done := range m.activeConnections {
		connections = append(connections, connection)
		doneChannels = append(doneChannels, done)
	}
	m.conn = nil
	m.connDone = nil
	m.generation++
	m.state = StateDisconnected
	m.readOnly = false
	m.connectedAt = nil
	m.lastActivity = nil
	m.chunks = nil
	m.scrollbackBytes = 0
	m.signalLocked()
	m.mu.Unlock()
	cancelDials(cancels)
	for _, connection := range connections {
		_ = connection.Close()
	}
	for _, done := range doneChannels {
		waitForConnection(done)
	}
	close(m.closeDone)
	return nil
}

func compactNetworkError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 240 {
		message = message[:240] + "…"
	}
	return message
}
