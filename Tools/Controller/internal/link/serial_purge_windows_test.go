//go:build windows

package link

import (
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"
)

type pendingWindowsReadPort struct {
	readStarted chan struct{}
	readAborted chan struct{}
	closed      chan struct{}
	startOnce   sync.Once
	abortOnce   sync.Once
	closeOnce   sync.Once

	inputPurgeErr  error
	outputPurgeErr error
	closeErr       error
	inputPurges    int
	outputPurges   int
	handleOpen     bool
}

func newPendingWindowsReadPort() *pendingWindowsReadPort {
	return &pendingWindowsReadPort{
		readStarted: make(chan struct{}),
		readAborted: make(chan struct{}),
		closed:      make(chan struct{}),
		handleOpen:  true,
	}
}

func (port *pendingWindowsReadPort) SetMode(*serial.Mode) error { return nil }
func (port *pendingWindowsReadPort) Read([]byte) (int, error) {
	port.startOnce.Do(func() { close(port.readStarted) })
	<-port.readAborted
	return 0, errors.New("overlapped read aborted")
}
func (port *pendingWindowsReadPort) Write(data []byte) (int, error) {
	return len(data), nil
}
func (port *pendingWindowsReadPort) Drain() error { return nil }
func (port *pendingWindowsReadPort) ResetInputBuffer() error {
	port.inputPurges++
	port.abortOnce.Do(func() { close(port.readAborted) })
	return port.inputPurgeErr
}
func (port *pendingWindowsReadPort) ResetOutputBuffer() error {
	port.outputPurges++
	return port.outputPurgeErr
}
func (port *pendingWindowsReadPort) SetDTR(bool) error { return nil }
func (port *pendingWindowsReadPort) SetRTS(bool) error { return nil }
func (port *pendingWindowsReadPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (port *pendingWindowsReadPort) SetReadTimeout(time.Duration) error { return nil }
func (port *pendingWindowsReadPort) Close() error {
	// Model the live Windows driver invariant: closing cannot finish while an
	// overlapped read is pending. Purging must abort that read first.
	<-port.readAborted
	port.handleOpen = false
	port.closeOnce.Do(func() { close(port.closed) })
	return port.closeErr
}
func (port *pendingWindowsReadPort) Break(time.Duration) error { return nil }

func TestSessionClosePurgesPendingWindowsReadBeforeClosingHandle(t *testing.T) {
	port := newPendingWindowsReadPort()
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
	if port.handleOpen {
		t.Fatal("close returned while the simulated serial handle remained open")
	}
	if port.inputPurges != 1 || port.outputPurges != 1 {
		t.Fatalf("purge calls = input %d, output %d; want one each", port.inputPurges, port.outputPurges)
	}
}

func TestSessionCloseAttemptsHandleCloseAndJoinsWindowsPurgeErrors(t *testing.T) {
	inputErr := errors.New("input purge failed")
	outputErr := errors.New("output purge failed")
	closeErr := errors.New("handle close failed")
	port := newPendingWindowsReadPort()
	port.inputPurgeErr = inputErr
	port.outputPurgeErr = outputErr
	port.closeErr = closeErr
	session := NewForPort("COM3", port)
	awaitSignal(t, port.readStarted, "pending read")

	err := session.Close()
	for _, expected := range []error{inputErr, outputErr, closeErr} {
		if !errors.Is(err, expected) {
			t.Fatalf("close error %v does not include %v", err, expected)
		}
	}
	if port.handleOpen {
		t.Fatal("underlying close was not attempted after purge errors")
	}
	if repeatedErr := session.Close(); repeatedErr != err {
		t.Fatalf("repeated close error = %v; want stored result %v", repeatedErr, err)
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
