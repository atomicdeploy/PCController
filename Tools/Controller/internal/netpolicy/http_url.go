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

// HTTPDestinationScope selects whether an operator-configured LAN/loopback
// peer is valid or whether a source must resolve only to the public Internet.
type HTTPDestinationScope uint8

const (
	HTTPDestinationConfigured HTTPDestinationScope = iota
	HTTPDestinationPublic
)

var (
	// The anchored allow-list deliberately excludes credentials, fragments,
	// whitespace, backslashes, and non-HTTP schemes before URL parsing.
	absoluteHTTPURL = regexp.MustCompile(`^https?://(?:localhost|\[[0-9A-Fa-f:.]+\]|[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?\.?)(?::[0-9]{1,5})?(?:[/?][^\s\\#]*)?$`)
	hostLabel       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
)

var localDestinationSuffixes = []string{
	".home.arpa", ".internal", ".lan", ".local", ".localdomain", ".localhost",
}

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

// ParsePublicHTTPURL applies the stricter policy used for remote artifact and
// update sources. Local controller bridges deliberately use ParseHTTPURL;
// downloads must not be able to reach host, LAN, link-local, or cloud metadata
// services through a literal address or an explicitly local DNS name.
// Hostname resolution is validated and pinned by PublicRoundTripper and
// PinnedDialContext immediately before network use.
func ParsePublicHTTPURL(raw, subject string) (*url.URL, error) {
	parsed, err := ParseHTTPURL(raw, subject)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if address := net.ParseIP(host); address != nil {
		if !publicIP(address) {
			return nil, fmt.Errorf("%s must use a public network destination", normalizedSubject(subject))
		}
		return parsed, nil
	}
	if strings.Count(host, ".") == 0 || knownMetadataHost(host) {
		return nil, fmt.Errorf("%s must use a public DNS name", normalizedSubject(subject))
	}
	for _, suffix := range localDestinationSuffixes {
		if strings.HasSuffix(host, suffix) {
			return nil, fmt.Errorf("%s must use a public DNS name", normalizedSubject(subject))
		}
	}
	return parsed, nil
}

// ValidatePublicHTTPURL validates a remote download destination when the
// caller does not need its parsed representation.
func ValidatePublicHTTPURL(raw, subject string) error {
	_, err := ParsePublicHTTPURL(raw, subject)
	return err
}

// ParseHTTPURLForScope keeps the destination trust decision in one shared
// policy instead of duplicating URL selection logic in each HTTP consumer.
func ParseHTTPURLForScope(raw, subject string, scope HTTPDestinationScope) (*url.URL, error) {
	switch scope {
	case HTTPDestinationConfigured:
		return ParseHTTPURL(raw, subject)
	case HTTPDestinationPublic:
		return ParsePublicHTTPURL(raw, subject)
	default:
		return nil, fmt.Errorf("%s has an unsupported destination policy", normalizedSubject(subject))
	}
}

// ValidateHTTPURLForScope validates a destination under the selected shared
// scope when its parsed form is not needed.
func ValidateHTTPURLForScope(raw, subject string, scope HTTPDestinationScope) error {
	_, err := ParseHTTPURLForScope(raw, subject, scope)
	return err
}

func normalizedSubject(subject string) string {
	if subject = strings.TrimSpace(subject); subject != "" {
		return subject
	}
	return "remote URL"
}

func knownMetadataHost(host string) bool {
	switch host {
	case "instance-data", "metadata.google", "metadata.google.internal", "metadata.azure.internal":
		return true
	default:
		return false
	}
}

func publicIP(address net.IP) bool {
	if address == nil || address.IsUnspecified() || address.IsLoopback() ||
		address.IsPrivate() || address.IsMulticast() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
		return false
	}
	// Carrier-grade NAT, protocol assignment, benchmarking, documentation,
	// reserved, and discard-only ranges are not public update destinations,
	// but net.IP does not classify all of them as private.
	for _, network := range nonPublicNetworks {
		if network.Contains(address) {
			return false
		}
	}
	return address.IsGlobalUnicast()
}

var nonPublicNetworks = mustIPNetworks(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "240.0.0.0/4", "64:ff9b:1::/48", "100::/64",
	"2001:db8::/32", "3fff::/20", "5f00::/16",
)

func mustIPNetworks(values ...string) []*net.IPNet {
	result := make([]*net.IPNet, 0, len(values))
	for _, value := range values {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			panic(err)
		}
		result = append(result, network)
	}
	return result
}
