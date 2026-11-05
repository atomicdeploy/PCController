package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"pccontroller.local/desktop/internal/ipc"
)

func TestClientCallUsesLivingEnvelopeAndUniqueIDs(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		requests []ipc.Request
	)
	client, err := newClientWithCall(Options{}, func(
		_ context.Context,
		request ipc.Request,
	) (ipc.Response, error) {
		mu.Lock()
		requests = append(requests, request)
		mu.Unlock()
		return ipc.Success(request.ID, map[string]string{"state": "on"}), nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first, err := client.On(context.Background())
	if err != nil {
		t.Fatalf("On: %v", err)
	}
	second, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !first.OK || !second.OK {
		t.Fatalf("responses were not successful: %#v %#v", first, second)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("request count = %d, want 2", len(requests))
	}
	if requests[0].Method != "device.on" || requests[1].Method != "status" {
		t.Fatalf("methods = %q, %q", requests[0].Method, requests[1].Method)
	}
	if requests[0].ID == "" || requests[0].ID == requests[1].ID {
		t.Fatalf("request IDs are not unique: %q %q", requests[0].ID, requests[1].ID)
	}
	if firstJSON, marshalErr := json.Marshal(first); marshalErr != nil {
		t.Fatalf("marshal response: %v", marshalErr)
	} else if strings.Contains(string(firstJSON), "protocol_version") {
		t.Fatalf("public response leaked internal protocol version: %s", firstJSON)
	}
}

func TestClientPreservesStructuredCommandFailure(t *testing.T) {
	t.Parallel()

	client, err := newClientWithCall(Options{}, func(
		_ context.Context,
		request ipc.Request,
	) (ipc.Response, error) {
		return ipc.Failure(
			request.ID,
			"device_unavailable",
			"device is offline",
		), nil
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	response, err := client.Toggle(context.Background())
	if err != nil {
		t.Fatalf("Toggle transport error: %v", err)
	}
	if response.OK || response.Error == nil {
		t.Fatalf("unexpected response: %#v", response)
	}
	if response.Error.Code != "device_unavailable" {
		t.Fatalf("error code = %q", response.Error.Code)
	}
	if err := response.DecodeResult(nil); err == nil ||
		!strings.Contains(err.Error(), "device is offline") {
		t.Fatalf("DecodeResult error = %v", err)
	}
}

func TestClientTimeoutAndCallerDeadline(t *testing.T) {
	t.Parallel()

	var observed []time.Duration
	var mu sync.Mutex
	client, err := newClientWithCall(
		Options{Timeout: 250 * time.Millisecond},
		func(ctx context.Context, request ipc.Request) (ipc.Response, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("transport context has no deadline")
			}
			mu.Lock()
			observed = append(observed, time.Until(deadline))
			mu.Unlock()
			return ipc.Success(request.ID, nil), nil
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), "ping", nil); err != nil {
		t.Fatalf("default timeout call: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 125*time.Millisecond)
	defer cancel()
	if _, err := client.Call(ctx, "ping", nil); err != nil {
		t.Fatalf("caller timeout call: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(observed) != 2 {
		t.Fatalf("observed deadlines = %d", len(observed))
	}
	if observed[0] <= 150*time.Millisecond ||
		observed[0] > 300*time.Millisecond {
		t.Fatalf("default timeout = %s", observed[0])
	}
	if observed[1] <= 50*time.Millisecond ||
		observed[1] > 175*time.Millisecond {
		t.Fatalf("caller timeout = %s", observed[1])
	}
}

func TestClientValidationAndTransportError(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Timeout: time.Millisecond}); !errors.Is(
		err,
		ErrInvalidOptions,
	) {
		t.Fatalf("short timeout error = %v", err)
	}
	sentinel := errors.New("transport down")
	client, err := newClientWithCall(Options{}, func(
		context.Context,
		ipc.Request,
	) (ipc.Response, error) {
		return ipc.Response{}, sentinel
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Call(context.Background(), " \x00 ", nil); err == nil {
		t.Fatal("control-character method was accepted")
	}
	if _, err := client.Call(
		context.Background(),
		string([]byte{0xff}),
		nil,
	); err == nil {
		t.Fatal("invalid UTF-8 method was accepted")
	}
	if _, err := client.Call(
		context.Background(),
		strings.Repeat("x", 65),
		nil,
	); err == nil {
		t.Fatal("oversized method was accepted")
	}
	if _, err := client.Call(
		context.Background(),
		"ping",
		nil,
	); !errors.Is(err, sentinel) {
		t.Fatalf("transport error = %v", err)
	}
}
