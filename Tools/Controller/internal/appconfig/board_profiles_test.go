package appconfig

import (
	"fmt"
	"testing"
)

func TestResolveBoardIdentityPrefersProvisionedSerial(t *testing.T) {
	identity := ResolveBoardIdentity(" BOARD-42 ", `USB\VID_1A86&PID_7523\PATH`, "COM18")
	if identity.Value != "serial:board-42" || identity.Source != "usb-serial" || !identity.Stable {
		t.Fatalf("identity=%+v", identity)
	}
	fallback := ResolveBoardIdentity("", ` USB\PATH `, "COM18")
	if fallback.Value != `instance:usb\path` || fallback.Source != "usb-instance" || fallback.Stable {
		t.Fatalf("fallback=%+v", fallback)
	}
}

func TestProfileDescriptorsAdvertiseOnlyConfiguredWiring(t *testing.T) {
	legacy := map[string]string{"motion.a": "Left legacy", "relay.5": "Lamp"}
	presentation := map[string]PeripheralPresentation{
		"seat.a": {Name: "Left seats", Icon: "seat", Group: "auditorium", Hidden: true, Locked: true},
	}
	_, cinema := ProfileDescriptors(BoardModeCinemaSeatMotion, false, legacy, presentation)
	if len(cinema) != 17 {
		t.Fatalf("cinema controls=%d, want 17", len(cinema))
	}
	seenSeat, seenLegacy, seenRelayOne := false, false, false
	for _, control := range cinema {
		switch control.Key {
		case "seat.a":
			seenSeat = control.Name == "Left seats" && control.Icon == "seat" && control.Group == "auditorium" &&
				control.Hidden && control.Locked &&
				len(control.Actions) == 3 && control.Actions[0].ID == "seat.a.up"
		case "motion.a":
			seenLegacy = true
		case "relay.1":
			seenRelayOne = true
		}
	}
	if !seenSeat || seenLegacy || seenRelayOne {
		t.Fatalf("cinema controls=%+v", cinema)
	}

	_, rawCinema := ProfileDescriptors(BoardModeCinemaSeatMotion, true, legacy, presentation)
	if len(rawCinema) != 21 {
		t.Fatalf("raw cinema controls=%d, want 21", len(rawCinema))
	}
	for relay := 1; relay <= 4; relay++ {
		key := fmt.Sprintf("relay.%d", relay)
		found := false
		for _, control := range rawCinema {
			if control.Key == key {
				found = control.Kind == "relay" && control.Control == "seat-internal" && len(control.Actions) == 2
				break
			}
		}
		if !found {
			t.Fatalf("raw cinema control %s missing from %+v", key, rawCinema)
		}
	}

	_, ordinary := ProfileDescriptors(BoardModeOrdinaryRelays, false, legacy, nil)
	if len(ordinary) != 19 || ordinary[0].Key != "relay.1" || len(ordinary[0].Actions) != 2 || ordinary[0].Actions[0].ID != "relay.1.on" {
		t.Fatalf("ordinary controls=%+v", ordinary)
	}
	_, unconfigured := ProfileDescriptors(BoardModeUnconfigured, false, legacy, nil)
	if len(unconfigured) != 15 {
		t.Fatalf("unconfigured controls=%d, want 15", len(unconfigured))
	}
}

func TestBoardProfileValidationAndRevision(t *testing.T) {
	config := Defaults()
	config.BoardProfiles = map[string]BoardProfile{
		"serial:board-42": {
			Key: "cafe-cinema", Mode: BoardModeCinemaSeatMotion,
			Presentation: map[string]PeripheralPresentation{"seat.a": {Name: "Left seats"}},
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	profile := config.BoardProfiles["serial:board-42"]
	first := BoardProfileRevision("serial:board-42", profile, config.UI.PeripheralNames)
	profile.Presentation["seat.a"] = PeripheralPresentation{Name: "VIP left"}
	second := BoardProfileRevision("serial:board-42", profile, config.UI.PeripheralNames)
	if first == second || len(first) != 24 || len(second) != 24 {
		t.Fatalf("revisions first=%q second=%q", first, second)
	}
	profile.Presentation["seat.a"] = PeripheralPresentation{Name: "VIP left", Hidden: true, Locked: true}
	presentationRevision := BoardProfileRevision("serial:board-42", profile, config.UI.PeripheralNames)
	if second == presentationRevision || len(presentationRevision) != 24 {
		t.Fatalf("presentation revision second=%q updated=%q", second, presentationRevision)
	}
	profile.ExposeRawRelays = true
	third := BoardProfileRevision("serial:board-42", profile, config.UI.PeripheralNames)
	if presentationRevision == third || len(third) != 24 {
		t.Fatalf("raw-relay revision presentation=%q third=%q", presentationRevision, third)
	}
	profile.Mode = "unknown"
	config.BoardProfiles["serial:board-42"] = profile
	if err := config.Validate(); err == nil {
		t.Fatal("invalid board mode accepted")
	}
}
