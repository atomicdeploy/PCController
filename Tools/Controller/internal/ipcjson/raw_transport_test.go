package ipcjson

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestServeRawUsesCanonicalDispatcherWithoutHTTPSniffing(t *testing.T) {
	listener := newMemoryListener()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime := control.New(control.Options{})
	defer runtime.Close()
	service := &Service{
		Client:                controllerapi.AttachSharedRuntime(runtime, shell.New(8)),
		HostInstanceID:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		HostProcessID:         77,
		HostSurface:           "host",
		AuthorizationDisabled: true,
	}
	done := make(chan error, 1)
	go func() {
		done <- ServeRaw(ctx, listener, service, func(net.Conn) Access {
			return Access{Transport: "unix", Principal: "same-user"}
		})
	}()
	server, client := net.Pipe()
	listener.deliver(server)
	request := Request{JSONRPC: Version, ID: json.RawMessage("9"), Method: "controller.ping"}
	if err := json.NewEncoder(client).Encode(request); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := json.NewDecoder(client).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != nil || string(response.ID) != "9" {
		t.Fatalf("response=%#v error=%+v", response, response.Error)
	}
	_ = client.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("raw RPC server did not stop")
	}
}

type memoryListener struct {
	connections chan net.Conn
	done        chan struct{}
	closeOnce   sync.Once
}

func newMemoryListener() *memoryListener {
	return &memoryListener{connections: make(chan net.Conn), done: make(chan struct{})}
}

func (listener *memoryListener) deliver(connection net.Conn) {
	listener.connections <- connection
}

func (listener *memoryListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.done:
		return nil, net.ErrClosed
	}
}

func (listener *memoryListener) Close() error {
	listener.closeOnce.Do(func() { close(listener.done) })
	return nil
}

func (listener *memoryListener) Addr() net.Addr { return memoryAddress("in-memory") }

type memoryAddress string

func (address memoryAddress) Network() string { return "memory" }
func (address memoryAddress) String() string  { return string(address) }
