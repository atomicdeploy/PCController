// Package envfile provides the single, non-shell `.env` contract shared by
// Controller executables. It deliberately never overrides inherited process
// variables, so service-manager and CI configuration remain authoritative.
package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type Result struct {
	Path    string
	Loaded  bool
	Applied []string
}

func projectFile(cwd string, lookup func(string) string) (string, error) {
	if explicit := strings.TrimSpace(lookup("PCCONTROLLER_ENV_FILE")); explicit != "" {
		if filepath.IsAbs(explicit) {
			return explicit, nil
		}
		return filepath.Abs(filepath.Join(cwd, explicit))
	}
	current, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return filepath.Join(current, ".env"), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			// An installed executable has no repository marker. It may load an
			// explicitly placed file from its working directory, but must not
			// walk into a user's parent/home directory and adopt an unrelated .env.
			return filepath.Join(cwd, ".env"), nil
		}
		current = parent
	}
}

func unquote(value, source string, line int) (string, error) {
	if len(value) == 0 || (value[0] != '\'' && value[0] != '"') {
		if comment := strings.Index(value, " #"); comment >= 0 {
			value = value[:comment]
		}
		return strings.TrimRight(value, " \t"), nil
	}
	quote := value[0]
	var output strings.Builder
	escaped := false
	for index := 1; index < len(value); index++ {
		character := value[index]
		if quote == '"' && escaped {
			switch character {
			case 'n':
				output.WriteByte('\n')
			case 'r':
				output.WriteByte('\r')
			case 't':
				output.WriteByte('\t')
			default:
				output.WriteByte(character)
			}
			escaped = false
			continue
		}
		if quote == '"' && character == '\\' {
			escaped = true
			continue
		}
		if character == quote {
			suffix := strings.TrimSpace(value[index+1:])
			if suffix != "" && !strings.HasPrefix(suffix, "#") {
				return "", fmt.Errorf("%s:%d: unexpected content after quoted value", source, line)
			}
			return output.String(), nil
		}
		output.WriteByte(character)
	}
	return "", fmt.Errorf("%s:%d: unterminated quoted value", source, line)
}

func parse(content, source string) (map[string]string, error) {
	values := map[string]string{}
	for index, raw := range strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n") {
		line := index + 1
		text := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		separator := strings.IndexByte(text, '=')
		if separator <= 0 {
			return nil, fmt.Errorf("%s:%d: expected KEY=VALUE", source, line)
		}
		key := strings.TrimSpace(text[:separator])
		if !keyPattern.MatchString(key) {
			return nil, fmt.Errorf("%s:%d: invalid environment variable name", source, line)
		}
		value, err := unquote(strings.TrimLeft(text[separator+1:], " \t"), source, line)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

// LoadProcess applies the nearest source-root `.env`, or the explicit
// PCCONTROLLER_ENV_FILE, to the current process. Existing variables win.
func LoadProcess() (Result, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Result{}, err
	}
	path, err := projectFile(cwd, os.Getenv)
	if err != nil {
		return Result{}, err
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Result{Path: path}, nil
	}
	if err != nil {
		return Result{}, err
	}
	values, err := parse(string(content), path)
	if err != nil {
		return Result{}, err
	}
	result := Result{Path: path, Loaded: true}
	for key, value := range values {
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return Result{}, err
		}
		result.Applied = append(result.Applied, key)
	}
	return result, nil
}
