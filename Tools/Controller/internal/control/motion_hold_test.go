package control

import (
	"context"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/link"
	"pccontroller.local/controller/internal/native"
)

func motionHoldFixture(t *testing.T) (*Runtime, *buzzerSettingsWirePort) {
	t.Helper()
	port := newBuzzerSettingsWirePort(native.DefaultSettings())
	session := link.NewForPort("MOTION-HOLD-FIXTURE", port)
	r := New(Options{})
	r.mu.Lock()
	r.session = session
	r.generation = 7
	r.connectionState = "connected"
	r.mu.Unlock()
	t.Cleanup(func() { r.Close() })
	return r, port
}

func TestMotionHoldRenewDoesNotResendAndExpiryStops(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease, err := r.StartMotionHold(context.Background(), 7, 1, 1, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	first := <-port.writes
	if first.Opcode != native.OpRelaySide || first.Payload[1] != 1 {
		t.Fatalf("start: %+v", first)
	}
	if _, err := r.RenewMotionHold(lease); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-port.writes:
		t.Fatalf("renew wrote to serial: %+v", frame)
	default:
	}
	r.mu.Lock()
	r.motionHolds[0].ExpiresAt = time.Now().Add(-time.Millisecond)
	r.mu.Unlock()
	r.expireMotionHold(lease, 0)
	stop := <-port.writes
	if stop.Opcode != native.OpRelaySide || len(stop.Payload) != 2 || stop.Payload[0] != 0 || stop.Payload[1] != 0 {
		t.Fatalf("expiry did not stop: %+v", stop)
	}
	if _, err := r.RenewMotionHold(lease); err == nil {
		t.Fatal("expired hold revived")
	}
}

func TestMotionHoldLateReleaseCannotStopNewerWrite(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease, err := r.StartMotionHold(context.Background(), 7, 1, 1, strings.Repeat("b", 32))
	if err != nil {
		t.Fatal(err)
	}
	<-port.writes
	if err := r.Command(context.Background(), native.OpRelaySide, []byte{0, 2}); err != nil {
		t.Fatal(err)
	}
	<-port.writes
	if err := r.ReleaseMotionHold(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-port.writes:
		t.Fatalf("old release stopped new intent: %+v", frame)
	default:
	}
	if _, err := r.RenewMotionHold(lease); err == nil {
		t.Fatal("superseded hold renewed")
	}
}

func TestMotionHoldReleaseBeforeStartAndGenerationFence(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease := MotionHoldLease{ID: strings.Repeat("c", 32), Side: 1, Generation: 7}
	if err := r.ReleaseMotionHold(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartMotionHold(context.Background(), 7, 1, 1, lease.ID); err == nil {
		t.Fatal("released pending press started later")
	}
	if _, err := r.StartMotionHold(context.Background(), 6, 1, 1, strings.Repeat("d", 32)); err == nil {
		t.Fatal("old board generation actuated")
	}
	select {
	case frame := <-port.writes:
		t.Fatalf("rejected hold wrote: %+v", frame)
	default:
	}
}

func TestMotionHoldWatchdogAndEmergencyRenewal(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease, err := r.StartMotionHold(context.Background(), 7, 2, 2, strings.Repeat("e", 32))
	if err != nil {
		t.Fatal(err)
	}
	<-port.writes
	r.emergencyStop.Store(true)
	if _, err := r.RenewMotionHold(lease); err == nil {
		t.Fatal("emergency latch allowed hold renewal")
	}
	select {
	case frame := <-port.writes:
		if frame.Opcode != native.OpRelaySide || frame.Payload[0] != 1 || frame.Payload[1] != 0 {
			t.Fatalf("watchdog wrote movement: %+v", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("abandoned hold did not issue bounded stop")
	}
}

func TestMotionHoldRejectedCompetingCommandDoesNotRevokeWatchdog(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease, err := r.StartMotionHold(context.Background(), 7, 1, 1, strings.Repeat("f", 32))
	if err != nil {
		t.Fatal(err)
	}
	<-port.writes
	r.emergencyStop.Store(true)
	if err := r.Command(context.Background(), native.OpRelaySide, []byte{0, 2}); err == nil {
		t.Fatal("emergency-gated movement accepted")
	}
	r.mu.RLock()
	valid := r.motionHoldValidLocked(lease)
	r.mu.RUnlock()
	if !valid {
		t.Fatal("rejected movement revoked the existing safety watchdog")
	}
	if err := r.ReleaseMotionHold(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	frame := <-port.writes
	if frame.Payload[1] != 0 {
		t.Fatal("release failed to stop")
	}
}

func TestMotionHoldOwnedStopSurvivesNewMediaReservation(t *testing.T) {
	r, port := motionHoldFixture(t)
	lease, err := r.StartMotionHold(context.Background(), 7, 1, 1, strings.Repeat("9", 32))
	if err != nil {
		t.Fatal(err)
	}
	<-port.writes
	r.mediaPlayback.mu.Lock()
	r.mediaPlayback.authority = MediaAuthorityStatus{OwnerID: "player", Exclusive: true}
	r.mediaPlayback.mu.Unlock()
	if err := r.ReleaseMotionHold(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	frame := <-port.writes
	if frame.Opcode != native.OpRelaySide || frame.Payload[1] != 0 {
		t.Fatal("owned release was not a stop")
	}
	if status := r.MediaAuthority(); status.OwnerID != "player" || !status.Exclusive {
		t.Fatal("hold release stole media authority")
	}
}
