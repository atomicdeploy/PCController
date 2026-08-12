package lanresolver

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestCachedAddressIsRetainedUntilReconnectFails(t *testing.T) {
	resolver := New()
	lookups := 0
	resolver.lookup = func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("192.0.2.10")}, nil
	}
	dials := []string{}
	resolver.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		dials = append(dials, address)
		return &net.TCPConn{}, nil
	}
	for range 2 {
		connection, err := resolver.DialContext(context.Background(), "tcp", "cafe-pc.local:8787")
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
	}
	if lookups != 1 || len(dials) != 2 || dials[0] != "192.0.2.10:8787" || dials[1] != "192.0.2.10:8787" {
		t.Fatalf("lookups=%d dials=%v", lookups, dials)
	}
}

func TestFailedCachedAddressIsReResolvedAndReplaced(t *testing.T) {
	resolver := New()
	values := [][]netip.Addr{{netip.MustParseAddr("192.0.2.11")}}
	lookups := 0
	resolver.lookup = func(context.Context, string) ([]netip.Addr, error) {
		value := values[lookups]
		lookups++
		return value, nil
	}
	resolver.dial = func(_ context.Context, _ string, address string) (net.Conn, error) {
		if address == "192.0.2.10:8787" {
			return nil, errors.New("stale")
		}
		return &net.TCPConn{}, nil
	}
	resolver.remember("cafe-pc.local", netip.MustParseAddr("192.0.2.10"))
	connection, err := resolver.DialContext(context.Background(), "tcp", "cafe-pc.local:8787")
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if lookups != 1 {
		t.Fatalf("lookups=%d", lookups)
	}
	if cached, ok := resolver.cached("cafe-pc.local"); !ok || cached.String() != "192.0.2.11" {
		t.Fatalf("cache=%v %t", cached, ok)
	}
}

func TestParseAddressesAcceptsNSSAndNetBIOSOutput(t *testing.T) {
	values := parseAddresses("192.168.100.155 STREAM cafe-pc.local\nquerying CAFE-PC on 192.168.100.255\n192.168.100.155 CAFE-PC<00>\n")
	if len(values) != 1 || values[0].String() != "192.168.100.155" {
		t.Fatalf("%v", values)
	}
}
