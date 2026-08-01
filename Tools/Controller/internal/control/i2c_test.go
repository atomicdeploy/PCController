package control

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

type i2cProtocolPort struct {
	cap16  bool
	short  bool
	reads  chan []byte
	closed chan struct{}
	once   sync.Once
}

func newI2CProtocolPort(cap16 bool) *i2cProtocolPort {
	return &i2cProtocolPort{
		cap16: cap16, reads: make(chan []byte, 8), closed: make(chan struct{}),
	}
}

func (*i2cProtocolPort) SetMode(*serial.Mode) error { return nil }
func (port *i2cProtocolPort) Read(buffer []byte) (int, error) {
	select {
	case data := <-port.reads:
		return copy(buffer, data), nil
	case <-port.closed:
		return 0, io.EOF
	}
}
func (port *i2cProtocolPort) Write(data []byte) (int, error) {
	var decoder native.Decoder
	frames, failures := decoder.Feed(data)
	if len(failures) != 0 || len(frames) != 1 {
		return 0, io.ErrUnexpectedEOF
	}
	request := frames[0]
	response := native.Frame{Seq: request.Seq}
	if !port.cap16 {
		response.Opcode = native.OpI2CResult
		response.Payload = []byte{3, 0x27, 0x40, 0x41}
	} else if len(request.Payload) >= 4 && request.Payload[0] == 0 {
		response.Opcode = native.OpACK
		response.Payload = []byte{native.OpI2CTransfer, 0}
	} else {
		address := request.Payload[0]
		status := byte(2)
		if address == 0x27 || address == 0x40 || address == 0x41 {
			status = 0
		}
		count := byte(0)
		if status == 0 && len(request.Payload) >= 4 {
			count = request.Payload[3]
			if port.short && count != 0 {
				count--
			}
		}
		response.Opcode = native.OpI2CTransferResp
		response.Payload = []byte{status, address, count}
		for index := byte(0); index < count; index++ {
			response.Payload = append(response.Payload, index)
		}
	}
	encoded, err := native.Encode(response)
	if err != nil {
		return 0, err
	}
	select {
	case port.reads <- encoded:
		return len(data), nil
	case <-port.closed:
		return 0, io.EOF
	}
}
func (*i2cProtocolPort) Drain() error             { return nil }
func (*i2cProtocolPort) ResetInputBuffer() error  { return nil }
func (*i2cProtocolPort) ResetOutputBuffer() error { return nil }
func (*i2cProtocolPort) SetDTR(bool) error        { return nil }
func (*i2cProtocolPort) SetRTS(bool) error        { return nil }
func (*i2cProtocolPort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (*i2cProtocolPort) SetReadTimeout(time.Duration) error { return nil }
func (port *i2cProtocolPort) Close() error {
	port.once.Do(func() { close(port.closed) })
	return nil
}
func (*i2cProtocolPort) Break(time.Duration) error { return nil }

func i2cTestRuntime(t *testing.T, cap16 bool) (*Runtime, *i2cProtocolPort) {
	t.Helper()
	port := newI2CProtocolPort(cap16)
	session := link.NewForPort("i2c-test", port)
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = session
	runtime.connectionState = "connected"
	if cap16 {
		runtime.hello.Capabilities = native.CapabilityI2CTransfer
	}
	runtime.mu.Unlock()
	t.Cleanup(func() { _ = session.Close() })
	return runtime, port
}

func TestScanI2CPreservesLegacyAndCap16Contracts(t *testing.T) {
	for _, cap16 := range []bool{false, true} {
		runtime, _ := i2cTestRuntime(t, cap16)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		addresses, err := ScanI2C(ctx, runtime)
		cancel()
		if err != nil {
			t.Fatalf("cap16=%t: %v", cap16, err)
		}
		want := []byte{0x27, 0x40, 0x41}
		if string(addresses) != string(want) {
			t.Fatalf("cap16=%t addresses=% X want=% X", cap16, addresses, want)
		}
	}
}

func TestTransferI2CRejectsShortSuccessfulRead(t *testing.T) {
	runtime, port := i2cTestRuntime(t, true)
	port.short = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := TransferI2C(ctx, runtime, 0x40, 0, nil, 4); err == nil {
		t.Fatal("accepted a short read as a successful transaction")
	}
}
