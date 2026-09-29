package ipcjson

import (
	"net/http/httptest"
	"testing"
)

func TestOriginIdentityCannotBeAssertedByHostHeader(t *testing.T) {
	for _, target := range []string{"http://unconfigured.example/api/rpc", "http://localhost:8787/api/rpc"} {
		request := httptest.NewRequest("POST", target, nil)
		request.Header.Set("Origin", "http://unconfigured.example")
		if httpOriginAllowed(request, []string{"localhost:*"}) {
			t.Fatal("unconfigured browser origin accepted")
		}
	}
	request := httptest.NewRequest("POST", "http://localhost/api/rpc", nil)
	for _, origin := range []string{"http://HOST.EXAMPLE:8787", "http://host.example.:8787", "http://[::1]:8787"} {
		request.Header.Set("Origin", origin)
		if !httpOriginAllowed(request, []string{"host.example:8787", "[::1]:*"}) {
			t.Fatalf("equivalent allowed identity rejected: %s", origin)
		}
	}
	request.Header.Set("Origin", "https://host.example")
	if !httpOriginAllowed(request, []string{"host.example:443"}) {
		t.Fatal("implicit standard port rejected")
	}
}

func TestLoopbackControlCenterOriginSurvivesNarrowLANConfiguration(t *testing.T) {
	request := httptest.NewRequest("GET", "http://127.0.0.1:8787/ipc", nil)
	for _, origin := range []string{
		"http://localhost:8787",
		"http://127.0.0.1:8787",
		"http://127.0.0.2:8787",
		"http://[::1]:8787",
	} {
		request.Header.Set("Origin", origin)
		if !httpOriginAllowed(request, []string{"cafe-pc.local:*"}) {
			t.Fatalf("loopback control-center origin rejected: %s", origin)
		}
	}
	request.Header.Set("Origin", "https://hostile.example")
	if httpOriginAllowed(request, []string{"cafe-pc.local:*"}) {
		t.Fatal("unconfigured remote origin accepted")
	}
}
