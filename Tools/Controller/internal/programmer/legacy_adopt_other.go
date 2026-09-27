//go:build !linux

package programmer

import "errors"

// AdoptKnownLegacyHostDataPaths intentionally fails closed outside Linux. The
// narrowly validated legacy layout and its metadata checks are Linux-specific.
func AdoptKnownLegacyHostDataPaths(paths HostDataPaths) error {
	return errors.New("legacy host-data adoption is supported only on Linux")
}
