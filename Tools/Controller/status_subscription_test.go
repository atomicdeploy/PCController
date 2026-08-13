package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"pccontroller.local/controller/internal/native"
)

func TestStatusSubscriptionHubDoesNotMultiplyPhysicalPolling(t *testing.T) {
	var fetches atomic.Int32
	client := &Client{statusFetch: func(context.Context) (Status, error) {
		fetches.Add(1)
		return native.Status{SupplyMV: 12_345}, nil
	}}
	firstContext, stopFirst := context.WithCancel(context.Background())
	secondContext, stopSecond := context.WithCancel(context.Background())
	defer stopFirst()
	defer stopSecond()
	first, err := client.SubscribeStatus(firstContext, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.SubscribeStatus(secondContext, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
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
	time.Sleep(125 * time.Millisecond)
	if got := fetches.Load(); got < 2 || got > 4 {
		t.Fatalf("physical fetches=%d; two 20Hz clients must share one cadence", got)
	}
}
