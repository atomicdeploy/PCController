package webui

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

var testAsset = []byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ-_")

func testHandler(t *testing.T, additionalReserved ...string) http.Handler {
	t.Helper()
	modified := time.Date(2026, time.August, 2, 8, 30, 0, 0, time.UTC)
	files := fstest.MapFS{
		"index.html":                {Data: []byte("<!doctype html><title>PCController</title><main>app shell</main>"), ModTime: modified},
		"favicon.ico":               {Data: []byte{0, 0, 1, 0, 1, 0, 16, 16}, ModTime: modified},
		"favicon.svg":               {Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), ModTime: modified},
		"assets/chunk-telemetry.bin": {Data: append([]byte(nil), testAsset...), ModTime: modified},
		"assets/app.js":              {Data: []byte("export const ready = true\n"), ModTime: modified},
		"assets/app.css":             {Data: []byte(":root { color-scheme: dark }\n"), ModTime: modified},
		"assets/index.css":           {Data: []byte("body { color: white }\n"), ModTime: modified},
		"assets/icon.svg":            {Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>"), ModTime: modified},
		"assets/ui.woff2":            {Data: []byte("test-font-data"), ModTime: modified},
		"assets/plain.js":           {Data: []byte("export {}\n"), ModTime: modified},
		"manifest.webmanifest":      {Data: []byte(`{"name":"PCController"}`), ModTime: modified},
		"service-worker.js":         {Data: []byte("self.addEventListener('fetch', () => {})\n"), ModTime: modified},
	}
	handler, err := NewHandler(files, additionalReserved...)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func request(
	t *testing.T,
	handler http.Handler,
	method, target string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "http://pccontroller.test"+target, nil)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func TestEmbeddedHandlerServesUsefulAppShell(t *testing.T) {
	response := request(t, Handler(), http.MethodGet, "/", map[string]string{"Accept": "text/html"})
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, expected := range []string{"data-pccontroller-shell", "type=\"module\"", "/theme-init.js", "/assets/app.js"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("embedded app shell is missing %q", expected)
		}
	}
	for _, match := range regexp.MustCompile(`(?is)<script(?:\s+[^>]*)?>(.*?)</script>`).FindAllStringSubmatch(body, -1) {
		if strings.TrimSpace(match[1]) != "" {
			t.Fatalf("embedded app shell contains executable inline JavaScript: %q", match[1])
		}
	}
	assertSecurityHeaders(t, response.Header())
	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("index cache policy=%q", got)
	}
	for _, match := range regexp.MustCompile(`(?:src|href)="(/[^"]+)"`).FindAllStringSubmatch(body, -1) {
		asset := request(t, Handler(), http.MethodHead, match[1], nil)
		if asset.Code != http.StatusOK {
			t.Fatalf("embedded app shell reference %q returned status %d", match[1], asset.Code)
		}
	}
}

func TestStaleFingerprintedEntryAssetsRedirectToCurrentBundle(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		path     string
		location string
	}{
		{path: "/assets/app-stale000.js", location: "/assets/app.js"},
		{path: "/assets/index-stale000.css", location: "/assets/index.css"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, test.path, nil)
			if response.Code != http.StatusTemporaryRedirect {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Location"); got != test.location {
				t.Fatalf("Location=%q want=%q", got, test.location)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control=%q", got)
			}
			assertSecurityHeaders(t, response.Header())
		})
	}
}

func TestStaleEntryRecoveryDoesNotMaskMissingChunksOrAmbiguousBundles(t *testing.T) {
	handler := testHandler(t)
	missingChunk := request(t, handler, http.MethodGet, "/assets/chunk-stale000.js", nil)
	if missingChunk.Code != http.StatusNotFound {
		t.Fatalf("missing chunk status=%d", missingChunk.Code)
	}

	files := fstest.MapFS{
		"index.html":             {Data: []byte("<!doctype html><title>PCController</title>")},
		"assets/app-first000.js":  {Data: []byte("export {}")},
		"assets/app-second00.js": {Data: []byte("export {}")},
	}
	ambiguous, err := NewHandler(files)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, ambiguous, http.MethodGet, "/assets/app-stale000.js", nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ambiguous entry status=%d", response.Code)
	}
}

