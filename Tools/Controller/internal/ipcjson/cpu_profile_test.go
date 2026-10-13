package ipcjson

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCPUProfileRejectsNonlocalAndCrossOriginRequests(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	for _, test := range []struct {
		address, origin, method string
		status                  int
	}{
		{"192.168.1.40:12000", "", http.MethodPost, http.StatusForbidden},
		{"bad-address", "", http.MethodPost, http.StatusForbidden},
		{"127.0.0.1:12000", "https://hostile.example", http.MethodPost, http.StatusForbidden},
		{"[::1]:12000", "", http.MethodGet, http.StatusMethodNotAllowed},
	} {
		request := httptest.NewRequest(test.method, "/api/diagnostics/cpu-profile", nil)
		request.RemoteAddr = test.address
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		service.serveCPUProfile(response, request)
		if response.Code != test.status {
			t.Fatalf("%+v returned %d", test, response.Code)
		}
	}
}
