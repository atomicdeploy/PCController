package fleet

import "testing"

func TestMirrorRGBDoesNotApplyBrightnessTwice(t *testing.T) {
	command := MirrorCommand{Kind: "status_led", RGB: [3]byte{60, 15, 5}, Brightness: 255}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	command.Brightness = 128
	if command.Validate() == nil {
		t.Fatal("already-scaled mirror color would be dimmed twice")
	}
}

func TestMirrorPolicyRejectsMotionRawAndAmbiguousFields(t *testing.T) {
	for _, command := range []MirrorCommand{{Kind: "relay", Channel: 1, Active: true}, {Kind: "i2c"}, {Kind: "reset"}, {Kind: "pwm", Value: 4096}, {Kind: "buzzer", FrequencyHz: 440}, {Kind: "menu", Page: 1, Active: true}, {Kind: "lcd", Lines: [2]string{"\n", ""}}} {
		if command.Validate() == nil {
			t.Fatalf("unsafe command accepted: %+v", command)
		}
	}
	policy := MirrorPolicy{Domains: []string{"pwm", "relay"}, PWMChannels: []int{0}, Relays: []int{5}}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if !policy.Allows(MirrorCommand{Kind: "pwm", Value: 123}) || policy.Allows(MirrorCommand{Kind: "pwm", Channel: 1}) || policy.Allows(MirrorCommand{Kind: "relay", Channel: 1}) {
		t.Fatal("channel allowlist bypass")
	}
}
func TestMirrorConfigRequiresExplicitOwnerIdentityAndReceiver(t *testing.T) {
	for _, config := range []MirrorConfig{
		{Receive: MirrorPolicy{Domains: []string{"menu"}}},
		{LocalBoardID: "one", CoordinatorID: "owner", Groups: []MirrorGroup{{ID: "pair", Members: []MirrorMember{{BoardID: "one"}, {BoardID: "one", Peer: "remote"}}}}},
		{LocalBoardID: "one", CoordinatorID: "owner", Groups: []MirrorGroup{{ID: "pair", Members: []MirrorMember{{BoardID: "one"}, {BoardID: "two"}}}}},
	} {
		if config.Validate() == nil {
			t.Fatalf("ambiguous config accepted: %+v", config)
		}
	}
	if err := (MirrorConfig{}).Validate(); err != nil {
		t.Fatal("disabled configuration not accepted:", err)
	}
}
