//go:build !linux

package programmer

import "errors"

// AdoptKnownHostDataPaths intentionally fails closed outside Linux. The
// narrowly validated existing alpha layout and its metadata checks are Linux-specific.
func AdoptKnownHostDataPaths(paths HostDataPaths) error {
	return errors.New("existing host-data adoption is supported only on Linux")
}
