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
	closeOnce   sync.Once

	mu         sync.Mutex
	closeCalls int
	resetCalls int
	handleOpen bool
	closeErr   error
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
	port.closeOnce.Do(func() {
		// Model the pinned Windows transport: Close itself cancels the pending
		// overlapped operation before invalidating the underlying handle.
		port.abortOnce.Do(func() { close(port.readAborted) })
		port.mu.Lock()
		port.closeCalls++
		port.handleOpen = false
		port.mu.Unlock()
	})
	return port.closeErr
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

func TestSessionCloseRetainsTransportErrorForRepeatedCallers(t *testing.T) {
	closeErr := errors.New("handle close failed")
	port := newCancelOnCloseWindowsPort()
	port.closeErr = closeErr
	session := NewForPort("COM3", port)
	awaitSignal(t, port.readStarted, "pending read")

	firstErr := session.Close()
	if !errors.Is(firstErr, closeErr) {
		t.Fatalf("close error = %v, want %v", firstErr, closeErr)
	}
	if repeatedErr := session.Close(); repeatedErr != firstErr {
		t.Fatalf("repeated close error = %v, want stored result %v", repeatedErr, firstErr)
	}

	port.mu.Lock()
	defer port.mu.Unlock()
	if port.closeCalls != 1 {
		t.Fatalf("transport close calls = %d, want 1", port.closeCalls)
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
