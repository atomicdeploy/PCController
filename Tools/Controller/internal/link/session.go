package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"

	"pccontroller.local/controller/internal/native"
)

const (
	DefaultBaudRate    = 115200
	DefaultReadTimeout = 100 * time.Millisecond
)

var (
	ErrClosed                  = errors.New("serial session is closed")
	ErrSequenceExhaust         = errors.New("all request sequence numbers are in use")
	ErrControlLinesUnsupported = errors.New("transport does not support DTR/RTS")
)

type Event struct {
	Frame native.Frame
	Err   error
}

type Session struct {
	name string
	port sessionPort

	writeMu sync.Mutex
	stateMu sync.RWMutex
	waiters map[byte]*pendingRequest
	nextSeq byte
	hello   native.Hello

	events chan Event
	done   chan struct{}

	closeOnce sync.Once
	readDone  sync.WaitGroup
}

type sessionPort interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Close() error
	SetDTR(bool) error
	SetRTS(bool) error
}

type pendingRequest struct {
	requestOpcode byte
	responses     map[byte]bool
	channel       chan native.Frame
}

func Open(name string, baudRate int) (*Session, error) {
	return OpenContext(context.Background(), name, baudRate)
}

func OpenContext(ctx context.Context, name string, baudRate int) (*Session, error) {
	if name == "" {
		return nil, errors.New("serial port is required")
	}
	if IsNetworkEndpoint(name) {
		return openNetwork(ctx, name)
	}
	if baudRate == 0 {
		baudRate = DefaultBaudRate
	}
	port, err := serial.Open(name, serialMode(baudRate))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", name, err)
	}
	if err := port.SetReadTimeout(DefaultReadTimeout); err != nil {
		_ = port.Close()
		return nil, fmt.Errorf("configure %s: %w", name, err)
	}
	_ = port.ResetInputBuffer()

	session := NewForPort(name, port)
	return session, nil
}

func serialMode(baudRate int) *serial.Mode {
	return &serial.Mode{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
		// go.bug.st/serial otherwise defaults both lines to true. On its
		// Windows backend that is the asserted/physical-low state, so merely
		// opening a CH340 produces the falling edge used by Arduino auto-reset.
		// Start inactive/high; an explicit reset uses a deliberate pulse below.
		InitialStatusBits: &serial.ModemOutputBits{DTR: false, RTS: false},
	}
}

// NewForPort is exported for deterministic transport tests and adapters.
func NewForPort(name string, port serial.Port) *Session {
	return newForTransport(name, port)
}

func newForTransport(name string, port sessionPort) *Session {
	session := &Session{
		name:    name,
		port:    port,
		waiters: make(map[byte]*pendingRequest),
		nextSeq: 1,
		events:  make(chan Event, 256),
		done:    make(chan struct{}),
	}
	session.readDone.Add(1)
	go session.readLoop()
	return session
}

func IsNetworkEndpoint(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "tcp://")
}

type networkPort struct {
	net.Conn
}

func (port *networkPort) SetDTR(bool) error { return ErrControlLinesUnsupported }
func (port *networkPort) SetRTS(bool) error { return ErrControlLinesUnsupported }

func openNetwork(ctx context.Context, endpoint string) (*Session, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.EqualFold(parsed.Scheme, "tcp") ||
		parsed.Host == "" || parsed.Path != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid virtual-board endpoint %q; expected tcp://host:port", endpoint)
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	connection, err := dialer.DialContext(ctx, "tcp", parsed.Host)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", endpoint, err)
	}
	return newForTransport(endpoint, &networkPort{Conn: connection}), nil
}

func (s *Session) Authenticate(ctx context.Context) (native.Hello, error) {
	frame, err := s.Request(ctx, native.OpHello, nil, native.OpHelloResp)
	if err != nil {
		return native.Hello{}, err
	}
	hello, err := native.ParseHello(frame.Payload)
	if err != nil {
		return native.Hello{}, err
	}
	if !hello.IsPCController() {
		return native.Hello{}, fmt.Errorf(
			"unexpected device identity kind=%d name=%q",
			hello.BoardKind,
			hello.Name,
		)
	}
	s.stateMu.Lock()
	s.hello = hello
	s.stateMu.Unlock()
	return hello, nil
}

