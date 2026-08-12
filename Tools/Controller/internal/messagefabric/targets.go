// Package messagefabric contains the transport-neutral parts of the host
// message contract. It deliberately has no UI, serial, or network dependency.
package messagefabric

import "strings"

// TargetsSurface reports whether a normalized message envelope names a
// presentation surface. The legacy comma-separated target is accepted beside
// the canonical targets array so every adapter applies identical semantics.
func TargetsSurface(target string, targets []string, surface string) bool {
	surface = strings.ToLower(strings.TrimSpace(surface))
	if surface == "" {
		return false
	}
	values := append([]string(nil), targets...)
	values = append(values, strings.Split(target, ",")...)
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if value == "all" || value == surface {
			return true
		}
	}
	return false
}
