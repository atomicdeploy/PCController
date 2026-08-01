package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

func configFormat(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return "json", nil
	case ".yaml", ".yml":
		return "yaml", nil
	case ".toml":
		return "toml", nil
	default:
		return "", fmt.Errorf("configuration extension must be .json, .yaml, .yml, or .toml")
	}
}

func decodeConfig(path string, content []byte, target *Config) error {
	format, err := configFormat(path)
	if err != nil {
		return err
	}
	if format == "json" {
		return decodeCompatibleJSON(content, target)
	}
	// Decode to a generic document first and then through JSON. This makes the
	// canonical json tags (including snake_case names) identical across all
	// three formats. Unknown future fields are ignored, while every known field
	// still uses encoding/json's strict destination type checks and Config's
	// semantic validation.
	var document any
	switch format {
	case "yaml":
		decoder := yaml.NewDecoder(bytes.NewReader(content))
		if err := decoder.Decode(&document); err != nil {
			return err
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				err = errors.New("multiple YAML documents are not supported")
			}
			return err
		}
	case "toml":
		if err := toml.Unmarshal(content, &document); err != nil {
			return err
		}
	}
	canonical, err := json.Marshal(document)
	if err != nil {
		return fmt.Errorf("normalize %s: %w", format, err)
	}
	return decodeCompatibleJSON(canonical, target)
}

func decodeCompatibleJSON(content []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func encodeConfig(path string, value Config) ([]byte, error) {
	format, err := configFormat(path)
	if err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode configuration: %w", err)
	}
	if format == "json" {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, canonical, "", "  "); err != nil {
			return nil, err
		}
		return exactlyOneFinalNewline(pretty.Bytes()), nil
	}
	var document any
	if err := json.Unmarshal(canonical, &document); err != nil {
		return nil, err
	}
	switch format {
	case "yaml":
		encoded, err := yaml.Marshal(document)
		if err != nil {
			return nil, fmt.Errorf("encode YAML configuration: %w", err)
		}
		return exactlyOneFinalNewline(encoded), nil
	case "toml":
		encoded, err := toml.Marshal(document)
		if err != nil {
			return nil, fmt.Errorf("encode TOML configuration: %w", err)
		}
		return exactlyOneFinalNewline(encoded), nil
	default:
		panic("unreachable configuration format")
	}
}

func exactlyOneFinalNewline(value []byte) []byte {
	value = bytes.TrimRight(value, "\r\n")
	return append(value, '\n')
}
