package ipcjson

import (
	"net/http"
	"time"

	"pccontroller.local/controller/internal/boardsensors"
)

func registerBoardSensorsHTTP(mux *http.ServeMux, service *Service) {
	for _, path := range []string{"/api/sensors/board", "/api/sensors/metrics"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if !authorizeHTTPRequest(w, r, service) {
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", "GET")
				http.Error(w, "method not allowed", 405)
				return
			}
			if !authorizeHTTPCapability(w, r, service, capabilityRead) {
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			// BoardSnapshot deliberately excludes catalog I/O and never issues UART reads.
			snapshot := boardsensors.FromBoard(service.Client.BoardSnapshot(), service.HostInstanceID, time.Now())
			if r.URL.Path == "/api/sensors/metrics" {
				w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
				snapshot.Prometheus(w)
			} else {
				writeHTTPJSON(w, http.StatusOK, snapshot)
			}
		})
	}
}
