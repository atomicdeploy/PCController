package tui

import "charm.land/huh/v2"

// formIsFiltering reports whether a Huh select currently owns printable key
// input. Overlay shortcuts must stand down while this is true: otherwise
// typing a search such as "refresh" would accidentally execute Refresh, and
// Escape would close the entire workspace instead of accepting the filter.
func formIsFiltering(form *huh.Form) bool {
	if form == nil {
		return false
	}
	field := form.GetFocusedField()
	filter, ok := field.(interface {
		GetFiltering() bool
	})
	return ok && filter.GetFiltering()
}
