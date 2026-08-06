package integrationproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const testMountPrefix = "/api/integrations"

func TestProxyRewritesPathAndStripsClientCredentials(t *testing.T) {
	type observation struct {
		method, path, rawQuery, host, body string
		header                             http.Header
	}
	observed := make(chan observation, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read upstream body: %v", err)
		}
		observed <- observation{
			method: request.Method, path: request.URL.Path,
			rawQuery: request.URL.RawQuery, host: request.Host,
			body: string(body), header: request.Header.Clone(),
		}
		writer.Header().Set("X-Upstream", "preserved")
		writer.Header().Add("Set-Cookie", "upstream=forbidden; Path=/")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte("proxied"))
	}))
	defer upstream.Close()

	handler := mustProxy(t, map[string]Target{
		"datahub": mustDataHubTarget(t, upstream.URL),
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	defer handler.CloseIdleConnections()

	request, err := http.NewRequest(
		http.MethodPatch,
		proxy.URL+testMountPrefix+
			"/datahub/api/records/A%20B?keep=1&access_token=secret&empty=&keep=2&access%5Ftoken=encoded",
		strings.NewReader("streamed request body"),
	)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer pccontroller-secret")
	request.Header.Set("Proxy-Authorization", "Basic proxy-secret")
	request.Header.Set("Cookie", "session=pccontroller-secret")
	request.Header.Set("X-Api-Key", "client-key")
	request.Header.Set("X-Auth-Token", "client-token")
	request.Header.Set("Connection", "keep-alive, X-Hop")
	request.Header.Set("X-Hop", "remove-me")

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || string(data) != "proxied" {
		t.Fatalf("response = %d %q", response.StatusCode, data)
	}
	if got := response.Header.Get("X-Upstream"); got != "preserved" {
		t.Fatalf("X-Upstream = %q", got)
	}
	if got := response.Header.Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("Set-Cookie leaked from upstream: %q", got)
	}

	got := <-observed
	if got.method != http.MethodPatch {
		t.Errorf("method = %q", got.method)
	}
	if got.path != "/api/records/A B" {
		t.Errorf("path = %q", got.path)
	}
	if got.rawQuery != "keep=1&empty=&keep=2" {
		t.Errorf("raw query = %q", got.rawQuery)
	}
	if got.host != strings.TrimPrefix(upstream.URL, "http://") {
		t.Errorf("host = %q, want upstream host", got.host)
	}
	if got.body != "streamed request body" {
		t.Errorf("body = %q", got.body)
	}
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "Cookie", "X-Api-Key",
		"X-Auth-Token", "X-Hop",
	} {
		if value := got.header.Get(name); value != "" {
			t.Errorf("%s leaked upstream as %q", name, value)
		}
	}
	if got.header.Get("X-Forwarded-For") != "" || got.header.Get("Forwarded") != "" {
		t.Errorf("client forwarding headers leaked upstream: %v", got.header)
	}
}

