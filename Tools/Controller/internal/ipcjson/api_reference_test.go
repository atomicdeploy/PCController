package ipcjson

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestAPIExplorerRetainsHTTPReadPolicy(t *testing.T) {
	h := websocketMux(context.Background(), &Service{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/docs/", nil)
	h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("remote docs ignored host policy", w.Code)
	}
	w = httptest.NewRecorder()
	req.RemoteAddr = "127.0.0.1:12345"
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("local docs unavailable", w.Code)
	}
}
