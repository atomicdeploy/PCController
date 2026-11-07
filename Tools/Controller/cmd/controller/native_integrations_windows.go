//go:build windows && !no_native_gui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostos"
	udp "pccontroller.local/controller/transport/udp"
	"pccontroller.local/desktop/desktop"
)

type nativeLocalPreferences struct {
	LocalDevice appconfig.LocalDevice     `json:"local_device"`
	DataHub     appconfig.DataHub         `json:"data_hub"`
	Lifecycle   appconfig.LifecycleSafety `json:"lifecycle_safety"`
	Buzzer      appconfig.BuzzerMirror    `json:"buzzer_mirror"`
}

func nativeConfigActivity(key string) (title, get, set string, defaults any) {
	switch key {
	case "status-policy":
		return "Reactive status lighting", "controller.status_led.policy.get", "controller.status_led.policy.set", appconfig.DefaultStatusLEDPolicy()
	case "integration-settings":
		return "Host integrations", "controller.integrations.local.get", "controller.integrations.local.set", nativeLocalPreferences{Lifecycle: appconfig.DefaultLifecycleSafety(), Buzzer: appconfig.DefaultBuzzerMirror()}
	case "discovery-settings":
		return "Discovery preferences", "controller.discovery.config.get", "controller.discovery.config.set", appconfig.DefaultDiscovery()
	case "udp-settings":
		return "Binary UDP configuration", "controller.udp.config.get", "controller.udp.config.set", udp.Config{Listen: "127.0.0.1:37890", Peers: map[string]string{}}
	case "os-policy":
		return "OS action policy", "controller.os.policy", "controller.os.configure", hostos.Policy{}
	}
	return "", "", "", nil
}

func nativeJSONMap(value any) (map[string]any, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(encoded, &result)
	return result, err
}

func (p nativePages) readNativeConfig(ctx context.Context, key string) (map[string]any, error) {
	_, method, _, defaults := nativeConfigActivity(key)
	if defaults == nil {
		return nil, errors.New("unknown native configuration")
	}
	// Decode into a typed default to include optional empty fields without
	// forwarding runtime status or unknown result keys back to configuration.
	target := reflect.New(reflect.TypeOf(defaults))
	target.Elem().Set(reflect.ValueOf(defaults))
	if !p.preview {
		if err := p.call(ctx, method, map[string]any{}, target.Interface()); err != nil {
			return nil, err
		}
	}
	values, err := nativeJSONMap(target.Elem().Interface())
	if err != nil {
		return nil, err
	}
	if key == "integration-settings" {
		for _, section := range []string{"local_device", "data_hub"} {
			if item, ok := values[section].(map[string]any); ok {
				if _, exists := item["base_url"]; !exists {
					item["base_url"] = ""
				}
			}
		}
		if item, ok := values["buzzer_mirror"].(map[string]any); ok {
			for _, name := range []string{"path", "executable"} {
				if _, exists := item[name]; !exists {
					item[name] = ""
				}
			}
		}
	}
	return values, nil
}

