package link

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type blockingAuthenticationWritePort struct {
	*fakePort

	writeStarted  chan struct{}
	writeOnce     sync.Once
	closeMu       sync.Mutex
	closeCalls    int
	closeFailures int
	closeErr      error
}

func newBlockingAuthenticationWritePort() *blockingAuthenticationWritePort {
	return &blockingAuthenticationWritePort{
		fakePort:     newFakePort(),
		writeStarted: make(chan struct{}),
	}
}

func (port *blockingAuthenticationWritePort) Write([]byte) (int, error) {
	port.writeOnce.Do(func() { close(port.writeStarted) })
	<-port.closed
	return 0, errors.New("serial write canceled by close")
}

func (port *blockingAuthenticationWritePort) Close() error {
	port.closeMu.Lock()
	port.closeCalls++
	if port.closeFailures > 0 {
		port.closeFailures--
		err := port.closeErr
		port.closeMu.Unlock()
		return err
	}
	port.closeMu.Unlock()
	return port.fakePort.Close()
}

func TestOpenAuthenticatedCancellationClosesBlockedWrite(t *testing.T) {
	port := newBlockingAuthenticationWritePort()
	session := NewForPort("COM3", port)
	originalOpen := openSessionContext
	openSessionContext = func(context.Context, string, int) (*Session, error) {
		return session, nil
	}
	t.Cleanup(func() { openSessionContext = originalOpen })

	ctx, cancel := context.WithCancel(context.Background())
	type openOutcome struct {
		result OpenResult
		err    error
	}
	opened := make(chan openOutcome, 1)
	go func() {
		result, err := OpenAuthenticated(ctx, ports.Info{Name: "COM3"}, DiscoveryOptions{
			HelloAttempts:  1,
			RequestTimeout: time.Second,
		})
		opened <- openOutcome{result: result, err: err}
	}()

	awaitDiscoverySignal(t, port.writeStarted, "blocked authentication write")
	cancel()
	select {
	case outcome := <-opened:
		if outcome.result.Session != nil || !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("OpenAuthenticated result=%+v error=%v, want canceled and fully closed", outcome.result, outcome.err)
		}
	case <-time.After(time.Second):
		t.Fatal("OpenAuthenticated did not close and join the blocked authentication write")
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("canceled authentication returned before Session.Close completed")
	}
	port.closeMu.Lock()
	defer port.closeMu.Unlock()
	if port.closeCalls != 1 {
		t.Fatalf("transport close calls = %d, want 1", port.closeCalls)
	}
}

func TestOpenAuthenticatedCancellationReturnsRetryableCloseOwner(t *testing.T) {
	closeErr := errors.New("CancelIoEx failed")
	port := newBlockingAuthenticationWritePort()
	port.closeFailures = 1
	port.closeErr = closeErr
	session := NewForPort("COM3", port)
	originalOpen := openSessionContext
	openSessionContext = func(context.Context, string, int) (*Session, error) {
		return session, nil
	}
	t.Cleanup(func() { openSessionContext = originalOpen })

	ctx, cancel := context.WithCancel(context.Background())
	type openOutcome struct {
		result OpenResult
		err    error
	}
	opened := make(chan openOutcome, 1)
	go func() {
		result, err := OpenAuthenticated(ctx, ports.Info{Name: "COM3"}, DiscoveryOptions{
			HelloAttempts:  1,
			RequestTimeout: time.Second,
		})
		opened <- openOutcome{result: result, err: err}
	}()

	awaitDiscoverySignal(t, port.writeStarted, "blocked authentication write")
	cancel()
	var outcome openOutcome
	select {
	case outcome = <-opened:
	case <-time.After(time.Second):
		t.Fatal("OpenAuthenticated did not return the retryable close owner")
	}
	if outcome.result.Session != session ||
		!errors.Is(outcome.err, context.Canceled) ||
		!errors.Is(outcome.err, closeErr) {
		t.Fatalf("OpenAuthenticated session=%p error=%v, want retained %p with cancellation and close errors", outcome.result.Session, outcome.err, session)
	}
	if err := outcome.result.Session.Close(); err != nil {
		t.Fatalf("retry retained authentication close: %v", err)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("successful retry did not finish the retained Session")
	}
	port.closeMu.Lock()
	defer port.closeMu.Unlock()
	if port.closeCalls != 2 {
		t.Fatalf("transport close calls = %d, want failed cancellation plus retry", port.closeCalls)
	}
}

