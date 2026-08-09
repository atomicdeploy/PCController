//go:build !windows

package hostfacts

import (
	"context"
	"errors"
)

type nativeBackend struct{}

func (nativeBackend) query(
	context.Context,
	querySpec,
) ([]map[string]any, bool, error) {
	return nil, false, errors.New("host facts through WMI are available on Windows only")
}
