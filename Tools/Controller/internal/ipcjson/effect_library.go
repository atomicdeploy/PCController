package ipcjson

import (
	"encoding/json"
	"errors"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
)

type effectLibrarySettings struct {
	Library control.EffectLibrary `json:"library"`
	ETag    string                `json:"etag"`
}

func effectLibraryFor(config appconfig.Config) effectLibrarySettings {
	library := control.ExportEffectLibrary(config)
	return effectLibrarySettings{Library: library, ETag: canonicalSettingsDigest(library)}
}

func (service *Service) updateEffectLibrary(raw json.RawMessage) (any, error) {
	if len(raw) > 2<<20 {
		return nil, errors.New("effect library request exceeds 2 MiB")
	}
	var params struct {
		Library *control.EffectLibrary `json:"library"`
		IfMatch string                 `json:"if_match"`
		Mode    string                 `json:"mode"`
	}
	if err := decodeStrictParams(raw, &params); err != nil {
		return nil, &RPCError{Code: -32602, Message: err.Error()}
	}
	if params.Library == nil || len(params.IfMatch) != 64 || (params.Mode != "merge" && params.Mode != "replace") {
		return nil, errors.New("library, current if_match and merge/replace mode are required")
	}
	if service.UpdateHostConfig == nil {
		return nil, errors.New("persistent host configuration is unavailable")
	}
	currentConfig := service.persistentHostConfig()
	current := effectLibraryFor(currentConfig)
	if current.ETag != params.IfMatch {
		return nil, errors.New("effect library changed; refresh before saving")
	}
	// Prepare and validate without touching the store. Even a no-op must be a
	// valid document, and unrelated host configuration remains untouched.
	candidate := currentConfig
	if err := control.ImportEffectLibrary(*params.Library, params.Mode == "replace", func(change func(*appconfig.Config) error) error { return change(&candidate) }); err != nil {
		return nil, err
	}
	if err := candidate.Validate(); err != nil {
		return nil, err
	}
	if effectLibraryFor(candidate).ETag == current.ETag {
		return current, nil
	}
	err := control.ImportEffectLibrary(*params.Library, params.Mode == "replace", func(change func(*appconfig.Config) error) error {
		return service.UpdateHostConfig(func(config *appconfig.Config) error {
			if effectLibraryFor(*config).ETag != params.IfMatch {
				return errors.New("effect library changed during save; refresh before retrying")
			}
			if err := change(config); err != nil {
				return err
			}
			return config.Validate()
		})
	})
	if err != nil {
		return nil, err
	}
	service.Client.EmitHostEvent("effects.changed", "Effect library saved")
	return effectLibraryFor(service.persistentHostConfig()), nil
}
