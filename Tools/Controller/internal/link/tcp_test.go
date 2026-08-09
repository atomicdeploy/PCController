package link

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

func TestTCPVirtualBoardAcceptsDelayedFragmentedHello(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		time.Sleep(45 * time.Millisecond)
		payload := currentHelloPayload(1)
		response, _ := native.Encode(native.Frame{
			Opcode: native.OpHelloResp, Seq: 0, Payload: payload,
		})
		for _, fragment := range [][]byte{
			response[:2],
			response[2:7],
			response[7:],
		} {
			_, _ = connection.Write(fragment)
			time.Sleep(4 * time.Millisecond)
		}
		<-time.After(100 * time.Millisecond)
	}()

	endpoint := "tcp://" + listener.Addr().String()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := OpenAuthenticated(
		ctx,
		ports.Info{Name: endpoint},
		DiscoveryOptions{
			StartupWait: 5 * time.Millisecond, RequestTimeout: 30 * time.Millisecond,
			HelloAttempts: 3,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Hello.IsPCController() {
		t.Fatalf("unexpected HELLO: %#v", result.Hello)
	}
	if err := result.Session.PulseReset(ctx, time.Millisecond); !errors.Is(err, ErrControlLinesUnsupported) {
		t.Fatalf("TCP reset got %v, want ErrControlLinesUnsupported", err)
	}
	_ = result.Session.Close()
	<-serverDone
}

func TestRejectsMalformedTCPEndpoint(t *testing.T) {
	if _, err := Open("tcp://127.0.0.1:8765/path", 0); err == nil {
		t.Fatal("expected malformed endpoint error")
	}
}
