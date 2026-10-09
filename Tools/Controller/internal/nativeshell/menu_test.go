package nativeshell

import (
	"context"
	"reflect"
	"testing"
)

func TestBuildMenuHidesEveryPageActionWhileOffline(t *testing.T) {
	items := BuildMenu(State{Title: " Controller ", ConnectionState: "offline"})
	var commands []Command
	for _, item := range items {
		if item.Kind == ItemAction {
			commands = append(commands, item.Command)
		}
	}
	if want := []Command{CommandReconnect, CommandExit}; !reflect.DeepEqual(commands, want) {
		t.Fatalf("offline commands=%v; want %v", commands, want)
	}
	if got := items[0].Label; got != "Controller offline" {
		t.Fatalf("offline status=%q", got)
	}
}

func TestBuildMenuExposesPagesOnlyForAuthenticatedConnection(t *testing.T) {
	items := BuildMenu(State{Connected: true, Port: " COM18 "})
	var commands []Command
	for _, item := range items {
		if item.Kind == ItemAction {
			commands = append(commands, item.Command)
		}
	}
	want := []Command{
		CommandDashboard, CommandControls, CommandWorkbench,
		CommandUpdates, CommandSettings, CommandReconnect, CommandExit,
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("connected commands=%v; want %v", commands, want)
	}
	if got := items[0].Label; got != "Controller connected — COM18" {
		t.Fatalf("connected status=%q", got)
	}
}

func TestStateIconUsesAuthenticatedConnectionSemantics(t *testing.T) {
	tests := []struct {
		state State
		want  IconState
	}{
		{state: State{Connected: true, Paused: true}, want: IconConnected},
		{state: State{Paused: true, ConnectionState: "reconnecting"}, want: IconPaused},
		{state: State{ConnectionState: "connecting"}, want: IconReconnecting},
		{state: State{ConnectionState: "reconnecting"}, want: IconReconnecting},
		{state: State{ConnectionState: "disconnected"}, want: IconOffline},
	}
	for _, test := range tests {
		if got := StateIcon(test.state); got != test.want {
			t.Fatalf("StateIcon(%+v)=%q; want %q", test.state, got, test.want)
		}
	}
}

func TestPageForCommand(t *testing.T) {
	tests := map[Command]string{
		CommandDashboard: "dashboard",
		CommandControls:  "controls",
		CommandWorkbench: "workbench",
		CommandUpdates:   "updates",
		CommandSettings:  "settings",
	}
	for command, want := range tests {
		if got, ok := PageForCommand(command); !ok || got != want {
			t.Errorf("PageForCommand(%d)=(%q,%t); want (%q,true)", command, got, ok, want)
		}
	}
	for _, command := range []Command{CommandNone, CommandReconnect, CommandExit} {
		if page, ok := PageForCommand(command); ok || page != "" {
			t.Errorf("PageForCommand(%d)=(%q,%t); want no page", command, page, ok)
		}
	}
}

func TestDispatchRechecksConnectionBeforeOpeningPage(t *testing.T) {
	opened := 0
	state := State{Connected: false}
	options := Options{
		Snapshot:  func() State { return state },
		OpenPage:  func(string) error { opened++; return nil },
		Reconnect: func(context.Context) error { return nil },
		Exit:      func() {},
	}
	dispatch(context.Background(), options, CommandDashboard)
	if opened != 0 {
		t.Fatalf("offline page dispatch opened browser %d time(s)", opened)
	}
	state.Connected = true
	dispatch(context.Background(), options, CommandDashboard)
	if opened != 1 {
		t.Fatalf("connected page dispatch opened browser %d time(s); want 1", opened)
	}
}
