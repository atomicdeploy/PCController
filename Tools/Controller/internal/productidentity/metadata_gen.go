// Code generated from ../../web/package.json by generate.mjs; DO NOT EDIT.

package productidentity

const (
	Version         = "0.0.0-development"
	ShortName       = "PC"
	Tagline         = "CONTROL CENTER"
	Description     = "Unified hardware and integration control center"
	StableAppID     = "DRSDavidSoft.PCController"
	ProtocolScheme  = "pccontroller"
	ConfigDirectory = "PCController"
)

// Presentation and TUI console defaults are variables so product builds can
// replace them with Go -ldflags -X. appconfig seeds its defaults from these
// values before decoding the user's file, preserving config-over-build
// precedence.
var (
	DefaultTitle               = "PCController"
	DefaultFirstRunTagline     = "One host. Every board surface."
	BuildTitleBase64           = ""
	BuildFirstRunTaglineBase64 = ""
	DefaultTUIConsoleEnabled   = "true"
	DefaultTUIConsoleColumns   = "132"
	DefaultTUIConsoleRows      = "38"
	DefaultTUIConsoleFontFace  = "Consolas"
	DefaultTUIConsoleFontSize  = "18"
)