func (s *Session) AuthenticateWithRetry(
	ctx context.Context,
	attempts int,
	requestTimeout time.Duration,
) (native.Hello, error) {
	if attempts < 1 {
		attempts = 1
	}
	if requestTimeout <= 0 {
		requestTimeout = 700 * time.Millisecond
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if hello := s.Hello(); hello.IsPCController() {
			return hello, nil
		}
		requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
		hello, err := s.Authenticate(requestContext)
		cancel()
		if err == nil {
			return hello, nil
		}
		lastErr = err
		if cached := s.Hello(); cached.IsPCController() {
			return cached, nil
		}
		if ctx.Err() != nil {
			return native.Hello{}, ctx.Err()
		}
		if attempt != attempts {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return native.Hello{}, ctx.Err()
			case <-s.done:
				timer.Stop()
				return native.Hello{}, ErrClosed
			case <-timer.C:
			}
		}
	}
	return native.Hello{}, fmt.Errorf(
		"no valid response after %d attempts (last error: %w)",
		attempts,
		lastErr,
	)
}

func (s *Session) Request(
	ctx context.Context,
	opcode byte,
	payload []byte,
	expected ...byte,
) (native.Frame, error) {
	sequence, waiter, err := s.reserveSequence(opcode, expected)
	if err != nil {
		return native.Frame{}, err
	}
	defer s.releaseSequence(sequence, waiter)

	if err := s.WriteFrame(native.Frame{
		Opcode:  opcode,
		Seq:     sequence,
		Payload: append([]byte(nil), payload...),
	}); err != nil {
		return native.Frame{}, err
	}

	for {
		select {
		case <-ctx.Done():
			return native.Frame{}, ctx.Err()
		case <-s.done:
			return native.Frame{}, ErrClosed
		case response := <-waiter.channel:
			if response.Opcode == native.OpError {
				return native.Frame{}, decodeRemoteError(response)
			}
			for _, allowed := range expected {
				if response.Opcode == allowed {
					if allowed == native.OpACK &&
						(len(response.Payload) < 2 ||
							response.Payload[0] != opcode ||
							response.Payload[1] != 0) {
						continue
					}
					return response, nil
				}
			}
		}
	}
}

func (s *Session) Command(ctx context.Context, opcode byte, payload []byte) error {
	_, err := s.Request(ctx, opcode, payload, native.OpACK)
	return err
}

func (s *Session) WriteFrame(frame native.Frame) error {
	encoded, err := native.Encode(frame)
	if err != nil {
		return err
	}
	return s.WriteRaw(encoded)
}

func (s *Session) WriteRaw(data []byte) error {
	select {
	case <-s.done:
		return ErrClosed
	default:
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	written, err := s.port.Write(data)
	if err != nil {
		return fmt.Errorf("write %s: %w", s.name, err)
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

func (s *Session) PulseReset(ctx context.Context, lowTime time.Duration) error {
	// Arduino-class CH340 boards auto-reset from DTR. Avoid touching RTS:
	// go.bug.st/serial documents Windows drivers where changing RTS can
	// unexpectedly reassert DTR and leave RESET electrically active.
	return s.PulseDTR(ctx, lowTime)
}

func (s *Session) Name() string {
	return s.name
}

func (s *Session) Hello() native.Hello {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.hello
}

// PulseDTR performs the reconnect-only reset gesture without touching RTS.
// Keeping it separate from PulseReset makes the one-shot policy observable and
// avoids surprising adapters that assign RTS another function.
func (s *Session) PulseDTR(ctx context.Context, lowTime time.Duration) error {
	if lowTime <= 0 {
		lowTime = 120 * time.Millisecond
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.port.SetDTR(true); err != nil {
		return fmt.Errorf("assert DTR: %w", err)
	}
	timer := time.NewTimer(lowTime)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = s.port.SetDTR(false)
		return ctx.Err()
	case <-s.done:
		_ = s.port.SetDTR(false)
		return ErrClosed
	case <-timer.C:
	}
	if err := s.port.SetDTR(false); err != nil {
		return fmt.Errorf("release DTR: %w", err)
	}
	return nil
}

func (s *Session) Events() <-chan Event {
	return s.events
}

func (s *Session) Done() <-chan struct{} {
	return s.done
}

func (s *Session) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.done)
		closeErr = s.port.Close()
	})
	s.readDone.Wait()
	return closeErr
}

