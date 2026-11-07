package ipcjson

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"pccontroller.local/controller/internal/appconfig"
)

// Full UI preferences are presentation-only. Credentials, transports, scripts,
// board settings and process policies remain outside this mutation boundary.
type fullUIPreferences struct {
	Settings appconfig.UI `json:"settings"`
	ETag     string       `json:"etag"`
}

func fullUIPreferencesFor(settings appconfig.UI) fullUIPreferences {
	encoded, _ := json.Marshal(settings)
	digest := sha256.Sum256(encoded)
	return fullUIPreferences{Settings: settings, ETag: hex.EncodeToString(digest[:])}
}

func (service *Service) updateFullUIPreferences(raw json.RawMessage) (any, error) {
	var params struct {
		Settings *appconfig.UI `json:"settings"`
		IfMatch  string        `json:"if_match"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	if params.Settings == nil || len(params.IfMatch) != 64 {
		return nil, errors.New("settings and current if_match revision are required")
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	current := fullUIPreferencesFor(service.persistentHostConfig().UI)
	if current.ETag != params.IfMatch {
		return nil, errors.New("UI preferences changed; refresh before saving")
	}
	if reflect.DeepEqual(current.Settings, *params.Settings) {
		return current, nil
	}
	err := service.UpdateHostConfig(func(value *appconfig.Config) error {
		if fullUIPreferencesFor(value.UI).ETag != params.IfMatch {
			return errors.New("UI preferences changed; refresh before saving")
		}
		value.UI = *params.Settings
		return value.Validate()
	})
	if err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("ui.preferences.changed", "UI preferences saved")
	return fullUIPreferencesFor(service.persistentHostConfig().UI), nil
}
