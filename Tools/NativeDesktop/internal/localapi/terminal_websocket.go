package localapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"rayanlamp.local/app/internal/deviceterminal"
	"rayanlamp.local/app/internal/ipc"
)

const (
	maxTerminalWebSocketClients = 8
	terminalPollWait            = time.Second
	// The shared terminal manager already retains a 256 KiB cursor-addressed
	// transcript. A small per-socket queue smooths ordinary network jitter
	// while backpressure leaves unread bytes in that one canonical replay
	// buffer instead of duplicating up to 64 frames for every browser tab.
	terminalSocketBuffer = 16
)

var errTerminalSessionExpired = errors.New("browser session expired")

// Keep the terminal-specific name for lifecycle tests and internal callers
// while sharing the admission/attachment implementation with event sockets.
type terminalSocketRegistry = webSocketRegistry

func newTerminalSocketRegistry() *terminalSocketRegistry {
	return newWebSocketRegistry(maxTerminalWebSocketClients)
}

type terminalSocketFrame struct {
	Type    string                   `json:"type"`
	Status  *deviceterminal.Snapshot `json:"status,omitempty"`
	Code    string                   `json:"code,omitempty"`
	Message string                   `json:"message,omitempty"`
	Reset   bool                     `json:"reset,omitempty"`
}

type terminalSocketCommand struct {
	Type string `json:"type"`
}

type terminalOutbound struct {
	messageType int
	payload     []byte
}

// terminalWebSocket is a browser-session-only byte bridge. It is deliberately
// separate from /events so terminal traffic cannot starve ordered lamp,
// configuration, or diagnostics events.
func (a *api) terminalWebSocket(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	origin, validOriginHeader := requestOrigin(r)
	if !validOriginHeader || !a.validBrowserHost(r) || !a.sameOrigin(origin) {
		writeError(w, http.StatusForbidden, "forbidden", "invalid browser origin")
		return
	}
	session, ok := a.sessions.authenticate(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid browser session required")
		return
	}
	releaseAdmission, ok := a.terminalSockets.begin()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "busy", "too many terminal clients")
		return
	}
	defer releaseAdmission()

	if a.terminalBeforeUpgrade != nil {
		a.terminalBeforeUpgrade()
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:    4096,
		WriteBufferSize:   8192,
		EnableCompression: false,
		CheckOrigin: func(request *http.Request) bool {
			originValue, valid := requestOrigin(request)
			return a.validBrowserHost(request) &&
				valid &&
				a.sameOrigin(originValue)
		},
	}
	connection, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	if !a.terminalSockets.attach(connection) {
		closeWebSocketGoingAway(connection)
		return
	}
	defer a.terminalSockets.detach(connection)

	ctx, cancel := context.WithCancel(r.Context())
	outbound := make(chan terminalOutbound, terminalSocketBuffer)
	readDone := make(chan error, 1)
	pollDone := make(chan error, 1)
	var socketWorkers sync.WaitGroup
	socketWorkers.Add(2)
	go func() {
		defer socketWorkers.Done()
		readDone <- a.readTerminalSocket(ctx, connection, outbound, session.expires)
	}()
	go func() {
		defer socketWorkers.Done()
		pollDone <- a.pollTerminalSocket(ctx, outbound, session.expires)
	}()
	defer func() {
		cancel()
		_ = connection.Close()
		socketWorkers.Wait()
	}()

	ticker := time.NewTicker(webSocketPingInterval)
	defer ticker.Stop()
	expiryTimer := time.NewTimer(time.Until(session.expires))
	defer expiryTimer.Stop()
	for {
		select {
		case frame := <-outbound:
			if !time.Now().Before(session.expires) {
				closeExpiredTerminalSession(connection)
				return
			}
			if err := connection.SetWriteDeadline(time.Now().Add(webSocketWriteTimeout)); err != nil {
				return
			}
			if err := connection.WriteMessage(frame.messageType, frame.payload); err != nil {
				return
			}
		case <-ticker.C:
			if !time.Now().Before(session.expires) {
				closeExpiredTerminalSession(connection)
				return
			}
			if err := connection.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(webSocketWriteTimeout),
			); err != nil {
				return
			}
		case err := <-readDone:
			if errors.Is(err, errTerminalSessionExpired) {
				closeExpiredTerminalSession(connection)
			}
			return
		case err := <-pollDone:
			if errors.Is(err, errTerminalSessionExpired) {
				closeExpiredTerminalSession(connection)
			}
			return
		case <-expiryTimer.C:
			closeExpiredTerminalSession(connection)
			return
		case <-ctx.Done():
			return
		}
	}
}

