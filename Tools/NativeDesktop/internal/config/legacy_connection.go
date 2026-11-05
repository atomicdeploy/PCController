package config

import (
	"encoding/json"
	"errors"
)

// This is a read-only compatibility boundary for saved pre-adaptation desktop
// settings. New saves only emit the current device key. Keep the historical key
// here, not in runtime models, UI labels, routes or product identity.
func migrateConnectionConfig(data []byte) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return data, nil // The strict configuration decoder owns syntax errors.
	}
	previous, exists := fields["lamp"]
	if !exists {
		return data, nil
	}
	if _, exists := fields["device"]; exists {
		return nil, errors.New("both legacy and current connection settings are present; keep only one")
	}
	fields["device"] = previous
	delete(fields, "lamp")
	return json.Marshal(fields)
}
