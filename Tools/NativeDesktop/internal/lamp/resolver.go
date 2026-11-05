package lamp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// Resolver is implemented by net.Resolver and permits deterministic resolver
// behavior in tests.
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type resolutionCache struct {
	mu       sync.Mutex
	resolver Resolver
	host     string
	ip       string
}

func newResolutionCache(resolver Resolver) *resolutionCache {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &resolutionCache{resolver: resolver}
}

// WithResolver changes the resolver used for .local hosts. HTTP and WebSocket
// share the resulting one-entry cache.
func WithResolver(resolver Resolver) Option {
	return func(c *Client) error {
		if resolver == nil {
			return errors.New("lamp: nil resolver")
		}
		c.resolution = newResolutionCache(resolver)
		return nil
	}
}

func (c *Client) configureNetworkClients() error {
	httpCopy := *c.httpClient
	httpCopy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		// Device endpoints are exact and diagnostic fetches must not be redirected
		// to another host or a state-changing path.
		return http.ErrUseLastResponse
	}

	if c.usesLocalResolution() {
		var transport *http.Transport
		switch current := httpCopy.Transport.(type) {
		case nil:
			transport = http.DefaultTransport.(*http.Transport).Clone()
		case *http.Transport:
			transport = current.Clone()
		default:
			return errors.New("lamp: .local resolution requires an *http.Transport")
		}
		if transport.DialTLSContext != nil || transport.DialTLS != nil {
			return errors.New("lamp: .local resolution cannot use a custom HTTP TLS dialer")
		}
		fallback := transport.DialContext
		if fallback == nil {
			netDialer := &net.Dialer{}
			fallback = netDialer.DialContext
		}
		transport.DialContext = c.cachedDialContext(fallback)
		// A local hardware endpoint should never be sent through an HTTP proxy.
		transport.Proxy = nil
		httpCopy.Transport = transport

		if c.wsDialer.NetDialTLSContext != nil {
			return errors.New("lamp: .local resolution cannot use a custom WebSocket TLS dialer")
		}
		wsFallback := c.wsDialer.NetDialContext
		if wsFallback == nil {
			netDialer := &net.Dialer{}
			wsFallback = netDialer.DialContext
		}
		c.wsDialer.NetDialContext = c.cachedDialContext(wsFallback)
		c.wsDialer.Proxy = nil
	}
	c.httpClient = &httpCopy
	return nil
}

func (c *Client) usesLocalResolution() bool {
	if c == nil || c.baseURL == nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(c.baseURL.Hostname(), "."))
	return strings.HasSuffix(host, ".local") && net.ParseIP(host) == nil
}

func (c *Client) cachedDialContext(fallback func(context.Context, string, string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(c.baseURL.Hostname(), ".")) {
			return fallback(ctx, network, address)
		}

		ip, err := c.resolution.lookup(ctx, c.baseURL.Hostname())
		if err != nil {
			return nil, err
		}
		resolvedAddress := net.JoinHostPort(ip, port)
		connection, err := fallback(ctx, network, resolvedAddress)
		if err != nil {
			c.resolution.invalidateIP(ip)
			return nil, fmt.Errorf("dial %s via %s: %w", address, resolvedAddress, err)
		}
		return connection, nil
	}
}

// DialTCP opens a TCP connection to the configured lamp hostname on port. For
// .local hosts it shares the exact one-entry resolution cache used by HTTP and
// WebSocket traffic; a failed cached-address dial invalidates that entry so the
// next connection attempt resolves again.
func (c *Client) DialTCP(
	ctx context.Context,
	port int,
) (net.Conn, error) {
	if c == nil || c.baseURL == nil {
		return nil, errors.New("lamp: nil client")
	}
	if ctx == nil {
		return nil, ErrNilContext
	}
	if port < 1 || port > 65535 {
		return nil, errors.New("lamp: TCP port must be between 1 and 65535")
	}
	address := net.JoinHostPort(c.baseURL.Hostname(), strconv.Itoa(port))
	netDialer := &net.Dialer{}
	dial := netDialer.DialContext
	if c.usesLocalResolution() {
		dial = c.cachedDialContext(dial)
	}
	return dial(ctx, "tcp", address)
}

// InvalidateResolution forgets the one cached .local address so an explicit
// recovery after resume or a host network change resolves the device again.
// Healthy HTTP and WebSocket operation never calls this method; ordinary
// cached-address failures already invalidate the same entry automatically.
func (c *Client) InvalidateResolution() {
	if c == nil || c.resolution == nil {
		return
	}
	c.resolution.invalidate()
}

func (r *resolutionCache) lookup(ctx context.Context, host string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.EqualFold(r.host, host) && r.ip != "" {
		return r.ip, nil
	}
	addresses, err := r.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", host, err)
	}
	ip := preferredIP(addresses)
	if ip == "" {
		return "", fmt.Errorf("resolve %s: no usable IP address", host)
	}
	r.host = host
	r.ip = ip
	return ip, nil
}

func preferredIP(addresses []net.IPAddr) string {
	for _, address := range addresses {
		if address.IP != nil && address.IP.To4() != nil {
			return address.IP.String()
		}
	}
	for _, address := range addresses {
		if address.IP != nil {
			return address.IP.String()
		}
	}
	return ""
}

func (r *resolutionCache) invalidateIP(ip string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ip == ip {
		r.ip = ""
	}
}

func (r *resolutionCache) invalidate() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.ip = ""
	r.mu.Unlock()
}
