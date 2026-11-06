//go:build !windows

package i18n

// SystemLocaleName returns a deterministic English fallback on platforms
// where PCControllerDesktop does not have a native system-locale integration.
func SystemLocaleName() (string, error) {
	return string(English), nil
}
