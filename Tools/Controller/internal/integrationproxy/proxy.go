// Package integrationproxy exposes explicitly configured local integrations
// through a narrow same-origin HTTP namespace. Callers resolve a short target
// name to a Target created by one of this package's normalization helpers; a
// request can never supply an upstream URL directly.
package integrationproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultTLSHandshakeTimeout   = 5 * time.Second
	defaultResponseHeaderTimeout = 15 * time.Second
	defaultIdleConnTimeout       = 60 * time.Second
	defaultMaxResponseHeaderSize = 1 << 20
)

// ErrTargetNotFound tells Handler that a requested integration name is not
// configured. Resolver implementations should return this error rather than a
// zero Target when a name is absent.
var ErrTargetNotFound = errors.New("integration target not found")

// Resolver maps a short, validated integration name (for example, "datahub")
// to a preconfigured Target. It must not derive a URL from request data.
type Resolver interface {
	ResolveIntegrationTarget(context.Context, string) (Target, error)
}

// ResolverFunc adapts a function to Resolver.
type ResolverFunc func(context.Context, string) (Target, error)

// ResolveIntegrationTarget implements Resolver.
func (resolve ResolverFunc) ResolveIntegrationTarget(
	ctx context.Context,
	name string,
) (Target, error) {
	return resolve(ctx, name)
}

// StaticResolver is an immutable, in-memory target allowlist.
type StaticResolver struct {
	targets map[string]Target
}

// NewStaticResolver copies and validates a target allowlist. Target names are
// intentionally strict and case-sensitive so URL-path aliases cannot diverge.
func NewStaticResolver(targets map[string]Target) (*StaticResolver, error) {
	copyTargets := make(map[string]Target, len(targets))
	for name, target := range targets {
		if !validTargetName(name) {
			return nil, fmt.Errorf("invalid integration target name %q", name)
		}
		if err := target.validate(); err != nil {
			return nil, fmt.Errorf("integration target %q: %w", name, err)
		}
		copyTargets[name] = target
	}
	return &StaticResolver{targets: copyTargets}, nil
}

// ResolveIntegrationTarget implements Resolver.
func (resolver *StaticResolver) ResolveIntegrationTarget(
	_ context.Context,
	name string,
) (Target, error) {
	if resolver == nil {
		return Target{}, ErrTargetNotFound
	}
	target, ok := resolver.targets[name]
	if !ok {
		return Target{}, ErrTargetNotFound
	}
	return target, nil
}

// Handler reverse-proxies requests below one mount prefix. It is safe for
// concurrent use.
type Handler struct {
	mountPrefix string
	resolver    Resolver
	transport   *http.Transport
	bufferPool  *proxyBufferPool
}

// NewHandler constructs a bounded integration proxy. A route has the form
//
//	<mountPrefix>/<target-name>/<upstream-path>
//
// The mount prefix is matched on a complete path-segment boundary. The target
// name is resolved only through resolver, then removed along with the prefix.
func NewHandler(mountPrefix string, resolver Resolver) (*Handler, error) {
	normalizedPrefix, err := NormalizeMountPrefix(mountPrefix)
	if err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, errors.New("integration target resolver is required")
	}
	return &Handler{
		mountPrefix: normalizedPrefix,
		resolver:    resolver,
		transport:   newBoundedTransport(),
		bufferPool:  newProxyBufferPool(),
	}, nil
}

// NormalizeMountPrefix validates and canonicalizes a non-root URL-path mount.
// A single trailing slash is accepted and removed for ergonomic mux wiring.
func NormalizeMountPrefix(value string) (string, error) {
	if value == "" || value[0] != '/' {
		return "", errors.New("integration proxy mount prefix must start with /")
	}
	if strings.ContainsAny(value, "\\?#%\x00\r\n\t") {
		return "", errors.New("integration proxy mount prefix contains unsafe characters")
	}
	value = strings.TrimSuffix(value, "/")
	if value == "" || value == "/" {
		return "", errors.New("integration proxy mount prefix must not be root")
	}
	if path.Clean(value) != value || strings.Contains(value, "//") {
		return "", errors.New("integration proxy mount prefix must be canonical")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(value, "/"), "/") {
		if !validMountSegment(segment) {
			return "", errors.New("integration proxy mount prefix contains an invalid segment")
		}
	}
	return value, nil
}

