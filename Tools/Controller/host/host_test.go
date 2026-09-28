package host

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/rpc"
)

type testLogger struct {
	output bytes.Buffer
}

func (logger *testLogger) Printf(format string, values ...any) {
	fmt.Fprintf(&logger.output, format, values...)
}

func TestLifecycleAndInProcessRPC(t *testing.T) {
	host := newTestHost(t, Options{
		DataRoot:           t.TempDir(),
		Branding:           Branding{AppID: uniqueAppID(t)},
		ControllerOptions:  &controller.Options{},
		DisableAutoConnect: true,
		DisableNative:      true,
	})
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background()); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("second Start error = %v, want ErrAlreadyStarted", err)
	}
	assertPing(t, host)
	if client, err := host.Controller(); err != nil || client == nil {
		t.Fatalf("Controller() = %v, %v", client, err)
	}
	stopContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.Stop(stopContext); err != nil {
		t.Fatal(err)
	}
	select {
	case <-host.Done():
	default:
		t.Fatal("Done was not closed after Stop")
	}
	if _, err := host.RPC(); !errors.Is(err, ErrNotStarted) {
		t.Fatalf("RPC after Stop error = %v, want ErrNotStarted", err)
	}
	if err := host.Stop(stopContext); err != nil {
		t.Fatalf("repeated Stop: %v", err)
	}
}

func TestDuplicateOwnershipAndRelease(t *testing.T) {
	root := t.TempDir()
	appID := uniqueAppID(t)
	options := Options{
		DataRoot:           root,
		Branding:           Branding{AppID: appID},
		ControllerOptions:  &controller.Options{},
		DisableAutoConnect: true,
		DisableNative:      true,
	}
	first := newTestHost(t, options)
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	second := newTestHost(t, options)
	if err := second.Start(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("duplicate Start error = %v, want ErrAlreadyRunning", err)
	}
	stopHost(t, first)

	third := newTestHost(t, options)
	if err := third.Start(context.Background()); err != nil {
		t.Fatalf("Start after ownership release: %v", err)
	}
	stopHost(t, third)
}

func TestNativeAndHTTPEndpointsCoexistWithInProcessRPC(t *testing.T) {
	host := newTestHost(t, Options{
		DataRoot:           t.TempDir(),
		Branding:           Branding{AppID: uniqueAppID(t)},
		ControllerOptions:  &controller.Options{},
		DisableAutoConnect: true,
		HTTP: &HTTPOptions{
			Address: "127.0.0.1:0",
		},
	})
	if err := host.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer stopHost(t, host)
	assertPing(t, host)

	endpoints := host.Endpoints()
	if len(endpoints) != 2 {
		t.Fatalf("Endpoints count = %d, want native and TCP", len(endpoints))
	}
	seen := make(map[string]bool)
	for _, endpoint := range endpoints {
		seen[endpoint.Transport] = true
		callContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		response, err := rpc.Call(callContext, endpoint, rpc.Request{
			Method: "controller.ping",
			Params: json.RawMessage(`{}`),
		}, rpc.ClientOptions{})
		cancel()
		if err != nil {
			t.Fatalf("ping through %s: %v", endpoint.Transport, err)
		}
		assertPingResponse(t, response)
	}
	if !seen[rpc.TransportTCP] {
		t.Fatal("TCP endpoint was not advertised")
	}
	if !seen[rpc.TransportNamedPipe] && !seen[rpc.TransportUnix] {
		t.Fatal("native endpoint was not advertised")
	}
}

func TestParentCancellationStopsHost(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	host := newTestHost(t, Options{
		DataRoot:           t.TempDir(),
		Branding:           Branding{AppID: uniqueAppID(t)},
		ControllerOptions:  &controller.Options{},
		DisableAutoConnect: true,
		DisableNative:      true,
	})
	if err := host.Start(parent); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-host.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("host did not stop after parent cancellation")
	}
}

func TestBackgroundErrorDetailsStayOnErrorsChannel(t *testing.T) {
	logger := &testLogger{}
	host := newTestHost(t, Options{
		DataRoot:           t.TempDir(),
		Branding:           Branding{AppID: uniqueAppID(t)},
		ControllerOptions:  &controller.Options{},
		DisableAutoConnect: true,
		DisableNative:      true,
		Logger:             logger,
	})
	host.report(errors.New("untrusted\r\nforged log entry"))
	if bytes.Contains(logger.output.Bytes(), []byte("untrusted")) ||
		bytes.Contains(logger.output.Bytes(), []byte("forged")) {
		t.Fatalf("raw error details reached logger: %q", logger.output.String())
	}
	select {
	case err := <-host.Errors():
		if err == nil || err.Error() != "untrusted\r\nforged log entry" {
			t.Fatalf("Errors() = %v", err)
		}
	default:
		t.Fatal("detailed background error was not reported")
	}
}

func newTestHost(t *testing.T, options Options) *Host {
	t.Helper()
	host, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func uniqueAppID(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("pccontroller.test.%s.%d", t.Name(), time.Now().UnixNano())
}

func assertPing(t *testing.T, host *Host) {
	t.Helper()
	var result struct {
		OK bool `json:"ok"`
	}
	callContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.Call(callContext, "controller.ping", map[string]any{}, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("in-process ping did not return ok")
	}
}

func assertPingResponse(t *testing.T, response rpc.Response) {
	t.Helper()
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatal("endpoint ping did not return ok")
	}
}

func stopHost(t *testing.T, host *Host) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}
