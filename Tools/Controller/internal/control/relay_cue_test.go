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

type relayCuePort struct {
	mu      sync.Mutex
	frames  []native.Frame
	reads   chan []byte
	closed  chan struct{}
	closeMu sync.Once
}

func newRelayCuePort() *relayCuePort {
	return &relayCuePort{reads: make(chan []byte, 4), closed: make(chan struct{})}
}

func (*relayCuePort) SetMode(*serial.Mode) error { return nil }
func (port *relayCuePort) Read(buffer []byte) (int, error) {
	select {
	case data := <-port.reads:
		return copy(buffer, data), nil
	case <-port.closed:
		return 0, io.EOF
	}
}
func (port *relayCuePort) Write(data []byte) (int, error) {
	frame, err := native.Decode(data)
	if err != nil {
		return 0, err
	}
	port.mu.Lock()
	port.frames = append(port.frames, frame)
	port.mu.Unlock()
	response, err := native.Encode(native.Frame{
		Opcode: native.OpACK, Seq: frame.Seq, Payload: []byte{frame.Opcode, 0},
	})
	if err != nil {
		return 0, err
	}
	port.reads <- response
	return len(data), nil
}
func (*relayCuePort) Drain() error                       { return nil }
func (*relayCuePort) ResetInputBuffer() error            { return nil }
func (*relayCuePort) ResetOutputBuffer() error           { return nil }
func (*relayCuePort) SetDTR(bool) error                  { return nil }
func (*relayCuePort) SetRTS(bool) error                  { return nil }
func (*relayCuePort) SetReadTimeout(time.Duration) error { return nil }
func (*relayCuePort) Break(time.Duration) error          { return nil }
func (*relayCuePort) GetModemStatusBits() (*serial.ModemStatusBits, error) {
	return &serial.ModemStatusBits{}, nil
}
func (port *relayCuePort) Close() error {
	port.closeMu.Do(func() { close(port.closed) })
	return nil
}

func TestRelayCommandEmitsOneConfiguredCue(t *testing.T) {
	port := newRelayCuePort()
	session := link.NewForPort("relay-cue", port)
	defer session.Close()
	runtime := New(Options{})
	runtime.mu.Lock()
	runtime.session = session
	runtime.connectionState = "connected"
	runtime.haveSettings = true
	runtime.settings = native.Settings{}
	runtime.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := commandRelayWithCue(
		ctx, runtime, native.OpRelaySide, []byte{0, 2}, true, true,
	); err != nil {
		t.Fatal(err)
	}
	port.mu.Lock()
	defer port.mu.Unlock()
	if len(port.frames) != 2 ||
		port.frames[0].Opcode != native.OpRelaySide ||
		port.frames[1].Opcode != native.OpBuzzer ||
		string(port.frames[1].Payload) != string(native.BuzzerPayload(1900, 35)) {
		t.Fatalf("relay/cue frames=%#v", port.frames)
	}
}

func TestRelayCueHonorsPersistedDisableAndNoOp(t *testing.T) {
	for _, test := range []struct {
		name     string
		settings native.Settings
		changed  bool
	}{
		{name: "disabled", settings: native.Settings{Flags: native.SettingsRelayAudioDisabled}, changed: true},
		{name: "silent", settings: native.Settings{Flags: native.SettingsSilent}, changed: true},
		{name: "no-op", settings: native.Settings{}, changed: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			port := newRelayCuePort()
			session := link.NewForPort("relay-cue", port)
			defer session.Close()
			runtime := New(Options{})
			runtime.mu.Lock()
			runtime.session = session
			runtime.connectionState = "connected"
			runtime.haveSettings = true
			runtime.settings = test.settings
			runtime.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := commandRelayWithCue(
				ctx, runtime, native.OpRelaySet, []byte{4, 1}, true, test.changed,
			); err != nil {
				t.Fatal(err)
			}
			port.mu.Lock()
			defer port.mu.Unlock()
			if len(port.frames) != 1 || port.frames[0].Opcode != native.OpRelaySet {
				t.Fatalf("suppressed cue frames=%#v", port.frames)
			}
		})
	}
}

func TestRelaySideChangeDetectionUsesLogicalSettledState(t *testing.T) {
	snapshot := Snapshot{HaveStatus: true}
	snapshot.Status.ActiveRelays = 0x02
	if relaySideStateChanged(snapshot, 0, 1) {
		t.Fatal("identical forward request was treated as a relay transition")
	}
	if !relaySideStateChanged(snapshot, 0, 2) {
		t.Fatal("forward-to-reverse request was not treated as one logical transition")
	}
}
