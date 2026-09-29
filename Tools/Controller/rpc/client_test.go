package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

func TestInProcessClientUsesCanonicalEnvelopeAndErrors(t *testing.T) {
	client, err := NewInProcess(func(_ context.Context, request Request) Response {
		if request.JSONRPC != Version || string(request.ID) != "1" || request.Method != "controller.ping" {
			t.Fatalf("normalized request=%#v", request)
		}
		return Response{JSONRPC: Version, ID: request.ID, Result: map[string]bool{"ok": true}}
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Call(context.Background(), Request{Method: "controller.ping"})
	if err != nil || response.Error != nil || string(response.ID) != "1" {
		t.Fatalf("response=%#v err=%v", response, err)
	}

	failing, err := NewInProcess(func(_ context.Context, request Request) Response {
		return Response{JSONRPC: Version, ID: request.ID, Error: &RPCError{Code: -32003, Message: "denied"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err = failing.Call(context.Background(), Request{Method: "controller.snapshot"})
	var rpcError *RPCError
	if !errors.As(err, &rpcError) || rpcError.Code != -32003 || response.Error != rpcError {
		t.Fatalf("response=%#v err=%v", response, err)
	}
}

func TestStreamClientUsesSameCanonicalEnvelope(t *testing.T) {
	server, clientConnection := net.Pipe()
	defer server.Close()
	dialed := false
	client, err := Dial(Endpoint{Transport: TransportTCP, Address: "127.0.0.1:8787"}, ClientOptions{
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "127.0.0.1:8787" || dialed {
				t.Fatalf("dial network=%q address=%q dialed=%t", network, address, dialed)
			}
			dialed = true
			return clientConnection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var request Request
		if err := json.NewDecoder(server).Decode(&request); err != nil {
			done <- err
			return
		}
		if request.JSONRPC != Version || request.Method != "controller.ping" || string(request.ID) != "7" {
			done <- errors.New("wire request did not preserve the canonical envelope")
			return
		}
		done <- json.NewEncoder(server).Encode(Response{
			JSONRPC: Version, ID: request.ID, Result: map[string]bool{"ok": true},
		})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Call(ctx, Request{
		ID: json.RawMessage("7"), Method: "controller.ping",
	})
	if err != nil || string(response.ID) != "7" {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestEndpointValidationRejectsSemanticFallbacks(t *testing.T) {
	for _, endpoint := range []Endpoint{
		{},
		{Transport: TransportInProcess, Address: "not-a-wire-address"},
		{Transport: "serial", Address: "COM18"},
	} {
		if err := endpoint.Validate(); err == nil {
			t.Fatalf("invalid endpoint accepted: %#v", endpoint)
		}
	}
}

func TestClientRejectsOversizeBeforeDispatchOrDial(t *testing.T) {
	dispatched := false
	client, err := NewInProcess(func(context.Context, Request) Response {
		dispatched = true
		return Response{}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Call(context.Background(), Request{
		Method: "controller.message.send",
		Params: bytes.Repeat([]byte{'x'}, MaxMessageBytes),
	})
	if err == nil || dispatched {
		t.Fatalf("oversize err=%v dispatched=%t", err, dispatched)
	}
}

func TestStreamClientCancellationInterruptsResponseRead(t *testing.T) {
	server, clientConnection := net.Pipe()
	defer server.Close()
	client, err := Dial(Endpoint{Transport: TransportTCP, Address: "127.0.0.1:8787"}, ClientOptions{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return clientConnection, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, callErr := client.Call(ctx, Request{Method: "controller.ping"})
		done <- callErr
	}()
	var request Request
	if err := json.NewDecoder(server).Decode(&request); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("call error=%v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled RPC call remained blocked reading a response")
	}
}
