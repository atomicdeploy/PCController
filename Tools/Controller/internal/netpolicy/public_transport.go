package netpolicy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

// IPResolver is the small resolver surface needed to validate a destination.
// net.Resolver implements it; the interface also makes DNS policy deterministic
// in tests without changing process-global resolver state.
type IPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

// DialContextFunc matches net.Dialer.DialContext.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

// ProxySelector matches http.Transport.Proxy. The selected proxy is evaluated
// for each request before a transport/dial policy is chosen.
type ProxySelector func(*http.Request) (*url.URL, error)

// PublicHTTPClientOptions describes one public-only HTTP consumer.
type PublicHTTPClientOptions struct {
	Timeout          time.Duration
	Operation        string
	Subject          string
	MaximumRedirects int
	Resolver         IPResolver
}

// NewPublicHTTPClient copies safe client settings while enforcing the same
// public destination, redirect, proxy-selection, and pinned-dial invariant for
// nil and non-nil templates. HTTP-client timeout, cookie jar, and redirect
// policy are retained, but transport capabilities are not. A custom
// RoundTripper cannot be proven to use the pinned dialer and is rejected;
// callers that deliberately trust such a transport must use an explicitly
// trusted higher-level constructor.
func NewPublicHTTPClient(template *http.Client, options PublicHTTPClientOptions) (*http.Client, error) {
	client := &http.Client{}
	base := http.DefaultTransport.(*http.Transport).Clone()
	if template != nil {
		*client = *template
		switch template.Transport.(type) {
		case nil:
			// A nil transport has ordinary net/http semantics, including the
			// process proxy environment.
		case *http.Transport:
			// A transport is a capability container, not a settings value: even
			// apparently harmless fields can install proxy headers, client
			// certificates, protocol handlers, or dial hooks. Public consumers
			// therefore use the canonical transport below.
		default:
			return nil, fmt.Errorf("%s public client requires *http.Transport; use an explicit trusted client for custom transports", normalizedSubject(options.Subject))
		}
	}
	// Proxy and dial hooks are security-sensitive capabilities, not passive
	// client settings. Public consumers always take proxy configuration from
	// the standard environment and replace every supplied dial hook. Callers
	// that deliberately inject either capability must choose an explicit
	// trusted constructor instead.
	client.Transport = NewPublicTransport(base, EnvironmentProxySelector(), options.Resolver, options.Subject)
	if client.Timeout == 0 {
		client.Timeout = options.Timeout
	}
	client.CheckRedirect = (HTTPRedirectPolicy{
		Operation: options.Operation, Subject: options.Subject,
		MaximumHops: options.MaximumRedirects, Scope: HTTPDestinationPublic,
		Previous: client.CheckRedirect,
	}).CheckRedirect
	return client, nil
}

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
	requestedURL, err := snapshotHTTPURL(request.URL)
	if err != nil {
		return fmt.Errorf("%s received an invalid redirect: %w", operation, err)
	}
	historyURLs, callbackHistory, err := snapshotRedirectHistory(via)
	if err != nil {
		return fmt.Errorf("%s received an invalid redirect chain: %w", operation, err)
	}
	for index := range historyURLs {
		if err := ValidateHTTPURLForScope(historyURLs[index].String(), policy.Subject, policy.Scope); err != nil {
			return fmt.Errorf("%s redirect chain hop %d: %w", operation, index, err)
		}
	}
	if err := ValidateHTTPURLForScope(requestedURL.String(), policy.Subject, policy.Scope); err != nil {
		return fmt.Errorf("%s redirect: %w", operation, err)
	}
	previousURL := historyURLs[len(historyURLs)-1]
	if isHTTPDowngrade(&previousURL, &requestedURL) {
		return fmt.Errorf("%s refused an HTTPS-to-HTTP redirect", operation)
	}
	if policy.Previous != nil {
		// Historical requests are snapshots. A composed callback may inspect or
		// mutate its copies, but cannot corrupt the chain used for this or a
		// later hop. Mutations to the candidate request remain supported and are
		// treated as untrusted input below.
		if err := policy.Previous(request, callbackHistory); err != nil {
			return err
		}
	}
	finalURL, err := snapshotHTTPURL(request.URL)
	if err != nil {
		return fmt.Errorf("%s callback produced an invalid redirect: %w", operation, err)
	}
	if err := ValidateHTTPURLForScope(finalURL.String(), policy.Subject, policy.Scope); err != nil {
		return fmt.Errorf("%s callback redirect: %w", operation, err)
	}
	if isHTTPDowngrade(&previousURL, &finalURL) {
		return fmt.Errorf("%s refused an HTTPS-to-HTTP redirect after callback", operation)
	}
	// Detach the final URL/header values from pointers retained by the callback,
	// then enforce credential confinement against the trusted historical
	// snapshot. This also removes credentials a callback tried to restore.
	request.URL = &finalURL
	request.Header = request.Header.Clone()
	if !sameHTTPAuthority(&previousURL, &finalURL) {
		deleteHTTPHeader(request.Header, "Authorization")
	}
	return nil
}

func snapshotHTTPURL(source *url.URL) (url.URL, error) {
	if source == nil {
		return url.URL{}, errors.New("redirect URL is missing")
	}
	result := *source
	if source.User != nil {
		if password, present := source.User.Password(); present {
			result.User = url.UserPassword(source.User.Username(), password)
		} else {
			result.User = url.User(source.User.Username())
		}
	}
	return result, nil
}