func (p nativePages) loadNativeConfig(ctx context.Context, key string) (desktop.Page, error) {
	title, _, _, _ := nativeConfigActivity(key)
	values, err := p.readNativeConfig(ctx, key)
	if err != nil {
		return desktop.Page{}, err
	}
	encoded, _ := json.Marshal(values)
	page := desktop.Page{Title: title, Fields: []desktop.Field{{Key: "baseline", Value: string(encoded), Hidden: true}, {Key: "revision", Value: nativeDigest(values), Hidden: true}}, Actions: []desktop.PageAction{{ID: "save", Label: "Save preferences", Enabled: !p.preview, Confirm: true}}}
	appendNativePreferenceFields(&page, "", values)
	if key == "status-policy" {
		for i := range page.Fields {
			f := &page.Fields[i]
			if f.Hidden {
				continue
			}
			if prefix, _, found := strings.Cut(f.Key, "."); found {
				f.Section = strings.ReplaceAll(prefix, "_", " ")
			}
			switch {
			case strings.HasSuffix(f.Key, ".effect"):
				f.Choices = []string{"steady", "breathe", "flash", "crossfade"}
			case strings.HasSuffix(f.Key, ".red") || strings.HasSuffix(f.Key, ".green") || strings.HasSuffix(f.Key, ".blue") || strings.HasSuffix(f.Key, "brightness"):
				f.Min = 0
				f.Max = 255
			}
		}
	}
	for i := range page.Fields {
		f := &page.Fields[i]
		switch f.Key {
		case "buzzer_mirror.backend":
			f.Choices = []string{"auto", "native", "external", "off"}
		case "buzzer_mirror.path":
			f.Choices = []string{"", "board", "host", "both", "none"}
		case "lifecycle_safety.session_lock", "lifecycle_safety.suspend":
			f.Choices = []string{"leave", "stop-motion", "all-off"}
		case "power.confirmation_token":
			f.Hidden = true // Preserve; never echo a policy credential into an editor.
		}
	}
	// Explicit expert collection editors keep empty collections configurable;
	// scalar preferences remain native checkboxes, choices and literal edits.
	var collections func(string, map[string]any)
	collections = func(prefix string, current map[string]any) {
		for k, value := range current {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			if nested, ok := value.(map[string]any); ok {
				if path == "peers" {
					encoded, _ := json.MarshalIndent(nested, "", "  ")
					page.Fields = append(page.Fields, desktop.Field{Key: path, Label: "Named peer endpoints (JSON)", Value: string(encoded), Multiline: true, Section: "Peers"})
				} else {
					collections(path, nested)
				}
			} else if _, ok := value.([]any); ok || value == nil {
				encoded, _ := json.MarshalIndent(value, "", "  ")
				page.Fields = append(page.Fields, desktop.Field{Key: path, Label: strings.ReplaceAll(k, "_", " ") + " (JSON)", Value: string(encoded), Multiline: true, Section: strings.ReplaceAll(prefix, "_", " ")})
			}
		}
	}
	collections("", values)
	sort.SliceStable(page.Fields, func(i, j int) bool { return page.Fields[i].Section < page.Fields[j].Section })
	return page, nil
}

func applyNativeConfigDraft(current map[string]any, values map[string]string) error {
	var apply func(string, map[string]any) error
	apply = func(prefix string, obj map[string]any) error {
		for key, old := range obj {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			raw, edited := values[path]
			if nested, ok := old.(map[string]any); ok && !edited {
				if err := apply(path, nested); err != nil {
					return err
				}
				continue
			}
			if !edited {
				continue
			}
			switch old.(type) {
			case bool:
				b, e := strconv.ParseBool(raw)
				if e != nil {
					return fmt.Errorf("%s: %w", path, e)
				}
				obj[key] = b
			case float64:
				n, e := strconv.ParseFloat(raw, 64)
				if e != nil {
					return fmt.Errorf("%s: %w", path, e)
				}
				obj[key] = n
			case string:
				obj[key] = raw
			default:
				var value any
				if err := json.Unmarshal([]byte(raw), &value); err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				obj[key] = value
			}
		}
		return nil
	}
	return apply("", current)
}

