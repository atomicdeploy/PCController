package ipcjson

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestApplicationProfileCancellationStopsCaptureAndReleasesOwnership(t *testing.T) {
	service, _ := browserUIConfigTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodPost, "/api/diagnostics/execution-trace", nil).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:12000"
	started, stopped := false, false
	response := httptest.NewRecorder()
	service.serveApplicationProfile(response, request, func(w io.Writer) error {
		started = true
		_, err := w.Write([]byte("trace fixture"))
		return err
	}, func() { stopped = true }, "controller-execution.trace")
	if response.Code != http.StatusOK || !started || !stopped || cpuProfileBusy.Load() {
		t.Fatalf("capture ownership leaked: status=%d started=%v stopped=%v busy=%v", response.Code, started, stopped, cpuProfileBusy.Load())
	}
}

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
		for _, handler := range []func(http.ResponseWriter, *http.Request){service.serveCPUProfile, service.serveExecutionTrace} {
			request := httptest.NewRequest(test.method, "/api/diagnostics/cpu-profile", nil)
			request.RemoteAddr = test.address
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != test.status {
				t.Fatalf("%+v returned %d", test, response.Code)
			}
		}
	}
}
