//go:build rayanlamp_develop && rayanlamp_production

package buildinfo

// Keep the two public build tags mutually exclusive even when somebody
// bypasses the supported build scripts.
var _ = rayanlamp_develop_and_production_tags_are_mutually_exclusive