func (p nativePages) submitNativeConfig(ctx context.Context, key, action string, values map[string]string) (desktop.Page, error) {
	if p.preview || action != "save" {
		return desktop.Page{}, errors.New("configuration action unavailable")
	}
	current, err := p.readNativeConfig(ctx, key)
	if err != nil {
		return desktop.Page{}, err
	}
	if nativeDigest(current) != values["revision"] {
		return desktop.Page{}, errors.New("Preferences changed; refresh before saving. Your draft was not applied.")
	}
	if err := applyNativeConfigDraft(current, values); err != nil {
		return desktop.Page{}, err
	}
	if key == "integration-settings" {
		if buzzer, ok := current["buzzer_mirror"].(map[string]any); ok {
			if route, ok := buzzer["path"].(string); ok && route != "" {
				buzzer["enabled"] = route == "host" || route == "both"
			}
			if buzzer["backend"] == "off" {
				buzzer["native_enabled"] = false
			}
		}
	}
	_, _, method, _ := nativeConfigActivity(key)
	if key == "status-policy" {
		current["if_match"] = values["revision"]
	}
	var result any
	if err := p.call(ctx, method, current, &result); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativeConfig(ctx, key)
	if err != nil {
		return page, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	return page, nil
}

func (p nativePages) loadNativePWMPreferences(ctx context.Context) (desktop.Page, error) {
	prefs := nativeHostPreferences{Settings: appconfig.Defaults().UI}
	if !p.preview {
		if err := p.call(ctx, "controller.ui.preferences.get", map[string]any{}, &prefs); err != nil {
			return desktop.Page{}, err
		}
	}
	page := desktop.Page{Title: "PWM channel calibration", Columns: []string{"Channel", "Name", "Type", "Curve", "Gamma", "Step", "On behavior"}, RowValues: map[string]map[string]string{}}
	for channel := 0; channel < appconfig.PWMChannelCount; channel++ {
		key := appconfig.PWMChannelKey(channel)
		v := appconfig.PWMChannel(prefs.Settings, channel)
		values := map[string]string{"channel": strconv.Itoa(channel), "output_type": v.OutputType, "curve": v.Curve, "gamma": strconv.FormatFloat(v.Gamma, 'f', 2, 64), "icon": v.Icon, "step_percent": strconv.FormatFloat(appconfig.PWMStepPercent(v), 'f', 2, 64), "on_behavior": appconfig.PWMOnBehavior(v), "revision": prefs.ETag}
		page.Rows = append(page.Rows, []string{key, prefs.Settings.PeripheralNames[key], v.OutputType, v.Curve, values["gamma"], values["step_percent"] + "%", values["on_behavior"]})
		page.RowValues[key] = values
	}
	page.Fields = []desktop.Field{{Key: "channel", Hidden: true}, {Key: "revision", Hidden: true}, choice("output_type", "Output type", "lighting", "lighting", "indicator", "motor", "general"), choice("curve", "Response curve", "linear", "linear", "gamma"), {Key: "gamma", Label: "Gamma", Value: "2.2", Numeric: true, Min: 0.1, Max: 5, Decimals: 2}, field("icon", "Icon ID", "lightbulb"), {Key: "step_percent", Label: "Step (%)", Value: "1", Numeric: true, Min: 0.01, Max: 100, Decimals: 2}, choice("on_behavior", "On behavior", "restore", "full", "restore")}
	page.Actions = []desktop.PageAction{nativeSelectionAction("save", "Save channel calibration", !p.preview)}
	return page, nil
}

func (p nativePages) submitNativePWMPreferences(ctx context.Context, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot modify PWM calibration")
	}
	channel, err := nativeFormInteger(values, "channel", 0, 15)
	if err != nil {
		return desktop.Page{}, err
	}
	var prefs nativeHostPreferences
	if err := p.call(ctx, "controller.ui.preferences.get", map[string]any{}, &prefs); err != nil {
		return desktop.Page{}, err
	}
	if prefs.ETag != values["revision"] {
		return desktop.Page{}, errors.New("Channel preferences changed; refresh before saving")
	}
	gamma, err := strconv.ParseFloat(values["gamma"], 64)
	if err != nil {
		return desktop.Page{}, err
	}
	step, err := strconv.ParseFloat(values["step_percent"], 64)
	if err != nil {
		return desktop.Page{}, err
	}
	// The full preferences CAS makes the selected-channel edit atomic while
	// preserving every other channel and all UI preferences.
	if prefs.Settings.PWMChannels == nil {
		prefs.Settings.PWMChannels = map[string]appconfig.PWMChannelConfig{}
	}
	prefs.Settings.PWMChannels[appconfig.PWMChannelKey(channel)] = appconfig.PWMChannelConfig{OutputType: values["output_type"], Icon: values["icon"], Curve: values["curve"], Gamma: gamma, StepPercent: &step, OnBehavior: values["on_behavior"]}
	var result any
	if err := p.call(ctx, "controller.ui.preferences.set", map[string]any{"settings": prefs.Settings, "if_match": prefs.ETag}, &result); err != nil {
		return desktop.Page{}, err
	}
	page, err := p.loadNativePWMPreferences(ctx)
	if err != nil {
		return page, fmt.Errorf("save acknowledged; refresh failed: %w", err)
	}
	return page, nil
}
