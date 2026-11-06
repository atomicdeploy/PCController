//go:build !windows

package artifacts

// Unix update scratch paths already use dot-prefixed names.
func setUpdateHidden(_ string, _ bool) error { return nil }
