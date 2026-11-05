package device

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"pccontroller.local/desktop/internal/model"
)

const (
	defaultPingInterval = 25 * time.Second
	defaultPongTimeout  = 55 * time.Second
	defaultWriteTimeout = 5 * time.Second
	defaultReadLimit    = int64(64 << 10)
)

// ReconnectPolicy controls bounded exponential reconnect delays. Jitter is a
// fraction from 0 through 1; 0.2 means +/-20 percent.
type ReconnectPolicy struct {
	Initial    time.Duration
	Maximum    time.Duration
	Multiplier float64
	Jitter     float64
	ResetAfter time.Duration
}

// DefaultReconnectPolicy is suitable for a continuously running tray app.
func DefaultReconnectPolicy() ReconnectPolicy {
	return ReconnectPolicy{
		Initial:    500 * time.Millisecond,
		Maximum:    30 * time.Second,
		Multiplier: 2,
		Jitter:     0.2,
		ResetAfter: time.Minute,
	}
}

type websocketConfig struct {
	policy       ReconnectPolicy
	pingInterval time.Duration
	pongTimeout  time.Duration
	writeTimeout time.Duration
	readLimit    int64
	randomMu     sync.Mutex
	random       *rand.Rand
}

func newWebSocketConfig() websocketConfig {
	return websocketConfig{
		policy:       DefaultReconnectPolicy(),
		pingInterval: defaultPingInterval,
		pongTimeout:  defaultPongTimeout,
		writeTimeout: defaultWriteTimeout,
		readLimit:    defaultReadLimit,
		random:       rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// WithReconnectPolicy changes the listener's retry behavior.
func WithReconnectPolicy(policy ReconnectPolicy) Option {
	return func(c *Client) error {
		c.ws.policy = policy
		return nil
	}
}

// WithKeepAlive configures WebSocket ping/pong behavior. A zero ping interval
// disables client pings and read deadlines.
func WithKeepAlive(pingInterval, pongTimeout, writeTimeout time.Duration) Option {
	return func(c *Client) error {
		c.ws.pingInterval = pingInterval
		c.ws.pongTimeout = pongTimeout
		c.ws.writeTimeout = writeTimeout
		return nil
	}
}

// WithWebSocketReadLimit changes the maximum accepted frame/message size.
func WithWebSocketReadLimit(bytes int64) Option {
	return func(c *Client) error {
		c.ws.readLimit = bytes
		return nil
	}
}

func (c *Client) validateConfiguration() error {
	if c.httpClient == nil || c.wsDialer == nil || c.resolution == nil {
		return errors.New("device: incomplete client configuration")
	}
	policy := c.ws.policy
	if policy.Initial <= 0 || policy.Maximum < policy.Initial ||
		policy.Multiplier < 1 || math.IsNaN(policy.Multiplier) ||
		policy.Jitter < 0 || policy.Jitter > 1 || math.IsNaN(policy.Jitter) ||
		policy.ResetAfter < 0 {
		return errors.New("device: invalid reconnect policy")
	}
	if c.ws.pingInterval < 0 || c.ws.pongTimeout < 0 || c.ws.writeTimeout <= 0 {
		return errors.New("device: invalid WebSocket keepalive durations")
	}
	if c.ws.pingInterval > 0 && c.ws.pongTimeout <= c.ws.pingInterval {
		return errors.New("device: pong timeout must exceed ping interval")
	}
	if c.ws.readLimit < 1 {
		return errors.New("device: WebSocket read limit must be positive")
	}
	return nil
}

// ConnectionPhase describes the listener lifecycle.
type ConnectionPhase uint8

const (
	ConnectionConnecting ConnectionPhase = iota
	ConnectionOnline
	ConnectionWaiting
	ConnectionStopped
)

func (p ConnectionPhase) String() string {
	switch p {
	case ConnectionConnecting:
		return "connecting"
	case ConnectionOnline:
		return "online"
	case ConnectionWaiting:
		return "waiting"
	case ConnectionStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ConnectionStatus is delivered on each meaningful connect/reconnect state
// change. Attempt is one-based while waiting and zero while online.
type ConnectionStatus struct {
	Phase   ConnectionPhase
	Online  bool
	Attempt int
	RetryIn time.Duration
	Err     error
	At      time.Time
}

// Handlers receives parsed remote events and connection lifecycle changes.
// Callbacks are invoked serially on the listener goroutine.
type Handlers struct {
	OnEvent      func(model.Event)
	OnConnection func(ConnectionStatus)
}

// Listen connects to /ws until ctx is canceled. It sends exactly one text
// command, "get", after every successful (re)connect, then passively consumes
// events. This is intentional: the firmware leaks memory for every command.
func (c *Client) Listen(ctx context.Context, handlers Handlers) error {
	if ctx == nil {
		return ErrNilContext
	}

	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			notifyConnection(handlers, ConnectionStatus{
				Phase: ConnectionStopped,
				Err:   err,
				At:    time.Now().UTC(),
			})
			return err
		}

		notifyConnection(handlers, ConnectionStatus{
			Phase:   ConnectionConnecting,
			Attempt: failures,
			At:      time.Now().UTC(),
		})
		lifetime, err := c.listenOnce(ctx, handlers)
		if ctx.Err() != nil {
			notifyConnection(handlers, ConnectionStatus{
				Phase: ConnectionStopped,
				Err:   ctx.Err(),
				At:    time.Now().UTC(),
			})
			return ctx.Err()
		}

		// An unexpected WebSocket loss is a cached-address connection failure.
		// The next reconnect resolves .local again; normal operation never polls
		// DNS repeatedly.
		c.resolution.invalidate()
		if lifetime >= c.ws.policy.ResetAfter {
			failures = 0
		}
		failures++
		delay := c.reconnectDelay(failures)
		notifyConnection(handlers, ConnectionStatus{
			Phase:   ConnectionWaiting,
			Attempt: failures,
			RetryIn: delay,
			Err:     err,
			At:      time.Now().UTC(),
		})
		if err := waitContext(ctx, delay); err != nil {
			notifyConnection(handlers, ConnectionStatus{
				Phase: ConnectionStopped,
				Err:   err,
				At:    time.Now().UTC(),
			})
			return err
		}
	}
}

func (c *Client) listenOnce(ctx context.Context, handlers Handlers) (time.Duration, error) {
	headers := make(http.Header)
	headers.Set("User-Agent", c.userAgent)

	connection, response, err := c.wsDialer.DialContext(ctx, c.WebSocketURL(), headers)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return 0, fmt.Errorf("device WebSocket connect: %w", err)
	}
	connectedAt := time.Now()
	defer connection.Close()

	connection.SetReadLimit(c.ws.readLimit)
	if c.ws.pingInterval > 0 {
		if err := connection.SetReadDeadline(time.Now().Add(c.ws.pongTimeout)); err != nil {
			return 0, err
		}
		connection.SetPongHandler(func(_ string) error {
			return connection.SetReadDeadline(time.Now().Add(c.ws.pongTimeout))
		})
	}
	if err := connection.SetWriteDeadline(time.Now().Add(c.ws.writeTimeout)); err != nil {
		return 0, err
	}
	// Exactly one state request per connection. Do not repeat on a timer or pong.
	if err := connection.WriteMessage(websocket.TextMessage, []byte("get")); err != nil {
		return time.Since(connectedAt), fmt.Errorf("device WebSocket initial get: %w", err)
	}

	notifyConnection(handlers, ConnectionStatus{
		Phase:  ConnectionOnline,
		Online: true,
		At:     time.Now().UTC(),
	})

	stop := make(chan struct{})
	defer close(stop)
	go closeOnCancellation(ctx, connection, stop, c.ws.writeTimeout)

	var pingErrors <-chan error
	if c.ws.pingInterval > 0 {
		errorsChannel := make(chan error, 1)
		pingErrors = errorsChannel
		go c.pingLoop(connection, stop, errorsChannel)
	}

	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			select {
			case pingErr := <-pingErrors:
				if pingErr != nil {
					err = pingErr
				}
			default:
			}
			if ctx.Err() != nil {
				return time.Since(connectedAt), ctx.Err()
			}
			return time.Since(connectedAt), fmt.Errorf("device WebSocket read: %w", err)
		}
		if messageType != websocket.TextMessage {
			continue
		}
		event, ok := ParseEvent(string(payload))
		if ok && handlers.OnEvent != nil {
			handlers.OnEvent(event)
		}
	}
}

