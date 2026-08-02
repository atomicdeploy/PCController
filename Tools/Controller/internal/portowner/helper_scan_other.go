//go:build !windows

package portowner

import (
	"context"
	"errors"
)

func scanLegacyNativeOwner(context.Context, string) (Owner, bool, error) {
	return Owner{}, false, errors.New("serial-owner native helper is available only on Windows")
}
