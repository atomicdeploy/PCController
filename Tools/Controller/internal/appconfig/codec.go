package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
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
	document, err := sparseConfigDocument(value)
	if err != nil {
		return nil, fmt.Errorf("encode sparse configuration: %w", err)
	}
	canonical, err := json.Marshal(document)
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
	var portableDocument any
	if err := json.Unmarshal(canonical, &portableDocument); err != nil {
		return nil, err
	}
	switch format {
	case "yaml":
		encoded, err := yaml.Marshal(portableDocument)
		if err != nil {
			return nil, fmt.Errorf("encode YAML configuration: %w", err)
		}
		return exactlyOneFinalNewline(encoded), nil
	case "toml":
		encoded, err := toml.Marshal(portableDocument)
		if err != nil {
			return nil, fmt.Errorf("encode TOML configuration: %w", err)
		}
		return exactlyOneFinalNewline(encoded), nil
	default:
		panic("unreachable configuration format")
	}
}

// sparseConfigDocument persists only values that differ from the current
// built-in defaults. Loading already decodes over Defaults(), so the on-disk
// file remains a stable set of user choices instead of a copied snapshot that
// silently freezes old defaults. Reflection is used before JSON marshaling so
// explicit false, zero, empty slices, and nested overrides survive `omitempty`.
func sparseConfigDocument(value Config) (map[string]any, error) {
	difference, changed := sparseValue(reflect.ValueOf(value), reflect.ValueOf(Defaults()))
	document := make(map[string]any)
	if changed {
		var ok bool
		document, ok = difference.(map[string]any)
		if !ok {
			return nil, errors.New("configuration root is not an object")
		}
	}
	// Keep an explicit compatibility boundary even when every user value is at
	// its default; a future loader can reject unsupported schema generations.
	document["schema"] = value.Schema
	return document, nil
}

func sparseValue(current, baseline reflect.Value) (any, bool) {
	if !current.IsValid() || !baseline.IsValid() {
		if current.IsValid() == baseline.IsValid() {
			return nil, false
		}
		if !current.IsValid() {
			return nil, true
		}
		return current.Interface(), true
	}
	if current.Type() != baseline.Type() {
		return current.Interface(), true
	}
	if current.Kind() == reflect.Interface {
		if current.IsNil() || baseline.IsNil() {
			if current.IsNil() == baseline.IsNil() {
				return nil, false
			}
			if current.IsNil() {
				return nil, true
			}
			return current.Elem().Interface(), true
		}
		return sparseValue(current.Elem(), baseline.Elem())
	}
	if current.Kind() == reflect.Pointer {
		if current.IsNil() || baseline.IsNil() {
			if current.IsNil() == baseline.IsNil() {
				return nil, false
			}
			if current.IsNil() {
				return nil, true
			}
			return current.Interface(), true
		}
		return sparseValue(current.Elem(), baseline.Elem())
	}
	// Values with custom JSON representations (notably time.Time) are atomic.
	if current.CanInterface() {
		if _, ok := current.Interface().(json.Marshaler); ok {
			if reflect.DeepEqual(current.Interface(), baseline.Interface()) {
				return nil, false
			}
			return current.Interface(), true
		}
	}
	switch current.Kind() {
	case reflect.Struct:
		result := make(map[string]any)
		typeInfo := current.Type()
		for index := 0; index < current.NumField(); index++ {
			field := typeInfo.Field(index)
			if field.PkgPath != "" {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			if value, changed := sparseValue(current.Field(index), baseline.Field(index)); changed {
				result[name] = value
			}
		}
		if len(result) == 0 {
			return nil, false
		}
		return result, true
	case reflect.Map, reflect.Slice, reflect.Array:
		if reflect.DeepEqual(current.Interface(), baseline.Interface()) {
			return nil, false
		}
		return current.Interface(), true
	default:
		if reflect.DeepEqual(current.Interface(), baseline.Interface()) {
			return nil, false
		}
		return current.Interface(), true
	}
}

func exactlyOneFinalNewline(value []byte) []byte {
	value = bytes.TrimRight(value, "\r\n")
	return append(value, '\n')
}
