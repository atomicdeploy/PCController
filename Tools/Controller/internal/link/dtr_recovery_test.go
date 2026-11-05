package link

import (
	"context"
	"errors"
	"testing"
	"time"

	"pccontroller.local/controller/internal/ports"
)

type failingDTRPort struct {
	*fakePort
	assertError, releaseError error
	assertAttempted           bool
}

func (port *failingDTRPort) SetDTR(value bool) error {
	_ = port.fakePort.SetDTR(value)
	if value {
		port.assertAttempted = true
		return port.assertError
	}
	if port.assertAttempted {
		return port.releaseError
	}
	return nil
}

func TestDTRAssertionFailureStillReleasesAndReportsBothErrors(t *testing.T) {
	assertErr, releaseErr := errors.New("assert failed"), errors.New("release failed")
	port := &failingDTRPort{fakePort: newFakePort(), assertError: assertErr, releaseError: releaseErr}
	session := NewForPort("TEST", port)
	defer session.Close()
	err := session.PulseDTR(context.Background(), time.Millisecond)
	if !errors.Is(err, assertErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("lost a driver failure: %v", err)
	}
	if len(port.dtr) != 3 || port.dtr[0] || !port.dtr[1] || port.dtr[2] {
		t.Fatalf("failed assertion must still release DTR: %v", port.dtr)
	}
}

func TestDTRCancelDuringPreReleaseDoesNotAssert(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := &cancelPreReleasePort{fakePort: newFakePort(), cancel: cancel}
	session := NewForPort("TEST", port)
	defer session.Close()
	if err := session.PulseDTR(ctx, time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("reset cancellation: %v", err)
	}
	if len(port.dtr) != 1 || port.dtr[0] {
		t.Fatalf("canceled reset asserted DTR: %v", port.dtr)
	}
}

type cancelPreReleasePort struct {
	*fakePort
	cancel context.CancelFunc
}

func (port *cancelPreReleasePort) SetDTR(value bool) error {
	_ = port.fakePort.SetDTR(value)
	port.cancel()
	return nil
}

func TestAuthenticationTimeoutReservesDTRPulseAndReleaseSettle(t *testing.T) {
	options := DiscoveryOptions{StartupWait: time.Second, RequestTimeout: time.Second, HelloAttempts: 3}
	withoutReset := authenticationTimeout(options)
	options.ResetAfterOpen = func(ports.Info) bool { t.Fatal("budget consumed reset permit"); return false }
	for _, pulse := range []time.Duration{0, 2 * time.Second} {
		options.ResetPulse = pulse
		wantPulse := pulse
		if wantPulse == 0 {
			wantPulse = defaultResetPulse
		}
		if got := authenticationTimeout(options) - withoutReset; got != dtrReleaseSettle+wantPulse {
			t.Fatalf("reset budget = %s, want %s", got, dtrReleaseSettle+wantPulse)
		}
	}
}
