//go:build windows

package ui

import (
	"github.com/lxn/walk"
	"rayanlamp.local/app/presentation"
	"testing"
)

func TestControllerCardActionOrder(t *testing.T) {
	input := []presentation.Action{{ID: "off", Verb: "off"}, {ID: "inc", Verb: "increase"}, {ID: "on", Verb: "on"}, {ID: "dec", Verb: "decrease"}}
	got := orderedControllerActions(input)
	for i, want := range []string{"on", "off", "dec", "inc"} {
		if got[i].ID != want {
			t.Fatalf("slot %d=%s want %s", i, got[i].ID, want)
		}
	}
	if input[0].ID != "off" {
		t.Fatal("mutated authoritative catalog")
	}
}

func TestControllerIconSearchMatchesAliasesAndEveryChoiceRenders(t *testing.T) {
	for _, query := range []string{"loostr", "window", "garden", "lantern", "stage light", "light bulb"} {
		if len(filterControllerIcons(query, controllerIconChoices)) == 0 {
			t.Fatalf("missing icon search %q", query)
		}
	}
	for _, choice := range controllerIconChoices {
		if choice.id != "" && controllerChannelGlyph("lucide:"+choice.id, "missing") != choice.glyph {
			t.Fatalf("picker cannot render %q", choice.id)
		}
	}
	if len(filterControllerIcons("there-is-no-such-icon", controllerIconChoices)) != 0 {
		t.Fatal("search ignored")
	}
}

func TestControllerCardReorderIsKindLocalAndPreservesIdentity(t *testing.T) {
	s := &Shell{controller: &presentation.Options{}, controllerState: presentation.Snapshot{Preview: true, Channels: []presentation.Channel{
		{Key: "relay.5", Kind: "relay", Order: 0}, {Key: "relay.6", Kind: "relay", Order: 1},
		{Key: "pwm.0", Kind: "mosfet", Order: 0}, {Key: "pwm.1", Kind: "mosfet", Order: 1},
	}}}
	s.saveControllerChannel("pwm.1", "", map[string]any{"order": 0})
	for i, key := range []string{"relay.5", "relay.6", "pwm.1", "pwm.0"} {
		if s.controllerState.Channels[i].Key != key {
			t.Fatalf("slot %d=%+v", i, s.controllerState.Channels)
		}
	}
	if s.controllerState.Channels[2].Order != 0 || s.controllerState.Channels[3].Order != 1 {
		t.Fatal("mixed global and kind-local ranks")
	}
}

func TestControllerDragTargetAndCaptureGeneration(t *testing.T) {
	hits := []controllerChannelHit{{key: "pwm.0", channel: presentation.Channel{Kind: "mosfet"}, rect: walk.Rectangle{Width: 100, Height: 100}},
		{key: "pwm.1", channel: presentation.Channel{Kind: "mosfet"}, rect: walk.Rectangle{X: 110, Width: 100, Height: 100}},
		{key: "relay.5", channel: presentation.Channel{Kind: "relay"}, rect: walk.Rectangle{X: 220, Width: 100, Height: 100}}}
	g := controllerChannelGesture{key: "pwm.0", dragging: true, token: 2}
	if controllerDropTarget(g, hits, walk.Point{X: 150, Y: 30}) != "pwm.1" {
		t.Fatal("compatible drop rejected")
	}
	if controllerDropTarget(g, hits, walk.Point{X: 250, Y: 30}) != "" {
		t.Fatal("cross-kind drop accepted")
	}
	s := &Shell{controllerGesture: g}
	s.cancelControllerGestureToken(1)
	if s.controllerGesture.key != "pwm.0" {
		t.Fatal("stale capture callback cancelled new drag")
	}
	s.cancelControllerGestureToken(2)
	if s.controllerGesture.key != "" {
		t.Fatal("capture loss retained drag")
	}
}
