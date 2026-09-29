package control

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

// relayConfirmationPort deliberately withholds the changed-only relay event.
// The host must still confirm and publish the applied state from STATUS before
// a general-relay command returns success.
type relayConfirmationPort struct {
	mu          sync.Mutex
	reads       chan []byte
	closed      chan struct{}
	active      byte
	applyRelay  bool
	statusReads int
}

func newRelayConfirmationPort(applyRelay bool) *relayConfirmationPort {
	return &relayConfirmationPort{
		reads: make(chan []byte, 4), closed: make(chan struct{}),
		applyRelay: applyRelay,
	}
}

func (*relayConfirmationPort) SetMode(*serial.Mode) error { return nil }
func (port *relayConfirmationPort) Read(destination []byte) (int, error) {
	select {
	case data := <-port.reads:
		return copy(destination, data), nil
	case <-port.closed:
		return 0, errors.New("relay confirmation port closed")
	}
}
func (port *relayConfirmationPort) Write(encoded []byte) (int, error) {
	request, err := native.Decode(encoded)
	if err != nil {
		return 0, err
	}
	port.mu.Lock()
	var response native.Frame
	switch request.Opcode {
	case native.OpRelaySet:
		if port.applyRelay && len(request.Payload) == 2 {
			bit := byte(1 << request.Payload[0])
			if request.Payload[1] != 0 {
				port.active |= bit
			} else {
				port.active &^= bit
			}
		}
		response = native.Frame{
			Opcode: native.OpACK, Seq: request.Seq,
			Payload: []byte{request.Opcode, 0},
		}
	case native.OpGetStatus:
		port.statusReads++
		payload := make([]byte, native.StatusPayloadSize)
		payload[28] = port.active
		response = native.Frame{Opcode: native.OpStatus, Seq: request.Seq, Payload: payload}
	default:
		port.mu.Unlock()
		return 0, errors.New("unexpected relay confirmation opcode")
	}
	port.mu.Unlock()
	responseBytes, err := native.Encode(response)
	if err != nil {
		return 0, err
	}
	port.reads <- responseBytes
	return len(encoded), nil
}
func (*relayConfirmationPort) Drain() error             { return nil }
func (*relayConfirmationPort) ResetInputBuffer() error  { return nil }
func (*relayConfirmationPort) ResetOutputBuffer() error { return nil }
func (*relayConfirmationPort) SetDTR(bool) error        { return nil }
func (*relayConfirmationPort) SetRTS(bool) error        { return nil }
func (*relayConfirmationPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*relayConfirmationPort) SetReadTimeout(time.Duration) error { return nil }
func (port *relayConfirmationPort) Close() error {
	select {
	case <-port.closed:
	default:
		close(port.closed)
	}
	return nil
}
func (*relayConfirmationPort) Break(time.Duration) error { return nil }

func runtimeForRelayConfirmationTest(t *testing.T, applyRelay bool) (*Runtime, *relayConfirmationPort) {
	t.Helper()
	port := newRelayConfirmationPort(applyRelay)
	session := link.NewForPort("TEST", port)
	t.Cleanup(func() { _ = session.Close() })
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = session
	runtime.generation = 1
	runtime.connectionState = "connected"
	runtime.mu.Unlock()
	return runtime, port
}

func TestSetGeneralRelayConfirmsStatusWithoutRelayEvent(t *testing.T) {
	runtime, port := runtimeForRelayConfirmationTest(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.SetRelay(ctx, 4, true); err != nil {
		t.Fatal(err)
	}
	if snapshot := runtime.Snapshot(); snapshot.Status.ActiveRelays != 0x10 {
		t.Fatalf("active relay mask = 0x%02X, want 0x10", snapshot.Status.ActiveRelays)
	}
	port.mu.Lock()
	statusReads := port.statusReads
	port.mu.Unlock()
	if statusReads != 1 {
		t.Fatalf("STATUS readbacks = %d, want 1", statusReads)
	}
}

func TestSetGeneralRelayRejectsAcknowledgedReadbackMismatch(t *testing.T) {
	runtime, _ := runtimeForRelayConfirmationTest(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := runtime.SetRelay(ctx, 4, true)
	if err == nil || !strings.Contains(err.Error(), "acknowledged") ||
		!strings.Contains(err.Error(), "readback mask 0x00") {
		t.Fatalf("SetRelay error = %v, want acknowledged/readback mismatch", err)
	}
}

func TestSetMotionRelayKeepsSequencerAcknowledgementSemantics(t *testing.T) {
	runtime, port := runtimeForRelayConfirmationTest(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.SetRelay(ctx, 0, true); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	statusReads := port.statusReads
	port.mu.Unlock()
	if statusReads != 0 {
		t.Fatalf("motion relay triggered %d immediate STATUS readbacks", statusReads)
	}
}
