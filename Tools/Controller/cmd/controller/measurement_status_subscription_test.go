package main

import (
	"context"
	"sync"
	"testing"
	"time"

	controllerapi "pccontroller.local/controller"
	"pccontroller.local/controller/internal/appconfig"
)

type recordingStatusSubscriber struct {
	mu        sync.Mutex
	intervals []time.Duration
}

func (subscriber *recordingStatusSubscriber) SubscribeStatus(
	ctx context.Context,
	interval time.Duration,
) (<-chan controllerapi.StatusUpdate, error) {
	updates := make(chan controllerapi.StatusUpdate)
	subscriber.mu.Lock()
	subscriber.intervals = append(subscriber.intervals, interval)
	subscriber.mu.Unlock()
	go func() {
		<-ctx.Done()
		close(updates)
	}()
	return updates, nil
}

func (subscriber *recordingStatusSubscriber) recorded() []time.Duration {
	subscriber.mu.Lock()
	defer subscriber.mu.Unlock()
	return append([]time.Duration(nil), subscriber.intervals...)
}

func TestConfiguredStatusSubscriptionAdoptsPushedCadenceWithoutRestart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	configuration := make(chan appconfig.Config, 3)
	client := &recordingStatusSubscriber{}
	updates := subscribeConfiguredStatus(ctx, client, configuration)

	value := appconfig.Defaults()
	configuration <- value
	value.UI.StatusIntervalMS = 300
	configuration <- value
	configuration <- value // unchanged updates must not recreate the subscription

	deadline := time.Now().Add(time.Second)
	for len(client.recorded()) < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := client.recorded(); len(got) != 2 || got[0] != 250*time.Millisecond || got[1] != 300*time.Millisecond {
		t.Fatalf("status subscription intervals=%v", got)
	}
	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("configured subscription output remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("configured subscription did not stop")
	}
}
