package ipcjson

import (
	"context"
	"encoding/json"
	"pccontroller.local/controller/internal/appconfig"
	"strings"
	"testing"
)

func TestAuthoringCollectionsCASValidationNoOpAndPolicy(t *testing.T) {
	s, c, updates := appearanceTestService(t)
	rule := appconfig.Automation{Name: "Notify", Match: appconfig.AutomationMatch{Kind: "door"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "door.seen"}}}
	c.Automations = []appconfig.Automation{rule}
	call := func(kind string, items any, revision string) Response {
		raw, _ := json.Marshal(map[string]any{"items": items, "if_match": revision})
		return s.Dispatch(context.Background(), Request{Method: "controller." + kind + ".set", Params: raw})
	}
	doc := authoringDocument(c.Automations)
	if r := call("automations", doc.Items, doc.ETag); r.Error != nil || *updates != 0 {
		t.Fatalf("no-op: %+v", r.Error)
	}
	rule.Name = "Renamed"
	if r := call("automations", []appconfig.Automation{rule}, doc.ETag); r.Error != nil || *updates != 1 || c.Automations[0].Name != "Renamed" {
		t.Fatalf("save: %+v", r.Error)
	}
	if r := call("automations", doc.Items, doc.ETag); r.Error == nil || *updates != 1 {
		t.Fatal("stale draft accepted")
	}
	doc = authoringDocument(c.Automations)
	rule.Actions = []appconfig.AutomationAction{{Type: "power", Power: "shutdown"}}
	if r := call("automations", []appconfig.Automation{rule}, doc.ETag); r.Error == nil || *updates != 1 {
		t.Fatal("OS policy bypassed")
	}
	if c.Automations[0].Actions[0].Type != "emit" {
		t.Fatal("invalid action leaked")
	}
	melodies := authoringDocument(c.Melodies)
	m := appconfig.Melody{Name: "Custom", Notes: []appconfig.MelodyNote{{FrequencyHz: 440, DurationMS: 100, GapMS: 25}, {FrequencyHz: 0, DurationMS: 50}}}
	if r := call("melodies", []appconfig.Melody{m}, melodies.ETag); r.Error != nil || c.Melodies[0].Notes[0].FrequencyHz != 440 {
		t.Fatalf("melody save: %+v", r.Error)
	}
	m.Notes[0].DurationMS = 0
	before := *updates
	if r := call("melodies", []appconfig.Melody{m}, authoringDocument(c.Melodies).ETag); r.Error == nil || *updates != before {
		t.Fatal("invalid note accepted")
	}
	for _, kind := range []string{"automations", "melodies"} {
		if requestCapability("controller."+kind+".get", nil) != capabilityRead || requestCapability("controller."+kind+".set", nil) != capabilityHostConfig {
			t.Fatal("wrong capability")
		}
		if r := call(kind, []any{}, ""); r.Error == nil {
			t.Fatal("unfenced replacement")
		}
	}
	if !bridgeIngressCanPivot("controller.automations.set", nil) {
		t.Fatal("bridge could inject persistent host actions")
	}
}
func TestAuthoringCollectionRechecksWriterAndRejectsAmbiguousRF(t *testing.T) {
	s, c, _ := appearanceTestService(t)
	c.Automations = []appconfig.Automation{}
	code := uint32(42)
	r := appconfig.Automation{Name: "RF A", Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", Gesture: "tap", RFCode: &code, RFBits: 24, RFProtocol: 1}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "rf.seen"}}}
	b := r
	b.Name = "RF B"
	raw, _ := json.Marshal(map[string]any{"items": []appconfig.Automation{r, b}, "if_match": authoringDocument(c.Automations).ETag})
	if _, err := s.updateAutomations(raw); err == nil || !strings.Contains(err.Error(), "already assigned") {
		t.Fatalf("ambiguous enabled RF: %v", err)
	}
	raw, _ = json.Marshal(map[string]any{"items": []appconfig.Automation{r}, "if_match": authoringDocument(c.Automations).ETag})
	original := s.UpdateHostConfig
	s.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		c.Automations = []appconfig.Automation{{Name: "Other author", Match: appconfig.AutomationMatch{Kind: "door"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "other"}}}}
		return original(change)
	}
	if _, err := s.updateAutomations(raw); err == nil {
		t.Fatal("store race accepted")
	}
	if c.Automations[0].Name != "Other author" {
		t.Fatal("other author's rule lost")
	}
}

func TestAuthoringSavePreservesDisabledRFOutsideCurrentProfile(t *testing.T) {
	s, c, _ := appearanceTestService(t)
	code := uint32(123)
	old := appconfig.Automation{Name: "Lantern disabled", Enabled: false, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", Gesture: "hold", RFCode: &code, RFBits: 24, RFProtocol: 1}, Actions: []appconfig.AutomationAction{{Type: "control", ActionID: "retired-channel.hold"}}}
	c.Automations = []appconfig.Automation{old}
	rules := append([]appconfig.Automation(nil), c.Automations...)
	rules = append(rules, appconfig.Automation{Name: "New host rule", Match: appconfig.AutomationMatch{Kind: "door"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "door.seen"}}})
	raw, _ := json.Marshal(map[string]any{"items": rules, "if_match": authoringDocument(c.Automations).ETag})
	if _, err := s.updateAutomations(raw); err != nil {
		t.Fatal(err)
	}
	if c.Automations[0].Enabled || c.Automations[0].Actions[0].ActionID != "retired-channel.hold" {
		t.Fatal("disabled RF rule changed")
	}
}
