package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestStatusSubscriptionHubDoesNotMultiplyPhysicalPolling(t *testing.T) {
	var fetches atomic.Int32
	fetchStarted := make(chan struct{}, 2)
	releaseFetch := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFetch) }) }
	defer release()
	client := &Client{statusFetch: func(context.Context) (Status, error) {
		fetchStarted <- struct{}{}
		<-releaseFetch
		fetches.Add(1)
		return native.Status{SupplyMV: 12_345}, nil
	}}
	firstContext, stopFirst := context.WithCancel(context.Background())
	secondContext, stopSecond := context.WithCancel(context.Background())
	defer stopFirst()
	defer stopSecond()
	first, err := client.SubscribeStatus(firstContext, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fetchStarted:
	case <-time.After(time.Second):
		t.Fatal("first physical status fetch did not start")
	}
	second, err := client.SubscribeStatus(secondContext, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-fetchStarted:
		release()
		t.Fatal("second subscriber started a concurrent physical status fetch")
	case <-time.After(250 * time.Millisecond):
	}
	release()
	for index, updates := range []<-chan StatusUpdate{first, second} {
		select {
		case update := <-updates:
			if update.Status.SupplyMV != 12_345 {
				t.Fatalf("subscriber %d status=%#v", index, update.Status)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive shared status", index)
		}
	}
	stopFirst()
	stopSecond()
	deadline := time.Now().Add(time.Second)
	for {
		client.statusHub.mu.Lock()
		running := client.statusHub.running
		client.statusHub.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("status subscription hub did not stop after both subscribers cancelled")
		}
		time.Sleep(time.Millisecond)
	}
	if got := fetches.Load(); got < 1 || got > 2 {
		t.Fatalf("physical fetches=%d; an in-flight join may use one next sequential fetch", got)
	}
}
