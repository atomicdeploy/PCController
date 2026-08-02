// Package productidentity exposes the host's branded identity independently
// from stable wire, filesystem, and executable identifiers.
package productidentity

//go:generate node generate.mjs

import (
	"os"
	"strings"
	"unicode"
)

// RuntimeTitleEnvironment overrides the persisted title for one process. The
// package metadata remains the fallback and ui.app_title remains persistent.
const RuntimeTitleEnvironment = "APP_TITLE"

// Title resolves the effective user-facing product title. A process-local
// environment override wins over the persisted PC-side configuration.
func Title(configured string) string {
	if override := strings.TrimSpace(os.Getenv(RuntimeTitleEnvironment)); override != "" {
		return override
	}
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return DefaultTitle
}

// ServiceName adds a role suffix to the effective title without duplicating
// the product name at each visible API or console surface.
func ServiceName(configured, role string) string {
	title := Title(configured)
	if role = strings.TrimSpace(role); role != "" {
		return title + " " + role
	}
	return title
}

// ProtocolToken returns an ASCII-safe product token for protocol metadata that
// cannot contain whitespace. It is branding metadata, not a wire identifier.
func ProtocolToken() string {
	var token strings.Builder
	for _, value := range DefaultTitle {
		if value <= unicode.MaxASCII && (unicode.IsLetter(value) || unicode.IsDigit(value) || value == '-' || value == '_') {
			token.WriteRune(value)
		}
	}
	if token.Len() == 0 {
		return "Controller"
	}
	return token.String()
}
