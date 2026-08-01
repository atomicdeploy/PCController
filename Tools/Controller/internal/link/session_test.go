package link

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
)

type fakePort struct {
	mu      sync.Mutex
	reads   chan []byte
	closed  chan struct{}
	onWrite func([]byte)
	dtr     []bool
	rts     []bool
}

func newFakePort() *fakePort {
	return &fakePort{reads: make(chan []byte, 8), closed: make(chan struct{})}
}

func (port *fakePort) SetMode(*serial.Mode) error { return nil }
func (port *fakePort) Read(dst []byte) (int, error) {
	select {
	case data := <-port.reads:
		return copy(dst, data), nil
	case <-port.closed:
		return 0, errors.New("closed")
	case <-time.After(5 * time.Millisecond):
		return 0, nil
	}
}
func (port *fakePort) Write(data []byte) (int, error) {
	if port.onWrite != nil {
		port.onWrite(append([]byte(nil), data...))
	}
	return len(data), nil
}
func (port *fakePort) Drain() error             { return nil }
func (port *fakePort) ResetInputBuffer() error  { return nil }
func (port *fakePort) ResetOutputBuffer() error { return nil }
func (port *fakePort) SetDTR(value bool) error {
	port.mu.Lock()
	port.dtr = append(port.dtr, value)
	port.mu.Unlock()
	return nil
}
func (port *fakePort) SetRTS(value bool) error {
	port.mu.Lock()
	port.rts = append(port.rts, value)
	port.mu.Unlock()
	return nil
}
func (port *fakePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (port *fakePort) SetReadTimeout(time.Duration) error { return nil }
func (port *fakePort) Close() error {
	select {
	case <-port.closed:
	default:
		close(port.closed)
	}
	return nil
}
func (port *fakePort) Break(time.Duration) error { return nil }

func TestAuthenticateRequiresPCControllerIdentity(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		name := []byte("PCController")
		payload := []byte{1, 2, 3, native.BoardKindPCController, 1, 0, 0, 0, byte(len(name))}
		payload = append(payload, name...)
		response, err := native.Encode(native.Frame{
			Opcode:  native.OpHelloResp,
			Seq:     request.Seq,
			Payload: payload,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
		port.reads <- response
	}

	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hello, err := session.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hello.Name != "PCController" || hello.FirmwareMinor != 2 {
		t.Fatalf("unexpected identity: %#v", hello)
	}
}

func TestAuthenticateAcceptsCompactHelloSchema3(t *testing.T) {
	payload := []byte{
		0x03, native.BoardKindPCController, 0x00, 0x00, 0x00, 0x00,
		0x1C, 0xF8, 0xD9, 0x2F, 0x5D, 0x9D, 0x01, 0x35,
	}
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request.Opcode != native.OpHello {
			t.Errorf("request opcode=0x%02X, want HELLO", request.Opcode)
			return
		}
		response, err := native.Encode(native.Frame{
			Opcode: native.OpHelloResp, Seq: request.Seq, Payload: payload,
		})
		if err != nil {
			t.Errorf("encode response: %v", err)
			return
		}
		port.reads <- response
	}

	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hello, err := session.Authenticate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.IsPCController() || hello.IdentitySchema != native.IdentitySchemaCompact ||
		hello.BuildHash != 0x2FD9F81C || hello.BuildStamp != "260801194258" {
		t.Fatalf("unexpected compact identity: %#v", hello)
	}
}

func TestReserveSequenceSkipsMacroExecutionSequence(t *testing.T) {
	port := newFakePort()
	session := NewForPort("TEST", port)
	defer session.Close()
	session.nextSeq = native.MacroExecutionSequence
	sequence, waiter, err := session.reserveSequence(native.OpGetStatus, []byte{native.OpStatus})
	if err != nil {
		t.Fatal(err)
	}
	defer session.releaseSequence(sequence, waiter)
	if sequence == native.MacroExecutionSequence || sequence != 0xFF {
		t.Fatalf("reserved sequence 0x%02X; expected 0xFF after skipping 0xFE", sequence)
	}
}

func TestAuthenticateRetryAcceptsDelayedUnsolicitedBootHello(t *testing.T) {
	port := newFakePort()
	session := NewForPort("TEST", port)
	defer session.Close()

	go func() {
		time.Sleep(35 * time.Millisecond)
		name := []byte("PCController")
		payload := []byte{0, 0, 0, native.BoardKindPCController, 1, 0, 0, 0, byte(len(name))}
		payload = append(payload, name...)
		response, err := native.Encode(native.Frame{
			Opcode:  native.OpHelloResp,
			Seq:     0,
			Payload: payload,
		})
		if err == nil {
			port.reads <- response
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	hello, err := session.AuthenticateWithRetry(ctx, 3, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !hello.IsPCController() {
		t.Fatalf("unexpected identity: %#v", hello)
	}
}

func TestAuthenticateRetryResendsAfterBootloaderDropsFirstRequest(t *testing.T) {
	port := newFakePort()
	writes := 0
	port.onWrite = func(encoded []byte) {
		writes++
		if writes == 1 {
			return
		}
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		name := []byte("PCController")
		payload := []byte{0, 0, 0, native.BoardKindPCController, 1, 0, 0, 0, byte(len(name))}
		payload = append(payload, name...)
		response, _ := native.Encode(native.Frame{
			Opcode:  native.OpHelloResp,
			Seq:     request.Seq,
			Payload: payload,
		})
		port.reads <- response
	}
	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := session.AuthenticateWithRetry(ctx, 3, 25*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if writes != 2 {
		t.Fatalf("got %d HELLO writes, want 2", writes)
	}
}

func TestReconnectResetIsOneShotAndHelloRetriesDoNotRepulse(t *testing.T) {
	resetPermit := true
	hookCalls := 0
	resetHook := func(ports.Info) bool {
		hookCalls++
		if !resetPermit {
			return false
		}
		resetPermit = false
		return true
	}
	makeSession := func(dropFirst bool) (*Session, *fakePort) {
		port := newFakePort()
		writes := 0
		port.onWrite = func(encoded []byte) {
			writes++
			if dropFirst && writes == 1 {
				return
			}
			request, err := native.Decode(encoded)
			if err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			name := []byte("PCController")
			payload := []byte{0, 0, 0, native.BoardKindPCController, 1, 0, 0, 0, byte(len(name))}
			payload = append(payload, name...)
			response, _ := native.Encode(native.Frame{
				Opcode: native.OpHelloResp, Seq: request.Seq, Payload: payload,
			})
			port.reads <- response
		}
		return NewForPort("TEST", port), port
	}

	session, firstPort := makeSession(true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := authenticateOpened(ctx, session, ports.Info{Name: "COM18"}, DiscoveryOptions{
		HelloAttempts: 3, RequestTimeout: 20 * time.Millisecond,
		ResetAfterOpen: resetHook, ResetPulse: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	firstPort.mu.Lock()
	if len(firstPort.dtr) != 2 {
		t.Fatalf("DTR toggled %d times during HELLO retries, want one pulse", len(firstPort.dtr))
	}
	if len(firstPort.rts) != 0 {
		t.Fatalf("reconnect pulse unexpectedly touched RTS: %#v", firstPort.rts)
	}
	firstPort.mu.Unlock()

	// A second transport open in the same reconnect epoch (for example after
	// authentication failure) must not pulse again.
	session, secondPort := makeSession(false)
	_, err = authenticateOpened(ctx, session, ports.Info{Name: "COM18"}, DiscoveryOptions{
		HelloAttempts: 3, RequestTimeout: 20 * time.Millisecond,
		ResetAfterOpen: resetHook, ResetPulse: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	secondPort.mu.Lock()
	if len(secondPort.dtr) != 0 {
		t.Fatalf("second open repeated DTR pulse: %#v", secondPort.dtr)
	}
	secondPort.mu.Unlock()
	if hookCalls != 2 {
		t.Fatalf("reset hook calls=%d, want once per physical open", hookCalls)
	}
}

func TestPulseResetUsesOnlyDTRAndLeavesItInactive(t *testing.T) {
	port := newFakePort()
	session := NewForPort("TEST", port)
	defer session.Close()
	if err := session.PulseReset(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.dtr) != 2 || !port.dtr[0] || port.dtr[1] {
		t.Fatalf("DTR sequence: %#v", port.dtr)
	}
	if len(port.rts) != 0 {
		t.Fatalf("CH340-safe reset unexpectedly touched RTS: %#v", port.rts)
	}
}

func TestSerialModeStartsControlLinesInactive(t *testing.T) {
	mode := serialMode(115200)
	if mode.InitialStatusBits == nil {
		t.Fatal("initial modem output bits are nil; library defaults would reset Arduino")
	}
	if mode.InitialStatusBits.DTR || mode.InitialStatusBits.RTS {
		t.Fatalf("control lines start asserted: %#v", mode.InitialStatusBits)
	}
}

func TestRequestRoutesUnexpectedSameSequenceFrameToEvents(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		status, err := native.Encode(native.Frame{
			Opcode:  native.OpStatus,
			Seq:     request.Seq,
			Payload: make([]byte, native.StatusPayloadSize),
		})
		if err != nil {
			t.Errorf("encode status: %v", err)
			return
		}
		ack, err := native.Encode(native.Frame{
			Opcode:  native.OpACK,
			Seq:     request.Seq,
			Payload: []byte{request.Opcode, 0},
		})
		if err != nil {
			t.Errorf("encode ACK: %v", err)
			return
		}
		port.reads <- append(status, ack...)
	}

	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Command(ctx, native.OpPWMAllOff, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-session.Events():
		if event.Frame.Opcode != native.OpStatus {
			t.Fatalf("unexpected event opcode: %s", native.OpcodeName(event.Frame.Opcode))
		}
	case <-ctx.Done():
		t.Fatal("unsolicited status frame was not published")
	}
}

func TestRequestRoutesMismatchedErrorOpcodeToEvents(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		wrongError, err := native.Encode(native.Frame{
			Opcode: native.OpError,
			Seq:    request.Seq,
			Payload: []byte{
				native.OpRelaySet,
				3,
			},
		})
		if err != nil {
			t.Errorf("encode mismatched ERROR: %v", err)
			return
		}
		ack, err := native.Encode(native.Frame{
			Opcode:  native.OpACK,
			Seq:     request.Seq,
			Payload: []byte{request.Opcode, 0},
		})
		if err != nil {
			t.Errorf("encode ACK: %v", err)
			return
		}
		port.reads <- append(wrongError, ack...)
	}

	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Command(ctx, native.OpPWMAllOff, nil); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-session.Events():
		if event.Frame.Opcode != native.OpError ||
			len(event.Frame.Payload) < 1 ||
			event.Frame.Payload[0] != native.OpRelaySet {
			t.Fatalf("unexpected event: %#v", event.Frame)
		}
	case <-ctx.Done():
		t.Fatal("mismatched ERROR frame was not published")
	}
}

func TestRequestAcceptsMatchingErrorOpcode(t *testing.T) {
	port := newFakePort()
	port.onWrite = func(encoded []byte) {
		request, err := native.Decode(encoded)
		if err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		response, err := native.Encode(native.Frame{
			Opcode:  native.OpError,
			Seq:     request.Seq,
			Payload: []byte{request.Opcode, 4},
		})
		if err != nil {
			t.Errorf("encode ERROR: %v", err)
			return
		}
		port.reads <- response
	}

	session := NewForPort("TEST", port)
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := session.Command(ctx, native.OpPWMAllOff, nil)
	var remoteError *RemoteError
	if !errors.As(err, &remoteError) {
		t.Fatalf("got %v, want RemoteError", err)
	}
	if remoteError.RequestOpcode != native.OpPWMAllOff ||
		remoteError.Code != 4 {
		t.Fatalf("unexpected remote error: %#v", remoteError)
	}
}
