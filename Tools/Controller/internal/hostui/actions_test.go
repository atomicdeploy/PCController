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

func TestParseAndValidateTerminalActions(t *testing.T) {
	progress, err := ParseAction("app progress normal 42", "test")
	if err != nil || progress.Kind != "app.progress" || progress.Value != "normal 42" {
		t.Fatalf("progress=%#v err=%v", progress, err)
	}
	title, err := ParseActionURI("pccontroller://title/Bench%20controller")
	if err != nil || title.Kind != "app.title" || title.Value != "Bench controller" {
		t.Fatalf("title=%#v err=%v", title, err)
	}
	broker := NewActionBroker()
	for _, action := range []AppAction{
		{Kind: "app.title", Value: "Bench controller", Target: "tui"},
		{Kind: "app.osc", Value: "9;4;1;42", Target: "tui"},
		{Kind: "app.progress", Value: "warning 73", Target: "tui"},
	} {
		if err := broker.Publish(action); err != nil {
			t.Fatalf("Publish(%#v): %v", action, err)
		}
	}
	if err := broker.Publish(AppAction{Kind: "app.osc", Value: "2;bad\x07"}); err == nil {
		t.Fatal("control-bearing OSC action was accepted")
	}
}

func TestActionBrokerValidatesAndDelivers(t *testing.T) {
	broker := NewActionBroker()
	events := broker.Events()
	observed := make(chan AppAction, 1)
	broker.SetObserver(func(action AppAction) { observed <- action })
	if err := broker.Publish(AppAction{Kind: "app.page", Value: "events", Target: "webui"}); err != nil {
		t.Fatal(err)
	}
	if action := <-events; action.Kind != "app.page" || action.Value != "events" || action.Target != "webui" {
		t.Fatalf("action=%#v", action)
	}
	if action := <-observed; action.Kind != "app.page" || action.Value != "events" || action.At.IsZero() {
		t.Fatalf("observed action=%#v", action)
	}
	if err := broker.Publish(AppAction{Kind: "app.page"}); err == nil {
		t.Fatal("expected missing page validation")
	}
	if err := broker.Publish(AppAction{Kind: "app.page", Value: "events", Target: "bad target"}); err == nil {
		t.Fatal("expected invalid target validation")
	}
	select {
	case action := <-observed:
		t.Fatalf("invalid action reached observer: %#v", action)
	default:
	}
}

func TestActionBrokerValidatesAndCopiesBoundedMetadata(t *testing.T) {
	broker := NewActionBroker()
	events := broker.Events()
	metadata := map[string]string{" Navigation_Group ": " default "}
	if err := broker.Publish(AppAction{
		Kind: "app.page", Value: "events", Metadata: metadata,
	}); err != nil {
		t.Fatal(err)
	}
	metadata[" Navigation_Group "] = "mutated"
	action := <-events
	if action.Metadata[NavigationGroupKey] != DefaultNavigationGroup {
		t.Fatalf("metadata was not normalized/copied: %#v", action.Metadata)
	}
	for _, invalid := range []map[string]string{
		{"session_token": "secret"},
		{"bad key": "value"},
		{"detail": "line one\nline two"},
	} {
		if err := broker.Publish(AppAction{Kind: "app.page", Value: "events", Metadata: invalid}); err == nil {
			t.Fatalf("invalid metadata accepted: %#v", invalid)
		}
	}
}

func TestActionBrokerObserverOutlivesBoundedTUIQueue(t *testing.T) {
	broker := NewActionBroker()
	events := broker.Events()
	var mu sync.Mutex
	observed := make([]AppAction, 0, cap(broker.events)+1)
	broker.SetObserver(func(action AppAction) {
		mu.Lock()
		observed = append(observed, action)
		mu.Unlock()
	})

	for index := 0; index < cap(events); index++ {
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
	if len(observed) != cap(events)+1 {
		t.Fatalf("observer deliveries=%d want=%d", len(observed), cap(events)+1)
	}
	last := observed[len(observed)-1]
	if last.Kind != "app.page" || last.Value != "settings" || last.Source != "global-hotkey" {
		t.Fatalf("overflow observer action=%#v", last)
	}
}

func TestActionBrokerHeadlessObserverDoesNotAccumulateTUIActions(t *testing.T) {
	broker := NewActionBroker()
	var observed int
	broker.SetObserver(func(AppAction) { observed++ })

	for index := 0; index < cap(broker.events)*2; index++ {
		if err := broker.Publish(AppAction{Kind: "app.page", Value: "events"}); err != nil {
			t.Fatalf("headless publish %d: %v", index, err)
		}
	}
	if observed != cap(broker.events)*2 {
		t.Fatalf("observer deliveries=%d want=%d", observed, cap(broker.events)*2)
	}
	if got := len(broker.events); got != 0 {
		t.Fatalf("unsubscribed TUI queue contains %d actions", got)
	}
}

func TestNormalizeTrackedAppActionRequiresAdvertisedCustomNamespace(t *testing.T) {
	registry := NewInstanceRegistry()
	if _, err := registry.Upsert(AppInstance{
		ID: "pealayer:desktop", Surface: "pealayer", State: "active",
		Values: map[string]string{ActionCapabilitiesKey: "pealayer.open,pealayer.play"},
	}); err != nil {
		t.Fatal(err)
	}
	action, err := NormalizeTrackedAppAction(AppAction{
		Kind: " PEALAYER.OPEN ", Value: " movie.mp4 ", Target: "pealayer:desktop",
	}, registry)
	if err != nil || action.Kind != "pealayer.open" || action.Value != "movie.mp4" {
		t.Fatalf("action=%#v err=%v", action, err)
	}
	if _, err := NormalizeAppAction(action); err == nil {
		t.Fatal("custom action entered the untracked legacy validator")
	}
	for _, invalid := range []AppAction{
		{Kind: "pealayer.pause", Target: "pealayer:desktop"},
		{Kind: "app.future", Target: "pealayer:desktop"},
		{Kind: "controller.future", Target: "pealayer:desktop"},
		{Kind: "pealayer.open", Value: "first\nsecond", Target: "pealayer:desktop"},
	} {
		if _, err := NormalizeTrackedAppAction(invalid, registry); err == nil {
			t.Fatalf("invalid custom action accepted: %#v", invalid)
		}
	}
}