func snapshotRedirectHistory(via []*http.Request) ([]url.URL, []*http.Request, error) {
	urls := make([]url.URL, len(via))
	requests := make([]*http.Request, len(via))
	for index, source := range via {
		if source == nil || source.URL == nil {
			return nil, nil, fmt.Errorf("hop %d is missing", index)
		}
		snapshot, err := snapshotHTTPURL(source.URL)
		if err != nil {
			return nil, nil, fmt.Errorf("hop %d: %w", index, err)
		}
		urls[index] = snapshot
		clone := source.Clone(source.Context())
		callbackURL, _ := snapshotHTTPURL(&snapshot)
		clone.URL = &callbackURL
		requests[index] = clone
	}
	return urls, requests, nil
}

func isHTTPDowngrade(previous, next *url.URL) bool {
	return previous != nil && next != nil &&
		strings.EqualFold(previous.Scheme, "https") && strings.EqualFold(next.Scheme, "http")
}

func deleteHTTPHeader(header http.Header, name string) {
	for key := range header {
		if strings.EqualFold(key, name) {
			delete(header, key)
		}
	}
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
		if !publicAddress(literal) {
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
		if !publicAddress(address) {
			return nil, fmt.Errorf("remote destination %q resolved to a non-public address", host)
		}
		if !seen[address] {
			seen[address] = true
			result = append(result, address)
		}
	}
	return result, nil
}

// pinnedDialContext resolves and validates a direct destination, then passes a
// numeric address to the actual dialer. selectedProxyAuthority is non-empty
// only on the cached transport created after ProxySelector actually selected
// that proxy for the current request class. A configured-but-bypassed proxy can
// therefore never exempt a direct destination from validation.
func pinnedDialContext(resolver IPResolver, dial DialContextFunc, selectedProxyAuthority string) DialContextFunc {
	if dial == nil {
		dialer := &net.Dialer{}
		dial = dialer.DialContext
	}
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		if selectedProxyAuthority != "" && canonicalAuthority(address) == selectedProxyAuthority {
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

// NewPublicTransport validates every request, evaluates its actual proxy
// selection, and uses a cached transport whose only dial exemption is that
// selected proxy. Direct and per-proxy pools stay separate.
func NewPublicTransport(template *http.Transport, proxy ProxySelector, resolver IPResolver, subject string) http.RoundTripper {
	if template == nil {
		template = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		template = template.Clone()
	}
	// All TLS connections must flow through the pinned DialContext. A custom
	// TLS dial hook would otherwise bypass destination pinning.
	template.DialTLS = nil
	template.DialTLSContext = nil
	template.Proxy = nil
	return &publicRoundTripper{
		template: template, proxy: proxy, resolver: resolver,
		subject: normalizedSubject(subject), transports: make(map[string]*http.Transport),
		dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

type publicRoundTripper struct {
	template   *http.Transport
	proxy      ProxySelector
	resolver   IPResolver
	subject    string
	dial       DialContextFunc
	mu         sync.Mutex
	transports map[string]*http.Transport
}

func (transport *publicRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil {
		return nil, errors.New("remote request has no URL")
	}
	if err := validateResolvedPublicURL(request.Context(), transport.resolver, request.URL, transport.subject); err != nil {
		return nil, err
	}
	var selectedProxy *url.URL
	var err error
	if transport.proxy != nil {
		selectedProxy, err = transport.proxy(request)
		if err != nil {
			return nil, fmt.Errorf("select proxy for %s: %w", transport.subject, err)
		}
	}
	selectedTransport, err := transport.transportFor(selectedProxy)
	if err != nil {
		return nil, err
	}
	response, err := selectedTransport.RoundTrip(request)
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

func (transport *publicRoundTripper) transportFor(selectedProxy *url.URL) (*http.Transport, error) {
	key := "direct"
	selectedAuthority := ""
	if selectedProxy != nil {
		var err error
		selectedAuthority, err = proxyDialAuthority(selectedProxy)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256([]byte(selectedProxy.String()))
		key = "proxy:" + hex.EncodeToString(digest[:])
	}
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if existing := transport.transports[key]; existing != nil {
		return existing, nil
	}
	created := transport.template.Clone()
	created.DialTLS = nil
	created.DialTLSContext = nil
	created.DialContext = pinnedDialContext(transport.resolver, transport.dial, selectedAuthority)
	if selectedProxy == nil {
		created.Proxy = nil
	} else {
		copy := *selectedProxy
		created.Proxy = http.ProxyURL(&copy)
	}
	transport.transports[key] = created
	return created, nil
}

// CloseIdleConnections releases every direct/per-proxy cached pool.
func (transport *publicRoundTripper) CloseIdleConnections() {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	for _, candidate := range transport.transports {
		candidate.CloseIdleConnections()
	}
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

func proxyDialAuthority(proxy *url.URL) (string, error) {
	if proxy == nil || strings.TrimSpace(proxy.Hostname()) == "" {
		return "", errors.New("selected proxy has no host")
	}
	port := proxy.Port()
	if port == "" {
		switch strings.ToLower(strings.TrimSpace(proxy.Scheme)) {
		case "http":
			port = "80"
		case "https":
			port = "443"
		case "socks5", "socks5h":
			port = "1080"
		default:
			return "", fmt.Errorf("selected proxy uses unsupported scheme %q", proxy.Scheme)
		}
	}
	return canonicalAuthority(net.JoinHostPort(proxy.Hostname(), port)), nil
}

func canonicalAuthority(value string) string {
	host, port, err := net.SplitHostPort(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(net.JoinHostPort(strings.TrimSuffix(host, "."), port))
}
