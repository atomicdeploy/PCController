//go:build !windows

package pcspeaker

import (
	"context"
	"errors"
)

func play(context.Context, string, uint32, uint32) error {
	return errors.New("WinRing0 PC-speaker playback is available only on Windows")
}
