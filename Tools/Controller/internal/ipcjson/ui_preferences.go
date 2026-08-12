package ipcjson

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

type browserAppearance struct {
	Theme          string  `json:"theme"`
	Locale         string  `json:"locale"`
	Direction      string  `json:"direction"`
	ReduceMotion   bool    `json:"reduceMotion"`
	CompactNumbers bool    `json:"compactNumbers"`
	AudioMuted     bool    `json:"audioMuted"`
	AudioVolume    float64 `json:"audioVolume"`
}

type browserAppearancePatch struct {
	Theme          *string  `json:"theme,omitempty"`
	Locale         *string  `json:"locale,omitempty"`
	Direction      *string  `json:"direction,omitempty"`
	ReduceMotion   *bool    `json:"reduceMotion,omitempty"`
	CompactNumbers *bool    `json:"compactNumbers,omitempty"`
	AudioMuted     *bool    `json:"audioMuted,omitempty"`
	AudioVolume    *float64 `json:"audioVolume,omitempty"`
}

type browserUIConfigMutation struct {
	AppTitle               *string                  `json:"app_title,omitempty"`
	Tagline                *string                  `json:"tagline,omitempty"`
	SetupComplete          *bool                    `json:"setup_complete,omitempty"`
	SegmentScroll          *appconfig.SegmentScroll `json:"segment_scroll,omitempty"`
	PeripheralNames        *map[string]string       `json:"peripheral_names,omitempty"`
	Appearance             *browserAppearancePatch  `json:"appearance,omitempty"`
	StatusIntervalMS       *int                     `json:"status_interval_ms,omitempty"`
	MeasurementFreshnessMS *int                     `json:"measurement_freshness_ms,omitempty"`
	IfMatch                string                   `json:"if_match,omitempty"`
}

var errNoBrowserUIChange = errors.New("browser UI configuration is unchanged")

func browserAppearanceFromConfig(value appconfig.Appearance) browserAppearance {
	value = appconfig.NormalizeAppearance(value)
	return browserAppearance{
		Theme: value.Theme, Locale: value.Locale, Direction: value.Direction,
		ReduceMotion: value.ReduceMotion, CompactNumbers: value.CompactNumbers,
		AudioMuted: value.AudioMuted, AudioVolume: value.AudioVolume,
	}
}