func TestProxyPreservesRangeHEADContentRangeAndETag(t *testing.T) {
	content := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	const entityTag = `"fixture-v7"`
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/static/archive.bin" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("ETag", entityTag)
		http.ServeContent(
			writer,
			request,
			"archive.bin",
			time.Unix(1_700_000_000, 0).UTC(),
			bytes.NewReader(content),
		)
	}))
	defer upstream.Close()

	handler := mustProxy(t, map[string]Target{
		"datahub": mustDataHubTarget(t, upstream.URL),
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	defer handler.CloseIdleConnections()
	resourceURL := proxy.URL + testMountPrefix + "/datahub/static/archive.bin"

	request, err := http.NewRequest(http.MethodGet, resourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Range", "bytes=7-15")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != http.StatusPartialContent {
		t.Fatalf("range status = %d", response.StatusCode)
	}
	if got, want := string(data), string(content[7:16]); got != want {
		t.Errorf("range body = %q, want %q", got, want)
	}
	if got := response.Header.Get("Content-Range"); got != "bytes 7-15/36" {
		t.Errorf("Content-Range = %q", got)
	}
	if got := response.Header.Get("ETag"); got != entityTag {
		t.Errorf("ETag = %q", got)
	}
	if got := response.Header.Get("Content-Length"); got != "9" {
		t.Errorf("range Content-Length = %q", got)
	}

	head, err := http.NewRequest(http.MethodHead, resourceURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	headResponse, err := http.DefaultClient.Do(head)
	if err != nil {
		t.Fatal(err)
	}
	headBody, readErr := io.ReadAll(headResponse.Body)
	headResponse.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if headResponse.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d", headResponse.StatusCode)
	}
	if len(headBody) != 0 {
		t.Errorf("HEAD body length = %d", len(headBody))
	}
	if got := headResponse.Header.Get("ETag"); got != entityTag {
		t.Errorf("HEAD ETag = %q", got)
	}
	if got := headResponse.Header.Get("Content-Length"); got != "36" {
		t.Errorf("HEAD Content-Length = %q", got)
	}
}

func TestProxyFlushesStreamingResponse(t *testing.T) {
	release := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		_, _ = writer.Write([]byte("first\n"))
		writer.(http.Flusher).Flush()
		<-release
		_, _ = writer.Write([]byte("second\n"))
	}))
	defer upstream.Close()

	handler := mustProxy(t, map[string]Target{
		"datahub": mustDataHubTarget(t, upstream.URL),
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	defer handler.CloseIdleConnections()

	type result struct {
		response *http.Response
		err      error
	}
	started := make(chan result, 1)
	go func() {
		response, err := http.Get(proxy.URL + testMountPrefix + "/datahub/api/stream")
		started <- result{response: response, err: err}
	}()
	var response *http.Response
	select {
	case got := <-started:
		if got.err != nil {
			close(release)
			t.Fatal(got.err)
		}
		response = got.response
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("proxy did not flush upstream response headers")
	}
	defer response.Body.Close()

	first := make([]byte, len("first\n"))
	read := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(response.Body, first)
		read <- err
	}()
	select {
	case err := <-read:
		if err != nil {
			close(release)
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("proxy buffered the first streaming response chunk")
	}
	if got := string(first); got != "first\n" {
		close(release)
		t.Fatalf("first stream chunk = %q", got)
	}
	close(release)
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(rest); got != "second\n" {
		t.Fatalf("remaining stream = %q", got)
	}
}

func TestProxyReturnsJSONForUnknownTargetAndUpstreamFailure(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedURL := "http://" + listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	handler := mustProxy(t, map[string]Target{
		"datahub": mustDataHubTarget(t, closedURL),
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	defer handler.CloseIdleConnections()

	for _, test := range []struct {
		name, route, message string
		status               int
	}{
		{
			name: "unknown name", route: "/device/capability",
			status: http.StatusNotFound, message: "integration target not found",
		},
		{
			name: "client URL cannot select upstream", route: "/http://example.com/",
			status: http.StatusNotFound, message: "integration route not found",
		},
		{
			name: "unreachable configured upstream", route: "/datahub/api/status",
			status: http.StatusBadGateway, message: "integration upstream unavailable",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := http.Get(proxy.URL + testMountPrefix + test.route)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != test.status {
				t.Fatalf("status = %d", response.StatusCode)
			}
			if got := response.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			var envelope map[string]string
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if envelope["error"] != test.message {
				t.Errorf("error = %q", envelope["error"])
			}
		})
	}
}

func TestProxyStreamsWebSocketAndSanitizesHandshake(t *testing.T) {
	observed := make(chan *http.Request, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestCopy := request.Clone(context.Background())
		requestCopy.Header = request.Header.Clone()
		observed <- requestCopy
		connection, err := websocket.Accept(writer, request, nil)
		if err != nil {
			t.Errorf("accept WebSocket: %v", err)
			return
		}
		defer connection.CloseNow()
		messageType, message, err := connection.Read(request.Context())
		if err != nil {
			t.Errorf("read WebSocket: %v", err)
			return
		}
		if err := connection.Write(
			request.Context(),
			messageType,
			append([]byte("echo:"), message...),
		); err != nil {
			t.Errorf("write WebSocket: %v", err)
		}
	}))
	defer upstream.Close()

	handler := mustProxy(t, map[string]Target{
		"datahub": mustDataHubTarget(t, upstream.URL),
	})
	proxy := httptest.NewServer(handler)
	defer proxy.Close()
	defer handler.CloseIdleConnections()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	websocketURL := "ws" + strings.TrimPrefix(proxy.URL, "http") +
		testMountPrefix + "/datahub/ws?keep=yes&access_token=pc-secret"
	connection, _, err := websocket.Dial(ctx, websocketURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization": []string{"Bearer pc-secret"},
			"Cookie":        []string{"pc-session=secret"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err := connection.Write(ctx, websocket.MessageText, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	messageType, message, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if messageType != websocket.MessageText || string(message) != "echo:payload" {
		t.Fatalf("WebSocket response = type %v, body %q", messageType, message)
	}

	handshake := <-observed
	if handshake.URL.Path != "/ws" || handshake.URL.RawQuery != "keep=yes" {
		t.Errorf("upstream WebSocket URL = %s", handshake.URL.String())
	}
	if handshake.Header.Get("Authorization") != "" || handshake.Header.Get("Cookie") != "" {
		t.Errorf("WebSocket credentials leaked upstream: %v", handshake.Header)
	}
}

func TestDeviceTargetFailsClosedWithoutDialing(t *testing.T) {
	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		upstreamCalls.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	handler := mustProxy(t, map[string]Target{
		"device": mustDeviceTarget(t, upstream.URL),
	})
	request := httptest.NewRequest(
		http.MethodPost,
		testMountPrefix+"/device/arbitrary-capability",
		strings.NewReader("unvalidated payload"),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
	if got := upstreamCalls.Load(); got != 0 {
		t.Fatalf("generic device proxy reached upstream %d time(s)", got)
	}
	var envelope map[string]string
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["error"] != "device target requires typed controller operations" {
		t.Fatalf("error = %q", envelope["error"])
	}
}

func TestProxyRejectsTraversalAndPrefixConfusion(t *testing.T) {
	called := false
	resolver := ResolverFunc(func(context.Context, string) (Target, error) {
		called = true
		return Target{}, errors.New("must not resolve malformed route")
	})
	handler, err := NewHandler(testMountPrefix, resolver)
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{
		"/api/integrations-extra/datahub/api/status",
		"/api/integrations/datahub/%2e%2e/admin",
		"/api/integrations/datahub//example.com/admin",
		"/api/integrations/DATAHUB/api/status",
	} {
		request := httptest.NewRequest(http.MethodGet, route, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d", route, response.Code)
		}
	}
	if called {
		t.Fatal("resolver was called for a malformed route")
	}
}

func mustProxy(t *testing.T, targets map[string]Target) *Handler {
	t.Helper()
	resolver, err := NewStaticResolver(targets)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(testMountPrefix+"/", resolver)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func mustDataHubTarget(t *testing.T, value string) Target {
	t.Helper()
	target, err := NormalizeDataHubTarget(value)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func mustDeviceTarget(t *testing.T, value string) Target {
	t.Helper()
	target, err := NormalizeDeviceTarget(value)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func TestStripAccessTokenPreservesOtherRawQueryParts(t *testing.T) {
	const raw = "z=1&access_token=one&a=%2F&access%5ftoken=two&empty=&z=2"
	if got, want := stripAccessToken(raw), "z=1&a=%2F&empty=&z=2"; got != want {
		t.Fatalf("stripAccessToken() = %q, want %q", got, want)
	}
}

func TestTargetURLReturnsCopy(t *testing.T) {
	target := mustDataHubTarget(t, "http://127.0.0.1:6060/")
	first := target.URL()
	first.Host = "example.com"
	if got := target.URL().Host; got != "127.0.0.1:6060" {
		t.Fatalf("mutating URL copy changed target: %q", got)
	}
}

func TestNormalizeMountPrefix(t *testing.T) {
	for _, test := range []struct {
		value, want string
		valid       bool
	}{
		{value: "/api/integrations/", want: "/api/integrations", valid: true},
		{value: "api/v1/integrations", valid: false},
		{value: "/", valid: false},
		{value: "/api//integrations", valid: false},
		{value: "/api/../integrations", valid: false},
		{value: "/api/%2e%2e/integrations", valid: false},
	} {
		got, err := NormalizeMountPrefix(test.value)
		if test.valid && (err != nil || got != test.want) {
			t.Errorf("NormalizeMountPrefix(%q) = %q, %v", test.value, got, err)
		}
		if !test.valid && err == nil {
			t.Errorf("NormalizeMountPrefix(%q) unexpectedly accepted %q", test.value, got)
		}
	}
}

func TestNormalizedURLHasNoClientControlledComponents(t *testing.T) {
	target := mustDeviceTarget(t, "HTTP://Node.LOCAL:8080/")
	got := target.URL()
	want, _ := url.Parse("http://node.local:8080")
	if got.String() != want.String() {
		t.Fatalf("normalized URL = %q, want %q", got, want)
	}
}