func (s *Session) readLoop() {
	defer s.readDone.Done()
	var decoder native.Decoder
	buffer := make([]byte, 256)

	for {
		n, err := s.port.Read(buffer)
		if n > 0 {
			frames, decodeErrors := decoder.Feed(buffer[:n])
			for _, decodeErr := range decodeErrors {
				s.publish(Event{Err: decodeErr})
			}
			for _, frame := range frames {
				s.observeHello(frame)
				if !s.deliver(frame) {
					s.publish(Event{Frame: frame})
				}
			}
		}
		if err != nil {
			select {
			case <-s.done:
			default:
				s.publish(Event{Err: fmt.Errorf("read %s: %w", s.name, err)})
				s.closeOnce.Do(func() {
					close(s.done)
					_ = s.port.Close()
				})
			}
			return
		}
		select {
		case <-s.done:
			return
		default:
		}
	}
}

func (s *Session) observeHello(frame native.Frame) {
	if frame.Opcode != native.OpHelloResp {
		return
	}
	hello, err := native.ParseHello(frame.Payload)
	if err != nil || !hello.IsPCController() {
		return
	}
	s.stateMu.Lock()
	s.hello = hello
	s.stateMu.Unlock()
}

func (s *Session) deliver(frame native.Frame) bool {
	s.stateMu.RLock()
	waiter := s.waiters[frame.Seq]
	s.stateMu.RUnlock()
	if waiter == nil {
		return false
	}
	if frame.Opcode == native.OpError {
		if len(frame.Payload) < 2 ||
			frame.Payload[0] != waiter.requestOpcode {
			return false
		}
	} else if !waiter.responses[frame.Opcode] {
		return false
	}
	select {
	case waiter.channel <- frame:
		return true
	case <-s.done:
		return false
	}
}

func (s *Session) publish(event Event) {
	select {
	case s.events <- event:
	default:
		select {
		case <-s.events:
		default:
		}
		select {
		case s.events <- event:
		default:
		}
	}
}

func (s *Session) reserveSequence(
	requestOpcode byte,
	expected []byte,
) (byte, *pendingRequest, error) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	responses := make(map[byte]bool, len(expected))
	for _, opcode := range expected {
		responses[opcode] = true
	}
	for attempt := 0; attempt < 255; attempt++ {
		sequence := s.nextSeq
		s.nextSeq++
		if s.nextSeq == 0 {
			s.nextSeq = 1
		}
		// 0xFE is emitted by the MCU macro executor. Never allocate it to a
		// host request, so timed execution ACK/error frames stay unsolicited.
		if sequence == native.MacroExecutionSequence {
			continue
		}
		if _, exists := s.waiters[sequence]; exists {
			continue
		}
		waiter := &pendingRequest{
			requestOpcode: requestOpcode,
			responses:     responses,
			channel:       make(chan native.Frame, 4),
		}
		s.waiters[sequence] = waiter
		return sequence, waiter, nil
	}
	return 0, nil, ErrSequenceExhaust
}

func (s *Session) releaseSequence(sequence byte, waiter *pendingRequest) {
	s.stateMu.Lock()
	if s.waiters[sequence] == waiter {
		delete(s.waiters, sequence)
	}
	s.stateMu.Unlock()
}

type RemoteError struct {
	RequestOpcode byte
	Code          byte
	Detail        []byte
}

func (err *RemoteError) Error() string {
	return fmt.Sprintf(
		"device rejected %s (0x%02X): code=0x%02X detail=% X",
		native.OpcodeName(err.RequestOpcode),
		err.RequestOpcode,
		err.Code,
		err.Detail,
	)
}

func decodeRemoteError(frame native.Frame) error {
	remote := &RemoteError{}
	if len(frame.Payload) > 0 {
		remote.RequestOpcode = frame.Payload[0]
	}
	if len(frame.Payload) > 1 {
		remote.Code = frame.Payload[1]
	}
	if len(frame.Payload) > 2 {
		remote.Detail = append([]byte(nil), frame.Payload[2:]...)
	}
	return remote
}
