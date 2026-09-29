//go:build windows

package link

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/ports"
)

type cancelOnCloseWindowsPort struct {
	readStarted chan struct{}
	readAborted chan struct{}
	startOnce   sync.Once
	abortOnce   sync.Once

	mu            sync.Mutex
	closeCalls    int
	closeFailures int
	resetCalls    int
	handleOpen    bool
	closeErr      error
	configureErr  error
}

type readFailureRetryPort struct {
	mu            sync.Mutex
	closeCalls    int
	closeFailures int
	readErr       error
	closeErr      error
}

func (port *readFailureRetryPort) Read([]byte) (int, error)  { return 0, port.readErr }
func (*readFailureRetryPort) Write(data []byte) (int, error) { return len(data), nil }
func (*readFailureRetryPort) SetDTR(bool) error              { return nil }
func (*readFailureRetryPort) SetRTS(bool) error              { return nil }
func (port *readFailureRetryPort) Close() error {
	port.mu.Lock()
	defer port.mu.Unlock()
	port.closeCalls++
	if port.closeFailures > 0 {
		port.closeFailures--
		return port.closeErr
	}
	return nil
}

func newCancelOnCloseWindowsPort() *cancelOnCloseWindowsPort {
	return &cancelOnCloseWindowsPort{
		readStarted: make(chan struct{}),
		readAborted: make(chan struct{}),
		handleOpen:  true,
	}
}

