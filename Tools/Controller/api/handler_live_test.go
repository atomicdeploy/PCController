package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

// Opt-in, source-owned visual fixture. No controller runtime or board is created.
func TestExplorerBrowserFixture(t *testing.T) {
	if os.Getenv("PCCONTROLLER_API_BROWSER_FIXTURE") != "1" {
		t.Skip("opt-in browser fixture")
	}
	stop := make(chan struct{})
	var once sync.Once
	mux := http.NewServeMux()
	mux.Handle("/api/docs/", Handler())
	mux.HandleFunc("/__test-stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		once.Do(func() { close(stop) })
		w.WriteHeader(204)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	t.Log("BROWSER_FIXTURE=" + server.URL + "/api/docs/")
	select {
	case <-stop:
	case <-time.After(20 * time.Minute):
		t.Fatal("browser fixture deadline")
	}
}
