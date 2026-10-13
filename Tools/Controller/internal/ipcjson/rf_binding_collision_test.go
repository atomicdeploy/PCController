package ipcjson

import (
	"context"
	"encoding/json"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
)

func rfCollisionRule(name string, code uint32, verb string) appconfig.Automation {
	return appconfig.Automation{Name: name, Enabled: true, Match: appconfig.AutomationMatch{
		Kind: "rf.gesture", Source: "rf", RFCode: &code, RFBits: 24, RFProtocol: 1, Gesture: "down",
	}, Actions: []appconfig.AutomationAction{{Type: "control", ActionID: "pwm.1." + verb}}}
}

func putCollisionRule(service *Service, rule appconfig.Automation, previous string, keyboard bool) Response {
	raw, _ := json.Marshal(map[string]any{"binding": rule, "previous_name": previous, "allow_keyboard": keyboard})
	return service.Dispatch(context.Background(), Request{Method: "controller.rf.binding.put", Params: raw})
}

func TestRFCreateNameCollisionNeverReplacesConfigOrGrantsKeyboard(t *testing.T) {
	for _, name := range []string{"Lighting", "lighting", " Lighting "} {
		service, config := rfBindingTestService(t)
		config.Automations = []appconfig.Automation{rfCollisionRule("Lighting", 42, "on")}
		before, _ := json.Marshal(config)
		commits := 0
		update := service.UpdateHostConfig
		service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
			err := update(change)
			if err == nil {
				commits++
			}
			return err
		}
		draft := rfCollisionRule(name, 43, "off")
		draft.Actions = append(draft.Actions, appconfig.AutomationAction{Type: "virtual-key", VirtualKey: "F23", HoldMS: 50})
		response := putCollisionRule(service, draft, "", true)
		if response.Error == nil || response.Error.Code != -32009 {
			t.Fatalf("collision accepted: %+v", response)
		}
		data, ok := response.Error.Data.(map[string]any)
		if !ok || data["kind"] != "conflict" || data["resource"] != "rf_binding" || data["reason"] != "name_exists" {
			t.Fatal("missing typed conflict")
		}
		after, _ := json.Marshal(config)
		if commits != 0 || string(before) != string(after) {
			t.Fatal("collision replaced assignment or changed keyboard policy")
		}
	}
}

func TestRFExplicitEditEnableAndRenamePreserveOtherBindings(t *testing.T) {
	service, config := rfBindingTestService(t)
	first := rfCollisionRule("Lighting On", 42, "on")
	second := rfCollisionRule("Lighting Off", 43, "off")
	config.Automations = []appconfig.Automation{first, second}
	before, _ := json.Marshal(config)
	for _, test := range []struct {
		rule             appconfig.Automation
		previous, reason string
	}{
		{rfCollisionRule("LIGHTING OFF", 44, "increase"), first.Name, "name_exists"},
		{rfCollisionRule("New name", 44, "increase"), "Removed name", "previous_missing"},
	} {
		response := putCollisionRule(service, test.rule, test.previous, false)
		if response.Error == nil || response.Error.Data.(map[string]any)["reason"] != test.reason {
			t.Fatalf("wrong rejection %+v", response)
		}
		after, _ := json.Marshal(config)
		if string(before) != string(after) {
			t.Fatal("rejected edit mutated config")
		}
	}
	first.Enabled = false
	if response := putCollisionRule(service, first, first.Name, false); response.Error != nil {
		t.Fatal(response.Error)
	}
	if len(config.Automations) != 2 || config.Automations[1].Enabled || config.Automations[0].Name != second.Name {
		t.Fatal("explicit enable edit lost another binding")
	}
	first.Name = "Lighting renamed"
	if response := putCollisionRule(service, first, "Lighting On", false); response.Error != nil {
		t.Fatal(response.Error)
	}
	if len(config.Automations) != 2 || config.Automations[0].Name != second.Name || config.Automations[1].Name != first.Name {
		t.Fatal("explicit rename lost another binding")
	}
	first.Enabled = true
	if response := putCollisionRule(service, first, first.Name, false); response.Error != nil {
		t.Fatal(response.Error)
	}
	if len(config.Automations) != 2 || !config.Automations[1].Enabled || config.Automations[0].Name != second.Name {
		t.Fatal("explicit enable did not preserve other assignment")
	}
}

func TestRFNameCollisionCannotReplaceNonRFAutomation(t *testing.T) {
	service, config := rfBindingTestService(t)
	config.Automations = []appconfig.Automation{{Name: "Other automation", Enabled: true, Match: appconfig.AutomationMatch{Kind: "host.ready"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "ready"}}}}
	before, _ := json.Marshal(config)
	for _, test := range []struct{ name, previous string }{{"OTHER AUTOMATION", ""}, {"New RF name", "Other automation"}} {
		response := putCollisionRule(service, rfCollisionRule(test.name, 42, "on"), test.previous, false)
		if response.Error == nil || response.Error.Code != -32009 || response.Error.Data.(map[string]any)["reason"] != "not_rf" {
			t.Fatal("non-RF target accepted")
		}
		after, _ := json.Marshal(config)
		if string(before) != string(after) {
			t.Fatal("non-RF automation mutated")
		}
	}
}

func TestRFFourDistinctPWMKeysArePreservedAndConcurrentCollisionRejected(t *testing.T) {
	service, config := rfBindingTestService(t)
	for i, verb := range []string{"on", "off", "increase", "decrease"} {
		if response := putCollisionRule(service, rfCollisionRule("Lighting "+verb, uint32(42+i), verb), "", false); response.Error != nil {
			t.Fatal(response.Error)
		}
	}
	if len(config.Automations) != 4 {
		t.Fatal("four assignments were not preserved")
	}
	update := service.UpdateHostConfig
	service.UpdateHostConfig = func(change func(*appconfig.Config) error) error {
		config.Automations = append(config.Automations, rfCollisionRule("Concurrent", 99, "on"))
		return update(change)
	}
	response := putCollisionRule(service, rfCollisionRule("Concurrent", 100, "off"), "", false)
	if response.Error == nil || len(config.Automations) != 5 || *config.Automations[4].Match.RFCode != 99 || config.Automations[4].Actions[0].ActionID != "pwm.1.on" {
		t.Fatal("stale create overwrote concurrent assignment")
	}
}
