//go:build !linux

package runtimeinstall

import (
	"context"
	"errors"
)

func Stage(context.Context, StageOptions) (StageReport, error) {
	return StageReport{Applied: false}, errors.New("PCController runtime input staging is supported only on Linux; no state was changed")
}