func appearanceETag(value appconfig.Appearance) string {
	encoded, err := json.Marshal(browserAppearanceFromConfig(value))
	if err != nil {
		panic("encode validated appearance: " + err.Error())
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (patch browserAppearancePatch) apply(value appconfig.Appearance) appconfig.Appearance {
	if patch.Theme != nil {
		value.Theme = *patch.Theme
	}
	if patch.Locale != nil {
		value.Locale = *patch.Locale
	}
	if patch.Direction != nil {
		value.Direction = *patch.Direction
	}
	if patch.ReduceMotion != nil {
		value.ReduceMotion = *patch.ReduceMotion
	}
	if patch.CompactNumbers != nil {
		value.CompactNumbers = *patch.CompactNumbers
	}
	if patch.AudioMuted != nil {
		value.AudioMuted = *patch.AudioMuted
	}
	if patch.AudioVolume != nil {
		value.AudioVolume = *patch.AudioVolume
	}
	return appconfig.NormalizeAppearance(value)
}

func (params browserUIConfigMutation) empty() bool {
	return params.AppTitle == nil && params.Tagline == nil && params.SetupComplete == nil &&
		params.SegmentScroll == nil && params.PeripheralNames == nil &&
		params.Appearance == nil && params.StatusIntervalMS == nil && params.MeasurementFreshnessMS == nil
}

func (params browserUIConfigMutation) apply(value *appconfig.Config) error {
	if params.AppTitle != nil {
		value.UI.AppTitle = strings.TrimSpace(*params.AppTitle)
	}
	if params.Tagline != nil {
		value.UI.Tagline = strings.TrimSpace(*params.Tagline)
	}
	if params.SetupComplete != nil {
		value.UI.SetupComplete = *params.SetupComplete
	}
	if params.SegmentScroll != nil {
		value.UI.SegmentScroll = *params.SegmentScroll
	}
	if params.PeripheralNames != nil {
		names, err := normalizePeripheralNames(*params.PeripheralNames)
		if err != nil {
			return err
		}
		value.UI.PeripheralNames = clonePeripheralNames(names)
	}
	if params.Appearance != nil {
		value.UI.Appearance = params.Appearance.apply(value.UI.Appearance)
	}
	if params.StatusIntervalMS != nil {
		value.UI.StatusIntervalMS = *params.StatusIntervalMS
	}
	if params.MeasurementFreshnessMS != nil {
		value.UI.MeasurementFreshnessMS = *params.MeasurementFreshnessMS
	}
	return value.Validate()
}

func browserUIConfigDiff(before, after appconfig.Config) ([]string, map[string]any, map[string]any) {
	fields := make([]string, 0, 13)
	oldValues := make(map[string]any)
	newValues := make(map[string]any)
	add := func(name string, oldValue, newValue any) {
		if reflect.DeepEqual(oldValue, newValue) {
			return
		}
		fields = append(fields, name)
		oldValues[name], newValues[name] = oldValue, newValue
	}
	add("app_title", before.UI.AppTitle, after.UI.AppTitle)
	add("tagline", before.UI.Tagline, after.UI.Tagline)
	add("setup_complete", before.UI.SetupComplete, after.UI.SetupComplete)
	add("segment_scroll", before.UI.SegmentScroll, after.UI.SegmentScroll)
	add("peripheral_names", before.UI.PeripheralNames, after.UI.PeripheralNames)
	add("status_interval_ms", before.UI.StatusIntervalMS, after.UI.StatusIntervalMS)
	add("measurement_freshness_ms", before.UI.MeasurementFreshnessMS, after.UI.MeasurementFreshnessMS)
	oldAppearance := browserAppearanceFromConfig(before.UI.Appearance)
	newAppearance := browserAppearanceFromConfig(after.UI.Appearance)
	add("appearance.theme", oldAppearance.Theme, newAppearance.Theme)
	add("appearance.locale", oldAppearance.Locale, newAppearance.Locale)
	add("appearance.direction", oldAppearance.Direction, newAppearance.Direction)
	add("appearance.reduceMotion", oldAppearance.ReduceMotion, newAppearance.ReduceMotion)
	add("appearance.compactNumbers", oldAppearance.CompactNumbers, newAppearance.CompactNumbers)
	add("appearance.audioMuted", oldAppearance.AudioMuted, newAppearance.AudioMuted)
	add("appearance.audioVolume", oldAppearance.AudioVolume, newAppearance.AudioVolume)
	return fields, oldValues, newValues
}

func (service *Service) updateBrowserUISettings(raw json.RawMessage) (any, error) {
	var params browserUIConfigMutation
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	if params.SegmentScroll != nil {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		merged := service.hostConfig().UI.SegmentScroll
		if err := json.Unmarshal(fields["segment_scroll"], &merged); err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		params.SegmentScroll = &merged
	}
	if params.empty() {
		return nil, errors.New("app_title, tagline, setup_complete, segment_scroll, peripheral_names, appearance, status_interval_ms, or measurement_freshness_ms is required")
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}

	initial := service.hostConfig()
	if params.Appearance != nil && params.IfMatch != "" && params.IfMatch != appearanceETag(initial.UI.Appearance) {
		return nil, &RPCError{Code: -32000, Message: "appearance changed on the host; refresh before saving"}
	}
	candidate := initial
	if err := params.apply(&candidate); err != nil {
		return nil, err
	}
	fields, beforeValues, afterValues := browserUIConfigDiff(initial, candidate)
	if len(fields) == 0 {
		changed := false
		result := service.browserUISettings()
		result.Changed, result.ChangedFields = &changed, []string{}
		result.Before, result.After = map[string]any{}, map[string]any{}
		return result, nil
	}

	var appliedBefore, appliedAfter appconfig.Config
	err := service.UpdateHostConfig(func(value *appconfig.Config) error {
		if params.Appearance != nil && params.IfMatch != "" && params.IfMatch != appearanceETag(value.UI.Appearance) {
			return &RPCError{Code: -32000, Message: "appearance changed on the host; refresh before saving"}
		}
		appliedBefore = *value
		if err := params.apply(value); err != nil {
			return err
		}
		appliedAfter = *value
		changed, _, _ := browserUIConfigDiff(appliedBefore, appliedAfter)
		if len(changed) == 0 {
			return errNoBrowserUIChange
		}
		return nil
	})
	if errors.Is(err, errNoBrowserUIChange) {
		changed := false
		result := service.browserUISettings()
		result.Changed, result.ChangedFields = &changed, []string{}
		result.Before, result.After = map[string]any{}, map[string]any{}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	fields, beforeValues, afterValues = browserUIConfigDiff(appliedBefore, appliedAfter)
	if service.Client != nil {
		metadata := map[string]string{"scope": "ui"}
		if params.PeripheralNames != nil {
			metadata["scope"] = "peripherals"
		}
		if params.StatusIntervalMS != nil || params.MeasurementFreshnessMS != nil {
			metadata["scope"] = "measurements"
		}
		service.Client.EmitHostActionEvent("config", "Host UI configuration updated", "host", "ui.config.set", metadata)
	}
	changed := true
	result := service.browserUISettings()
	result.Changed, result.ChangedFields = &changed, fields
	result.Before, result.After = beforeValues, afterValues
	return result, nil
}
