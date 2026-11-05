//go:build windows

package winfx

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var ErrInvalidExecutablePath = errors.New("winfx: invalid executable path")

func quoteRunCommand(exePath string) (string, error) {
	exePath = strings.TrimSpace(exePath)
	if exePath == "" {
		return "", fmt.Errorf("%w: path is empty", ErrInvalidExecutablePath)
	}
	if strings.ContainsAny(exePath, "\"\r\n\x00") {
		return "", fmt.Errorf("%w: path contains an unsafe character", ErrInvalidExecutablePath)
	}
	exePath = filepath.Clean(exePath)
	if !filepath.IsAbs(exePath) {
		return "", fmt.Errorf("%w: path must be absolute", ErrInvalidExecutablePath)
	}
	return `"` + exePath + `"`, nil
}
