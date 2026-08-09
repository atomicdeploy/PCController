package netpolicy

import "testing"

func TestParseHTTPURLAcceptsExplicitLocalAndRemotePeers(t *testing.T) {
	for _, target := range []string{
		"https://updates.example.com/releases/board.hex?channel=stable",
		"http://127.0.0.1:8787/api/discovery/manifest",
		"http://[::1]:8787/api/artifacts",
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

func TestParsePublicHTTPURLRejectsLocalAndMetadataDestinations(t *testing.T) {
	for _, target := range []string{
		"http://127.0.0.1:8787/artifact.hex",
		"http://10.20.30.40/artifact.hex",
		"http://100.64.0.1/artifact.hex",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]/artifact.hex",
		"http://[::ffff:127.0.0.1]/artifact.hex",
		"http://[::ffff:169.254.169.254]/latest/meta-data/",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"https://controller.lan/artifact.hex",
		"https://metadata.google.internal/computeMetadata/v1/",
	} {
		if parsed, err := ParsePublicHTTPURL(target, "artifact URL"); err == nil {
			t.Fatalf("non-public artifact URL accepted: %q -> %v", target, parsed)
		}
	}
}

func TestParsePublicHTTPURLAcceptsPublicNamesAndAddresses(t *testing.T) {
	for _, target := range []string{
		"https://updates.example.com/releases/board.hex",
		"https://1.1.1.1/releases/board.hex",
		"https://[2606:4700:4700::1111]/releases/board.hex",
	} {
		parsed, err := ParsePublicHTTPURL(target, "artifact URL")
		if err != nil || parsed.String() != target {
			t.Fatalf("ParsePublicHTTPURL(%q) = %v, %v", target, parsed, err)
		}
	}
}
