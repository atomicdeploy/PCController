//go:build windows

package rpc

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func TestNamedPipeEndpointRoundTripAndExclusiveBind(t *testing.T) {
	endpoint, err := DefaultNativeEndpoint("PCController.Test." + strings.ReplaceAll(t.Name(), "/", "."))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := Listen(endpoint, ListenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if second, err := Listen(endpoint, ListenOptions{}); err == nil {
		second.Close()
		t.Fatal("second named-pipe bind succeeded")
	}
	done := make(chan error, 1)
	go echoNamedPipeOnce(listener, done)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	response, err := Call(ctx, endpoint, Request{Method: "controller.ping"}, ClientOptions{})
	if err != nil || response.Error != nil {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNamedPipeDialHonorsCancellation(t *testing.T) {
	endpoint, err := DefaultNativeEndpoint("PCController.Missing." + strings.ReplaceAll(t.Name(), "/", "."))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Call(ctx, endpoint, Request{Method: "controller.ping"}, ClientOptions{}); err == nil {
		t.Fatal("cancelled named-pipe dial succeeded")
	}
}

func echoNamedPipeOnce(listener net.Listener, done chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		done <- err
		return
	}
	defer connection.Close()
	var request Request
	if err := json.NewDecoder(connection).Decode(&request); err != nil {
		done <- err
		return
	}
	done <- json.NewEncoder(connection).Encode(Response{
		JSONRPC: Version, ID: request.ID, Result: map[string]bool{"ok": true},
	})
}
