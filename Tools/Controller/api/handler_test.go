package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExplorerEmbeddedAssetsAndBrowserSafety(t *testing.T) {
	for _, name := range []string{"", "reference.html", "rpc.html", "protocols.html", "openapi.json", "jsonrpc.schema.json", "asyncapi.json", "assets/scalar.js", "assets/catalog.js", "assets/icon.svg"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/docs/"+name, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(name, w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
			t.Fatal("reference can issue requests")
		}
	}
	for _, name := range []string{"../handler.go", "handler.go", "assets/", "no-such-file.json", "assets/../openapi.json"} {
		w := httptest.NewRecorder()
		Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/docs/"+name, nil))
		if w.Code != 404 {
			t.Fatal("unexpected docs resource", name, w.Code)
		}
	}
}
