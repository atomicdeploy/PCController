//go:build !windows

package hostui

// WarningBeep is a quiet no-op where no native desktop warning sound exists.
func WarningBeep() error { return nil }