func TestServeContentRangesAndHead(t *testing.T) {
	handler := testHandler(t)
	size := len(testAsset)
	tests := []struct {
		name         string
		method       string
		rangeHeader  string
		wantStatus   int
		wantBody     []byte
		contentRange string
	}{
		{name: "full", method: http.MethodGet, wantStatus: http.StatusOK, wantBody: testAsset},
		{name: "head", method: http.MethodHead, wantStatus: http.StatusOK, wantBody: []byte{}},
		{name: "first", method: http.MethodGet, rangeHeader: "bytes=0-15", wantStatus: http.StatusPartialContent, wantBody: testAsset[0:16], contentRange: "bytes 0-15/" + strconv.Itoa(size)},
		{name: "open ended", method: http.MethodGet, rangeHeader: "bytes=10-", wantStatus: http.StatusPartialContent, wantBody: testAsset[10:], contentRange: "bytes 10-" + strconv.Itoa(size-1) + "/" + strconv.Itoa(size)},
		{name: "suffix", method: http.MethodGet, rangeHeader: "bytes=-7", wantStatus: http.StatusPartialContent, wantBody: testAsset[size-7:], contentRange: "bytes " + strconv.Itoa(size-7) + "-" + strconv.Itoa(size-1) + "/" + strconv.Itoa(size)},
		{name: "head range", method: http.MethodHead, rangeHeader: "bytes=4-11", wantStatus: http.StatusPartialContent, wantBody: []byte{}, contentRange: "bytes 4-11/" + strconv.Itoa(size)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := map[string]string{}
			if test.rangeHeader != "" {
				headers["Range"] = test.rangeHeader
			}
			response := request(t, handler, test.method, "/assets/chunk-telemetry.bin", headers)
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if !bytes.Equal(response.Body.Bytes(), test.wantBody) {
				t.Fatalf("body=%q want=%q", response.Body.Bytes(), test.wantBody)
			}
			if got := response.Header().Get("Content-Range"); got != test.contentRange {
				t.Fatalf("Content-Range=%q want=%q", got, test.contentRange)
			}
			if got := response.Header().Get("Accept-Ranges"); got != "bytes" {
				t.Fatalf("Accept-Ranges=%q", got)
			}
			wantLength := size
			if test.rangeHeader != "" {
				wantLength = len(test.wantBody)
				if test.method == http.MethodHead {
					wantLength = 8
				}
			}
			if got := response.Header().Get("Content-Length"); got != strconv.Itoa(wantLength) {
				t.Fatalf("Content-Length=%q want=%d", got, wantLength)
			}
		})
	}
}

func TestServeContentMultipartAndUnsatisfiableRanges(t *testing.T) {
	handler := testHandler(t)
	response := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", map[string]string{
		"Range": "bytes=0-1,4-5",
	})
	if response.Code != http.StatusPartialContent {
		t.Fatalf("multipart status=%d body=%s", response.Code, response.Body.String())
	}
	mediaType, parameters, err := mime.ParseMediaType(response.Header().Get("Content-Type"))
	if err != nil || mediaType != "multipart/byteranges" || parameters["boundary"] == "" {
		t.Fatalf("multipart Content-Type=%q err=%v", response.Header().Get("Content-Type"), err)
	}
	reader := multipart.NewReader(response.Body, parameters["boundary"])
	var bodies []string
	var ranges []string
	for {
		part, partErr := reader.NextPart()
		if partErr == io.EOF {
			break
		}
		if partErr != nil {
			t.Fatal(partErr)
		}
		content, readErr := io.ReadAll(part)
		if readErr != nil {
			t.Fatal(readErr)
		}
		bodies = append(bodies, string(content))
		ranges = append(ranges, part.Header.Get("Content-Range"))
	}
	if strings.Join(bodies, ",") != "01,45" {
		t.Fatalf("multipart bodies=%q", bodies)
	}
	if len(ranges) != 2 || !strings.HasPrefix(ranges[0], "bytes 0-1/") || !strings.HasPrefix(ranges[1], "bytes 4-5/") {
		t.Fatalf("multipart ranges=%q", ranges)
	}

	unsatisfied := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", map[string]string{
		"Range": "bytes=999-1200",
	})
	if unsatisfied.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unsatisfied status=%d body=%s", unsatisfied.Code, unsatisfied.Body.String())
	}
	want := "bytes */" + strconv.Itoa(len(testAsset))
	if got := unsatisfied.Header().Get("Content-Range"); got != want {
		t.Fatalf("unsatisfied Content-Range=%q want=%q", got, want)
	}
	if got := unsatisfied.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("unsatisfied Accept-Ranges=%q", got)
	}
	assertSecurityHeaders(t, unsatisfied.Header())
}

func TestETagConditionalAndIfRangeBehavior(t *testing.T) {
	handler := testHandler(t)
	initial := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", nil)
	etag := initial.Header().Get("ETag")
	if len(etag) < 3 || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("strong ETag=%q", etag)
	}

	matching := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", map[string]string{
		"Range": "bytes=2-5", "If-Range": etag,
	})
	if matching.Code != http.StatusPartialContent || matching.Body.String() != string(testAsset[2:6]) {
		t.Fatalf("matching If-Range status=%d body=%q", matching.Code, matching.Body.String())
	}

	mismatch := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", map[string]string{
		"Range": "bytes=2-5", "If-Range": `"different"`,
	})
	if mismatch.Code != http.StatusOK || !bytes.Equal(mismatch.Body.Bytes(), testAsset) {
		t.Fatalf("mismatched If-Range status=%d body=%q", mismatch.Code, mismatch.Body.Bytes())
	}

	notModified := request(t, handler, http.MethodGet, "/assets/chunk-telemetry.bin", map[string]string{
		"If-None-Match": etag,
	})
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("If-None-Match status=%d body=%q", notModified.Code, notModified.Body.String())
	}
}