func (*cancelOnCloseWindowsPort) SetMode(*serial.Mode) error { return nil }
func (port *cancelOnCloseWindowsPort) Read([]byte) (int, error) {
	port.startOnce.Do(func() { close(port.readStarted) })
	<-port.readAborted
	return 0, errors.New("overlapped read aborted")
}
func (*cancelOnCloseWindowsPort) Write(data []byte) (int, error) { return len(data), nil }
func (*cancelOnCloseWindowsPort) Drain() error                   { return nil }
func (port *cancelOnCloseWindowsPort) ResetInputBuffer() error {
	port.mu.Lock()
	port.resetCalls++
	port.mu.Unlock()
	return nil
}
func (port *cancelOnCloseWindowsPort) ResetOutputBuffer() error {
	port.mu.Lock()
	port.resetCalls++
	port.mu.Unlock()
	return nil
}
func (*cancelOnCloseWindowsPort) SetDTR(bool) error { return nil }
func (*cancelOnCloseWindowsPort) SetRTS(bool) error { return nil }
func (*cancelOnCloseWindowsPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (port *cancelOnCloseWindowsPort) SetReadTimeout(time.Duration) error {
	return port.configureErr
}
func (port *cancelOnCloseWindowsPort) Close() error {
	port.mu.Lock()
	port.closeCalls++
	if port.closeFailures > 0 {
		port.closeFailures--
		err := port.closeErr
		port.mu.Unlock()
		return err
	}
	port.handleOpen = false
	port.mu.Unlock()

	// Model the pinned Windows transport: a successful Close cancels the
	// pending overlapped operation before invalidating the underlying handle.
	port.abortOnce.Do(func() { close(port.readAborted) })
	return nil
}
func (*cancelOnCloseWindowsPort) Break(time.Duration) error { return nil }

func TestSessionCloseReliesOnTransportToCancelPendingWindowsRead(t *testing.T) {
	port := newCancelOnCloseWindowsPort()
	session := NewForPort("COM3", port)
	awaitSignal(t, port.readStarted, "pending read")

	closed := make(chan error, 1)
	go func() { closed <- session.Close() }()

	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("close session: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("close remained blocked behind a pending Windows read")
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if port.handleOpen {
		t.Fatal("close returned while the simulated serial handle remained open")
	}
	if port.closeCalls != 1 {
		t.Fatalf("transport close calls = %d, want 1", port.closeCalls)
	}
	if port.resetCalls != 0 {
		t.Fatalf("buffer reset calls = %d, want 0; cancellation belongs to transport Close", port.resetCalls)
	}
}

func TestSessionCloseReturnsRetryableTransportErrorWithoutWaitingForReader(t *testing.T) {
	closeErr := errors.New("CancelIoEx failed")
	port := newCancelOnCloseWindowsPort()
	port.closeErr = closeErr
	port.closeFailures = 1
	session := NewForPort("COM3", port)
	awaitSignal(t, port.readStarted, "pending read")

	firstClose := make(chan error, 1)
	go func() { firstClose <- session.Close() }()
	select {
	case err := <-firstClose:
		if !errors.Is(err, closeErr) {
			t.Fatalf("first close error = %v, want %v", err, closeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("first close waited for the pending reader after transport cancellation failed")
	}
	select {
	case <-port.readAborted:
		t.Fatal("failed transport close aborted the pending reader")
	default:
	}
	select {
	case <-session.Done():
		t.Fatal("session reported terminal Done after retryable transport failure")
	default:
	}
	if err := session.WriteRaw([]byte{0x01}); !errors.Is(err, ErrClosed) {
		t.Fatalf("write while close retry is pending = %v, want ErrClosed", err)
	}

	port.mu.Lock()
	if !port.handleOpen || port.closeCalls != 1 {
		t.Fatalf("after first close: handleOpen=%v calls=%d, want true/1", port.handleOpen, port.closeCalls)
	}
	port.mu.Unlock()

	secondClose := make(chan error, 1)
	go func() { secondClose <- session.Close() }()
	select {
	case err := <-secondClose:
		if err != nil {
			t.Fatalf("retry close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry close did not finish after transport cancellation succeeded")
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("session did not report terminal Done after successful retry")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("repeated close after successful retry: %v", err)
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if port.handleOpen || port.closeCalls != 2 {
		t.Fatalf("after retry: handleOpen=%v calls=%d, want false/2", port.handleOpen, port.closeCalls)
	}
}

func TestSessionCloseConcurrentCallersShareSuccessfulResult(t *testing.T) {
	port := newCancelOnCloseWindowsPort()
	session := NewForPort("COM3", port)
	awaitSignal(t, port.readStarted, "pending read")

	const callers = 8
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- session.Close()
		}()
	}
	close(start)
	for range callers {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("concurrent close: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent close caller did not finish")
		}
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if port.handleOpen || port.closeCalls != 1 {
		t.Fatalf("after concurrent close: handleOpen=%v calls=%d, want false/1", port.handleOpen, port.closeCalls)
	}
}

func TestReadLoopPublishesRetryableTransportCloseFailure(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("CancelIoEx failed")
	port := &readFailureRetryPort{
		closeFailures: 1,
		readErr:       readErr,
		closeErr:      closeErr,
	}
	session := newForTransport("COM3", port)

	var closeFailure Event
	deadline := time.After(time.Second)
	for !closeFailure.CloseFailure {
		select {
		case event := <-session.Events():
			if event.CloseFailure {
				closeFailure = event
			}
		case <-deadline:
			t.Fatal("read loop did not publish the retryable transport close failure")
		}
	}
	if !errors.Is(closeFailure.Err, closeErr) {
		t.Fatalf("published close failure = %v, want %v", closeFailure.Err, closeErr)
	}
	select {
	case <-session.Done():
		t.Fatal("read-loop close failure reported terminal Done")
	default:
	}
	if err := session.Close(); err != nil {
		t.Fatalf("retry close after read-loop failure: %v", err)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("successful retry after read-loop failure did not report Done")
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.closeCalls != 2 {
		t.Fatalf("transport close calls = %d, want failed read-loop attempt plus retry", port.closeCalls)
	}
}

func TestOpenContextReturnsCloseOwnerWhenConfigurationCleanupFails(t *testing.T) {
	configureErr := errors.New("configure failed")
	closeErr := errors.New("CancelIoEx failed")
	port := newCancelOnCloseWindowsPort()
	port.configureErr = configureErr
	port.closeErr = closeErr
	port.closeFailures = 1
	originalOpen := openSerialPort
	openSerialPort = func(string, *serial.Mode) (serial.Port, error) { return port, nil }
	defer func() { openSerialPort = originalOpen }()

	session, err := OpenContext(context.Background(), "COM3", DefaultBaudRate)
	if session == nil || !errors.Is(err, configureErr) || !errors.Is(err, closeErr) {
		t.Fatalf("OpenContext session=%p error=%v, want retained owner and joined errors", session, err)
	}
	if retryErr := session.Close(); retryErr != nil {
		t.Fatalf("retry configuration cleanup: %v", retryErr)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.handleOpen || port.closeCalls != 2 {
		t.Fatalf("configuration cleanup: handleOpen=%v calls=%d, want false/2", port.handleOpen, port.closeCalls)
	}
}

func TestOpenAuthenticatedReturnsSessionWhenAuthenticationCleanupFails(t *testing.T) {
	closeErr := errors.New("CancelIoEx failed")
	port := newCancelOnCloseWindowsPort()
	port.closeErr = closeErr
	port.closeFailures = 1
	session := NewForPort("COM3", port)
	originalOpen := openSessionContext
	openSessionContext = func(context.Context, string, int) (*Session, error) {
		return session, nil
	}
	defer func() { openSessionContext = originalOpen }()

	result, err := OpenAuthenticated(context.Background(), ports.Info{Name: "COM3"}, DiscoveryOptions{
		HelloAttempts:  1,
		RequestTimeout: 10 * time.Millisecond,
	})
	if result.Session != session || !errors.Is(err, closeErr) {
		t.Fatalf("OpenAuthenticated session=%p error=%v, want retained %p and close error", result.Session, err, session)
	}
	if retryErr := result.Session.Close(); retryErr != nil {
		t.Fatalf("retry authentication cleanup: %v", retryErr)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if port.handleOpen || port.closeCalls != 2 {
		t.Fatalf("authentication cleanup: handleOpen=%v calls=%d, want false/2", port.handleOpen, port.closeCalls)
	}
}

func awaitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
