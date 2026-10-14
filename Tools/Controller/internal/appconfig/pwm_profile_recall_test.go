package appconfig

import (
	"encoding/json"
	"testing"
)

func TestPWMProfileRecallSurvivesConfigRoundTripAndIsIsolated(t *testing.T) {
	config := Defaults()
	config.BoardProfiles = map[string]BoardProfile{
		"port:one": {Key: "one", Mode: BoardModeOrdinaryRelays, PWMRecall: map[string]uint16{"pwm.0": 239}},
		"port:two": {Key: "two", Mode: BoardModeOrdinaryRelays, PWMRecall: map[string]uint16{"pwm.0": 984}},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var reopened Config
	if err := json.Unmarshal(encoded, &reopened); err != nil {
		t.Fatal(err)
	}
	if PWMChannelForProfile(reopened, "port:one", 0).PersistedRecallRaw != 239 || PWMChannelForProfile(reopened, "port:two", 0).PersistedRecallRaw != 984 || PWMChannelForProfile(reopened, "port:unknown", 0).PersistedRecallRaw != 0 {
		t.Fatal("recall crossed board profiles")
	}
	copyProfiles := cloneBoardProfiles(reopened.BoardProfiles)
	copyProfiles["port:one"].PWMRecall["pwm.0"] = 1
	if reopened.BoardProfiles["port:one"].PWMRecall["pwm.0"] != 239 {
		t.Fatal("clone mutated original recall")
	}
}
