//go:build windows

package ui

import (
	"sync/atomic"
	"testing"
	"time"

	"rayanlamp.local/app/internal/model"
)

func TestSystemRecoveryDebouncesAndHealthGatesNetworkChanges(t *testing.T) {
	const delay = 15 * time.Millisecond
	var networkEvents atomic.Int32
	var reconnects atomic.Int32
	coordinator := newSystemRecoveryCoordinator(
		delay,
		func() model.Snapshot {
			return model.Snapshot{WebSocketOnline: true}
		},
		func() error {
			reconnects.Add(1)
			return nil
		},
		func() { networkEvents.Add(1) },
		nil,
	)
	defer coordinator.Close()

	for index := 0; index < 20; index++ {
		coordinator.NetworkChanged()
	}
	waitForAtomic(t, &networkEvents, 1)
	time.Sleep(3 * delay)
	if got := networkEvents.Load(); got != 1 {
		t.Fatalf("network automation events = %d, want 1", got)
	}
	if got := reconnects.Load(); got != 0 {
		t.Fatalf("healthy network reconnects = %d, want 0", got)
	}
}

func TestSystemRecoveryReconnectsOnceWhenUnhealthy(t *testing.T) {
	const delay = 12 * time.Millisecond
	var networkEvents atomic.Int32
	var reconnects atomic.Int32
	coordinator := newSystemRecoveryCoordinator(
		delay,
		func() model.Snapshot { return model.Snapshot{} },
		func() error {
			reconnects.Add(1)
			return nil
		},
		func() { networkEvents.Add(1) },
		nil,
	)
	defer coordinator.Close()

	for index := 0; index < 8; index++ {
		coordinator.NetworkChanged()
	}
	waitForAtomic(t, &reconnects, 1)
	time.Sleep(3 * delay)
	if got := reconnects.Load(); got != 1 {
		t.Fatalf("unhealthy network reconnects = %d, want 1", got)
	}
	if got := networkEvents.Load(); got != 1 {
		t.Fatalf("network automation events = %d, want 1", got)
	}
}

func TestSystemRecoverySuspendSuppressesAndResumeForces(t *testing.T) {
	const delay = 12 * time.Millisecond
	var networkEvents atomic.Int32
	var reconnects atomic.Int32
	coordinator := newSystemRecoveryCoordinator(
		delay,
		func() model.Snapshot {
			return model.Snapshot{HTTPReachable: true}
		},
		func() error {
			reconnects.Add(1)
			return nil
		},
		func() { networkEvents.Add(1) },
		nil,
	)
	defer coordinator.Close()

	coordinator.Suspend()
	coordinator.NetworkChanged()
	time.Sleep(3 * delay)
	if networkEvents.Load() != 0 || reconnects.Load() != 0 {
		t.Fatalf(
			"suspended work ran: network=%d reconnect=%d",
			networkEvents.Load(),
			reconnects.Load(),
		)
	}

	coordinator.Resume()
	waitForAtomic(t, &reconnects, 1)
	if networkEvents.Load() != 0 {
		t.Fatalf("resume fabricated network event count %d", networkEvents.Load())
	}
}

func TestSystemRecoveryUnlockIsHealthGated(t *testing.T) {
	const delay = 10 * time.Millisecond
	var healthy atomic.Bool
	healthy.Store(true)
	var reconnects atomic.Int32
	coordinator := newSystemRecoveryCoordinator(
		delay,
		func() model.Snapshot {
			if healthy.Load() {
				return model.Snapshot{WebSocketOnline: true}
			}
			return model.Snapshot{}
		},
		func() error {
			reconnects.Add(1)
			return nil
		},
		nil,
		nil,
	)
	defer coordinator.Close()

	coordinator.Unlock()
	time.Sleep(3 * delay)
	if reconnects.Load() != 0 {
		t.Fatal("healthy unlock triggered recovery")
	}
	healthy.Store(false)
	coordinator.Unlock()
	waitForAtomic(t, &reconnects, 1)
}

func TestSnapshotNeedsSystemRecovery(t *testing.T) {
	if !snapshotNeedsSystemRecovery(model.Snapshot{}) {
		t.Fatal("fully offline snapshot was considered healthy")
	}
	for _, snapshot := range []model.Snapshot{
		{HTTPReachable: true},
		{WebSocketOnline: true},
		{HTTPReachable: true, WebSocketOnline: true},
	} {
		if snapshotNeedsSystemRecovery(snapshot) {
			t.Fatalf("reachable snapshot considered unhealthy: %#v", snapshot)
		}
	}
}

func waitForAtomic(t *testing.T, value *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if value.Load() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("atomic value = %d, want at least %d", value.Load(), want)
}
