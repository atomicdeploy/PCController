//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"strings"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/desktop/desktop"
)

func (p nativePages) loadEffectsPage(state control.Snapshot) desktop.Page {
	ready := !p.preview && state.Connected && !state.EmergencyStop.Active && !state.Programming.Active
	page := desktop.Page{Title: "Programs & effects", Columns: []string{"Reference", "Name", "Folder", "Kind", "Duration"}, RowValues: map[string]map[string]string{}}
	mode := strings.ToLower(string(state.ProgramState.Mode))
	if mode == "" {
		mode = "idle"
	}
	groups := []string{""}
	for _, group := range state.EffectGroups {
		groups = append(groups, group.Name)
	}
	sort.Strings(groups)
	for _, effect := range state.Effects {
		encoded, _ := json.MarshalIndent(effect, "", "  ")
		values := map[string]string{"id": effect.ID, "name": effect.Name, "category": effect.Category, "icon": effect.Icon, "kind": effect.Kind, "definition": string(encoded), "definition_baseline": string(encoded), "revision": nativeDigest(effect), "name_baseline": effect.Name, "category_baseline": effect.Category, "icon_baseline": effect.Icon}
		color, _ := effect.Properties["color"].(string)
		if color == "" {
			color = "violet"
		}
		values["color"], values["color_baseline"] = color, color
		page.Rows = append(page.Rows, []string{effect.Reference, effect.Name, effect.Category, effect.Kind, fmt.Sprintf("%.3f s", float64(effect.DurationMS)/1000)})
		page.RowValues[effect.Reference] = values
	}
	page.Fields = []desktop.Field{
		field("id", "New effect ID", ""), field("name", "Name", ""), choice("category", "Folder", "", groups...), field("icon", "Icon", "sparkles"), field("color", "Color", "violet"), choice("kind", "New effect kind", "sequence", "sequence", "strip-stream"), choice("primitive", "New strip primitive", "rainbow", "rainbow", "solid", "alternating-zones", "envelope", "converging-points"),
		choice("execution", "Execution policy", "auto", "auto", "host", "mcu"), choice("capture", "Capture clock", "start", "start", "start-mcu", "start-board"), field("new_group", "New folder name", ""),
		choice("mode", "Application mode", mode, "idle", "running"),
		{Key: "definition", Label: "Advanced definition (JSON)", Value: "", Multiline: true},
		{Key: "revision", Hidden: true}, {Key: "definition_baseline", Hidden: true}, {Key: "name_baseline", Hidden: true}, {Key: "category_baseline", Hidden: true}, {Key: "icon_baseline", Hidden: true}, {Key: "color_baseline", Hidden: true},
	}
	for i := range page.Fields {
		page.Fields[i].Section = "Identity"
		switch page.Fields[i].Key {
		case "execution", "capture", "mode":
			page.Fields[i].Section = "Playback & capture"
		case "definition":
			page.Fields[i].Section = "Advanced"
		case "new_group":
			page.Fields[i].Section = "Folders"
		}
	}
	page.Actions = []desktop.PageAction{pageAction("new", "New effect", !p.preview), pageAction("create", "Create draft", !p.preview), nativeSelectionAction("save", "Save metadata", !p.preview), nativeSelectionAction("definition", "Save definition", !p.preview), nativeSelectionAction("play", "Play selected", ready), pageAction("group", "Create folder", !p.preview)}
	page.Actions = append(page.Actions, pageAction("mode", "Apply mode", !p.preview))
	remove := nativeSelectionAction("delete", "Delete selected", !p.preview)
	remove.Confirm = true
	page.Actions = append(page.Actions, remove)
	if state.Macros.Recording.Active {
		page.Fields = append(page.Fields, desktop.Field{Key: "recording", Label: "Recording", Value: fmt.Sprintf("%s · %d steps", state.Macros.Recording.Name, state.Macros.Recording.Steps), ReadOnly: true, Section: "Playback & capture"})
		page.Actions = append(page.Actions, pageAction("capture-save", "Finish capture", !p.preview), desktop.PageAction{ID: "capture-discard", Label: "Discard take", Enabled: !p.preview, Confirm: true})
	} else {
		page.Actions = append(page.Actions, pageAction("capture-start", "Record new take", ready))
	}
	if state.Macros.Playback.Running {
		page.Actions = append(page.Actions, pageAction("cancel", "Stop safely", ready), desktop.PageAction{ID: "cancel-keep", Label: "Stop · keep outputs", Enabled: ready, Confirm: true})
	}
	for i := range page.Actions {
		switch page.Actions[i].ID {
		case "definition":
			page.Actions[i].Section = "Advanced"
		case "group":
			page.Actions[i].Section = "Folders"
		case "play", "capture-start", "capture-save", "capture-discard", "cancel", "cancel-keep", "mode":
			page.Actions[i].Section = "Playback & capture"
		default:
			page.Actions[i].Section = "Identity"
		}
	}
	return page
}

