// Package shellintegration contains the optional Windows shell integrations
// for PCControllerDesktop. Parsing custom-protocol activations is platform independent so
// callers and tests use exactly the same bounded command allowlist.
package shellintegration

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

const (
	// Scheme is PCControllerDesktop's optional custom URI protocol.
	Scheme = "pccontroller-desktop"

	// AppUserModelID is shared by the native window, notifications, and shell
	// integrations. It must remain stable once released.
	AppUserModelID = "DRSDavidSoft.PCController"

	maxURIBytes = 2048
)

var (
	ErrUnsupported = errors.New("shellintegration: Windows shell integration is unavailable")
	ErrInvalidURI  = errors.New("shellintegration: invalid pccontroller-desktop URI")
)

var uriActions = map[string]string{
	"on":          "device.on",
	"off":         "device.off",
	"toggle":      "device.toggle",
	"show":        "window.show",
	"hide":        "window.hide",
	"reconnect":   "reconnect",
	"settings":    "settings.show",
	"diagnostics": "diagnostics.show",
	"web":         "web.show",
}

// ParseURI validates one external custom-protocol activation and maps it to a
// canonical dispatcher method. URI input is public and untrusted, so this
// intentionally exposes no arbitrary method names, parameters, queries, or
// fragments.
//
// The canonical form is pccontroller-desktop://on. The RFC-style pccontroller-desktop:/on form is
// also accepted for clients that do not represent an action as an authority.
func ParseURI(raw string) (string, error) {
	if raw == "" {
		return "", invalidURI("URI is empty")
	}
	if len(raw) > maxURIBytes {
		return "", invalidURI("URI exceeds %d bytes", maxURIBytes)
	}
	if raw != strings.TrimSpace(raw) {
		return "", invalidURI("surrounding whitespace is not allowed")
	}
	for _, character := range raw {
		if character > unicode.MaxASCII || unicode.IsControl(character) ||
			unicode.IsSpace(character) {
			return "", invalidURI("only visible ASCII characters are allowed")
		}
	}
	// Actions are an intentionally tiny ASCII vocabulary. Rejecting escapes
	// avoids alternate spellings of separators and makes registry activations
	// compare identically to hand-written activations.
	if strings.ContainsAny(raw, "%\\") {
		return "", invalidURI("escaping and backslashes are not allowed")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", invalidURI("parse URI: %v", err)
	}
	if !strings.EqualFold(parsed.Scheme, Scheme) {
		return "", invalidURI("scheme must be %s", Scheme)
	}
	if parsed.Opaque != "" {
		return "", invalidURI("opaque URI form is not supported")
	}
	if parsed.User != nil || parsed.Port() != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery ||
		parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", invalidURI("credentials, ports, queries, and fragments are not allowed")
	}

	var action string
	switch {
	case parsed.Host != "":
		if parsed.Path != "" && parsed.Path != "/" {
			return "", invalidURI("URI contains more than one action")
		}
		if strings.Contains(parsed.Host, ":") {
			return "", invalidURI("URI authority is not a plain action")
		}
		action = parsed.Host
	case strings.HasPrefix(strings.ToLower(raw), Scheme+":///"):
		return "", invalidURI("authority form requires an action host")
	case parsed.Path != "":
		if !strings.HasPrefix(parsed.Path, "/") ||
			strings.Count(parsed.Path, "/") != 1 {
			return "", invalidURI("URI path must contain exactly one action")
		}
		action = strings.TrimPrefix(parsed.Path, "/")
	default:
		return "", invalidURI("action is required")
	}

	action = strings.ToLower(action)
	if action == "" {
		return "", invalidURI("action is required")
	}
	for _, character := range action {
		if character < 'a' || character > 'z' {
			return "", invalidURI("action must contain ASCII letters only")
		}
	}
	method, ok := uriActions[action]
	if !ok {
		return "", invalidURI("unsupported action %q", action)
	}
	return method, nil
}

func invalidURI(format string, arguments ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidURI, fmt.Sprintf(format, arguments...))
}
