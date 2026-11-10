package ipcjson

import (
	"net/http"

	apireference "pccontroller.local/controller/api"
)

func registerAPIReferenceHTTP(mux *http.ServeMux, service *Service) {
	handler := apireference.Handler()
	mux.HandleFunc("/api/docs/", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeHTTPRequest(w, r, service) || !authorizeHTTPCapability(w, r, service, capabilityRead) {
			return
		}
		handler.ServeHTTP(w, r)
	})
}
