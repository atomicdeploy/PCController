//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/desktop/desktop"
)

type nativeEffectLibrary struct {
	Library control.EffectLibrary `json:"library"`
	ETag    string                `json:"etag"`
}

func (p nativePages) readNativeEffectLibrary(ctx context.Context) (nativeEffectLibrary, error) {
	var value nativeEffectLibrary
	if p.preview {
		return value, nil
	}
	err := p.call(ctx, "controller.effects.library.get", map[string]any{}, &value)
	return value, err
}

func (p nativePages) loadNativeSequence(ctx context.Context) (desktop.Page, error) {
	library, err := p.readNativeEffectLibrary(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Sequence editor", Columns: []string{"Reference", "Name", "Folder", "Steps", "Duration"}}
	for _, effect := range library.Library.Effects {
		if effect.Kind == "sequence" {
			page.Rows = append(page.Rows, []string{effect.Reference, effect.Name, effect.Category, fmt.Sprint(len(effect.Steps)), fmt.Sprintf("%.3f s", float64(effect.DurationMS)/1000)})
		}
	}
	page.Actions = []desktop.PageAction{{ID: "open", Label: "Edit sequence", Enabled: !p.preview, SelectionRequired: true}}
	return page, nil
}

func nativeSequenceEffect(library nativeEffectLibrary, reference string) (control.EffectDescriptor, error) {
	for _, effect := range library.Library.Effects {
		if effect.Reference == reference && effect.Kind == "sequence" {
			return effect, nil
		}
	}
	return control.EffectDescriptor{}, errors.New("select a current sequence")
}

func nativeSequenceFields(step appconfig.MacroStep) []desktop.Field {
	fields := []desktop.Field{choice("step.kind", "Step type", step.Kind, "relay", "relay-mask", "motion", "pwm", "relays-off", "pwm-off", "tone", "display", "rf", "rgb", "addressable", "menu", "raw"), nativeNumber("step.at_us", "Start (µs)", int(step.AtUS), 0, 2147483647)}
	fields[0].ChoiceLabels = []string{"Relay", "Relay mask", "Motion", "PWM", "All relays off", "All PWM off", "Tone", "Display", "RF", "Status RGB", "Addressable RGB", "Menu", "Raw opcode"}
	add := func(key, label string, min, max float64, kinds ...string) {
		f := desktop.Field{Key: "step." + key, Label: label, Numeric: true, Min: min, Max: max, Value: "0"}
		f.WhenKey = "step.kind"
		f.WhenValues = kinds
		fields = append(fields, f)
	}
	text := func(key, label string, kinds ...string) {
		fields = append(fields, desktop.Field{Key: "step." + key, Label: label, WhenKey: "step.kind", WhenValues: kinds})
	}
	add("target", "Hardware index (zero-based)", 0, 255, "relay", "motion", "side", "pwm", "mosfet", "addressable", "ws2812", "menu", "menu-action")
	add("value", "Value / raw duty", 0, 65535, "relay", "relay-mask", "motion", "side", "pwm", "mosfet", "menu", "menu-action")
	add("duration_ms", "Duration (ms)", 0, 65535, "pwm", "mosfet", "tone", "beep", "buzzer", "display", "message", "rgb", "status-led", "addressable", "ws2812")
	add("frequency_hz", "Frequency (Hz, 0 = silent)", 0, 20000, "tone", "beep", "buzzer")
	text("text", "Display text", "display", "message")
	destination := choice("step.destination", "Destination", step.Destination, "segments", "lcd", "both")
	destination.WhenKey = "step.kind"
	destination.WhenValues = []string{"display", "message"}
	fields = append(fields, destination)
	add("code", "RF code", 1, 4294967295, "rf", "radio")
	add("bits", "Bit count", 1, 32, "rf", "radio")
	add("protocol", "RF protocol", 1, 12, "rf", "radio")
	add("pulse_us", "Pulse (µs, 0 = default)", 0, 65535, "rf", "radio")
	for _, key := range []string{"red", "green", "blue", "brightness"} {
		add(key, strings.ToUpper(key[:1])+key[1:], 0, 255, "rgb", "status-led", "addressable", "ws2812")
	}
	add("opcode", "Opcode", 0, 255, "raw", "opcode")
	text("payload_hex", "Payload (hex)", "raw", "opcode")
	for _, key := range []string{"to_value", "to_red", "to_green", "to_blue", "to_brightness"} {
		limit := float64(255)
		kinds := []string{"rgb", "status-led", "addressable", "ws2812"}
		if key == "to_value" {
			limit = 4095
			kinds = []string{"pwm", "mosfet"}
		}
		add(key, strings.ReplaceAll(key, "_", " ")+" (optional)", 0, limit, kinds...)
		fields[len(fields)-1].OptionalNumeric = true
	}
	add("sample_rate_hz", "Transition samples (Hz, 0 = default)", 0, 60, "pwm", "mosfet", "rgb", "status-led", "addressable", "ws2812")
	easing := choice("step.easing", "Transition easing", step.Easing, "linear", "ease-in", "ease-out", "ease-in-out")
	easing.WhenKey = "step.kind"
	easing.WhenValues = []string{"pwm", "mosfet", "rgb", "status-led", "addressable", "ws2812"}
	fields = append(fields, easing)
	fields = append(fields, desktop.Field{Key: "step.repeat_count", Label: "Repeat count (optional)", Numeric: true, OptionalNumeric: true, Min: 0, Max: 65535}, nativeNumber("step.repeat_interval_ms", "Repeat interval (ms)", int(step.RepeatIntervalMS), 0, 4294967295))
	encoded, _ := json.Marshal(step)
	var values map[string]any
	_ = json.Unmarshal(encoded, &values)
	if (step.Kind == "tone" || step.Kind == "beep" || step.Kind == "buzzer") && step.FrequencyHz == 0 && step.Value > 0 {
		values["frequency_hz"] = step.Value
	}
	for i := range fields {
		fields[i].Section = "Step"
		key := strings.TrimPrefix(fields[i].Key, "step.")
		if value, ok := values[key]; ok {
			fields[i].Value = fmt.Sprint(value)
		} else if fields[i].Numeric && !fields[i].OptionalNumeric {
			fields[i].Value = "0"
		}
	}
	return fields
}

func nativeSequenceStepValues(step appconfig.MacroStep) map[string]string {
	v := map[string]string{}
	for _, field := range nativeSequenceFields(step) {
		v[field.Key] = field.Value
	}
	return v
}

func (p nativePages) nativeSequencePage(library nativeEffectLibrary, effect control.EffectDescriptor) desktop.Page {
	page := desktop.Page{Title: "Sequence editor · " + effect.Name, Columns: []string{"Step", "Start", "Type", "Target", "Value / text", "Duration"}, RowValues: map[string]map[string]string{}, Fields: []desktop.Field{{Key: "reference", Value: effect.Reference, Hidden: true}, {Key: "revision", Value: library.ETag, Hidden: true}}}
	page.Fields = append(page.Fields, nativeSequenceFields(appconfig.MacroStep{Kind: "relay"})...)
	for i, step := range effect.Steps {
		key := strconv.Itoa(i)
		value := fmt.Sprint(step.Value)
		if step.Text != "" {
			value = step.Text
		}
		if step.Kind == "rf" || step.Kind == "radio" {
			value = fmt.Sprint(step.Code)
		}
		page.Rows = append(page.Rows, []string{key, fmt.Sprintf("%.6f s", float64(step.AtUS)/1e6), step.Kind, fmt.Sprint(step.Target), value, fmt.Sprintf("%d ms", step.DurationMS)})
		page.RowValues[key] = nativeSequenceStepValues(step)
		page.Timeline = append(page.Timeline, desktop.TimelineCue{Key: key, Label: step.Kind, Lane: step.Kind, AtUS: step.AtUS, DurationMS: step.DurationMS})
	}
	page.Actions = []desktop.PageAction{{ID: "append", Label: "Add step", Enabled: !p.preview}, {ID: "save", Label: "Save step", Enabled: !p.preview, SelectionRequired: true}, {ID: "remove", Label: "Remove step", Enabled: !p.preview, SelectionRequired: true, Confirm: true}, {ID: "back", Label: "Back to library", Enabled: true, DiscardChanges: true}}
	return page
}

func nativeStepDraft(original appconfig.MacroStep, values map[string]string) (appconfig.MacroStep, error) {
	initial := original
	if values["step.kind"] != original.Kind {
		original = appconfig.MacroStep{Kind: values["step.kind"]}
	}
	encoded, _ := json.Marshal(original)
	var raw map[string]any
	_ = json.Unmarshal(encoded, &raw)
	for _, field := range nativeSequenceFields(original) {
		if field.WhenKey != "" {
			visible := false
			for _, kind := range field.WhenValues {
				visible = visible || kind == values[field.WhenKey]
			}
			if !visible {
				continue
			}
		}
		key := strings.TrimPrefix(field.Key, "step.")
		value := values[field.Key]
		if field.Numeric {
			if field.OptionalNumeric && strings.TrimSpace(value) == "" {
				delete(raw, key)
				continue
			}
			n, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
			if err != nil || float64(n) < field.Min || float64(n) > field.Max {
				return original, fmt.Errorf("%s must be %g–%g", field.Label, field.Min, field.Max)
			}
			raw[key] = n
		} else {
			raw[key] = value
		}
	}
	// Historical tone steps may store frequency in value instead of frequency_hz.
	if original.Kind == "tone" || original.Kind == "beep" || original.Kind == "buzzer" {
		raw["value"] = 0
	}
	encoded, _ = json.Marshal(raw)
	var step appconfig.MacroStep
	err := json.Unmarshal(encoded, &step)
	if step.Kind != initial.Kind || step.Target != initial.Target || step.Value != initial.Value {
		step.ActionIDs = nil
	}
	return step, err
}

func (p nativePages) submitNativeSequence(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if action == "back" {
		return p.loadNativeSequence(ctx)
	}
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify effects")
	}
	library, err := p.readNativeEffectLibrary(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	reference := v["reference"]
	if action == "open" {
		reference = v["selected"]
	}
	effect, err := nativeSequenceEffect(library, reference)
	if err != nil {
		return desktop.Page{}, err
	}
	if action == "open" {
		return p.nativeSequencePage(library, effect), nil
	}
	if library.ETag != v["revision"] {
		return desktop.Page{}, errors.New("effect library changed; refresh before saving")
	}
	index := -1
	if v["selected"] != "" {
		index, err = strconv.Atoi(v["selected"])
		if err != nil || index < 0 || index >= len(effect.Steps) {
			return desktop.Page{}, errors.New("select a current step")
		}
	}
	type indexedStep struct {
		step appconfig.MacroStep
		id   int
	}
	steps := make([]indexedStep, len(effect.Steps))
	for i, step := range effect.Steps {
		steps[i] = indexedStep{step, i}
	}
	selected := index
	switch action {
	case "save", "append":
		original := appconfig.MacroStep{Kind: "relay"}
		if index >= 0 {
			original = effect.Steps[index]
		}
		step, err := nativeStepDraft(original, v)
		if err != nil {
			return desktop.Page{}, err
		}
		if action == "save" {
			if index < 0 {
				return desktop.Page{}, errors.New("select a step first")
			}
			steps[index].step = step
		} else {
			selected = len(steps)
			steps = append(steps, indexedStep{step, selected})
		}
	case "remove":
		if index < 0 {
			return desktop.Page{}, errors.New("select a step first")
		}
		steps = append(steps[:index], steps[index+1:]...)
		selected = -1
	default:
		return desktop.Page{}, errors.New("unknown sequence action")
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].step.AtUS < steps[j].step.AtUS })
	effect.Steps = nil
	selectedKey := ""
	for i, step := range steps {
		effect.Steps = append(effect.Steps, step.step)
		if step.id == selected {
			selectedKey = strconv.Itoa(i)
		}
	}
	var saved nativeEffectLibrary
	if err := p.call(ctx, "controller.effects.library.set", map[string]any{"library": control.EffectLibrary{Effects: []control.EffectDescriptor{effect}}, "if_match": library.ETag, "mode": "merge"}, &saved); err != nil {
		return desktop.Page{}, err
	}
	effect, err = nativeSequenceEffect(saved, reference)
	if err != nil {
		return desktop.Page{}, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	page := p.nativeSequencePage(saved, effect)
	page.ClearSelection = true
	page.SelectedRow = selectedKey
	return page, nil
}
