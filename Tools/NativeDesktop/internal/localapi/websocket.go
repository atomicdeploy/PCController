package localapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	webSocketWriteTimeout = 5 * time.Second
	webSocketPongTimeout  = 60 * time.Second
	webSocketPingInterval = 25 * time.Second
	webSocketReadLimit    = 8 << 10
)

func (a *api) eventsWebSocket(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	origin, validOriginHeader := requestOrigin(r)
	if !validOriginHeader || !a.validBrowserHost(r) || !a.sameOrigin(origin) {
		writeError(w, http.StatusForbidden, "forbidden", "invalid browser origin")
		return
	}
	if _, ok := a.sessions.authenticate(r); !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "valid browser session required")
		return
	}
	if a.hub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "event stream is unavailable")
		return
	}
	releaseAdmission, ok := a.eventSockets.begin()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "busy", "event stream is shutting down or full")
		return
	}
	defer releaseAdmission()

	if a.eventBeforeUpgrade != nil {
		a.eventBeforeUpgrade()
	}

	upgrader := websocket.Upgrader{
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
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
	if !a.eventSockets.attach(connection) {
		closeWebSocketGoingAway(connection)
		return
	}
	defer a.eventSockets.detach(connection)

	client, err := a.hub.register()
	if err != nil {
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, err.Error()),
			time.Now().Add(webSocketWriteTimeout),
		)
		_ = connection.Close()
		return
	}
	defer a.hub.unregister(client)

	readDone := make(chan error, 1)
	var socketWorkers sync.WaitGroup
	socketWorkers.Add(1)
	go func() {
		defer socketWorkers.Done()
		readDone <- readWebSocket(connection)
	}()
	defer func() {
		_ = connection.Close()
		socketWorkers.Wait()
	}()

	ticker := time.NewTicker(webSocketPingInterval)
	defer ticker.Stop()
	closeCode := websocket.CloseNormalClosure
	closeText := "closed"
	for {
		select {
		case envelope, ok := <-client.send:
			if !ok {
				closeCode = websocket.CloseGoingAway
				closeText = webSocketShutdownReason
				goto closeConnection
			}
			payload, err := json.Marshal(envelope)
			if err != nil {
				return
			}
			if err := connection.SetWriteDeadline(time.Now().Add(webSocketWriteTimeout)); err != nil {
				return
			}
			if err := connection.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			if err := connection.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(webSocketWriteTimeout),
			); err != nil {
				return
			}
		case <-readDone:
			return
		}
	}

closeConnection:
	_ = connection.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(closeCode, closeText),
		time.Now().Add(webSocketWriteTimeout),
	)
}

func readWebSocket(connection *websocket.Conn) error {
	connection.SetReadLimit(webSocketReadLimit)
	if err := connection.SetReadDeadline(time.Now().Add(webSocketPongTimeout)); err != nil {
		return err
	}
	connection.SetPongHandler(func(string) error {
		return connection.SetReadDeadline(time.Now().Add(webSocketPongTimeout))
	})
	for {
		messageType, _, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
			return errors.New("application messages are not accepted on the event stream")
		}
	}
}
