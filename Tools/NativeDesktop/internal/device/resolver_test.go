package device

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestDialTCPReusesAndInvalidatesLocalResolution(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan error, 1)
	go func() {
		for index := 0; index < 2; index++ {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				accepted <- acceptErr
				return
			}
			_ = connection.Close()
		}
		accepted <- nil
	}()

	resolver := &countingResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}
	client, err := New("http://pccontroller-desktop.local", WithResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		connection, dialErr := client.DialTCP(context.Background(), port)
		if dialErr != nil {
			t.Fatalf("DialTCP attempt %d: %v", index+1, dialErr)
		}
		_ = connection.Close()
	}
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	if got := resolver.count(); got != 1 {
		t.Fatalf("resolver calls after two successful TCP dials = %d, want 1", got)
	}

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if connection, dialErr := client.DialTCP(ctx, port); dialErr == nil {
		_ = connection.Close()
		t.Fatal("DialTCP unexpectedly succeeded after listener shutdown")
	}
	if got := resolver.count(); got != 1 {
		t.Fatalf("failed cached dial resolved early: calls = %d, want 1", got)
	}
	if connection, dialErr := client.DialTCP(ctx, port); dialErr == nil {
		_ = connection.Close()
		t.Fatal("second DialTCP unexpectedly succeeded after listener shutdown")
	}
	if got := resolver.count(); got != 2 {
		t.Fatalf("resolver calls after cached-address failure = %d, want 2", got)
	}
}

func TestExplicitRecoveryInvalidatesLocalResolution(t *testing.T) {
	t.Parallel()

	resolver := &countingResolver{
		ips: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}},
	}
	client, err := New("http://pccontroller-desktop.local", WithResolver(resolver))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if _, err := client.resolution.lookup(
			context.Background(),
			"pccontroller-desktop.local",
		); err != nil {
			t.Fatal(err)
		}
	}
	if got := resolver.count(); got != 1 {
		t.Fatalf("healthy cache resolver calls = %d, want 1", got)
	}

	client.InvalidateResolution()
	if _, err := client.resolution.lookup(
		context.Background(),
		"pccontroller-desktop.local",
	); err != nil {
		t.Fatal(err)
	}
	if got := resolver.count(); got != 2 {
		t.Fatalf("post-recovery resolver calls = %d, want 2", got)
	}
}
