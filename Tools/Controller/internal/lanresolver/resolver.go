// Package lanresolver provides reconnect-sticky name resolution for trusted
// LAN controller connections. A successfully connected address stays preferred
// until that exact address cannot be dialled again.
package lanresolver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
)

type Resolver struct {
	mu     sync.Mutex
	cache  map[string]netip.Addr
	dial   func(context.Context, string, string) (net.Conn, error)
	lookup func(context.Context, string) ([]netip.Addr, error)
}

var defaultResolver = New()

func New() *Resolver {
	return &Resolver{
		cache:  make(map[string]netip.Addr),
		dial:   (&net.Dialer{}).DialContext,
		lookup: lookupHost,
	}
}

// Default returns the process-wide resolver. Its cache holds only addresses
// that have completed a TCP connection successfully.
func Default() *Resolver { return defaultResolver }

// DialContext first tries the last working address. It does not expire the
// cache on a timer: a fresh name lookup happens only after that reconnect
// fails. This prevents mDNS/NBNS flapping from breaking an established edge.
func (resolver *Resolver) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split LAN address %q: %w", address, err)
	}
	host = strings.Trim(host, "[]")
	if ip, err := netip.ParseAddr(host); err == nil {
		return resolver.dial(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	key := strings.ToLower(host)
	if cached, ok := resolver.cached(key); ok {
		connection, dialErr := resolver.dial(ctx, network, net.JoinHostPort(cached.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		resolver.forget(key, cached)
	}
	addresses, lookupErr := resolver.lookup(ctx, host)
	if lookupErr != nil && len(addresses) == 0 {
		return nil, fmt.Errorf("resolve LAN host %q: %w", host, lookupErr)
	}
	var dialErrors []error
	for _, candidate := range addresses {
		connection, dialErr := resolver.dial(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			resolver.remember(key, candidate)
			return connection, nil
		}
		dialErrors = append(dialErrors, fmt.Errorf("%s: %w", candidate, dialErr))
	}
	if lookupErr != nil {
		dialErrors = append(dialErrors, lookupErr)
	}
	return nil, fmt.Errorf("connect LAN host %q: %w", host, errors.Join(dialErrors...))
}

func (resolver *Resolver) cached(key string) (netip.Addr, bool) {
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	value, ok := resolver.cache[key]
	return value, ok
}

func (resolver *Resolver) remember(key string, address netip.Addr) {
	resolver.mu.Lock()
	resolver.cache[key] = address
	resolver.mu.Unlock()
}

func (resolver *Resolver) forget(key string, address netip.Addr) {
	resolver.mu.Lock()
	if resolver.cache[key] == address {
		delete(resolver.cache, key)
	}
	resolver.mu.Unlock()
}

// HTTPClient retains URL host/SNI while its transport dials through the same
// reconnect-sticky resolver. It is for trusted controller bridge endpoints,
// not public downloader traffic.
func HTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = Default().DialContext
	return &http.Client{Transport: transport}
}

func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	result := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.IsValid() {
			result = append(result, address.Unmap())
		}
	}
	if len(result) != 0 {
		return unique(result), nil
	}
	fallback, fallbackErr := lookupPlatform(ctx, host)
	if len(fallback) != 0 {
		return unique(fallback), nil
	}
	return nil, errors.Join(err, fallbackErr)
}

func unique(values []netip.Addr) []netip.Addr {
	seen := make(map[netip.Addr]struct{}, len(values))
	result := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		value = value.Unmap()
		if value.IsValid() {
			if _, exists := seen[value]; !exists {
				seen[value] = struct{}{}
				result = append(result, value)
			}
		}
	}
	return result
}
