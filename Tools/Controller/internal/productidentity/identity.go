// Package productidentity exposes the host's branded identity independently
// from stable wire, filesystem, and executable identifiers.
package productidentity

//go:generate node generate.mjs

import (
	"strings"
	"unicode"
)

const (
	// RuntimeTitleEnvironment is read by the executable's global option layer.
	// Keeping environment resolution out of Title lets an explicit flag retain
	// the documented highest precedence.
	RuntimeTitleEnvironment = "APP_NAME"
	// FirstRunTagline is the configurable default shown by first-run surfaces;
	// Tagline remains the shorter build-time product badge.
	FirstRunTagline = "One host. Every board surface."
)

// Title normalizes a title already resolved by the executable's precedence
// layer and falls back to immutable product metadata.
func Title(configured string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		return configured
	}
	return DefaultTitle
}

// ResolveTitle applies config < environment < flag precedence.
func ResolveTitle(configured, environment, commandLine string) string {
	if commandLine = strings.TrimSpace(commandLine); commandLine != "" {
		return commandLine
	}
	if environment = strings.TrimSpace(environment); environment != "" {
		return environment
	}
	return Title(configured)
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
