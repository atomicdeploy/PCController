package netpolicy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (resolve resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return resolve(ctx, network, host)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestResolvePublicHostRejectsMixedDNSAnswers(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, network, host string) ([]netip.Addr, error) {
		if network != "ip" || host != "updates.example.com" {
			t.Fatalf("lookup network=%q host=%q", network, host)
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.7")}, nil
	})
	if _, err := ResolvePublicHost(context.Background(), resolver, "updates.example.com"); err == nil ||
		!strings.Contains(err.Error(), "non-public") {
		t.Fatalf("mixed DNS answer error = %v", err)
	}
}

func TestPinnedDialContextDialsValidatedNumericAddress(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "updates.example.com" {
			t.Fatalf("resolved host=%q", host)
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
	var dialed string
	dial := func(_ context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Fatalf("dial network=%q", network)
		}
		dialed = address
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	connection, err := pinnedDialContext(resolver, dial, "")(context.Background(), "tcp", "updates.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if dialed != "1.1.1.1:443" {
		t.Fatalf("dialed %q instead of the validated address", dialed)
	}
}

func TestPublicTransportBindsProxyTrustToActualSelection(t *testing.T) {
	resolver := resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		switch host {
		case "updates.example.com":
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		case "proxy.example.com":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		default:
			return nil, errors.New("unexpected host " + host)
		}
	})
	proxyURL, _ := url.Parse("http://proxy.example.com:3128")
	selector := func(request *http.Request) (*url.URL, error) {
		if request.Header.Get("Use-Test-Proxy") == "yes" {
			return proxyURL, nil
		}
		return nil, nil // models NO_PROXY/direct selection
	}
	transport := NewPublicTransport(nil, selector, resolver, "artifact URL").(*publicRoundTripper)
	var dialed []string
	transport.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = append(dialed, address)
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}

	directRequest, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/file.hex", nil)
	selected, err := transport.proxy(directRequest)
	if err != nil || selected != nil {
		t.Fatalf("direct selection=%v, %v", selected, err)
	}
	direct, err := transport.transportFor(selected)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := direct.DialContext(context.Background(), "tcp", "proxy.example.com:3128"); err == nil ||
		!strings.Contains(err.Error(), "non-public") {
		t.Fatalf("bypassed proxy authority was trusted for a direct request: %v", err)
	}
	if len(dialed) != 0 {
		t.Fatalf("direct request dialed an unvalidated address: %v", dialed)
	}

	proxiedRequest := directRequest.Clone(context.Background())
	proxiedRequest.Header.Set("Use-Test-Proxy", "yes")
	selected, err = transport.proxy(proxiedRequest)
	if err != nil || selected == nil {
		t.Fatalf("proxy selection=%v, %v", selected, err)
	}
	proxied, err := transport.transportFor(selected)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := proxied.DialContext(context.Background(), "tcp", "proxy.example.com:3128")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if len(dialed) != 1 || dialed[0] != "proxy.example.com:3128" {
		t.Fatalf("selected proxy dial=%v", dialed)
	}
}

func TestPublicClientDoesNotInheritInjectedDialOrProxyCapabilities(t *testing.T) {
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")
	t.Setenv("no_proxy", "")
	unsafeDialUsed := false
	unsafeProxyUsed := false
	template := &http.Client{Transport: &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			unsafeDialUsed = true
			return nil, errors.New("unsafe injected dial")
		},
		Proxy: func(*http.Request) (*url.URL, error) {
			unsafeProxyUsed = true
			return url.Parse("http://127.0.0.1:3128")
		},
	}}
	client, err := NewPublicHTTPClient(template, PublicHTTPClientOptions{
		Subject: "artifact URL",
		Resolver: resolverFunc(func(_ context.Context, _, _ string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	public := client.Transport.(*publicRoundTripper)
	request, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/file.hex", nil)
	selected, err := public.proxy(request)
	if err != nil || selected != nil || unsafeProxyUsed {
		t.Fatalf("public proxy selection=%v, %v; injected=%v", selected, err, unsafeProxyUsed)
	}
	public.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		if address != "1.1.1.1:443" {
			t.Fatalf("public dial received %q", address)
		}
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	direct, err := public.transportFor(nil)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := direct.DialContext(context.Background(), "tcp", "updates.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if unsafeDialUsed {
		t.Fatal("public client inherited an injected dial capability")
	}
}

func TestNewPublicHTTPClientRejectsCustomRoundTripper(t *testing.T) {
	called := false
	template := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not run")
	})}
	_, err := NewPublicHTTPClient(template, PublicHTTPClientOptions{Subject: "artifact URL"})
	if err == nil || !strings.Contains(err.Error(), "explicit trusted client") {
		t.Fatalf("custom transport error=%v", err)
	}
	if called {
		t.Fatal("rejected custom transport was invoked")
	}
}

func TestHTTPRedirectPolicyRejectsDowngradeAndConfinesCredentials(t *testing.T) {
	origin, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/file.hex", nil)
	downgrade, _ := http.NewRequest(http.MethodGet, "http://updates.example.com/file.hex", nil)
	policy := HTTPRedirectPolicy{Operation: "artifact download", Subject: "artifact URL", Scope: HTTPDestinationPublic}
	if err := policy.CheckRedirect(downgrade, []*http.Request{origin}); err == nil || !strings.Contains(err.Error(), "HTTPS-to-HTTP") {
		t.Fatalf("downgrade error=%v", err)
	}

	crossAuthority, _ := http.NewRequest(http.MethodGet, "https://cdn.example.com/file.hex", nil)
	crossAuthority.Header.Set("Authorization", "Bearer secret")
	policy.Previous = func(request *http.Request, _ []*http.Request) error {
		request.Header.Set("Authorization", "Bearer restored")
		return nil
	}
	if err := policy.CheckRedirect(crossAuthority, []*http.Request{origin}); err != nil {
		t.Fatal(err)
	}
	if credential := crossAuthority.Header.Get("Authorization"); credential != "" {
		t.Fatalf("cross-authority credential=%q", credential)
	}
}

func TestValidateResolvedPublicURLRejectsUnsafeEffectiveDestination(t *testing.T) {
	target, _ := url.Parse("http://169.254.169.254/latest/meta-data/")
	err := validateResolvedPublicURL(context.Background(), nil, target, "artifact URL")
	if err == nil {
		t.Fatalf("effective URL error=%v", err)
	}
}
