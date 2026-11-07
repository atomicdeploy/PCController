//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

func TestNativeSequenceDraftPreservesTypedPayloads(t *testing.T) {
	end := uint16(3000)
	count := uint16(2)
	for _, step := range []appconfig.MacroStep{{AtUS: 123456, Kind: "pwm", Target: 3, Value: 400, ToValue: &end, DurationMS: 500, Easing: "ease-in-out", SampleRateHz: 60, RepeatCount: &count, RepeatIntervalMS: 900}, {Kind: "rf", Code: 123456, Bits: 24, Protocol: 1, PulseUS: 350}, {Kind: "display", Text: "Scene 1", Destination: "both", DurationMS: 1000}, {Kind: "raw", Opcode: 22, PayloadHex: "0102"}} {
		got, err := nativeStepDraft(step, nativeSequenceStepValues(step))
		if err != nil || !reflect.DeepEqual(got, step) {
			t.Fatalf("typed step lost fields: %+v -> %+v (%v)", step, got, err)
		}
	}
	legacy := appconfig.MacroStep{Kind: "beep", Value: 880, DurationMS: 115}
	got, err := nativeStepDraft(legacy, nativeSequenceStepValues(legacy))
	if err != nil || got.FrequencyHz != 880 || got.Value != 0 {
		t.Fatal("legacy tone became silent", got, err)
	}
	v := nativeSequenceStepValues(appconfig.MacroStep{Kind: "pwm", Target: 3, Value: 400, ToValue: &end, DurationMS: 500})
	v["step.kind"] = "relay"
	v["step.target"] = "4"
	v["step.value"] = "1"
	got, err = nativeStepDraft(appconfig.MacroStep{Kind: "pwm", ToValue: &end}, v)
	if err != nil || got.ToValue != nil || got.DurationMS != 0 {
		t.Fatal("changing type retained incompatible payload", got, err)
	}
	v["step.at_us"] = "invalid"
	if _, err := nativeStepDraft(got, v); err == nil {
		t.Fatal("invalid timing draft accepted")
	}
}

func TestNativeSequenceSaveIsRevisionFencedAndNeverPlays(t *testing.T) {
	library := nativeEffectLibrary{ETag: "revision", Library: control.EffectLibrary{Effects: []control.EffectDescriptor{{Reference: "effect:1", ID: "1", Name: "Scene", Kind: "sequence", Engine: "host", Steps: []appconfig.MacroStep{{Kind: "relay", AtUS: 1000000, Target: 4, Value: 1}, {Kind: "pwm", AtUS: 2000000, Target: 0, Value: 500}}}}}}
	writes := 0
	p := nativePages{rpc: func(_ context.Context, method string, params, result any) error {
		if method == "controller.effects.library.get" {
			b, _ := json.Marshal(library)
			return json.Unmarshal(b, result)
		}
		if method != "controller.effects.library.set" {
			t.Fatalf("authoring invoked %s", method)
		}
		v := params.(map[string]any)
		if v["if_match"] != library.ETag {
			return errors.New("stale")
		}
		writes++
		library.Library = v["library"].(control.EffectLibrary)
		library.ETag = "updated"
		b, _ := json.Marshal(library)
		return json.Unmarshal(b, result)
	}}
	v := nativeSequenceStepValues(library.Library.Effects[0].Steps[1])
	v["reference"] = "effect:1"
	v["revision"] = "revision"
	v["selected"] = "1"
	v["step.at_us"] = "500000"
	page, err := p.submitNativeSequence(context.Background(), "save", v)
	if err != nil || writes != 1 || page.SelectedRow != "0" || library.Library.Effects[0].Steps[0].Kind != "pwm" {
		t.Fatal("save lost sorted selection", page.SelectedRow, err)
	}
	if _, err := p.submitNativeSequence(context.Background(), "save", v); err == nil || writes != 1 {
		t.Fatal("stale draft wrote")
	}
}
