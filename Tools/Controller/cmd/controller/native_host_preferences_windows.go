//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/desktop/desktop"
)

type nativeHostPreferences struct {
	Settings appconfig.UI `json:"settings"`
	ETag     string       `json:"etag"`
}

func (p nativePages) loadHostPreferences(ctx context.Context) (desktop.Page, error) {
	preferences := nativeHostPreferences{Settings: appconfig.Defaults().UI}
	if !p.preview {
		if err := p.call(ctx, "controller.ui.preferences.get", map[string]any{}, &preferences); err != nil {
			return desktop.Page{}, err
		}
	}
	encoded, err := json.Marshal(preferences.Settings)
	if err != nil {
		return desktop.Page{}, err
	}
	var values map[string]any
	_ = json.Unmarshal(encoded, &values)
	page := desktop.Page{Title: "Host preferences", Fields: []desktop.Field{{Key: "revision", Value: preferences.ETag, Hidden: true}, {Key: "baseline", Value: string(encoded), Hidden: true}}, Actions: []desktop.PageAction{pageAction("save", "Save host preferences", !p.preview)}}
	appendNativePreferenceFields(&page, "", values)
	sort.SliceStable(page.Fields, func(i, j int) bool {
		if page.Fields[i].Hidden != page.Fields[j].Hidden {
			return page.Fields[i].Hidden
		}
		return page.Fields[i].Section < page.Fields[j].Section
	})
	return page, nil
}

func appendNativePreferenceFields(page *desktop.Page, prefix string, values map[string]any) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		// Channel/profile metadata has its own catalog-bound card editor; never
		// turn arbitrary maps into an unsafe second configuration editor.
		if key == "peripheral_names" || key == "pwm_channels" {
			continue
		}
		value := values[key]
		if nested, ok := value.(map[string]any); ok {
			appendNativePreferenceFields(page, path, nested)
			continue
		}
		label := strings.ReplaceAll(key, "_", " ")
		label = strings.ToUpper(label[:1]) + label[1:]
		field := desktop.Field{Key: path, Label: label, Section: "General"}
		if prefix != "" {
			field.Section = strings.ReplaceAll(prefix, "_", " ")
		} else {
			switch {
			case strings.HasPrefix(key, "show_") || strings.Contains(key, "decimal") || strings.Contains(key, "status_interval") || strings.Contains(key, "freshness"):
				field.Section = "Measurements"
			case strings.Contains(key, "history") || key == "event_log_limit":
				field.Section = "History"
			case strings.Contains(key, "lcd"):
				field.Section = "LCD"
			case strings.HasPrefix(key, "strip_"):
				field.Section = "LED strip"
			case key == "table_layout" || key == "control_value_colors" || key == "separate_port_buttons":
				field.Section = "Controls"
			}
		}
		switch typed := value.(type) {
		case bool:
			field.Choices = []string{"false", "true"}
			field.Value = strconv.FormatBool(typed)
		case string:
			field.Value = typed
		case float64:
			field.Value = strconv.FormatFloat(typed, 'f', -1, 64)
			field.Numeric = true
			field.Min = 0
			field.Max = 1000000
			if key == "audio_volume" {
				field.Max = 1
				field.Decimals = 2
			}
		default:
			continue
		}
		choices := map[string][]string{"intro_mode": {"auto", "always", "skip"}, "table_layout": {"compact", "expanded"}, "strip_color_order": {"GRB", "RGB", "BRG", "BGR", "RBG", "GBR"}, "strip_firmware_order": {"GRB", "RGB", "BRG", "BGR", "RBG", "GBR"}, "appearance.theme": {"system", "dark", "light"}, "appearance.locale": {"en", "fa"}, "appearance.direction": {"auto", "ltr", "rtl"}}
		if options, ok := choices[path]; ok {
			field.Choices = options
		}
		page.Fields = append(page.Fields, field)
	}
}

func (p nativePages) saveHostPreferences(ctx context.Context, values map[string]string) error {
	var baseline map[string]any
	if err := json.Unmarshal([]byte(values["baseline"]), &baseline); err != nil {
		return errors.New("Host preference draft baseline is invalid; refresh first")
	}
	var apply func(string, map[string]any) error
	apply = func(prefix string, current map[string]any) error {
		for key, old := range current {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			if nested, ok := old.(map[string]any); ok {
				if err := apply(path, nested); err != nil {
					return err
				}
				continue
			}
			raw, ok := values[path]
			if !ok {
				continue
			}
			switch old.(type) {
			case bool:
				value, err := strconv.ParseBool(raw)
				if err != nil {
					return fmt.Errorf("%s must be true or false", path)
				}
				current[key] = value
			case float64:
				value, err := strconv.ParseFloat(raw, 64)
				if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
					return fmt.Errorf("%s must be a finite number", path)
				}
				current[key] = value
			case string:
				current[key] = raw
			}
		}
		return nil
	}
	if err := apply("", baseline); err != nil {
		return err
	}
	encoded, err := json.Marshal(baseline)
	if err != nil {
		return err
	}
	var settings appconfig.UI
	if err := json.Unmarshal(encoded, &settings); err != nil {
		return err
	}
	var result any
	return p.call(ctx, "controller.ui.preferences.set", map[string]any{"settings": settings, "if_match": values["revision"]}, &result)
}
