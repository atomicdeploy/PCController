package integrationproxy

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

type targetPolicy uint8

const (
	policyInvalid targetPolicy = iota
	policyDataHub
	policyDevice
)

// Target is an opaque, normalized integration root. Values are constructed by
// NormalizeDataHubTarget or NormalizeDeviceTarget so Resolver cannot accidentally
// expose an arbitrary client-selected URL.
type Target struct {
	baseURL url.URL
	policy  targetPolicy
}

// NormalizeDataHubTarget accepts only an HTTP(S) root URL on loopback. Paths,
// credentials, query strings, and fragments are rejected rather than trimmed.
func NormalizeDataHubTarget(value string) (Target, error) {
	return normalizeTarget(value, policyDataHub, func(host string) bool {
		if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
			return true
		}
		address := net.ParseIP(host)
		return address != nil && address.IsLoopback()
	})
}

// NormalizeDeviceTarget validates a root HTTP(S) URL for a typed local-device
// client. Handler deliberately refuses to generically proxy this target kind.
func NormalizeDeviceTarget(value string) (Target, error) {
	return normalizeTarget(value, policyDevice, func(host string) bool {
		if address := net.ParseIP(host); address != nil {
			return isLANIP(address)
		}
		return isLocalDNSName(host)
	})
}

// URL returns a copy of the normalized URL for diagnostics and configuration
// display. Mutating the result cannot alter the Target used by Handler.
func (target Target) URL() *url.URL {
	return target.cloneURL()
}

// String deliberately includes only the normalized URL.
func (target Target) String() string {
	if target.policy != policyDataHub && target.policy != policyDevice {
		return "invalid integration target"
	}
	return target.baseURL.String()
}

// GoString prevents %#v diagnostics from reflecting unexported credentials.
func (target Target) GoString() string {
	if target.policy != policyDataHub && target.policy != policyDevice {
		return "integrationproxy.Target{invalid}"
	}
	return fmt.Sprintf("integrationproxy.Target{%q}", target.baseURL.String())
}

func (target Target) cloneURL() *url.URL {
	clone := target.baseURL
	return &clone
}

func (target Target) validate() error {
	if target.policy != policyDataHub && target.policy != policyDevice {
		return errors.New("target was not created by a normalization helper")
	}
	if target.baseURL.Scheme != "http" && target.baseURL.Scheme != "https" {
		return errors.New("target has an invalid scheme")
	}
	if target.baseURL.Host == "" || target.baseURL.User != nil ||
		target.baseURL.Path != "" || target.baseURL.RawPath != "" ||
		target.baseURL.RawQuery != "" || target.baseURL.ForceQuery ||
		target.baseURL.Fragment != "" {
		return errors.New("target is not a normalized root URL")
	}
	host := target.baseURL.Hostname()
	if target.policy == policyDataHub {
		if !strings.EqualFold(host, "localhost") {
			address := net.ParseIP(host)
			if address == nil || !address.IsLoopback() {
				return errors.New("datahub target is not loopback")
			}
		}
	} else if address := net.ParseIP(host); address != nil {
		if !isLANIP(address) {
			return errors.New("device target is outside the local network")
		}
	} else if !isLocalDNSName(host) {
		return errors.New("device target is outside the local network")
	}
	return nil
}

func normalizeTarget(
	value string,
	policy targetPolicy,
	hostAllowed func(string) bool,
) (Target, error) {
	label := "integration"
	if policy == policyDataHub {
		label = "datahub"
	} else if policy == policyDevice {
		label = "device"
	}
	if value == "" || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, "\\\x00\r\n\t") || strings.Contains(value, "#") {
		return Target{}, fmt.Errorf("%s target must be a canonical HTTP(S) root URL", label)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return Target{}, fmt.Errorf("parse %s target: %w", label, err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return Target{}, fmt.Errorf("%s target scheme must be HTTP or HTTPS", label)
	}
	if parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil {
		return Target{}, fmt.Errorf("%s target must not contain credentials", label)
	}
	if parsed.Path != "" && parsed.Path != "/" || parsed.RawPath != "" {
		return Target{}, fmt.Errorf("%s target must be a root URL without a path", label)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.RawFragment != "" {
		return Target{}, fmt.Errorf("%s target must not contain a query or fragment", label)
	}
	rawHost := strings.ToLower(parsed.Hostname())
	host := strings.TrimSuffix(rawHost, ".")
	if host == "" || strings.HasSuffix(host, ".") || strings.HasSuffix(parsed.Host, ":") {
		return Target{}, fmt.Errorf("%s target has an invalid host", label)
	}
	if host == "" || strings.Contains(host, "%") || !hostAllowed(host) {
		return Target{}, fmt.Errorf("%s target host is outside its allowed network scope", label)
	}
	port := parsed.Port()
	portNumber := 0
	if port != "" {
		portNumber, err = strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return Target{}, fmt.Errorf("%s target has an invalid port", label)
		}
	}
	canonicalHost := host
	if address := net.ParseIP(host); address != nil {
		canonicalHost = address.String()
	}
	if port != "" {
		canonicalHost = net.JoinHostPort(canonicalHost, strconv.Itoa(portNumber))
	} else if strings.Contains(canonicalHost, ":") {
		canonicalHost = "[" + canonicalHost + "]"
	}
	parsed.Host = canonicalHost
	parsed.Path = ""
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return Target{baseURL: *parsed, policy: policy}, nil
}

func isLANIP(address net.IP) bool {
	return address != nil && !address.IsUnspecified() &&
		(address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast())
}

func isLocalDNSName(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if !validDNSName(host) {
		return false
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".lan", ".home.arpa", ".localdomain"} {
		if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}

func validDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range []byte(label) {
			if character >= 'a' && character <= 'z' ||
				character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}
