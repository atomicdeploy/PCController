package wsrelay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestWebSocketClientReceivesValidatedFirmware(t *testing.T) {
	relay := newHub()
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		messages, unsubscribe := relay.subscribe()
		defer unsubscribe()
		select {
		case message := <-messages:
			_ = connection.Write(request.Context(), websocket.MessageText, message)
		case <-request.Context().Done():
		}
	}))
	defer server.Close()

	data := []byte(":0100000001FE\n")
	sum := sha256.Sum256(data)
	encoded, err := Encode(FirmwareMessage{
		Version: MessageVersion,
		Type:    MessageType,
		Name:    "firmware.hex",
		SHA256:  hex.EncodeToString(sum[:]),
		Data:    data,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan FirmwareMessage, 1)
	finished := make(chan error, 1)
	go func() {
		finished <- RunClient(ctx, ClientOptions{
			URL:            strings.Replace(server.URL, "http://", "ws://", 1),
			ReconnectDelay: 10 * time.Millisecond,
			Logger:         log.New(testWriter{t}, "", 0),
			OnFirmware: func(_ context.Context, message FirmwareMessage) error {
				received <- message
				cancel()
				return nil
			},
		})
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case message := <-received:
			if string(message.Data) != string(data) {
				t.Fatalf("received %q want %q", message.Data, data)
			}
			select {
			case err := <-finished:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("client did not stop")
			}
			return
		case <-ticker.C:
			relay.publish(encoded)
		case <-timeout.C:
			t.Fatal("timed out waiting for relayed firmware")
		}
	}
}

type testWriter struct{ t *testing.T }

func (writer testWriter) Write(data []byte) (int, error) {
	writer.t.Log(strings.TrimSpace(string(data)))
	return len(data), nil
}