// CloseIdleConnections releases idle upstream keep-alive connections. Active
// streams and WebSockets are not interrupted.
func (handler *Handler) CloseIdleConnections() {
	if handler != nil && handler.transport != nil {
		handler.transport.CloseIdleConnections()
	}
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	name, upstreamPath, ok := handler.route(request.URL.Path, request.URL.RawPath)
	if !ok {
		writeProxyError(writer, http.StatusNotFound, "integration route not found")
		return
	}

	target, err := handler.resolver.ResolveIntegrationTarget(request.Context(), name)
	if err != nil {
		if errors.Is(err, ErrTargetNotFound) {
			writeProxyError(writer, http.StatusNotFound, "integration target not found")
			return
		}
		writeProxyError(writer, http.StatusBadGateway, "integration target unavailable")
		return
	}
	if err := target.validate(); err != nil {
		writeProxyError(writer, http.StatusBadGateway, "integration target unavailable")
		return
	}
	if target.policy == policyDevice {
		// Device actions belong to the bounded typed controller API. A generic
		// path proxy would bypass its method, payload, and capability checks.
		writeProxyError(writer, http.StatusNotFound, "device target requires typed controller operations")
		return
	}

	baseURL := target.cloneURL()
	proxy := &httputil.ReverseProxy{
		Transport:     handler.transport,
		BufferPool:    handler.bufferPool,
		FlushInterval: -1,
		Rewrite: func(proxyRequest *httputil.ProxyRequest) {
			out := proxyRequest.Out
			out.URL.Scheme = baseURL.Scheme
			out.URL.Host = baseURL.Host
			out.URL.User = nil
			out.URL.Opaque = ""
			out.URL.Path = upstreamPath
			// Canonical re-escaping avoids preserving an encoded slash or dot
			// segment from the untrusted request path.
			out.URL.RawPath = ""
			out.URL.RawQuery = stripAccessToken(proxyRequest.In.URL.RawQuery)
			out.URL.ForceQuery = out.URL.RawQuery != "" && proxyRequest.In.URL.ForceQuery
			out.URL.Fragment = ""
			out.URL.RawFragment = ""
			out.URL.OmitHost = false
			out.Host = baseURL.Host
			stripClientCredentials(out.Header)
		},
		ModifyResponse: func(response *http.Response) error {
			// An integration must not plant cookies in the PCController origin.
			response.Header.Del("Set-Cookie")
			response.Header.Del("Set-Cookie2")
			return nil
		},
		ErrorHandler: func(response http.ResponseWriter, _ *http.Request, _ error) {
			writeProxyError(response, http.StatusBadGateway, "integration upstream unavailable")
		},
	}
	proxy.ServeHTTP(writer, request)
}

func (handler *Handler) route(requestPath, rawPath string) (string, string, bool) {
	prefix := handler.mountPrefix + "/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", "", false
	}
	if hasUnsafeEscapedPath(rawPath) {
		return "", "", false
	}
	remainder := strings.TrimPrefix(requestPath, prefix)
	name, tail, found := strings.Cut(remainder, "/")
	if !validTargetName(name) {
		return "", "", false
	}
	upstreamPath := "/"
	if found {
		upstreamPath += tail
	}
	if !safeUpstreamPath(upstreamPath) {
		return "", "", false
	}
	return name, upstreamPath, true
}

func validTargetName(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for index, character := range []byte(value) {
		if character >= 'a' && character <= 'z' ||
			(index > 0 && character >= '0' && character <= '9') ||
			(index > 0 && (character == '-' || character == '_')) {
			continue
		}
		return false
	}
	return true
}

