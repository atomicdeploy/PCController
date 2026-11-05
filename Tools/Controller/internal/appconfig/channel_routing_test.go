package appconfig

import (
	"encoding/json"
	"testing"
)

func routingFixture() ChannelRouting {
	return ChannelRouting{Enabled: true, Channels: []SemanticChannel{
		{Key: "lighting.aisle", Kind: "relay", Target: "relay.5", Name: "Aisle", Icon: "lamp"},
		{Key: "lighting.screen", Kind: "relay", Target: "relay.6", Name: "Screen", Icon: "stage-light"},
	}}
}

func TestSemanticRoutingAtomicSwapPreservesIdentity(t *testing.T) {
	original, next := routingFixture(), routingFixture()
	next.Channels[0].Target, next.Channels[1].Target = next.Channels[1].Target, next.Channels[0].Target
	if err := ValidateRoutingReplacement(original, next); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveSemanticChannel(next, "lighting.aisle", 1)
	if err != nil || got.Target != "relay.6" || got.Name != "Aisle" || got.Icon != "lamp" {
		t.Fatalf("resolved=%+v err=%v", got, err)
	}
	if got, _ := ResolveSemanticChannel(original, "lighting.aisle", 1); got.Target != "relay.5" {
		t.Fatal("source mutated")
	}
	profile := BoardProfile{Key: "cinema", Mode: BoardModeCinemaSeatMotion, ChannelRouting: next}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BoardProfile
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if BoardProfileRevision("serial:test", profile, nil) != BoardProfileRevision("serial:test", decoded, nil) {
		t.Fatal("routing did not round trip")
	}
	if BoardProfileRevision("serial:test", profile, nil) == BoardProfileRevision("serial:test", BoardProfile{Key: "cinema", Mode: BoardModeCinemaSeatMotion, ChannelRouting: original}, nil) {
		t.Fatal("swap did not invalidate profile revision")
	}
	cloned := cloneBoardProfiles(map[string]BoardProfile{"serial:test": profile})
	cloned["serial:test"].ChannelRouting.Channels[0].Target = "relay.7"
	if profile.ChannelRouting.Channels[0].Target != "relay.6" {
		t.Fatal("cloned profile shares mutable binding storage")
	}
}

func TestSemanticRoutingRejectsUnsafeOrAmbiguousBindings(t *testing.T) {
	cases := map[string]func(*ChannelRouting){
		"duplicate key":       func(v *ChannelRouting) { v.Channels[1].Key = v.Channels[0].Key },
		"duplicate target":    func(v *ChannelRouting) { v.Channels[1].Target = v.Channels[0].Target },
		"reserved key":        func(v *ChannelRouting) { v.Channels[0].Key = "relay.5" },
		"interlocked target":  func(v *ChannelRouting) { v.Channels[0].Target = "relay.1" },
		"autonomous target":   func(v *ChannelRouting) { v.Channels[0].Kind = "pwm"; v.Channels[0].Target = "pwm.11" },
		"type mismatch":       func(v *ChannelRouting) { v.Channels[0].Target = "pwm.0" },
		"noncanonical target": func(v *ChannelRouting) { v.Channels[0].Target = "relay.05" },
		"blank name":          func(v *ChannelRouting) { v.Channels[0].Name = " " },
		"control icon":        func(v *ChannelRouting) { v.Channels[0].Icon = "lamp\n" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			value := routingFixture()
			mutate(&value)
			if err := ValidateChannelRouting(value); err == nil {
				t.Fatal("invalid routing accepted")
			}
		})
	}
	value := routingFixture()
	value.Channels[0].Kind, value.Channels[0].Target = "pwm", "pwm.0"
	if err := ValidateRoutingReplacement(routingFixture(), value); err == nil {
		t.Fatal("stable key silently changed electrical type")
	}
	value = routingFixture()
	value.Enabled = false
	if _, err := ResolveSemanticChannel(value, "lighting.aisle", 1); err == nil {
		t.Fatal("disabled routing executed")
	}
	if _, err := ResolveSemanticChannel(routingFixture(), "missing", 1); err == nil {
		t.Fatal("unknown key executed")
	}
	if _, err := ResolveSemanticChannel(routingFixture(), "lighting.aisle", 2); err == nil {
		t.Fatal("invalid relay value executed")
	}
}
