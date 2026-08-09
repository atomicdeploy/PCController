package netpolicy

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

// localNetworkHostnameSuffixes is the single source for both public-source
// rejection and the default process-wide proxy bypass list.
var localNetworkHostnameSuffixes = []string{
	".home.arpa", ".internal", ".lan", ".local", ".localdomain", ".localhost",
}

// LocalNetworkNoProxyEntries are deliberately address ranges, not proxy
// endpoints. They keep controller IPC, WebSocket, bridge, discovery, and local
// integration traffic on the LAN even when an operator configures a proxy for
// dependency downloads.
var LocalNetworkNoProxyEntries = func() []string {
	result := []string{"localhost"}
	result = append(result, localNetworkHostnameSuffixes...)
	return append(result,
		"127.0.0.0/8", "::1",
		"10.0.0.0/8", "100.64.0.0/10", "169.254.0.0/16",
		"172.16.0.0/12", "192.168.0.0/16",
		"fc00::/7", "fe80::/10", "fec0::/10",
	)
}()

// EnvironmentProxySelector reads standard proxy variables through Go's
// canonical parser. It does not invent or persist a proxy endpoint.
func EnvironmentProxySelector() ProxySelector {
	configuration := httpproxy.FromEnvironment()
	selectProxy := configuration.ProxyFunc()
	return func(request *http.Request) (*url.URL, error) {
		if request == nil || request.URL == nil {
			return nil, errors.New("proxy selection requires a request URL")
		}
		return selectProxy(request.URL)
	}
}

// WithLocalNetworkNoProxy returns an isolated environment with the caller's
// NO_PROXY entries preserved and the standard loopback/private/link-local
// ranges appended. Proxy URLs remain entirely caller-owned.
func WithLocalNetworkNoProxy(environment []string) []string {
	values := make([]string, 0, len(LocalNetworkNoProxyEntries)+8)
	seen := make(map[string]bool)
	appendValues := func(csv string) {
		for _, entry := range strings.Split(csv, ",") {
			entry = strings.TrimSpace(entry)
			key := strings.ToLower(entry)
			if entry == "" || seen[key] {
				continue
			}
			seen[key] = true
			values = append(values, entry)
		}
	}
	result := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(strings.TrimSpace(name), "NO_PROXY") {
			appendValues(value)
			continue
		}
		result = append(result, entry)
	}
	for _, entry := range LocalNetworkNoProxyEntries {
		appendValues(entry)
	}
	merged := strings.Join(values, ",")
	// Emit both spellings because dependency CLIs vary, while Go's proxy
	// parser gives the uppercase form precedence.
	result = append(result, "NO_PROXY="+merged, "no_proxy="+merged)
	return result
}

// EnsureProcessLocalNetworkNoProxy installs only the merged bypass list into
// this process. It does not create, change, or persist any proxy endpoint.
func EnsureProcessLocalNetworkNoProxy() error {
	environment := WithLocalNetworkNoProxy(os.Environ())
	var merged string
	for _, entry := range environment {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name == "NO_PROXY" {
			merged = value
			break
		}
	}
	if err := os.Setenv("NO_PROXY", merged); err != nil {
		return err
	}
	return os.Setenv("no_proxy", merged)
}

// SortedLocalNetworkNoProxyEntries is a stable diagnostic/test view.
func SortedLocalNetworkNoProxyEntries() []string {
	result := append([]string(nil), LocalNetworkNoProxyEntries...)
	sort.Strings(result)
	return result
}
