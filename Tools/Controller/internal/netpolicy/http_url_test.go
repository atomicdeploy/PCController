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
		"http://0.1.2.3/artifact.hex",
		"http://127.0.0.1:8787/artifact.hex",
		"http://10.20.30.40/artifact.hex",
		"http://100.64.0.1/artifact.hex",
		"http://169.254.169.254/latest/meta-data/",
		"http://172.16.0.1/artifact.hex",
		"http://192.0.0.9/artifact.hex",
		"http://192.0.2.1/artifact.hex",
		"http://192.31.196.1/artifact.hex",
		"http://192.52.193.1/artifact.hex",
		"http://192.88.99.2/artifact.hex",
		"http://192.168.0.1/artifact.hex",
		"http://192.175.48.1/artifact.hex",
		"http://198.18.0.1/artifact.hex",
		"http://198.51.100.1/artifact.hex",
		"http://203.0.113.1/artifact.hex",
		"http://224.0.0.1/artifact.hex",
		"http://240.0.0.1/artifact.hex",
		"http://255.255.255.255/artifact.hex",
		"http://[::1]/artifact.hex",
		"http://[::ffff:127.0.0.1]/artifact.hex",
		"http://[::ffff:169.254.169.254]/latest/meta-data/",
		"http://[64:ff9b::808:808]/artifact.hex",
		"http://[64:ff9b:1::1]/artifact.hex",
		"http://[100::1]/artifact.hex",
		"http://[100:0:0:1::1]/artifact.hex",
		"http://[2001::1]/artifact.hex",
		"http://[2001:1::1]/artifact.hex",
		"http://[2001:1::2]/artifact.hex",
		"http://[2001:1::3]/artifact.hex",
		"http://[2001:2::1]/artifact.hex",
		"http://[2001:3::1]/artifact.hex",
		"http://[2001:4:112::1]/artifact.hex",
		"http://[2001:10::1]/artifact.hex",
		"http://[2001:20::1]/artifact.hex",
		"http://[2001:30::1]/artifact.hex",
		"http://[2001:db8::1]/artifact.hex",
		"http://[2002:c000:0201::1]/artifact.hex",
		"http://[2620:4f:8000::1]/artifact.hex",
		"http://[3fff::1]/artifact.hex",
		"http://[5f00::1]/artifact.hex",
		"http://[fd00:ec2::254]/latest/meta-data/",
		"http://[fe80::1]/artifact.hex",
		"http://[fec0::1]/artifact.hex",
		"http://[ff02::1]/artifact.hex",
		"https://controller.lan/artifact.hex",
		"https://controller.internal/artifact.hex",
		"https://controller.home.arpa/artifact.hex",
		"https://controller.local/artifact.hex",
		"https://controller.localdomain/artifact.hex",
		"https://controller.localhost/artifact.hex",
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
		"https://8.8.8.8/releases/board.hex",
		"https://[2001:4860:4860::8888]/releases/board.hex",
		"https://[2606:4700:4700::1111]/releases/board.hex",
		"https://[2a00:1450:4001:800::200e]/releases/board.hex",
	} {
		parsed, err := ParsePublicHTTPURL(target, "artifact URL")
		if err != nil || parsed.String() != target {
			t.Fatalf("ParsePublicHTTPURL(%q) = %v, %v", target, parsed, err)
		}
	}
}
