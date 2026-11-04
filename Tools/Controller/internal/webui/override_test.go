package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalWebOverridesAndEmbeddedFallback(t *testing.T) {
	handler := testHandler(t, "/custom-ws").(*staticHandler)
	root := t.TempDir()
	handler.overrideDirectory = root
	write := func(name, content string) {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "custom shell")
	write("assets/app-abcdef12.js", "custom entry")
	write("images/custom.svg", "<svg>custom</svg>")
	write("api/secret", "must not serve")
	write("custom-ws/secret", "must not serve")
	worker := httptest.NewRecorder()
	handler.ServeHTTP(worker, httptest.NewRequest("GET", "/service-worker.js", nil))
	if worker.Code != 200 || !strings.Contains(worker.Body.String(), "caches.delete") || strings.Contains(worker.Body.String(), "cachedAsset") {
		t.Fatal("editable UI retained cache-first service worker")
	}
	for _, tc := range []struct {
		target, body string
		status       int
	}{
		{"/", "custom shell", 200},
		{"/outputs/relays", "custom shell", 200},
		{"/assets/app-abcdef12.js", "custom entry", 200},
		{"/images/custom.svg", "<svg>custom</svg>", 200},
		{"/assets/plain.js", "export {}\n", 200},
		{"/missing.png", "", 404},
		{"/assets/app-missing000.js", "", 404},
		{"/api/secret", "", 404},
		{"/custom-ws/secret", "", 404},
		{"/%2e%2e/index.html", "", 404},
		{"/images", "", 404},
	} {
		t.Run(tc.target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.target, nil)
			req.Header.Set("Accept", "text/html")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.status || (tc.status == 200 && response.Body.String() != tc.body) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.status == 200 && response.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("editable overlay must revalidate")
			}
		})
	}
	read := func(method, etag, byteRange string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/assets/app-abcdef12.js", nil)
		req.Header.Set("If-None-Match", etag)
		req.Header.Set("Range", byteRange)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	first := read("GET", "", "")
	etag := first.Header().Get("ETag")
	if response := read("GET", etag, ""); response.Code != 304 {
		t.Fatalf("conditional=%d", response.Code)
	}
	if response := read("HEAD", "", ""); response.Code != 200 || response.Body.Len() != 0 {
		t.Fatal("HEAD failed")
	}
	if response := read("GET", "", "bytes=0-5"); response.Code != 206 || response.Body.String() != "custom" {
		t.Fatal("range failed")
	}
	if response := read("POST", "", ""); response.Code != 405 {
		t.Fatal("write accepted")
	}
	write("assets/app-abcdef12.js", "edited entry")
	if response := read("GET", etag, ""); response.Code != 200 || response.Body.String() != "edited entry" {
		t.Fatal("stale local asset")
	}
	if err := os.Remove(filepath.Join(root, "assets", "app-abcdef12.js")); err != nil {
		t.Fatal(err)
	}
	if response := read("GET", "", ""); !strings.Contains(response.Body.String(), "export const ready") {
		t.Fatal("removal did not restore embedded fallback")
	}
	write("service-worker.js", "// custom worker")
	worker = httptest.NewRecorder()
	handler.ServeHTTP(worker, httptest.NewRequest("GET", "/service-worker.js", nil))
	if worker.Body.String() != "// custom worker" {
		t.Fatal("custom worker was ignored")
	}
}

func TestWebOverrideCannotFollowLinkOutsidePublicRoot(t *testing.T) {
	handler := testHandler(t).(*staticHandler)
	handler.overrideDirectory = t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(handler.overrideDirectory, "escape.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/escape.txt", nil))
	if response.Code != 404 || strings.Contains(response.Body.String(), "private") {
		t.Fatal("override escaped public root")
	}
}