func (p nativePages) submitEffectsPage(ctx context.Context, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify effects")
	}
	var state control.Snapshot
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, err
	}
	var selected *control.EffectDescriptor
	for i := range state.Effects {
		if state.Effects[i].Reference == values["selected"] {
			selected = &state.Effects[i]
			break
		}
	}
	words := []string{"effect"}
	switch action {
	case "new":
		page := p.loadEffectsPage(state)
		page.ClearSelection = true
		return page, nil
	case "mode":
		var result any
		if err := p.call(ctx, "controller.program_state.set", map[string]string{"owner": "native-desktop", "mode": values["mode"]}, &result); err != nil {
			return desktop.Page{}, err
		}
		if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
			return desktop.Page{}, err
		}
		return p.loadEffectsPage(state), nil
	case "create":
		if values["definition"] != values["definition_baseline"] {
			return desktop.Page{}, errors.New("Advanced JSON has unapplied changes; save that draft or explicitly discard it before creating an effect.")
		}
		if strings.TrimSpace(values["id"]) == "" || strings.TrimSpace(values["name"]) == "" {
			return desktop.Page{}, errors.New("enter a new ID and name")
		}
		if values["kind"] == "sequence" {
			words = append(words, "create", "sequence", values["id"], values["name"], values["category"], values["color"], values["icon"])
		} else {
			words = append(words, "create", "strip", values["id"], values["name"], values["primitive"], values["category"])
		}
	case "save", "definition", "delete":
		if selected == nil {
			return desktop.Page{}, errors.New("select a current effect first")
		}
		if values["revision"] != nativeDigest(*selected) {
			return desktop.Page{}, errors.New("Effect changed; refresh before saving. Your draft was not applied.")
		}
		if action == "delete" {
			words = append(words, "delete", selected.Reference)
			break
		}
		metadataChanged := values["name"] != values["name_baseline"] || values["category"] != values["category_baseline"] || values["color"] != values["color_baseline"] || values["icon"] != values["icon_baseline"]
		definitionChanged := values["definition"] != values["definition_baseline"]
		if metadataChanged && definitionChanged {
			return desktop.Page{}, errors.New("Both metadata and advanced JSON have changes; apply one draft at a time.")
		}
		if action == "definition" {
			if metadataChanged {
				return desktop.Page{}, errors.New("Save metadata first; the advanced definition has not been applied.")
			}
			var definition control.EffectDescriptor
			if err := json.Unmarshal([]byte(values["definition"]), &definition); err != nil {
				return desktop.Page{}, fmt.Errorf("effect JSON: %w", err)
			}
			if definition.ID != selected.ID || definition.Kind != selected.Kind {
				return desktop.Page{}, errors.New("definition ID and kind must match the selected effect")
			}
			words = append(words, "upsert-json", hex.EncodeToString([]byte(values["definition"])))
		} else {
			if definitionChanged {
				return desktop.Page{}, errors.New("Advanced JSON has unapplied changes; save that draft first.")
			}
			copy := *selected
			copy.Properties = maps.Clone(selected.Properties)
			copy.Name, copy.Category, copy.Icon = values["name"], values["category"], values["icon"]
			if copy.Properties == nil {
				copy.Properties = map[string]any{}
			}
			copy.Properties["color"] = values["color"]
			encoded, _ := json.Marshal(copy)
			words = append(words, "upsert-json", hex.EncodeToString(encoded))
		}
	case "play":
		if selected == nil {
			return desktop.Page{}, errors.New("select an effect first")
		}
		words = append(words, "play", selected.Reference)
		if selected.Kind == "sequence" {
			words = append(words, values["execution"])
		}
	case "capture-start":
		if strings.TrimSpace(values["name"]) == "" {
			return desktop.Page{}, errors.New("enter a capture name")
		}
		words = append(words, "record", values["capture"], values["name"], values["category"], values["color"])
	case "capture-save":
		words = append(words, "record", "save")
	case "capture-discard":
		words = append(words, "record", "discard")
	case "cancel":
		words = append(words, "cancel")
	case "cancel-keep":
		words = append(words, "cancel", "keep")
	case "group":
		words = append(words, "group", "create", values["new_group"], values["icon"])
	default:
		return desktop.Page{}, fmt.Errorf("unknown effect action %q", action)
	}
	output, err := p.command(ctx, shell.Join(words))
	if err != nil {
		return desktop.Page{}, err
	}
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	page := p.loadEffectsPage(state)
	page.Output = output
	return page, nil
}
