//go:build !windows && !linux

package hostfacts

import (
	"context"
	"errors"
)

type nativeBackend struct{}

func platformHostFactsSource() string { return "unsupported" }

func platformHostFactsClass(_ string, windowsClass string) string { return windowsClass }

func (nativeBackend) query(
	context.Context,
	querySpec,
) ([]map[string]any, bool, error) {
	return nil, false, errors.New("native host facts are unavailable on this operating system")
}
