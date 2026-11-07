//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
	"reflect"
	"testing"
)

func nativeTestValues(p desktop.Page) map[string]string {
	v := map[string]string{}
	for _, f := range p.Fields {
		v[f.Key] = f.Value
	}
	return v
}

func TestNativeFixedWidthFormsRejectOverflowBeforeConversion(t *testing.T) {
	for _, raw := range []string{"255", " 255 ", "+255"} {
		got, err := nativeFormUint8(map[string]string{"n": raw}, "n", 0, 255)
		if err != nil || got != 255 {
			t.Fatalf("byte boundary %q: %d %v", raw, got, err)
		}
	}
	for _, raw := range []string{"65535", " 65535 ", "+65535"} {
		got, err := nativeFormUint16(map[string]string{"n": raw}, "n", 0, 65535)
		if err != nil || got != 65535 {
			t.Fatalf("word boundary %q: %d %v", raw, got, err)
		}
	}
	for _, raw := range []string{"256", "65536", "-1", "1.5", "", "18446744073709551616"} {
		if _, err := nativeFormUint8(map[string]string{"n": raw}, "n", 0, 255); err == nil {
			t.Fatalf("byte accepted %q", raw)
		}
	}
	for _, raw := range []string{"65536", "-1", "1.5", "", "18446744073709551616"} {
		if _, err := nativeFormUint16(map[string]string{"n": raw}, "n", 0, 65535); err == nil {
			t.Fatalf("word accepted %q", raw)
		}
	}
	if _, err := nativeFormUint8(map[string]string{"n": "33"}, "n", 1, 32); err == nil {
		t.Fatal("RF bit limit lost")
	}
	if _, err := nativeFormUint16(map[string]string{"n": "5001"}, "n", 1, 5000); err == nil {
		t.Fatal("note timing limit lost")
	}
}

