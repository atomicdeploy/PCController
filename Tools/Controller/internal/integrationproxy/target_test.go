package integrationproxy

import "testing"

func TestNormalizeDataHubTarget(t *testing.T) {
	for _, test := range []struct {
		value, want string
		valid       bool
	}{
		{value: "http://127.0.0.1:6060", want: "http://127.0.0.1:6060", valid: true},
		{value: "HTTPS://LOCALHOST:7443/", want: "https://localhost:7443", valid: true},
		{value: "http://[::1]:6060/", want: "http://[::1]:6060", valid: true},
		{value: "http://192.168.1.20:6060", valid: false},
		{value: "https://example.com", valid: false},
		{value: "http://user:password@localhost:6060", valid: false},
		{value: "http://localhost:6060/viewer", valid: false},
		{value: "http://localhost:6060?mode=viewer", valid: false},
		{value: "http://localhost:6060#viewer", valid: false},
		{value: "ftp://localhost:6060", valid: false},
		{value: " http://localhost:6060", valid: false},
		{value: "http://localhost:", valid: false},
		{value: "http://localhost../", valid: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			target, err := NormalizeDataHubTarget(test.value)
			if !test.valid {
				if err == nil {
					t.Fatalf("unexpectedly accepted as %q", target.URL())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := target.URL().String(); got != test.want {
				t.Fatalf("normalized URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeDeviceTarget(t *testing.T) {
	for _, test := range []struct {
		value, want string
		valid       bool
	}{
		{value: "http://192.168.4.1", want: "http://192.168.4.1", valid: true},
		{value: "https://10.20.30.40:8443/", want: "https://10.20.30.40:8443", valid: true},
		{value: "http://169.254.10.20", want: "http://169.254.10.20", valid: true},
		{value: "http://DEVICE-NODE.local:8080/", want: "http://device-node.local:8080", valid: true},
		{value: "http://DEVICE01/", want: "http://device01", valid: true},
		{value: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080", valid: true},
		{value: "http://8.8.8.8", valid: false},
		{value: "https://example.com", valid: false},
		{value: "http://user:password@192.168.4.1", valid: false},
		{value: "http://192.168.4.1/capability", valid: false},
		{value: "http://192.168.4.1?token=secret", valid: false},
		{value: "http://192.168.4.1#state", valid: false},
		{value: "ws://192.168.4.1", valid: false},
	} {
		t.Run(test.value, func(t *testing.T) {
			target, err := NormalizeDeviceTarget(test.value)
			if !test.valid {
				if err == nil {
					t.Fatalf("unexpectedly accepted as %q", target.URL())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := target.URL().String(); got != test.want {
				t.Fatalf("normalized URL = %q, want %q", got, test.want)
			}
		})
	}
}

func TestStaticResolverRejectsZeroTargetAndUnsafeName(t *testing.T) {
	for _, targets := range []map[string]Target{
		{"datahub": {}},
		{"http://example.com": mustDataHubTarget(t, "http://127.0.0.1")},
		{"DataHub": mustDataHubTarget(t, "http://127.0.0.1")},
	} {
		if _, err := NewStaticResolver(targets); err == nil {
			t.Fatalf("NewStaticResolver(%v) unexpectedly succeeded", targets)
		}
	}
}
