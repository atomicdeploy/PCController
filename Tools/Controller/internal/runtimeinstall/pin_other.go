//go:build !linux

package runtimeinstall

import (
	"context"
	"errors"
)

func ValidatePackage(context.Context, string, string) (ValidatedPackage, error) {
	return ValidatedPackage{}, errors.New("Linux runtime package pinning is unavailable on this platform")
}
