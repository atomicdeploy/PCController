//go:build !windows && !linux

package pcspeaker

import (
	"context"
	"errors"
)

func play(context.Context, string, uint32, uint32) error {
	return errors.New("native PC-speaker playback is unavailable on this operating system")
}
