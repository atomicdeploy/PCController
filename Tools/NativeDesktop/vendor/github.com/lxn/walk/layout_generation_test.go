//go:build windows

package walk

import "testing"

func TestLayoutWorkerGenerationRejectsOutOfOrderCompletion(t *testing.T) {
	var state layoutWorkerGeneration

	firstCancel := state.begin(41)
	secondCancel := state.begin(42)

	assertChannelClosed(t, firstCancel, "superseded generation")
	assertChannelOpen(t, secondCancel, "active generation")

	if state.complete(41) {
		t.Fatal("stale generation was accepted")
	}
	assertChannelOpen(
		t,
		secondCancel,
		"active generation after stale completion",
	)

	if !state.complete(42) {
		t.Fatal("latest generation was rejected")
	}
	if state.generation != 0 || state.cancelCh != nil {
		t.Fatalf(
			"completed generation was not released: generation=%d cancel=%p",
			state.generation,
			state.cancelCh,
		)
	}
}

func TestOfferLatestLayoutRequestCoalescesPendingWork(t *testing.T) {
	queue := make(chan layoutRequest, 1)

	offerLatestLayoutRequest(queue, layoutRequest{generation: 10})
	offerLatestLayoutRequest(queue, layoutRequest{generation: 11})
	offerLatestLayoutRequest(queue, layoutRequest{generation: 12})

	got := <-queue
	if got.generation != 12 {
		t.Fatalf("queued generation = %d, want 12", got.generation)
	}
	select {
	case extra := <-queue:
		t.Fatalf("unexpected extra queued generation %d", extra.generation)
	default:
	}
}

func TestNewestFormLayoutResultCannotRegressGeneration(t *testing.T) {
	newer := &formLayoutResult{generation: 8}
	older := &formLayoutResult{generation: 7}

	if got := newestFormLayoutResult(newer, older); got != newer {
		t.Fatal("out-of-order older result replaced newer queued result")
	}
	if got := newestFormLayoutResult(older, newer); got != newer {
		t.Fatal("newer result did not replace older queued result")
	}
	if got := newestFormLayoutResult(newer, nil); got != newer {
		t.Fatal("nil result replaced queued work")
	}
}

func assertChannelClosed(t *testing.T, ch <-chan struct{}, subject string) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatalf("%s cancellation channel is open", subject)
	}
}

func assertChannelOpen(t *testing.T, ch <-chan struct{}, subject string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s cancellation channel is closed", subject)
	default:
	}
}
