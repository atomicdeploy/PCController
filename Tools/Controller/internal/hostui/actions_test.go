package hostui

import (
	"sync"
	"testing"
)

func TestParseActionURIRoutesPagesAndCommands(t *testing.T) {
	page, err := ParseActionURI("pccontroller://page/events")
	if err != nil || page.Kind != "app.page" || page.Value != "events" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	command, err := ParseActionURI("pccontroller://command/relay%20off")
	if err != nil || command.Kind != "command" || command.Value != "relay off" {
		t.Fatalf("command=%#v err=%v", command, err)
	}
}

func TestActionBrokerValidatesAndDelivers(t *testing.T) {
	broker := NewActionBroker()
	observed := make(chan AppAction, 1)
	broker.SetObserver(func(action AppAction) { observed <- action })
	if err := broker.Publish(AppAction{Kind: "app.page", Value: "events"}); err != nil {
		t.Fatal(err)
	}
	if action := <-broker.Events(); action.Kind != "app.page" || action.Value != "events" {
		t.Fatalf("action=%#v", action)
	}
	if action := <-observed; action.Kind != "app.page" || action.Value != "events" || action.At.IsZero() {
		t.Fatalf("observed action=%#v", action)
	}
	if err := broker.Publish(AppAction{Kind: "app.page"}); err == nil {
		t.Fatal("expected missing page validation")
	}
	select {
	case action := <-observed:
		t.Fatalf("invalid action reached observer: %#v", action)
	default:
	}
}

func TestActionBrokerObserverOutlivesBoundedTUIQueue(t *testing.T) {
	broker := NewActionBroker()
	var mu sync.Mutex
	observed := make([]AppAction, 0, cap(broker.events)+1)
	broker.SetObserver(func(action AppAction) {
		mu.Lock()
		observed = append(observed, action)
		mu.Unlock()
	})

	for index := 0; index < cap(broker.events); index++ {
		if err := broker.Publish(AppAction{Kind: "app.page", Value: "events"}); err != nil {
			t.Fatalf("fill queue at %d: %v", index, err)
		}
	}
	overflow := AppAction{Kind: "app.page", Value: "settings", Source: "global-hotkey"}
	if err := broker.Publish(overflow); err == nil {
		t.Fatal("expected the full TUI queue to keep its original error semantics")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(observed) != cap(broker.events)+1 {
		t.Fatalf("observer deliveries=%d want=%d", len(observed), cap(broker.events)+1)
	}
	last := observed[len(observed)-1]
	if last.Kind != "app.page" || last.Value != "settings" || last.Source != "global-hotkey" {
		t.Fatalf("overflow observer action=%#v", last)
	}
}
