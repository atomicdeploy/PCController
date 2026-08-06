package netpolicy

import (
	"strings"
	"testing"
)

func TestWithLocalNetworkNoProxyPreservesCallerAndAddsPrivateRanges(t *testing.T) {
	environment := WithLocalNetworkNoProxy([]string{
		"PATH=test", "HTTPS_PROXY=http://proxy.invalid", "no_proxy=example.test,localhost",
	})
	joined := strings.Join(environment, "\n")
	for _, expected := range []string{
		"HTTPS_PROXY=http://proxy.invalid", "example.test", "localhost",
		"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"NO_PROXY=", "no_proxy=",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("merged environment missing %q:\n%s", expected, joined)
		}
	}
	for _, line := range strings.Split(joined, "\n") {
		if !strings.HasPrefix(strings.ToLower(line), "no_proxy=") {
			continue
		}
		count := 0
		for _, entry := range strings.Split(strings.TrimPrefix(line, strings.SplitN(line, "=", 2)[0]+"="), ",") {
			if entry == "localhost" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("caller duplicate was not removed from %q", line)
		}
	}
}

func TestWithLocalNetworkNoProxyDoesNotInventProxyEndpoint(t *testing.T) {
	joined := strings.ToUpper(strings.Join(WithLocalNetworkNoProxy([]string{"PATH=test"}), "\n"))
	for _, forbidden := range []string{"HTTP_PROXY=", "HTTPS_PROXY=", "ALL_PROXY=", "ARDUINO_NETWORK_PROXY="} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("bypass policy invented %s:\n%s", forbidden, joined)
		}
	}
}
