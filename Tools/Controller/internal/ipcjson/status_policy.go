package ipcjson

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"pccontroller.local/controller/internal/appconfig"
)

func canonicalSettingsDigest(value any) string {
	encoded, _ := json.Marshal(value)
	var object any
	_ = json.Unmarshal(encoded, &object)
	canonical, _ := json.Marshal(object)
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

func (service *Service) updateStatusPolicy(raw json.RawMessage) (any, error) {
	var params struct {
		appconfig.StatusLEDPolicy
		IfMatch string `json:"if_match"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, err
	}
	if len(params.IfMatch) != 64 {
		return nil, errors.New("current if_match revision is required")
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration unavailable")
	}
	current := service.persistentHostConfig().Integrations.StatusLED
	if canonicalSettingsDigest(current) != params.IfMatch {
		return nil, errors.New("Status lighting policy changed; refresh before saving")
	}
	if reflect.DeepEqual(current, params.StatusLEDPolicy) {
		return current, nil
	}
	err := service.UpdateHostConfig(func(value *appconfig.Config) error {
		if canonicalSettingsDigest(value.Integrations.StatusLED) != params.IfMatch {
			return errors.New("Status lighting policy changed; refresh before saving")
		}
		value.Integrations.StatusLED = params.StatusLEDPolicy
		return value.Validate()
	})
	if err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("ui.status_policy.changed", "Status lighting policy saved")
	return service.persistentHostConfig().Integrations.StatusLED, nil
}
