//go:build !windows

package nativeshell

import "context"

func startPlatform(context.Context, Options) (Shell, error) { return noopShell{}, nil }

func Supported() bool { return false }
