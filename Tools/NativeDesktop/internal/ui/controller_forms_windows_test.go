//go:build windows

package ui

import (
	"pccontroller.local/desktop/presentation"
	"strings"
	"testing"
)

func TestControllerFormPreservesAndRejectsInvalidNumericDrafts(t *testing.T) {
	fields := []presentation.Field{{Key: "frequency", Label: "Frequency", Numeric: true, Min: 20, Max: 20000}}
	for _, value := range []string{"", "invalid", "NaN", "+Inf", "19", "20001", "880.5"} {
		if validateControllerFormFields(fields, map[string]string{"frequency": value}) == nil {
			t.Fatalf("invalid draft %q accepted", value)
		}
	}
	if err := validateControllerFormFields(fields, map[string]string{"frequency": "880"}); err != nil {
		t.Fatal(err)
	}
}

func TestControllerFormSectionsAndUpdateConfirmation(t *testing.T) {
	fields := []presentation.Field{{Key: "a", Section: "One"}, {Key: "b", Section: "Two"}, {Key: "c", Section: "One"}}
	ordered := controllerFormOrderedFields(fields)
	if ordered[0].Key != "a" || ordered[1].Key != "c" || ordered[2].Key != "b" {
		t.Fatalf("sections split: %+v", ordered)
	}
	for _, kind := range []string{"firmware", "eeprom", "host-executable"} {
		message := controllerFormConfirmation("updates", presentation.PageAction{ID: "apply"}, map[string]string{"selected_kind": kind, "selected": "verified-digest"})
		if !strings.Contains(message, kind) || !strings.Contains(message, "verified-digest") {
			t.Fatalf("incomplete confirmation: %s", message)
		}
		if kind == "eeprom" && !strings.Contains(message, "RF assignments") {
			t.Fatal("EEPROM replacement warning missing")
		}
	}
}

func TestControllerFormActionDoesNotValidateUnrelatedTabs(t *testing.T) {
	fields := []presentation.Field{{Key: "tone", Label: "Frequency", Section: "Tone", Numeric: true, Min: 20, Max: 20000}, {Key: "repeats", Label: "Repeats", Section: "Melody", Numeric: true, Min: 1, Max: 255}}
	v := map[string]string{"tone": "invalid", "repeats": "2"}
	if err := validateControllerFormAction(fields, presentation.PageAction{Section: "Melody"}, v); err != nil {
		t.Fatal("unrelated invalid draft blocked melody action", err)
	}
	if err := validateControllerFormAction(fields, presentation.PageAction{}, v); err == nil {
		t.Fatal("whole-form save accepted invalid numeric draft")
	}
}

func TestControllerConditionalFieldsRetainHiddenDrafts(t *testing.T) {
	fields := []presentation.Field{{Key: "rf_code", Numeric: true, Min: 1, Max: 4294967295, WhenKey: "kind", WhenValues: []string{"rf"}}, {Key: "end", Numeric: true, Min: 0, Max: 4095, OptionalNumeric: true}}
	v := map[string]string{"kind": "pwm", "rf_code": "invalid", "end": ""}
	if err := validateControllerFormFields(fields, v); err != nil {
		t.Fatal("irrelevant hidden draft blocked another type", err)
	}
	v["kind"] = "rf"
	if err := validateControllerFormFields(fields, v); err == nil {
		t.Fatal("visible invalid RF draft accepted")
	}
	v["rf_code"] = "123"
	if err := validateControllerFormFields(fields, v); err != nil {
		t.Fatal(err)
	}
}

func TestControllerActivitiesHaveUniqueCompleteIdentities(t *testing.T) {
	seen := map[string]bool{}
	for _, activity := range controllerActivities {
		if activity.key == "" || activity.group == "" || activity.title == "" || seen[activity.key] {
			t.Fatalf("invalid activity: %+v", activity)
		}
		seen[activity.key] = true
	}
	for _, key := range []string{"board", "menus", "host", "terminal", "diagnostics", "rf", "firmware", "events"} {
		if !seen[key] {
			t.Fatalf("missing activity %s", key)
		}
	}
	if actionActivities >= actionChannelFirst {
		t.Fatal("activity launcher collides with channel action IDs")
	}
}
