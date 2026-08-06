// Package netpolicy validates operator-configured network destinations before
// they reach an outbound transport.
package netpolicy

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const maximumRemoteURLBytes = 4096

var (
	// The anchored allow-list deliberately excludes credentials, fragments,
	// whitespace, backslashes, and non-HTTP schemes before URL parsing.
	absoluteHTTPURL = regexp.MustCompile(`^https?://(?:localhost|\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\.?)(?::[0-9]{1,5})?(?:[/?][^\s\\#]*)?$`)
	hostLabel       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

// ParseHTTPURL returns a canonical HTTP(S) target whose authority and syntax
// passed the outbound-request allow-list. Private and loopback peers remain
// available for deliberately configured controller-to-controller workflows;
// link-local, unspecified, and multicast addresses are never valid targets.
func ParseHTTPURL(raw, subject string) (*url.URL, error) {
	value := strings.TrimSpace(raw)
	if subject = strings.TrimSpace(subject); subject == "" {
		subject = "remote URL"
	}
	if value == "" || len(value) > maximumRemoteURLBytes || !absoluteHTTPURL.MatchString(value) {
		return nil, fmt.Errorf("%s must be an absolute HTTP(S) URL without credentials or a fragment", subject)
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("%s must be an absolute HTTP(S) URL", subject)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("%s must use HTTP or HTTPS", subject)
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s cannot contain user information or a fragment", subject)
	}
	if !allowedHost(parsed.Hostname()) {
		return nil, fmt.Errorf("%s contains an unsupported host", subject)
	}
	if port := parsed.Port(); port != "" {
		value, portErr := strconv.ParseUint(port, 10, 16)
		if portErr != nil || value == 0 {
			return nil, fmt.Errorf("%s contains an invalid port", subject)
		}
	}
	return parsed, nil
}

func allowedHost(host string) bool {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if address := net.ParseIP(host); address != nil {
		return !address.IsUnspecified() && !address.IsMulticast() &&
			!address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast()
	}
	if len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}

// ValidateHTTPURL validates a destination when the caller does not need its
// parsed representation.
func ValidateHTTPURL(raw, subject string) error {
	_, err := ParseHTTPURL(raw, subject)
	return err
}