func TestNativeControlFixtureDocuments(t *testing.T) {
	directory := os.Getenv("PCCONTROLLER_NATIVE_PAGE_FIXTURES")
	if directory == "" {
		t.Skip("native renderer fixture export disabled")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	rule := appconfig.Automation{Name: "Window hold", Enabled: true, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", Gesture: "hold"}, Actions: []appconfig.AutomationAction{{Type: "control", ActionID: "pwm.0.on"}, {Type: "emit", Event: "window.changed"}}}
	code := uint32(123456)
	rule.Match.RFCode = &code
	rule.Match.RFBits = 24
	rule.Match.RFProtocol = 1
	automation := nativeRulePage(nativeAuthoringCollection[appconfig.Automation]{ETag: "fixture"}, rule, rule.Name, false)
	automation.SelectedRow = "0"
	melody := nativeMelodyPage(nativeAuthoringCollection[appconfig.Melody]{ETag: "fixture"}, appconfig.Melody{Name: "Notify", Notes: []appconfig.MelodyNote{{FrequencyHz: 1047, DurationMS: 75, GapMS: 25}, {FrequencyHz: 1319, DurationMS: 75, GapMS: 25}, {FrequencyHz: 1568, DurationMS: 150}}}, "Notify", false)
	melody.SelectedRow = "0"
	catalog := nativePeripheralCatalog{BoardProfile: nativeProfileDescriptor{Attached: true, Configured: true, Revision: "fixture"}, Folders: []appconfig.ChannelFolder{{Kind: "pwm", Name: "Lighting", Icon: "sun"}}}
	catalog.Controls = []appconfig.ControlDescriptor{{Key: "pwm.0", Name: "Window", Kind: "mosfet", Actions: []appconfig.ActionDescriptor{{ID: "pwm.0.off", Name: "Off"}, {ID: "pwm.0.on", Name: "On"}}}}
	adapter := nativePages{rpc: func(_ context.Context, m string, params, result any) error {
		if m != "controller.peripherals.get" {
			t.Fatalf("unexpected fixture RPC: %s", m)
		}
		data, _ := json.Marshal(catalog)
		return json.Unmarshal(data, result)
	}}
	automation, err := adapter.decorateNativeRulePage(context.Background(), automation)
	if err != nil {
		t.Fatal(err)
	}
	peripheral := nativePeripheralPage(catalog, appconfig.PeripheralDescriptor{Key: "pwm.0", Name: "Window", Kind: "pwm", Group: "Lighting", Icon: "blinds", Index: 0}, false)
	p := nativePages{preview: true}
	lcd, err := p.loadNativeLCD(context.Background(), control.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	for key, page := range map[string]desktop.Page{"automation": automation, "melody": melody, "peripheral": peripheral, "lcd": lcd} {
		data, err := json.MarshalIndent(page, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, key+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestNativeAutomationAllActionTypesRoundTrip(t *testing.T) {
	for _, a := range []appconfig.AutomationAction{{Type: "control", ActionID: "pwm.1.toggle"}, {Type: "board", Command: "relay 5 on"}, {Type: "macro", Macro: "effect:2"}, {Type: "effect", Macro: "strip:1"}, {Type: "host", Executable: "C:\\Tools\\worker.exe", Args: []string{"", "a b", " فارسی "}, Detached: true}, {Type: "script", Script: "saved"}, {Type: "emit", Event: "my.event"}, {Type: "virtual-key", VirtualKey: "F13", HoldMS: 50}, {Type: "vk", VirtualKey: "F14"}, {Type: "power", Power: "lock", Confirm: "token"}, {Type: "app", AppKind: "playback", AppTarget: "selected", AppValue: "pause"}, {Type: "rf", RF: &appconfig.RFTransmit{Code: 4294967295, Bits: 32, Protocol: 12, PulseUS: 350, Repeats: 20}}} {
		got, err := nativeActionDraft(nativeActionValues(a), a)
		if err != nil || !reflect.DeepEqual(got, a) {
			t.Fatalf("action lost data: %+v -> %+v (%v)", a, got, err)
		}
	}
	v := nativeActionValues(appconfig.AutomationAction{Type: "host", Executable: "old", Args: []string{"old"}})
	v["action_type"] = "emit"
	v["event"] = "new"
	got, err := nativeActionDraft(v, appconfig.AutomationAction{Type: "host", Executable: "old", Args: []string{"old"}})
	if err != nil || got.Executable != "" || got.Args != nil {
		t.Fatal("changed action retained old executable", got, err)
	}
	v["action_type"] = "rf"
	v["rf_code"] = "bad"
	if _, err := nativeActionDraft(v, got); err == nil {
		t.Fatal("invalid RF draft accepted")
	}
}
func TestNativeRuleMatchRoundTripIncludesOptionalZeroIdentity(t *testing.T) {
	id := byte(0)
	code := uint32(4294967295)
	r := appconfig.Automation{Name: "نام 1", Enabled: true, CooldownMS: 300, Match: appconfig.AutomationMatch{Kind: "rf.gesture", Source: "rf", Gesture: "hold", Key: 4, RFID: &id, RFCode: &code, RFBits: 32, RFProtocol: 12, Lifecycle: "ready", State: "on", Contains: "text"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "x"}}}
	page := nativeRulePage(nativeAuthoringCollection[appconfig.Automation]{ETag: "r"}, r, r.Name, false)
	got, err := nativeRuleDraft(nativeTestValues(page), r)
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatal("match lost fields", got, err)
	}
	v := nativeTestValues(page)
	v["match_key"] = "not a number"
	if _, err := nativeRuleDraft(v, r); err == nil {
		t.Fatal("invalid literal replaced with old number")
	}
}
func TestNativeRulesSaveReorderAndStaleDraftNeverRuns(t *testing.T) {
	ctx := context.Background()
	c := nativeAuthoringCollection[appconfig.Automation]{ETag: "r1", Items: []appconfig.Automation{{Name: "Existing", Match: appconfig.AutomationMatch{Kind: "door"}, Actions: []appconfig.AutomationAction{{Type: "emit", Event: "first"}, {Type: "emit", Event: "second"}}}}}
	writes := 0
	p := nativePages{rpc: func(_ context.Context, m string, params, result any) error {
		switch m {
		case "controller.peripherals.get":
			return nil
		case "controller.automations.get":
			b, _ := json.Marshal(c)
			return json.Unmarshal(b, result)
		case "controller.automations.set":
			v := params.(map[string]any)
			if v["if_match"] != c.ETag {
				return errors.New("stale")
			}
			c.Items = v["items"].([]appconfig.Automation)
			c.ETag = "r2"
			writes++
			return nil
		default:
			t.Fatalf("rule authoring invoked %s", m)
			return nil
		}
	}}
	page := nativeRulePage(c, c.Items[0], "Existing", false)
	v := nativeTestValues(page)
	for k, value := range nativeActionValues(c.Items[0].Actions[1]) {
		v[k] = value
	}
	v["selected"] = "1"
	v["event"] = "unsaved action text"
	page, err := p.submitNativeRules(ctx, "action-up", v, false)
	if err != nil || writes != 1 || c.Items[0].Actions[0].Event != "second" || page.SelectedRow != "0" || page.DraftBaseline["event"] != "second" {
		t.Fatal("reorder lost selected action or draft", page.DraftBaseline, err)
	}
	if _, err := p.submitNativeRules(ctx, "action-save", v, false); err == nil || writes != 1 {
		t.Fatal("stale rule overwrote")
	}
}
func TestNativeMelodyEditingIsTypedAndNeverPlays(t *testing.T) {
	c := nativeAuthoringCollection[appconfig.Melody]{ETag: "r", Items: []appconfig.Melody{{Name: "Custom", Notes: []appconfig.MelodyNote{{FrequencyHz: 440, DurationMS: 100}, {FrequencyHz: 0, DurationMS: 50}}}}}
	writes := 0
	p := nativePages{rpc: func(_ context.Context, m string, params, result any) error {
		switch m {
		case "controller.melodies.get":
			b, _ := json.Marshal(c)
			return json.Unmarshal(b, result)
		case "controller.melodies.set":
			v := params.(map[string]any)
			if v["if_match"] != c.ETag {
				return errors.New("stale")
			}
			c.Items = v["items"].([]appconfig.Melody)
			c.ETag = "new"
			writes++
			return nil
		default:
			t.Fatalf("note editing invoked %s", m)
			return nil
		}
	}}
	v := nativeTestValues(nativeMelodyPage(c, c.Items[0], "Custom", false))
	v["selected"] = "0"
	v["note_hz"] = "659"
	page, err := p.submitNativeMelodies(context.Background(), "note-save", v)
	if err != nil || writes != 1 || c.Items[0].Notes[0].FrequencyHz != 659 || page.SelectedRow != "0" {
		t.Fatal("note save failed", err)
	}
	v = nativeTestValues(page)
	v["selected"] = "0"
	v["note_ms"] = "bad"
	if _, err := p.submitNativeMelodies(context.Background(), "note-save", v); err == nil || writes != 1 {
		t.Fatal("invalid timing sent")
	}
}
func TestNativeLCDPreferencesPreserveOtherUIAndDrafts(t *testing.T) {
	prefs := nativeHostPreferences{Settings: appconfig.Defaults().UI, ETag: "r"}
	prefs.Settings.PeripheralNames = map[string]string{"pwm.1": "Window"}
	writes := 0
	p := nativePages{rpc: func(_ context.Context, m string, params, result any) error {
		var data any
		switch m {
		case "controller.ui.preferences.get":
			data = prefs
		case "controller.lcd.presentation.status":
			data = control.LCDPresentationState{Enabled: true}
		case "controller.snapshot":
			*result.(*control.Snapshot) = control.Snapshot{Connected: true}
			return nil
		case "controller.ui.preferences.set":
			v := params.(map[string]any)
			settings := v["settings"].(appconfig.UI)
			if settings.PeripheralNames["pwm.1"] != "Window" {
				t.Fatal("unrelated name lost")
			}
			prefs.Settings = settings
			prefs.ETag = "next"
			writes++
			return nil
		default:
			t.Fatalf("LCD preference edit invoked %s", m)
		}
		b, _ := json.Marshal(data)
		return json.Unmarshal(b, result)
	}}
	page, err := p.loadNativeLCD(context.Background(), control.Snapshot{})
	if err != nil {
		t.Fatal(err)
	}
	v := nativeTestValues(page)
	v["lcd_prompt_debounce_ms"] = "250"
	v["prompt1"] = "Unsent"
	next, err := p.submitNativeLCD(context.Background(), "save", v)
	if err != nil || writes != 1 || prefs.Settings.LCDPromptDebounceMS != 250 || next.DraftBaseline["prompt1"] != "" {
		t.Fatal("LCD save/draft failed", err)
	}
	found := false
	for _, f := range next.Fields {
		if f.Key == "prompt1" && f.Value == "Unsent" {
			found = true
		}
	}
	if !found {
		t.Fatal("unrelated prompt draft lost")
	}
}

func TestNativeBuzzerStopTracksHostSchedulerWithoutBusyCapability(t *testing.T) {
	playing := false
	p := nativePages{rpc: func(_ context.Context, m string, params, result any) error {
		switch m {
		case "controller.snapshot":
			*result.(*control.Snapshot) = control.Snapshot{Connected: true}
			return nil
		case "controller.integrations.local.get":
			return nil
		case "controller.melodies.list":
			return nil
		case "controller.command.execute":
			if params.(map[string]string)["command"] != "melody status" {
				t.Fatal("playback-state read actuated hardware")
			}
			output := "melody idle"
			if playing {
				output = `melody playing id=1 name="Custom"`
			}
			data, _ := json.Marshal(map[string]string{"output": output})
			return json.Unmarshal(data, result)
		default:
			t.Fatalf("unexpected method: %s", m)
			return nil
		}
	}}
	for _, active := range []bool{false, true, false} {
		playing = active
		page, err := p.load(context.Background(), "buzzer")
		if err != nil {
			t.Fatal(err)
		}
		stop := false
		for _, a := range page.Actions {
			if a.ID == "stop" {
				stop = true
			}
		}
		if stop != active {
			t.Fatal("Stop does not reflect scheduler", active)
		}
	}
}
