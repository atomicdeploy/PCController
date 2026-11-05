package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pccontroller.local/desktop/internal/diagnostics"
)

var (
	ErrUnsafeDiagnosticEndpoint  = errors.New("config: unsafe diagnostic endpoint")
	ErrUnknownDiagnosticEndpoint = errors.New("config: unknown diagnostic endpoint")
	ErrExpertDiagnosticsDisabled = errors.New("config: expert diagnostic endpoint is disabled")
	ErrUnsafeDiagnosticMethod    = errors.New("config: diagnostic method must be GET")
)

// legacySafeDiagnosticEndpoints and legacyExpertDiagnosticEndpoints describe
// the exact fixed arrays written by releases before the ASA scope audit. They
// are accepted only as a migration input and are never permissions.
var legacySafeDiagnosticEndpoints = [...]string{
	"/info",
	"/update/info",
	"/wifi/signal",
	"/wifi/status",
	"/wifi/country",
	"/datetime/get",
	"/description.xml",
}

var legacyExpertDiagnosticEndpoints = [...]string{
	"/pu",
	"/is_exec",
	"/page_id",
	"/eProcessState",
	"/eResponseStatus",
}

// SafeDiagnosticEndpoints returns the immutable normal-mode endpoint policy as
// a caller-owned slice.
func SafeDiagnosticEndpoints() []string {
	return diagnostics.SafeEndpoints()
}

// ExpertDiagnosticEndpoints returns the immutable expert-mode endpoint policy
// as a caller-owned slice.
func ExpertDiagnosticEndpoints() []string {
	return []string{}
}

// ValidateEndpoint accepts only exact, query-free GET endpoint paths from the
// firmware audit. It never treats the JSON arrays as extensible permissions.
func (d DiagnosticsConfig) ValidateEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	parsed, err := url.Parse(endpoint)
	if err != nil || endpoint == "" || parsed.IsAbs() || parsed.Host != "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != endpoint {
		return fmt.Errorf("%w: %q", ErrUnknownDiagnosticEndpoint, endpoint)
	}

	if diagnostics.IsPublicEndpoint(endpoint) {
		return nil
	}
	if isExplicitlyUnsafeEndpoint(endpoint) {
		return fmt.Errorf("%w: %s", ErrUnsafeDiagnosticEndpoint, endpoint)
	}
	return fmt.Errorf("%w: %s", ErrUnknownDiagnosticEndpoint, endpoint)
}

// EndpointAllowed is the boolean form of ValidateEndpoint.
func (d DiagnosticsConfig) EndpointAllowed(endpoint string) bool {
	return d.ValidateEndpoint(endpoint) == nil
}

// ValidateRequest enforces both the GET-only rule and the endpoint policy.
func (d DiagnosticsConfig) ValidateRequest(method, endpoint string) error {
	if !strings.EqualFold(strings.TrimSpace(method), http.MethodGet) {
		return fmt.Errorf("%w: %q", ErrUnsafeDiagnosticMethod, method)
	}
	return d.ValidateEndpoint(endpoint)
}

func (d *DiagnosticsConfig) normalize() error {
	var err error
	d.SafeEndpoints, err = canonicalEndpointListWithLegacy(
		"diagnostics.safe_endpoints",
		d.SafeEndpoints,
		diagnostics.SafeEndpoints(),
		legacySafeDiagnosticEndpoints[:],
	)
	if err != nil {
		return err
	}
	d.ExpertEndpoints, err = retiredEndpointList(
		"diagnostics.expert_endpoints",
		d.ExpertEndpoints,
		legacyExpertDiagnosticEndpoints[:],
	)
	// Expert diagnostics targeted the separate PU/debug subsystem, not the device
	// or ESP. Retire the old switch during migration rather than leaving a
	// misleading enabled flag in persisted configuration.
	d.ExpertEnabled = false
	return err
}

func canonicalEndpointListWithLegacy(
	name string,
	configured, canonical, legacy []string,
) ([]string, error) {
	if len(configured) == 0 {
		return append([]string(nil), canonical...), nil
	}
	if sameEndpointSet(configured, canonical) || sameEndpointSet(configured, legacy) {
		return append([]string(nil), canonical...), nil
	}
	return nil, fmt.Errorf(
		"%s: fixed allowlist must match the current %d-endpoint policy",
		name,
		len(canonical),
	)
}

func retiredEndpointList(name string, configured, legacy []string) ([]string, error) {
	if len(configured) == 0 || sameEndpointSet(configured, legacy) {
		return []string{}, nil
	}
	return nil, fmt.Errorf("%s: retired endpoint list cannot be modified", name)
}

func sameEndpointSet(configured, canonical []string) bool {
	if len(configured) != len(canonical) {
		return false
	}
	wanted := make(map[string]struct{}, len(canonical))
	for _, endpoint := range canonical {
		wanted[endpoint] = struct{}{}
	}
	seen := make(map[string]struct{}, len(configured))
	for _, endpoint := range configured {
		endpoint = strings.TrimSpace(endpoint)
		if _, ok := wanted[endpoint]; !ok {
			return false
		}
		if _, duplicate := seen[endpoint]; duplicate {
			return false
		}
		seen[endpoint] = struct{}{}
	}
	return true
}

func isExplicitlyUnsafeEndpoint(endpoint string) bool {
	switch endpoint {
	case "/crash", "/sessions_debug", "/login", "/update",
		"/datetime/get", "/pu", "/is_exec", "/page_id",
		"/eProcessState", "/eResponseStatus":
		return true
	}
	return endpoint == "/config" || strings.HasPrefix(endpoint, "/config/")
}
