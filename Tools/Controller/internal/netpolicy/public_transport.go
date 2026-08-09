package netpolicy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// IPResolver is the small resolver surface needed to validate a destination.
// net.Resolver implements it; the interface also makes DNS policy deterministic
// in tests without changing process-global resolver state.
type IPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// DialContextFunc matches net.Dialer.DialContext.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// HTTPRedirectPolicy is the canonical redirect boundary for downloads and
// release discovery. It composes with a caller's policy after enforcing URL,
// downgrade, credential, and hop-count rules.
type HTTPRedirectPolicy struct {
	Operation   string
	Subject     string
	MaximumHops int
	Scope       HTTPDestinationScope
	Previous    func(*http.Request, []*http.Request) error
}

// CheckRedirect has the signature expected by http.Client.CheckRedirect.
func (policy HTTPRedirectPolicy) CheckRedirect(request *http.Request, via []*http.Request) error {
	operation := strings.TrimSpace(policy.Operation)
	if operation == "" {
		operation = "remote request"
	}
	maximumHops := policy.MaximumHops
	if maximumHops <= 0 {
		maximumHops = 5
	}
	if len(via) >= maximumHops {
		return fmt.Errorf("%s exceeded %d redirects", operation, maximumHops)
	}
	if request == nil || request.URL == nil || len(via) == 0 {
		return fmt.Errorf("%s received an invalid redirect", operation)
	}
	if err := ValidateHTTPURLForScope(request.URL.String(), policy.Subject, policy.Scope); err != nil {
		return fmt.Errorf("%s redirect: %w", operation, err)
	}
	previousRequest := via[len(via)-1]
	if previousRequest == nil || previousRequest.URL == nil {
		return fmt.Errorf("%s received an invalid redirect chain", operation)
	}
	if strings.EqualFold(previousRequest.URL.Scheme, "https") && strings.EqualFold(request.URL.Scheme, "http") {
		return fmt.Errorf("%s refused an HTTPS-to-HTTP redirect", operation)
	}
	if !sameHTTPAuthority(previousRequest.URL, request.URL) {
		request.Header.Del("Authorization")
	}
	if policy.Previous != nil {
		return policy.Previous(request, via)
	}
	return nil
}

func sameHTTPAuthority(left, right *url.URL) bool {
	return left != nil && right != nil &&
		strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Host, right.Host)
}

// ResolvePublicHost resolves a DNS name once and rejects the entire answer set
// if any address can reach a non-public destination. Rejecting mixed answers is
// intentional: silently filtering a rebinding answer can turn DNS order into a
// security decision.
func ResolvePublicHost(ctx context.Context, resolver IPResolver, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.TrimSpace(host), ".")
	if host == "" {
		return nil, errors.New("remote destination has no host")
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		literal = literal.Unmap()
		if !publicIP(net.IP(literal.AsSlice())) {
			return nil, fmt.Errorf("remote destination %q is not public", host)
		}
		return []netip.Addr{literal}, nil
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve remote destination %q: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("remote destination %q resolved to no addresses", host)
	}
	result := make([]netip.Addr, 0, len(addresses))
	seen := make(map[netip.Addr]bool, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || !publicIP(net.IP(address.AsSlice())) {
			return nil, fmt.Errorf("remote destination %q resolved to a non-public address", host)
		}
		if !seen[address] {
			seen[address] = true
			result = append(result, address)
		}
	}
	return result, nil
}

// PinnedDialContext resolves and validates a direct destination, then passes a
// numeric address to the actual dialer. A second DNS lookup cannot redirect the
// connection after validation. Explicit proxy endpoints are trusted process
// configuration and are dialed normally; PublicRoundTripper still validates
// the requested target before the proxy sees it.
func PinnedDialContext(
	resolver IPResolver,
	dial DialContextFunc,
	trustedProxyURLs ...string,
) DialContextFunc {
	if dial == nil {
		dialer := &net.Dialer{}
		dial = dialer.DialContext
	}
	trusted := proxyAuthorities(trustedProxyURLs)
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if trusted[canonicalAuthority(address)] {
			return dial(ctx, network, address)
		}
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("parse remote destination: %w", err)
		}
		addresses, err := ResolvePublicHost(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		var failures []error
		for _, candidate := range addresses {
			connection, dialErr := dial(ctx, network, net.JoinHostPort(candidate.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			failures = append(failures, dialErr)
		}
		return nil, fmt.Errorf("dial validated remote destination: %w", errors.Join(failures...))
	}
}

// PublicRoundTripper validates every request passed to the base transport.
// net/http invokes RoundTrip separately for each redirect hop, so a redirect
// cannot escape the policy. The final response request is checked as a defense
// against custom transports that return a different effective URL.
func PublicRoundTripper(base http.RoundTripper, resolver IPResolver, subject string) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return publicRoundTripper{base: base, resolver: resolver, subject: normalizedSubject(subject)}
}

type publicRoundTripper struct {
	base     http.RoundTripper
	resolver IPResolver
	subject  string
}

func (transport publicRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("remote request has no URL")
	}
	if err := validateResolvedPublicURL(request.Context(), transport.resolver, request.URL, transport.subject); err != nil {
		return nil, err
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	effective := request.URL
	if response != nil && response.Request != nil && response.Request.URL != nil {
		effective = response.Request.URL
	}
	if err := validateResolvedPublicURL(request.Context(), transport.resolver, effective, transport.subject); err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return nil, fmt.Errorf("validate final remote destination: %w", err)
	}
	return response, nil
}

func validateResolvedPublicURL(ctx context.Context, resolver IPResolver, target *url.URL, subject string) error {
	if target == nil {
		return errors.New("remote destination is missing")
	}
	parsed, err := ParsePublicHTTPURL(target.String(), subject)
	if err != nil {
		return err
	}
	_, err = ResolvePublicHost(ctx, resolver, parsed.Hostname())
	return err
}

func proxyAuthorities(values []string) map[string]bool {
	result := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.Contains(value, "://") {
			value = "http://" + value
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		port := parsed.Port()
		if port == "" {
			switch strings.ToLower(parsed.Scheme) {
			case "https":
				port = "443"
			default:
				port = "80"
			}
		}
		result[canonicalAuthority(net.JoinHostPort(parsed.Hostname(), port))] = true
	}
	return result
}

func canonicalAuthority(value string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(net.JoinHostPort(strings.TrimSuffix(host, "."), port))
}
