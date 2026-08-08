// Code generated from ../../web/package.json by generate.mjs; DO NOT EDIT.

package productidentity

const (
	Version         = "0.0.0-development"
	DefaultTitle    = "PCController"
	ShortName       = "PC"
	Tagline         = "CONTROL CENTER"
	Description     = "Unified hardware and integration control center"
	StableAppID     = "DRSDavidSoft.PCController"
	ProtocolScheme  = "pccontroller"
	ConfigDirectory = "PCController"
)

// TUI console defaults are variables so product builds can replace them with
// Go -ldflags -X. appconfig seeds its defaults from these values before it
// decodes the user's file, preserving config-over-build precedence.
var (
	DefaultTUIConsoleEnabled  = "true"
	DefaultTUIConsoleColumns  = "132"
	DefaultTUIConsoleRows     = "38"
	DefaultTUIConsoleFontFace = "Consolas"
	DefaultTUIConsoleFontSize = "18"
)
