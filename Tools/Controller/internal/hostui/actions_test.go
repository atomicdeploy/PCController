package hostui

import "testing"

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
	if err := broker.Publish(AppAction{Kind: "app.page", Value: "events"}); err != nil {
		t.Fatal(err)
	}
	if action := <-broker.Events(); action.Kind != "app.page" || action.Value != "events" {
		t.Fatalf("action=%#v", action)
	}
	if err := broker.Publish(AppAction{Kind: "app.page"}); err == nil {
		t.Fatal("expected missing page validation")
	}
}
