package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
)

func TestStatusSubscriptionReservationDefersWithoutErrorOrFakeSample(t *testing.T) {
	var calls atomic.Int32
	secondFetch := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	client := &Client{statusFetch: func(context.Context) (Status, error) {
		if calls.Add(1) == 1 {
			return Status{}, &control.ResourceBusyError{Resource: "serial_channel", Owner: "media_timeline"}
		}
		if calls.Load() == 2 {
			close(secondFetch)
		}
		<-release
		return Status{SupplyMV: 12345}, nil
	}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := client.SubscribeStatus(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondFetch:
	case <-time.After(time.Second):
		t.Fatal("deferred subscription did not advance its cadence")
	}
	select {
	case update := <-updates:
		t.Fatalf("deferral published an error or fake sample: %+v", update)
	default:
	}
	once.Do(func() { close(release) })
	select {
	case update := <-updates:
		if update.Error != "" || update.Status.SupplyMV != 12345 {
			t.Fatalf("bad resumed observation: %+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not resume after deferral")
	}
}

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
