package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/appconfig"
)

type configPathPart struct {
	key   string
	index *int
}

func parseConfigPath(path string) ([]configPathPart, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil, nil
	}
	var parts []configPathPart
	for _, segment := range strings.Split(path, ".") {
		if segment == "" {
			return nil, errors.New("configuration path contains an empty segment")
		}
		for segment != "" {
			open := strings.IndexByte(segment, '[')
			if open < 0 {
				parts = append(parts, configPathPart{key: segment})
				break
			}
			if open > 0 {
				parts = append(parts, configPathPart{key: segment[:open]})
			}
			close := strings.IndexByte(segment[open:], ']')
			if close < 0 {
				return nil, fmt.Errorf("configuration path %q has an unterminated index", path)
			}
			close += open
			index, err := strconv.Atoi(segment[open+1 : close])
			if err != nil || index < 0 {
				return nil, fmt.Errorf("configuration path %q has an invalid index", path)
			}
			parts = append(parts, configPathPart{index: &index})
			segment = segment[close+1:]
		}
	}
	return parts, nil
}

func configValueAt(root any, parts []configPathPart) (any, error) {
	current := root
	for _, part := range parts {
		if part.index != nil {
			values, ok := current.([]any)
			if !ok || *part.index >= len(values) {
				return nil, fmt.Errorf("configuration index %d is unavailable", *part.index)
			}
			current = values[*part.index]
			continue
		}
		values, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("configuration field %q is unavailable", part.key)
		}
		value, ok := values[part.key]
		if !ok {
			return nil, fmt.Errorf("configuration field %q is unavailable", part.key)
		}
		current = value
	}
	return current, nil
}

func genericHostConfigGet(config appconfig.Config, path string) (string, error) {
	parts, err := parseConfigPath(path)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	var root any
	if err := json.Unmarshal(encoded, &root); err != nil {
		return "", err
	}
	value, err := configValueAt(root, parts)
	if err != nil {
		return "", err
	}
	if configPathIsSecret(parts) {
		if text, ok := value.(string); ok && text != "" {
			return path + "=<redacted>", nil
		}
	}
	encoded, err = json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return path + "=" + string(encoded), nil
}

func genericHostConfigSet(config appconfig.Config, path, raw string) (appconfig.Config, error) {
	parts, err := parseConfigPath(path)
	if err != nil {
		return config, err
	}
	if len(parts) == 0 {
		return config, errors.New("set requires a specific configuration path")
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return config, err
	}
	var root any
	if err := json.Unmarshal(encoded, &root); err != nil {
		return config, err
	}
	parent, err := configValueAt(root, parts[:len(parts)-1])
	if err != nil {
		return config, err
	}
	last := parts[len(parts)-1]
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		value = raw
	}
	if last.index != nil {
		values, ok := parent.([]any)
		if !ok || *last.index >= len(values) {
			return config, fmt.Errorf("configuration index %d is unavailable", *last.index)
		}
		values[*last.index] = value
	} else {
		values, ok := parent.(map[string]any)
		if !ok {
			return config, fmt.Errorf("configuration field %q is unavailable", last.key)
		}
		values[last.key] = value
	}
	encoded, err = json.Marshal(root)
	if err != nil {
		return config, err
	}
	var candidate appconfig.Config
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidate); err != nil {
		return config, fmt.Errorf("configuration value does not match %s: %w", path, err)
	}
	if err := candidate.Validate(); err != nil {
		return config, err
	}
	return candidate, nil
}

func configPathIsSecret(parts []configPathPart) bool {
	for _, part := range parts {
		key := strings.ToLower(part.key)
		if strings.HasSuffix(key, "_ref") {
			continue
		}
		if strings.Contains(key, "token") || strings.Contains(key, "password") || strings.Contains(key, "secret") || strings.Contains(key, "api_key") {
			return true
		}
	}
	return false
}
