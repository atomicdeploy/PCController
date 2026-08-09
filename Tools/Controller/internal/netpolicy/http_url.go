// Package netpolicy validates operator-configured network destinations before
// they reach an outbound transport.
package netpolicy

import (
	"fmt"
	"net"
	"net/netip"
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
// Hostname resolution is validated and pinned by NewPublicTransport
// immediately before network use.
func ParsePublicHTTPURL(raw, subject string) (*url.URL, error) {
	parsed, err := ParseHTTPURL(raw, subject)
	if err != nil {
		return nil, err
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		if !publicAddress(address) {
			return nil, fmt.Errorf("%s must use a public network destination", normalizedSubject(subject))
		}
		return parsed, nil
	}
	if strings.Count(host, ".") == 0 || knownMetadataHost(host) {
		return nil, fmt.Errorf("%s must use a public DNS name", normalizedSubject(subject))
	}
	for _, suffix := range localNetworkHostnameSuffixes {
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

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || address.IsUnspecified() || address.IsLoopback() ||
		address.IsPrivate() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return false
	}
	prefixes := nonPublicIPv6Prefixes
	if address.Is4() {
		prefixes = nonPublicIPv4Prefixes
	} else if !allocatedIPv6GlobalUnicast.Contains(address) {
		// IANA currently allocates ordinary global-unicast IPv6 only from
		// 2000::/3. Reject reserved, site-local, translation, and other
		// transition space even when netip labels it global-unicast.
		return false
	}
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return address.IsGlobalUnicast()
}

// These tables intentionally reject every IANA special-purpose block rather
// than trying to allow globally reachable anycast exceptions: those services
// are not generic update endpoints. Keep the vectors synchronized with the
// IANA IPv4/IPv6 special-purpose registries and IPv6 allocation registry.
// Sources (reviewed 2026-08-09):
// https://www.iana.org/assignments/iana-ipv4-special-registry/
// https://www.iana.org/assignments/iana-ipv6-special-registry/
// https://www.iana.org/assignments/ipv6-address-space/
var nonPublicIPv4Prefixes = mustPrefixes(
	"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24",
	"192.31.196.0/24", "192.52.193.0/24", "192.88.99.0/24",
	"192.175.48.0/24", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "240.0.0.0/4",
)

var (
	allocatedIPv6GlobalUnicast = netip.MustParsePrefix("2000::/3")
	nonPublicIPv6Prefixes      = mustPrefixes(
		"2001::/23", "2001:db8::/32", "2002::/16",
		"2620:4f:8000::/48", "3fff::/20",
	)
)

func mustPrefixes(values ...string) []netip.Prefix {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}
