package hostui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSurfaceLaunchRequestRejectsArbitraryExecutionFields(t *testing.T) {
	for _, request := range []SurfaceLaunchRequest{
		{Surface: "cmd.exe"},
		{Surface: "tui", Mode: "shell"},
		{Surface: "tui", Mode: "launch", Target: "tui:chosen"},
		{Surface: "webui", Page: "../secrets"},
		{Surface: "webui", Target: "*"},
	} {
		if _, err := NormalizeSurfaceLaunchRequest(request); err == nil {
			t.Fatalf("unsafe request accepted: %#v", request)
		}
	}
	normalized, err := NormalizeSurfaceLaunchRequest(SurfaceLaunchRequest{Surface: "web"})
	if err != nil || normalized.Surface != SurfaceWebUI || normalized.Mode != SurfaceLaunchEnsure {
		t.Fatalf("normalized=%#v err=%v", normalized, err)
	}
}

func TestSurfaceLaunchCoordinatorDeduplicatesConcurrentCalls(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	started := make(chan struct{})
	release := make(chan struct{})
	coordinator := NewSurfaceLaunchCoordinator(func(
		_ context.Context,
		_ SurfaceLaunchRequest,
	) (SurfaceLaunchResult, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return SurfaceLaunchResult{Effective: "started", Accepted: true}, nil
	})
	request := SurfaceLaunchRequest{
		Surface: SurfaceTUI, Mode: SurfaceLaunchLaunch, IdempotencyKey: "same-operation",
	}
	first := make(chan SurfaceLaunchResult, 1)
	go func() {
		result, _ := coordinator.Launch(context.Background(), request)
		first <- result
	}()
	<-started
	second := make(chan SurfaceLaunchResult, 1)
	go func() {
		result, _ := coordinator.Launch(context.Background(), request)
		second <- result
	}()
	close(release)
	if result := <-first; result.Deduplicated {
		t.Fatalf("first result was marked deduplicated: %#v", result)
	}
	if result := <-second; !result.Deduplicated || result.Effective != "started" {
		t.Fatalf("second result=%#v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("launch calls=%d", calls)
	}
}

func TestSurfaceLaunchCoordinatorRejectsIdempotencyKeyReuse(t *testing.T) {
	coordinator := NewSurfaceLaunchCoordinator(func(
		_ context.Context,
		_ SurfaceLaunchRequest,
	) (SurfaceLaunchResult, error) {
		return SurfaceLaunchResult{Effective: "existing", Accepted: true}, nil
	})
	if _, err := coordinator.Launch(context.Background(), SurfaceLaunchRequest{
		Surface: SurfaceTUI, IdempotencyKey: "operation-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Launch(context.Background(), SurfaceLaunchRequest{
		Surface: SurfaceWebUI, IdempotencyKey: "operation-1",
	}); err == nil {
		t.Fatal("different launch reused the same idempotency key")
	}
}

func TestSurfaceLaunchCoordinatorBoundsConcurrentOperations(t *testing.T) {
	coordinator := NewSurfaceLaunchCoordinator(func(
		_ context.Context,
		_ SurfaceLaunchRequest,
	) (SurfaceLaunchResult, error) {
		return SurfaceLaunchResult{Effective: "started", Accepted: true}, nil
	})
	for index := 0; index < maximumSurfaceLaunchEntries; index++ {
		key := fmt.Sprintf("busy-%d", index)
		coordinator.entries[key] = &surfaceLaunchOperation{
			fingerprint: key, ready: make(chan struct{}), createdAt: coordinator.now(),
		}
	}
	if _, err := coordinator.Launch(context.Background(), SurfaceLaunchRequest{
		Surface: SurfaceTUI, IdempotencyKey: "overflow",
	}); err == nil || !strings.Contains(err.Error(), "queue is busy") {
		t.Fatalf("capacity err=%v", err)
	}
}
