//go:build windows

package ipc

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

type testListener struct {
	connections chan net.Conn
	closed      chan struct{}
	closeOnce   sync.Once
}

func newTestListener() *testListener {
	return &testListener{
		connections: make(chan net.Conn, 1),
		closed:      make(chan struct{}),
	}
}

func (l *testListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *testListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *testListener) Addr() net.Addr {
	return testAddr("test-listener")
}

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

type deadlineSignalingConn struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *deadlineSignalingConn) SetDeadline(deadline time.Time) error {
	c.once.Do(func() {
		close(c.started)
	})
	return c.Conn.SetDeadline(deadline)
}

func startTestServer(listener net.Listener) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		listener: listener,
		handler: func(context.Context, Request) (any, error) {
			return nil, nil
		},
		ctx:         ctx,
		cancel:      cancel,
		connections: make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.serve()
	return server
}

func TestServerCloseUnblocksPartialRequest(t *testing.T) {
	listener := newTestListener()
	serverConnection, clientConnection := net.Pipe()
	defer clientConnection.Close()

	started := make(chan struct{})
	listener.connections <- &deadlineSignalingConn{
		Conn:    serverConnection,
		started: started,
	}
	server := startTestServer(listener)

	select {
	case <-started:
	case <-time.After(time.Second):
		_ = server.Close()
		t.Fatal("server did not start reading the accepted connection")
	}

	writeDone := make(chan error, 1)
	go func() {
		_, err := clientConnection.Write([]byte(`{"protocol_version":`))
		writeDone <- err
	}()
	select {
	case err := <-writeDone:
		if err != nil {
			_ = server.Close()
			t.Fatalf("write partial request: %v", err)
		}
	case <-time.After(time.Second):
		_ = clientConnection.Close()
		_ = server.Close()
		t.Fatal("server did not consume the partial request")
	}

	closeDone := make(chan error, 1)
	go func() {
		closeDone <- server.Close()
	}()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("close server: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		_ = clientConnection.Close()
		<-closeDone
		t.Fatal("Server.Close waited for the request read deadline")
	}

	if err := server.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestDecodeSingleJSONRejectsConcatenatedValues(t *testing.T) {
	requestJSON := `{"protocol_version":1,"method":"ping"}`
	for name, input := range map[string]string{
		"concatenated value": requestJSON + `{"protocol_version":1,"method":"toggle"}`,
		"trailing data":      requestJSON + `not-json`,
	} {
		t.Run("request/"+name, func(t *testing.T) {
			var request Request
			if err := decodeSingleJSON(bytes.NewBufferString(input), &request, true); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
	var request Request
	if err := decodeSingleJSON(bytes.NewBufferString(requestJSON+"\r\n\t"), &request, true); err != nil {
		t.Fatalf("rejected trailing whitespace: %v", err)
	}

	responseJSON := `{"protocol_version":1,"ok":true}`
	for name, input := range map[string]string{
		"concatenated value": responseJSON + `{"protocol_version":1,"ok":false}`,
		"trailing data":      responseJSON + `not-json`,
	} {
		t.Run("response/"+name, func(t *testing.T) {
			var response Response
			if err := decodeSingleJSON(bytes.NewBufferString(input), &response, false); err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}
