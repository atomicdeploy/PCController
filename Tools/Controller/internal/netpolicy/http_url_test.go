package netpolicy

import "testing"

func TestParseHTTPURLAcceptsExplicitLocalAndRemotePeers(t *testing.T) {
	for _, target := range []string{
		"https://updates.example.com/releases/board.hex?channel=stable",
		"http://127.0.0.1:8787/api/v1/discovery/manifest",
		"http://[::1]:8787/api/v1/artifacts",
		"https://controller.lan./firmware/latest.hex",
	} {
		parsed, err := ParseHTTPURL(target, "update URL")
		if err != nil || parsed.String() != target {
			t.Fatalf("ParseHTTPURL(%q) = %v, %v", target, parsed, err)
		}
	}
}

func TestParseHTTPURLRejectsUnsafeAuthoritiesAndSyntax(t *testing.T) {
	for _, target := range []string{
		"file:///etc/passwd",
		"https://user:secret@example.com/board.hex",
		"https://example.com/board.hex#section",
		"https://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0:8787/",
		"https://example.com\\@127.0.0.1/board.hex",
		"https://example.com/board.hex\nhttps://127.0.0.1/",
		"https://-invalid.example/board.hex",
		"https://example.com:0/board.hex",
	} {
		if parsed, err := ParseHTTPURL(target, "update URL"); err == nil {
			t.Fatalf("unsafe URL accepted: %q -> %v", target, parsed)
		}
	}
}
