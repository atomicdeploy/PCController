//go:build !windows

package playbackpriority

func Acquire() (func() error, error) { return func() error { return nil }, nil }
