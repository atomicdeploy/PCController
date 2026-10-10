package appconfig

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestChannelFoldersLifecyclePreservesHiddenMembersAndOtherMetadata(t *testing.T) {
	profile := BoardProfile{Key: "cinema", Mode: BoardModeCinemaSeatMotion,
		Presentation: map[string]PeripheralPresentation{
			"pwm.0":   {Name: "Accent", Group: "Lighting", Locked: true},
			"pwm.13":  {Group: "Lighting", Hidden: true, Color: "#123456"},
			"relay.5": {Group: "Lighting", Name: "Fan"},
		}}
	name, icon := "Accent lights", "lightbulb"
	keys, err := ApplyChannelFolderMutation(&profile, nil, ChannelFolderMutation{Operation: "update", Kind: "pwm", Name: "Lighting", NextName: &name, Icon: &icon})
	if err != nil || len(keys) != 2 {
		t.Fatalf("rename: keys=%v error=%v", keys, err)
	}
	if profile.Presentation["pwm.0"].Group != name || !profile.Presentation["pwm.0"].Locked || !profile.Presentation["pwm.13"].Hidden || profile.Presentation["pwm.13"].Color != "#123456" || profile.Presentation["relay.5"].Group != "Lighting" {
		t.Fatal("rename changed unrelated metadata/section")
	}
	if _, err = ApplyChannelFolderMutation(&profile, nil, ChannelFolderMutation{Operation: "create", Kind: "pwm", Name: "Empty"}); err != nil {
		t.Fatal(err)
	}
	if _, err = ApplyChannelFolderMutation(&profile, nil, ChannelFolderMutation{Operation: "move", Kind: "pwm", Name: "Empty", Keys: []string{"pwm.0", "pwm.14"}}); err != nil {
		t.Fatal(err)
	}
	if !profile.Presentation["pwm.14"].Hidden {
		t.Fatal("moving a default-hidden status channel unhid it")
	}
	if _, err = ApplyChannelFolderMutation(&profile, nil, ChannelFolderMutation{Operation: "delete", Kind: "pwm", Name: "Empty"}); err != nil {
		t.Fatal(err)
	}
	if profile.Presentation["pwm.0"].Group != "" || !profile.Presentation["pwm.0"].Locked || !profile.Presentation["pwm.14"].Hidden {
		t.Fatal("delete must only ungroup members")
	}
	encoded, _ := json.Marshal(profile)
	var restored BoardProfile
	if err = json.Unmarshal(encoded, &restored); err != nil || !reflect.DeepEqual(profile, restored) {
		t.Fatal("folder profile did not round trip")
	}
}

func TestChannelFoldersRejectInvalidTransactionsWithoutChangingProfile(t *testing.T) {
	profile := BoardProfile{Key: "cinema", Mode: BoardModeCinemaSeatMotion, ExposeRawRelays: true,
		Folders:      []ChannelFolder{{Kind: "pwm", Name: "Lighting"}, {Kind: "pwm", Name: "Spare"}},
		Presentation: map[string]PeripheralPresentation{"pwm.0": {Group: "Lighting"}}}
	duplicate := "spare"
	for _, change := range []ChannelFolderMutation{
		{Operation: "create", Kind: "pwm", Name: " lighting "},
		{Operation: "create", Kind: "unknown", Name: "Folder"},
		{Operation: "create", Kind: "pwm", Name: ""},
		{Operation: "create", Kind: "relay", Name: "Raw relays"},
		{Operation: "update", Kind: "pwm", Name: "Lighting", NextName: &duplicate},
		{Operation: "move", Kind: "pwm", Name: "Spare", Keys: []string{"pwm.0", "relay.5"}},
		{Operation: "move", Kind: "relay", Name: "", Keys: []string{"relay.1"}},
		{Operation: "move", Kind: "pwm", Name: "Missing", Keys: []string{"pwm.0"}},
		{Operation: "move", Kind: "pwm", Name: "Spare", Keys: []string{"pwm.0", "pwm.0"}},
		{Operation: "delete", Kind: "pwm", Name: "Missing"},
	} {
		before, _ := json.Marshal(profile)
		if _, err := ApplyChannelFolderMutation(&profile, nil, change); err == nil {
			t.Fatalf("accepted invalid change: %+v", change)
		}
		after, _ := json.Marshal(profile)
		if string(before) != string(after) {
			t.Fatalf("failed transaction mutated profile: %+v", change)
		}
	}
}

func TestChannelFoldersCloneEmptyFoldersAndRevision(t *testing.T) {
	profile := BoardProfile{Key: "ordinary", Mode: BoardModeOrdinaryRelays, Folders: []ChannelFolder{{Kind: "relay", Name: "Spare"}}}
	before := BoardProfileRevision("board", profile, nil)
	cloned := cloneBoardProfiles(map[string]BoardProfile{"board": profile})
	cloned["board"].Folders[0].Name = "Changed"
	if profile.Folders[0].Name != "Spare" {
		t.Fatal("folder clone shares storage")
	}
	if before == BoardProfileRevision("board", cloned["board"], nil) {
		t.Fatal("folder edit must change profile revision")
	}
	if len(ChannelFolderCatalog(profile, nil)) != 1 {
		t.Fatal("empty folder disappeared")
	}
	if _, err := ApplyChannelFolderMutation(&profile, nil, ChannelFolderMutation{Operation: "move", Kind: "relay", Name: "Spare", Keys: []string{"relay.1"}}); err != nil {
		t.Fatal("ordinary relay must remain movable:", err)
	}
}
