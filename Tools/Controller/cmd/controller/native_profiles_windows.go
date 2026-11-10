//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
)

type nativeProfileDescriptor struct {
	Key                  string `json:"key"`
	Identity             string `json:"board_identity"`
	Mode                 string `json:"mode"`
	ExposeRawRelays      bool   `json:"expose_raw_relays"`
	Configured, Attached bool
	Revision             string `json:"revision"`
}
type nativeRoutingCatalog struct {
	Profile nativeProfileDescriptor          `json:"board_profile"`
	Routing appconfig.ChannelRouting         `json:"routing"`
	Targets []appconfig.PeripheralDescriptor `json:"targets"`
}

func (p nativePages) readNativeRouting(ctx context.Context) (nativeRoutingCatalog, error) {
	var catalog nativeRoutingCatalog
	if p.preview {
		catalog.Profile.Mode = appconfig.BoardModeUnconfigured
		return catalog, nil
	}
	err := p.call(ctx, "controller.channel_routing.get", map[string]any{}, &catalog)
	return catalog, err
}

func (p nativePages) loadNativeProfiles(ctx context.Context) (desktop.Page, error) {
	catalog, err := p.readNativeRouting(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	page := desktop.Page{Title: "Board profile & channel routing", Columns: []string{"Key", "Name", "Kind", "Physical output"}, RowValues: map[string]map[string]string{}}
	for _, route := range catalog.Routing.Channels {
		page.Rows = append(page.Rows, []string{route.Key, route.Name, route.Kind, route.Target})
		page.RowValues[route.Key] = map[string]string{"channel_key": route.Key, "channel_name": route.Name, "channel_kind": route.Kind, "channel_target": route.Target, "channel_icon": route.Icon}
	}
	var targets, labels []string
	for _, target := range catalog.Targets {
		targets = append(targets, target.Key)
		labels = append(labels, target.Name+" · "+target.Key)
	}
	encoded, _ := json.Marshal(catalog)
	page.Fields = []desktop.Field{
		{Key: "baseline", Value: string(encoded), Hidden: true},
		{Key: "identity", Label: "Board identity", Value: catalog.Profile.Identity, ReadOnly: true, Section: "Profile"},
		{Key: "profile_key", Label: "Profile name", Value: catalog.Profile.Key, Section: "Profile"},
		{Key: "profile_mode", Label: "Board mode", Value: catalog.Profile.Mode, Choices: []string{appconfig.BoardModeOrdinaryRelays, appconfig.BoardModeCinemaSeatMotion}, ChoiceLabels: []string{"Independent relays", "Cinema-seat motion"}, Section: "Profile"},
		{Key: "expose_raw", Label: "Expose raw seat relays", Value: fmt.Sprint(catalog.Profile.ExposeRawRelays), Choices: []string{"false", "true"}, Section: "Profile"},
		{Key: "routing_enabled", Label: "Semantic routing enabled", Value: fmt.Sprint(catalog.Routing.Enabled), Choices: []string{"false", "true"}, Section: "Routing"},
		{Key: "channel_key", Label: "Semantic channel key", Section: "Routing"}, {Key: "channel_name", Label: "Display name", Section: "Routing"},
		{Key: "channel_kind", Label: "Output kind", Value: "relay", Choices: []string{"relay", "pwm"}, Section: "Routing"},
		{Key: "channel_target", Label: "Physical output", Choices: targets, ChoiceLabels: labels, Section: "Routing"}, {Key: "channel_icon", Label: "Icon ID", Value: "sliders-horizontal", Section: "Routing"},
	}
	ready := !p.preview && catalog.Profile.Attached
	page.Actions = []desktop.PageAction{{ID: "profile-save", Label: "Save board profile", Enabled: ready, Confirm: true, Section: "Profile"}, {ID: "new", Label: "New semantic channel", Enabled: ready && catalog.Profile.Configured, Section: "Routing"}, {ID: "routing-save", Label: "Save routing", Enabled: ready && catalog.Profile.Configured, Confirm: true, Section: "Routing"}}
	page.Actions = append(page.Actions, desktop.PageAction{ID: "routing-remove", Label: "Remove channel", Enabled: ready && catalog.Profile.Configured, Confirm: true, SelectionRequired: true, Section: "Routing"})
	return page, nil
}

func (p nativePages) submitNativeProfiles(ctx context.Context, action string, v map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot configure board profiles")
	}
	if action == "new" {
		page, err := p.loadNativeProfiles(ctx)
		page.ClearSelection = true
		return page, err
	}
	var baseline nativeRoutingCatalog
	if json.Unmarshal([]byte(v["baseline"]), &baseline) != nil {
		return desktop.Page{}, errors.New("profile draft is invalid; refresh first")
	}
	current, err := p.readNativeRouting(ctx)
	if err != nil {
		return desktop.Page{}, err
	}
	if current.Profile.Identity != baseline.Profile.Identity || current.Profile.Revision != baseline.Profile.Revision {
		return desktop.Page{}, errors.New("Board profile changed; refresh before saving")
	}
	var result any
	switch action {
	case "profile-save":
		if v["profile_key"] == baseline.Profile.Key && v["profile_mode"] == baseline.Profile.Mode && v["expose_raw"] == fmt.Sprint(baseline.Profile.ExposeRawRelays) {
			page, err := p.loadNativeProfiles(ctx)
			return preserveNativeProfileDraft(page, action, v), err
		}
		err = p.call(ctx, "controller.board_profile.update", map[string]any{"key": v["profile_key"], "mode": v["profile_mode"], "expose_raw_relays": v["expose_raw"] == "true", "expected_revision": baseline.Profile.Revision}, &result)
	case "routing-save", "routing-remove":
		routing := baseline.Routing
		routing.Channels = append([]appconfig.SemanticChannel(nil), routing.Channels...)
		routing.Enabled = v["routing_enabled"] == "true"
		if action == "routing-remove" {
			found := false
			for i, route := range routing.Channels {
				if route.Key == v["selected"] && v["selected"] != "" {
					routing.Channels = append(routing.Channels[:i], routing.Channels[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				return desktop.Page{}, errors.New("select a current semantic channel")
			}
		} else if v["channel_key"] != "" {
			route := appconfig.SemanticChannel{Key: v["channel_key"], Name: v["channel_name"], Kind: v["channel_kind"], Target: v["channel_target"], Icon: v["channel_icon"]}
			if v["selected"] != "" && v["selected"] != route.Key {
				return desktop.Page{}, errors.New("Semantic keys are stable references; create a new channel instead of renaming its key")
			}
			found := false
			for i, existing := range routing.Channels {
				if existing.Key == route.Key {
					routing.Channels[i] = route
					found = true
					break
				}
			}
			if !found {
				routing.Channels = append(routing.Channels, route)
			}
		}
		if err := appconfig.ValidateChannelRouting(routing); err != nil {
			return desktop.Page{}, err
		}
		err = p.call(ctx, "controller.channel_routing.update", map[string]any{"board_identity": baseline.Profile.Identity, "expected_revision": baseline.Profile.Revision, "routing": routing}, &result)
	default:
		return desktop.Page{}, errors.New("unknown profile operation")
	}
	if err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativeProfiles(ctx)
	if err != nil {
		return page, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	page = preserveNativeProfileDraft(page, action, v)
	if action == "routing-remove" {
		page.ClearSelection = true
	}
	return page, nil
}

// Saving one domain never silently discards the other tab's draft. Refresh the
// authoritative baseline/revision, retaining only the unrelated editable values.
func preserveNativeProfileDraft(page desktop.Page, action string, values map[string]string) desktop.Page {
	section := "Profile"
	if action == "profile-save" {
		section = "Routing"
	}
	page.DraftBaseline = map[string]string{}
	selectedValues := page.RowValues[values["selected"]]
	for i := range page.Fields {
		field := &page.Fields[i]
		if field.Section == section && !field.ReadOnly && !field.Hidden {
			if value, exists := values[field.Key]; exists {
				authoritative := field.Value
				if selected, ok := selectedValues[field.Key]; ok {
					authoritative = selected
				}
				if authoritative != value {
					page.DraftBaseline[field.Key] = authoritative
				}
				field.Value = value
			}
		}
	}
	if section == "Routing" && page.RowValues[values["selected"]] != nil {
		for key := range page.RowValues[values["selected"]] {
			if value, exists := values[key]; exists {
				page.RowValues[values["selected"]][key] = value
			}
		}
	}
	return page
}
