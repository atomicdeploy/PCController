package appconfig

import "testing"

func TestAllowedOriginPatternsUseExactHostIdentity(t *testing.T) {
	for _, value := range []string{"localhost:*", "host.example:8787", "127.0.0.1:80", "[::1]:*"} {
		if err := validateAllowedOriginPattern(value); err != nil {
			t.Fatalf("%s: %v", value, err)
		}
	}
	for _, value := range []string{"*", "*:*", "*.example:8787", "ho?t.example:80", "http://localhost:8787", "localhost:0", "localhost:65536", "localhost", "-host:80", "host:abc"} {
		if err := validateAllowedOriginPattern(value); err == nil {
			t.Fatalf("unsafe origin accepted: %s", value)
		}
	}
}
