package control

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

// programStateWirePort acknowledges every valid command and records its exact
// decoded frame, providing deterministic wire coverage without real hardware.
type programStateWirePort struct {
	reads  chan []byte
	writes chan native.Frame
	closed chan struct{}
	once   sync.Once
}

func newProgramStateWirePort() *programStateWirePort {
	return &programStateWirePort{
		reads: make(chan []byte, 8), writes: make(chan native.Frame, 8),
		closed: make(chan struct{}),
	}
}

func (*programStateWirePort) SetMode(*serial.Mode) error { return nil }
func (port *programStateWirePort) Read(data []byte) (int, error) {
	select {
	case encoded := <-port.reads:
		return copy(data, encoded), nil
	case <-port.closed:
		return 0, errors.New("closed")
	}
}
func (port *programStateWirePort) Write(data []byte) (int, error) {
	frame, err := native.Decode(data)
	if err != nil {
		return 0, err
	}
	port.writes <- frame
	ack, err := native.Encode(native.Frame{
		Opcode: native.OpACK, Seq: frame.Seq, Payload: []byte{frame.Opcode, 0},
	})
	if err != nil {
		return 0, err
	}
	port.reads <- ack
	return len(data), nil
}
func (*programStateWirePort) Drain() error             { return nil }
func (*programStateWirePort) ResetInputBuffer() error  { return nil }
func (*programStateWirePort) ResetOutputBuffer() error { return nil }
func (*programStateWirePort) SetDTR(bool) error        { return nil }
func (*programStateWirePort) SetRTS(bool) error        { return nil }
func (*programStateWirePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*programStateWirePort) SetReadTimeout(time.Duration) error { return nil }
func (port *programStateWirePort) Close() error {
	port.once.Do(func() { close(port.closed) })
	return nil
}
func (*programStateWirePort) Break(time.Duration) error { return nil }

func TestProgramStateReassertsAfterAuthenticatedAttachAndEveryChange(t *testing.T) {
	if programStateHeartbeatPeriod >= 5*time.Second {
		t.Fatalf("program-state heartbeat %v cannot precede the firmware host-offline threshold", programStateHeartbeatPeriod)
	}
	runtime := New(Options{RequestTimeout: time.Second})
	if _, err := runtime.SetProgramState("test", ProgramRunning, "active"); err != nil {
		t.Fatal(err)
	}
	port := newProgramStateWirePort()
	runtime.attach(link.OpenResult{
		Session: link.NewForPort("PROGRAM-STATE", port),
		Port:    ports.Info{Name: "PROGRAM-STATE"},
		Hello: native.Hello{
			Name: "PCController", Capabilities: native.CapabilityProgramState,
		},
	})
	assertProgramStateFrame(t, port, 1)

	if _, err := runtime.SetProgramState("test", ProgramIdle, "done"); err != nil {
		t.Fatal(err)
	}
	assertProgramStateFrame(t, port, 0)
	_ = runtime.Close()
}

func TestProgramStateDoesNotProbeFirmwareWithoutCapability(t *testing.T) {
	runtime := New(Options{RequestTimeout: 50 * time.Millisecond})
	port := newProgramStateWirePort()
	runtime.attach(link.OpenResult{
		Session: link.NewForPort("LEGACY", port),
		Port:    ports.Info{Name: "LEGACY"},
		Hello:   native.Hello{Name: "PCController"},
	})
	if _, err := runtime.SetProgramState("test", ProgramRunning, "active"); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-port.writes:
		t.Fatalf("legacy firmware received unexpected frame: %#v", frame)
	case <-time.After(75 * time.Millisecond):
	}
	_ = runtime.Close()
}

func assertProgramStateFrame(t *testing.T, port *programStateWirePort, value byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case frame := <-port.writes:
		if frame.Opcode != native.OpProgramState || len(frame.Payload) != 1 || frame.Payload[0] != value {
			t.Fatalf("PROGRAM_STATE frame=%#v, want payload [%d]", frame, value)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for PROGRAM_STATE frame")
	}
}