func (a *api) readTerminalSocket(
	ctx context.Context,
	connection *websocket.Conn,
	outbound chan<- terminalOutbound,
	sessionExpires time.Time,
) error {
	connection.SetReadLimit(webSocketReadLimit)
	if err := connection.SetReadDeadline(time.Now().Add(webSocketPongTimeout)); err != nil {
		return err
	}
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(webSocketPongTimeout))
	})
	for {
		messageType, payload, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		if !time.Now().Before(sessionExpires) {
			return errTerminalSessionExpired
		}
		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) == 0 {
				continue
			}
			if len(payload) > deviceterminal.MaxInputBytes {
				a.queueTerminalError(ctx, outbound, "request_too_large", deviceterminal.ErrInputTooLarge.Error())
				continue
			}
			result, commandErr := a.callTerminal(
				ctx,
				"terminal.write",
				map[string]string{
					"data_base64": base64.StdEncoding.EncodeToString(payload),
				},
			)
			if commandErr != nil {
				a.queueTerminalCommandError(ctx, outbound, commandErr)
				continue
			}
			a.queueTerminalStatus(ctx, outbound, result)
		case websocket.TextMessage:
			var command terminalSocketCommand
			decoder := json.NewDecoder(strings.NewReader(string(payload)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&command); err != nil {
				a.queueTerminalError(ctx, outbound, "invalid_argument", "invalid terminal control frame")
				continue
			}
			var method string
			switch strings.ToLower(strings.TrimSpace(command.Type)) {
			case "connect":
				method = "terminal.connect"
			case "disconnect":
				method = "terminal.disconnect"
			case "status":
				method = "terminal.status"
			default:
				a.queueTerminalError(ctx, outbound, "invalid_argument", "unknown terminal control frame")
				continue
			}
			result, commandErr := a.callTerminal(ctx, method, nil)
			if commandErr != nil {
				a.queueTerminalCommandError(ctx, outbound, commandErr)
				continue
			}
			a.queueTerminalStatus(ctx, outbound, result)
		default:
			return errors.New("unsupported terminal WebSocket message type")
		}
	}
}

func (a *api) pollTerminalSocket(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	sessionExpires time.Time,
) error {
	var cursor uint64
	var lastStatusJSON string
	for {
		if !time.Now().Before(sessionExpires) {
			return errTerminalSessionExpired
		}
		result, err := a.callTerminal(ctx, "terminal.read", map[string]any{
			"after":   cursor,
			"limit":   deviceterminal.MaxReadBytes,
			"wait_ms": int(terminalPollWait / time.Millisecond),
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.queueTerminalCommandError(ctx, outbound, err)
			timer := time.NewTimer(250 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if !time.Now().Before(sessionExpires) {
			return errTerminalSessionExpired
		}
		readResult, err := decodeTerminalReadResult(result)
		if err != nil {
			return err
		}
		if readResult.Reset {
			if !queueTerminalFrame(ctx, outbound, terminalSocketFrame{
				Type:  "reset",
				Reset: true,
			}) {
				return ctx.Err()
			}
		}
		statusData, _ := json.Marshal(readResult.Snapshot)
		statusJSON := string(statusData)
		if statusJSON != lastStatusJSON {
			status := readResult.Snapshot
			if !queueTerminalFrame(ctx, outbound, terminalSocketFrame{
				Type:   "status",
				Status: &status,
			}) {
				return ctx.Err()
			}
			lastStatusJSON = statusJSON
		}
		for _, chunk := range readResult.Chunks {
			data, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
			if err != nil {
				return errors.New("terminal output contained invalid base64")
			}
			if !queueTerminalOutbound(ctx, outbound, terminalOutbound{
				messageType: websocket.BinaryMessage,
				payload:     data,
			}) {
				return ctx.Err()
			}
			cursor = chunk.Sequence
		}
		if len(readResult.Chunks) == 0 && readResult.Snapshot.Sequence < cursor {
			cursor = readResult.Snapshot.Sequence
		}
	}
}

func closeExpiredTerminalSession(connection *websocket.Conn) {
	if connection == nil {
		return
	}
	deadline := time.Now().Add(webSocketWriteTimeout)
	_ = connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(
			websocket.ClosePolicyViolation,
			errTerminalSessionExpired.Error(),
		),
		deadline,
	)
}

func (a *api) callTerminal(
	ctx context.Context,
	method string,
	params any,
) (any, error) {
	request, err := ipc.NewRequest(a.requestID(), method, params)
	if err != nil {
		return nil, err
	}
	callContext, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	return a.handler(callContext, request)
}

func (a *api) queueTerminalStatus(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	value any,
) {
	status, err := decodeTerminalSnapshot(value)
	if err != nil {
		a.queueTerminalError(ctx, outbound, "invalid_response", err.Error())
		return
	}
	queueTerminalFrame(ctx, outbound, terminalSocketFrame{
		Type:   "status",
		Status: &status,
	})
}

func (a *api) queueTerminalCommandError(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	err error,
) {
	_, code, message := mapCommandError(err)
	a.queueTerminalError(ctx, outbound, code, message)
}

func (a *api) queueTerminalError(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	code string,
	message string,
) {
	queueTerminalFrame(ctx, outbound, terminalSocketFrame{
		Type:    "error",
		Code:    code,
		Message: message,
	})
}

func queueTerminalFrame(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	value terminalSocketFrame,
) bool {
	payload, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return queueTerminalOutbound(ctx, outbound, terminalOutbound{
		messageType: websocket.TextMessage,
		payload:     payload,
	})
}

func queueTerminalOutbound(
	ctx context.Context,
	outbound chan<- terminalOutbound,
	frame terminalOutbound,
) bool {
	select {
	case outbound <- frame:
		return true
	case <-ctx.Done():
		return false
	}
}

func decodeTerminalSnapshot(value any) (deviceterminal.Snapshot, error) {
	if snapshot, ok := value.(deviceterminal.Snapshot); ok {
		return snapshot, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return deviceterminal.Snapshot{}, err
	}
	var snapshot deviceterminal.Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return deviceterminal.Snapshot{}, err
	}
	return snapshot, nil
}

func decodeTerminalReadResult(value any) (deviceterminal.ReadResult, error) {
	if result, ok := value.(deviceterminal.ReadResult); ok {
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return deviceterminal.ReadResult{}, err
	}
	var result deviceterminal.ReadResult
	if err := json.Unmarshal(data, &result); err != nil {
		return deviceterminal.ReadResult{}, err
	}
	return result, nil
}
