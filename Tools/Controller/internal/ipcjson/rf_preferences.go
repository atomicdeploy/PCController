package ipcjson

import (
	"encoding/json"
	"errors"
	"pccontroller.local/controller/internal/appconfig"
)

func (service *Service) setRFLearningPreferences(raw json.RawMessage) (any, error) {
	var value appconfig.RFLearningPreferences
	if err := decodeStrictParams(raw, &value); err != nil {
		return nil, err
	}
	if value.TimeoutSeconds < 1 || value.TimeoutSeconds > 120 {
		return nil, &RPCError{Code: -32602, Message: "timeout_seconds must be 1..120"}
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	if err := service.UpdateHostConfig(func(config *appconfig.Config) error {
		config.RF.Learning = value
		return config.Validate()
	}); err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("rf.preferences.changed", "")
	return value, nil
}