func TestOpenAuthenticatedSuccessfulReturnStopsCancellationClose(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode HELLO: %v", err)
			return
		}
		response, err := native.Encode(native.Frame{
			Opcode:  native.OpHelloResp,
			Seq:     request.Seq,
			Payload: currentHelloPayload(1),
		})
		if err != nil {
			t.Errorf("encode HELLO: %v", err)
			return
		}
		port.reads <- response
	}
	session := NewForPort("COM3", port)
	originalOpen := openSessionContext
	openSessionContext = func(context.Context, string, int) (*Session, error) {
		return session, nil
	}
	t.Cleanup(func() { openSessionContext = originalOpen })

	ctx, cancel := context.WithCancel(context.Background())
	result, err := OpenAuthenticated(ctx, ports.Info{Name: "COM3"}, DiscoveryOptions{
		HelloAttempts:  1,
		RequestTimeout: time.Second,
	})
	if err != nil || result.Session != session {
		t.Fatalf("OpenAuthenticated result=%+v error=%v, want accepted Session", result, err)
	}
	cancel()
	select {
	case <-session.Done():
		t.Fatal("context cancellation after successful return closed the accepted Session")
	case <-time.After(25 * time.Millisecond):
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close accepted Session: %v", err)
	}
}

func TestOpenAuthenticatedSuccessCancelRaceHasSingleOwner(t *testing.T) {
	originalOpen := openSessionContext
	t.Cleanup(func() { openSessionContext = originalOpen })

	for iteration := 0; iteration < 100; iteration++ {
		port := newFakePort()
		responseSent := make(chan struct{})
		var responseOnce sync.Once
		port.onWrite = func(encoded []byte) {
			request, err := native.Decode(encoded)
			if err != nil {
				t.Errorf("iteration %d decode HELLO: %v", iteration, err)
				return
			}
			response, err := native.Encode(native.Frame{
				Opcode:  native.OpHelloResp,
				Seq:     request.Seq,
				Payload: currentHelloPayload(1),
			})
			if err != nil {
				t.Errorf("iteration %d encode HELLO: %v", iteration, err)
				return
			}
			port.reads <- response
			responseOnce.Do(func() { close(responseSent) })
		}
		session := NewForPort("COM3", port)
		openSessionContext = func(context.Context, string, int) (*Session, error) {
			return session, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-responseSent
			cancel()
		}()

		result, err := OpenAuthenticated(ctx, ports.Info{Name: "COM3"}, DiscoveryOptions{
			HelloAttempts:  1,
			RequestTimeout: time.Second,
		})
		cancel()
		if err == nil {
			if result.Session != session {
				t.Fatalf("iteration %d successful Session=%p, want %p", iteration, result.Session, session)
			}
			select {
			case <-session.Done():
				t.Fatalf("iteration %d late cancellation closed an accepted Session", iteration)
			default:
			}
			if closeErr := session.Close(); closeErr != nil {
				t.Fatalf("iteration %d close successful Session: %v", iteration, closeErr)
			}
			continue
		}
		if !errors.Is(err, context.Canceled) || result.Session != nil {
			t.Fatalf("iteration %d result=%+v error=%v, want either accepted Session or completed cancellation", iteration, result, err)
		}
		select {
		case <-session.Done():
		default:
			t.Fatalf("iteration %d canceled result returned before Session.Close", iteration)
		}
	}
}

func awaitDiscoverySignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