func TestMIMEAndCachePolicies(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		path        string
		contentType string
		cache       string
	}{
		{path: "/index.html", contentType: "text/html; charset=utf-8", cache: "no-cache"},
		{path: "/favicon.svg", contentType: "image/svg+xml", cache: "no-cache"},
		{path: "/favicon.ico", contentType: "image/x-icon", cache: "no-cache"},
		{path: "/assets/app.js", contentType: "text/javascript; charset=utf-8", cache: "no-cache"},
		{path: "/assets/app.css", contentType: "text/css; charset=utf-8", cache: "no-cache"},
		{path: "/assets/icon.svg", contentType: "image/svg+xml", cache: "no-cache"},
		{path: "/assets/ui.woff2", contentType: "font/woff2", cache: "no-cache"},
		{path: "/assets/plain.js", contentType: "text/javascript; charset=utf-8", cache: "no-cache"},
		{path: "/manifest.webmanifest", contentType: "application/manifest+json", cache: "no-cache"},
		{path: "/service-worker.js", contentType: "text/javascript; charset=utf-8", cache: "no-cache"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, http.MethodGet, test.path, nil)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != test.contentType {
				t.Fatalf("Content-Type=%q want=%q", got, test.contentType)
			}
			if got := response.Header().Get("Cache-Control"); got != test.cache {
				t.Fatalf("Cache-Control=%q want=%q", got, test.cache)
			}
			assertSecurityHeaders(t, response.Header())
		})
	}
}

func TestSPAFallbackReservedPathsMethodsAndTraversal(t *testing.T) {
	handler := testHandler(t, "/control")
	tests := []struct {
		name       string
		method     string
		target     string
		accept     string
		wantStatus int
		wantShell  bool
	}{
		{name: "route", method: http.MethodGet, target: "/settings/network", accept: "text/html", wantStatus: http.StatusOK, wantShell: true},
		{name: "xhtml route", method: http.MethodHead, target: "/fa/dashboard", accept: "application/xhtml+xml", wantStatus: http.StatusOK},
		{name: "non navigation", method: http.MethodGet, target: "/settings/network", accept: "application/json", wantStatus: http.StatusNotFound},
		{name: "missing asset", method: http.MethodGet, target: "/assets/missing.js", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "api", method: http.MethodGet, target: "/api/unknown", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "health", method: http.MethodGet, target: "/healthz/more", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "ipc", method: http.MethodGet, target: "/ipc/other", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "socket io", method: http.MethodGet, target: "/socket.io/other", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "custom websocket", method: http.MethodGet, target: "/control/events", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "similar nonreserved", method: http.MethodGet, target: "/apiary", accept: "text/html", wantStatus: http.StatusOK, wantShell: true},
		{name: "encoded traversal", method: http.MethodGet, target: "/%2e%2e/index.html", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "double encoded traversal", method: http.MethodGet, target: "/%252e%252e/index.html", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "backslash traversal", method: http.MethodGet, target: "/%5c..%5cindex.html", accept: "text/html", wantStatus: http.StatusNotFound},
		{name: "directory listing", method: http.MethodGet, target: "/assets", accept: "application/json", wantStatus: http.StatusNotFound},
		{name: "write method", method: http.MethodPost, target: "/settings", accept: "text/html", wantStatus: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, handler, test.method, test.target, map[string]string{"Accept": test.accept})
			if response.Code != test.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if test.wantShell && !strings.Contains(response.Body.String(), "app shell") {
				t.Fatalf("SPA route did not receive app shell: %s", response.Body.String())
			}
			if test.wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, HEAD" {
				t.Fatalf("Allow=%q", response.Header().Get("Allow"))
			}
			assertSecurityHeaders(t, response.Header())
		})
	}
}

func TestNewHandlerRequiresIndex(t *testing.T) {
	if _, err := NewHandler(nil); err == nil {
		t.Fatal("nil filesystem was accepted")
	}
	if _, err := NewHandler(fstest.MapFS{"asset.js": {Data: []byte("export {}")}}); err == nil {
		t.Fatal("filesystem without index.html was accepted")
	}
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	for _, name := range []string{
		"Content-Security-Policy",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
		"Permissions-Policy",
		"Referrer-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
	} {
		if strings.TrimSpace(header.Get(name)) == "" {
			t.Fatalf("missing security header %s", name)
		}
	}
	csp := header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") ||
		strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP must keep JavaScript external and same-origin only: %q", csp)
	}
	if !strings.Contains(csp, "style-src-attr 'unsafe-inline'") {
		t.Fatalf("CSP must allow Motion's runtime style attributes: %q", csp)
	}
	if !strings.Contains(csp, "worker-src 'self'") {
		t.Fatalf("CSP must keep installability workers same-origin: %q", csp)
	}
}
