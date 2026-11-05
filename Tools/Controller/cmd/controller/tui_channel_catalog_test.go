package main

import (
	"context"
	"testing"
)

func TestTUIChannelCatalogUsesOwnerRPC(t *testing.T) {
	methods := []string{}
	source := newTUIChannelCatalog(func(_ context.Context, method string, params, target any) error {
		methods = append(methods, method)
		if method == "controller.peripheral.presentation.update" {
			value := params.(map[string]any)
			if value["key"] != "motion.a" || value["name"] != "Lift" || value["expected_revision"] != "current" {
				t.Fatalf("lost mutation fence: %#v", params)
			}
		}
		return nil
	})
	_, _ = source.Read(context.Background())
	_, _ = source.Rename(context.Background(), "motion.a", "Lift", "current")
	if len(methods) != 2 || methods[0] != "controller.peripherals.get" {
		t.Fatalf("unexpected calls %v", methods)
	}
}

func TestRemoteTUIPresentationStateReachesCatalogConsumer(t *testing.T) {
	client := newRemoteTUIIPC(context.Background(), "", "")
	defer client.Close()
	data := []byte(`{"method":"controller.state","params":{"id":10,"kind":"peripherals.changed","stream":"state","text":"catalog changed"}}`)
	if _, err := client.consumeLiveMessage(data, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-client.events:
		if event.Kind != "peripherals.changed" {
			t.Fatal(event.Kind)
		}
	default:
		t.Fatal("state event was discarded")
	}
	_, _ = client.consumeLiveMessage(data, 0)
	select {
	case <-client.events:
		t.Fatal("replayed event was not deduplicated")
	default:
	}
}