func (c *Client) pingLoop(connection *websocket.Conn, stop <-chan struct{}, result chan<- error) {
	ticker := time.NewTicker(c.ws.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			err := connection.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(c.ws.writeTimeout),
			)
			if err != nil {
				select {
				case result <- fmt.Errorf("device WebSocket ping: %w", err):
				default:
				}
				connection.Close()
				return
			}
		}
	}
}

func closeOnCancellation(ctx context.Context, connection *websocket.Conn, stop <-chan struct{}, writeTimeout time.Duration) {
	select {
	case <-stop:
		return
	case <-ctx.Done():
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "context canceled"),
			time.Now().Add(writeTimeout),
		)
		connection.Close()
	}
}

func (c *Client) reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	policy := c.ws.policy
	delayFloat := float64(policy.Initial) * math.Pow(policy.Multiplier, float64(attempt-1))
	if delayFloat > float64(policy.Maximum) || math.IsInf(delayFloat, 1) {
		delayFloat = float64(policy.Maximum)
	}
	if policy.Jitter > 0 {
		c.ws.randomMu.Lock()
		random := c.ws.random.Float64()
		c.ws.randomMu.Unlock()
		factor := 1 - policy.Jitter + (2 * policy.Jitter * random)
		delayFloat *= factor
	}
	if delayFloat > float64(policy.Maximum) {
		delayFloat = float64(policy.Maximum)
	}
	if delayFloat < 0 {
		delayFloat = 0
	}
	return time.Duration(delayFloat)
}

func notifyConnection(handlers Handlers, status ConnectionStatus) {
	if handlers.OnConnection != nil {
		handlers.OnConnection(status)
	}
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
