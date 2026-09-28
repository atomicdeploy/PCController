//go:build windows

package link

import (
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
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
func (*cancelOnCloseWindowsPort) SetReadTimeout(time.Duration) error { return nil }
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

func awaitSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
