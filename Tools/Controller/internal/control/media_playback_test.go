package control

import (
	"encoding/binary"
	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/ports"
	"testing"
	"time"
)

func TestMediaPlaybackValidationAndClockPayload(t *testing.T) {
	duration := uint64(12_000)
	value := MediaPlaybackUpdate{ClientID: "player:one", Sequence: 1, Loaded: true, Playing: true, PositionMS: 10_000, DurationMS: &duration, Rate: 2}
	if err := value.validate(); err != nil {
		t.Fatal(err)
	}
	payload := mediaClockPayload(value, 250*time.Millisecond)
	if len(payload) != 14 || payload[0] != 3 || payload[1] != 3 || binary.LittleEndian.Uint32(payload[2:]) != 10_500 || binary.LittleEndian.Uint16(payload[6:]) != 512 {
		t.Fatalf("clock payload %v", payload)
	}
	value.Playing = false
	if position := binary.LittleEndian.Uint32(mediaClockPayload(value, time.Second)[2:]); position != 10_000 {
		t.Fatalf("paused position %d", position)
	}
	value.Playing = true
	if position := binary.LittleEndian.Uint32(mediaClockPayload(value, 10*time.Second)[2:]); position != 12_000 {
		t.Fatalf("duration cap %d", position)
	}
	value.Rate = 0
	if value.validate() == nil {
		t.Fatal("accepted invalid speed")
	}
	value.Rate = 1
	value.Loaded = false
	if value.validate() == nil {
		t.Fatal("accepted playing unloaded media")
	}
}
func TestMediaPlaybackRejectsStaleSequenceAndCompetingOwner(t *testing.T) {
	runtime := New(Options{})
	defer runtime.Close()
	update := MediaPlaybackUpdate{ClientID: "player:a", Sequence: 1, Loaded: true, Rate: 1}
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.UpdateMediaPlayback(update); err == nil {
		t.Fatal("accepted replay")
	}
	update.ClientID = "player:b"
	if _, err := runtime.UpdateMediaPlayback(update); err == nil {
		t.Fatal("accepted competing live owner")
	}
	update.ClientID = "player:a"
	update.Sequence = 2
	update.Loaded = false
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	update.ClientID = "player:b"
	update.Sequence = 1
	if _, err := runtime.UpdateMediaPlayback(update); err != nil {
		t.Fatal(err)
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
	runtime.mediaPlayback.mu.Unlock()
	if runtime.MediaPlayback().Connected {
		t.Fatal("expired owner is still connected")
	}
}
func TestMediaPlaybackFormattedSegments(t *testing.T) {
	for _, test := range []struct {
		position uint64
		playing  bool
		cells    [4]byte
	}{
		{65_000, false, [4]byte{0x3f, 0x86, 0x3f, 0x6d}},   // 01:05, steady colon
		{65_500, true, [4]byte{0x3f, 0x06, 0x3f, 0x6d}},    // blinking colon off
		{6_000_000, true, [4]byte{0x3f, 0x86, 0x66, 0x3f}}, // 01:40 after 99:59
	} {
		p := mediaClockPayload(MediaPlaybackUpdate{PositionMS: test.position, Loaded: true, Playing: test.playing, Rate: 1}, 0)
		for i, expected := range test.cells {
			if p[10+i] != expected {
				t.Fatalf("%d cell %d: %x != %x", test.position, i, p[10+i], expected)
			}
		}
	}
}
func TestMediaPlaybackBoardACKAndExpiredProgramClaim(t *testing.T) {
	runtime := New(Options{RequestTimeout: time.Second})
	defer runtime.Close()
	port := newProgramStateWirePort()
	runtime.attach(link.OpenResult{Session: link.NewForPort("MEDIA-TEST", port), Port: ports.Info{Name: "MEDIA-TEST"}, Hello: native.Hello{Name: "PCController", Capabilities: native.CapabilityProgramState}})
	if _, err := runtime.UpdateMediaPlayback(MediaPlaybackUpdate{ClientID: "player:test", Sequence: 1, Loaded: true, Playing: true, PositionMS: 65000, Rate: 1}); err != nil {
		t.Fatal(err)
	}
	waitClock := func(flags byte) {
		t.Helper()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case frame := <-port.writes:
				if frame.Opcode == native.OpMediaClock && frame.Payload[1] == flags {
					return
				}
			case <-timer.C:
				t.Fatalf("no acknowledged media clock flags=%d", flags)
			}
		}
	}
	waitClock(3)
	deadline := time.Now().Add(time.Second)
	for !runtime.MediaPlayback().BoardSynced && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !runtime.MediaPlayback().BoardSynced {
		t.Fatal("native ACK not reported")
	}
	runtime.mediaPlayback.mu.Lock()
	runtime.mediaPlayback.received = time.Now().Add(-4 * time.Second)
	runtime.mediaPlayback.mu.Unlock()
	runtime.mediaPlayback.wake <- struct{}{}
	waitClock(0)
	if runtime.ProgramState().Mode != ProgramIdle {
		t.Fatal("expired playback retained Running claim")
	}
	if runtime.MediaPlayback().Connected || runtime.MediaPlayback().BoardSynced {
		t.Fatal("expired playback reported live synchronization")
	}
}
