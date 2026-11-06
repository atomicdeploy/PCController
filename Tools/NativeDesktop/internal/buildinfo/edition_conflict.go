//go:build pccontroller_desktop_develop && pccontroller_desktop_production

package buildinfo

// Keep the two public build tags mutually exclusive even when somebody
// bypasses the supported build scripts.
var _ = pccontroller_desktop_develop_and_production_tags_are_mutually_exclusive