func validMountSegment(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, character := range []byte(value) {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-._~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func safeUpstreamPath(value string) bool {
	if value == "" || value[0] != '/' || strings.Contains(value, "//") ||
		strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

type proxyBufferPool struct {
	pool sync.Pool
}

func newProxyBufferPool() *proxyBufferPool {
	return &proxyBufferPool{pool: sync.Pool{New: func() any {
		return make([]byte, 32<<10)
	}}}
}

func (pool *proxyBufferPool) Get() []byte {
	return pool.pool.Get().([]byte)
}

func (pool *proxyBufferPool) Put(buffer []byte) {
	if cap(buffer) < 32<<10 {
		return
	}
	pool.pool.Put(buffer[:32<<10])
}

func hasUnsafeEscapedPath(rawPath string) bool {
	if rawPath == "" {
		return false
	}
	lower := strings.ToLower(rawPath)
	return strings.Contains(lower, "%2f") ||
		strings.Contains(lower, "%5c") ||
		strings.Contains(lower, "%2e")
}

func stripAccessToken(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	kept := parts[:0]
	for _, part := range parts {
		if rawQueryPartContainsAccessToken(part) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

func rawQueryPartContainsAccessToken(part string) bool {
	// A few legacy parsers treat semicolons as separators. Conservatively drop
	// the whole ampersand-delimited part if any such sub-part is the host token.
	for _, subpart := range strings.Split(part, ";") {
		key, _, _ := strings.Cut(subpart, "=")
		decoded, err := url.QueryUnescape(key)
		if err == nil && decoded == "access_token" {
			return true
		}
	}
	return false
}

func stripClientCredentials(header http.Header) {
	for _, name := range []string{
		"Authorization",
		"Proxy-Authorization",
		"Cookie",
		"Cookie2",
		"X-Api-Key",
		"X-Auth-Token",
		"X-Access-Token",
		"X-PCController-Token",
	} {
		header.Del(name)
	}
}

func writeProxyError(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(map[string]string{"error": message})
}

func newBoundedTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}
	return &http.Transport{
		// Local integrations must never be redirected through an environment
		// proxy, and dialLocalNetwork prevents DNS from escaping the LAN policy.
		Proxy:                  nil,
		DialContext:            dialLocalNetwork(dialer),
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           32,
		MaxIdleConnsPerHost:    8,
		MaxConnsPerHost:        32,
		IdleConnTimeout:        defaultIdleConnTimeout,
		TLSHandshakeTimeout:    defaultTLSHandshakeTimeout,
		ResponseHeaderTimeout:  defaultResponseHeaderTimeout,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: defaultMaxResponseHeaderSize,
		DisableCompression:     true,
		ReadBufferSize:         32 << 10,
		WriteBufferSize:        32 << 10,
	}
}

func dialLocalNetwork(dialer *net.Dialer) func(
	context.Context,
	string,
	string,
) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("invalid integration upstream address: %w", err)
		}
		if literal := net.ParseIP(strings.Trim(host, "[]")); literal != nil {
			if !isLANIP(literal) {
				return nil, errors.New("integration upstream resolved outside the local network")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(literal.String(), port))
		}

		lookupContext, cancel := context.WithTimeout(ctx, defaultDialTimeout)
		defer cancel()
		addresses, err := net.DefaultResolver.LookupIPAddr(lookupContext, host)
		if err != nil {
			return nil, fmt.Errorf("resolve integration upstream: %w", err)
		}
		var lastErr error
		for _, candidate := range addresses {
			if !isLANIP(candidate.IP) {
				continue
			}
			candidateAddress := net.JoinHostPort(candidate.IP.String(), port)
			connection, dialErr := dialer.DialContext(lookupContext, network, candidateAddress)
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("integration upstream resolved outside the local network")
	}
}
