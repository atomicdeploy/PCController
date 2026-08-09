package netpolicy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
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
	connection, err := PinnedDialContext(resolver, dial)(context.Background(), "tcp", "updates.example.com:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if dialed != "1.1.1.1:443" {
		t.Fatalf("dialed %q instead of the validated address", dialed)
	}
}

func TestPinnedDialContextKeepsConfiguredProxyCompatibility(t *testing.T) {
	resolver := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, errors.New("proxy address must not use destination resolution")
	})
	var dialed string
	dial := func(_ context.Context, _ string, address string) (net.Conn, error) {
		dialed = address
		left, right := net.Pipe()
		_ = right.Close()
		return left, nil
	}
	connection, err := PinnedDialContext(resolver, dial, "127.0.0.1:3128")(
		context.Background(), "tcp", "127.0.0.1:3128",
	)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if dialed != "127.0.0.1:3128" {
		t.Fatalf("proxy dial=%q", dialed)
	}
}

func TestPublicRoundTripperRejectsRedirectToPrivateDestination(t *testing.T) {
	resolver := publicTestResolver(t)
	calls := 0
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://127.0.0.1/private.hex"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})
	request, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/board.hex", nil)
	_, err := (&http.Client{Transport: PublicRoundTripper(base, resolver, "artifact URL")}).Do(request)
	if err == nil || !strings.Contains(err.Error(), "public network destination") {
		t.Fatalf("redirect error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("private redirect reached transport; calls=%d", calls)
	}
}

func TestPublicRoundTripperRejectsUnsafeEffectiveResponseURL(t *testing.T) {
	resolver := publicTestResolver(t)
	base := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		effective := request.Clone(request.Context())
		effective.URL, _ = effective.URL.Parse("http://169.254.169.254/latest/meta-data/")
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("payload")), Request: effective,
		}, nil
	})
	request, _ := http.NewRequest(http.MethodGet, "https://updates.example.com/board.hex", nil)
	_, err := PublicRoundTripper(base, resolver, "artifact URL").RoundTrip(request)
	if err == nil || !strings.Contains(err.Error(), "final remote destination") {
		t.Fatalf("effective URL error = %v", err)
	}
}

func publicTestResolver(t *testing.T) IPResolver {
	t.Helper()
	return resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if host != "updates.example.com" {
			return nil, errors.New("unexpected host " + host)
		}
		return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
	})
}
