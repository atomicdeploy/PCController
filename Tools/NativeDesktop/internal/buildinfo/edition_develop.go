//go:build !rayanlamp_production

package buildinfo

const (
	// Edition is selected at compile time. An ordinary `go build` is a
	// development build so local iteration never silently loses its native
	// symbols or browser source maps.
	Edition = EditionDevelop

	// DeveloperTools reports whether build-time debugging payloads are
	// available. Product diagnostics and raw lamp endpoint inspection are
	// support features and remain available in both editions.
	DeveloperTools = true
)
