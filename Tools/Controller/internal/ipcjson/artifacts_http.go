package ipcjson

import (
	"net/http"
	"strings"

	"pccontroller.local/controller/internal/artifacts"
)

func registerArtifactHTTP(mux *http.ServeMux, service *Service) {
	if mux == nil || service == nil || service.Artifacts == nil {
		return
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		access := accessFromHTTPRequest(request, "rest")
		// Artifact bytes and update requests are never anonymously exposed off
		// loopback, even if a deployment accidentally enables remote IPC without
		// configuring a bearer token.
		if access.Remote && strings.TrimSpace(service.currentAuthToken()) == "" {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
				"error": "remote artifact access requires a configured authentication token",
			})
			return
		}
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		capability := capabilityRead
		if artifacts.HTTPRequiresProgramming(request) {
			capability = capabilityProgramming
		}
		if !authorizeHTTPCapability(writer, request, service, capability) {
			return
		}
		service.Artifacts.Handler().ServeHTTP(writer, request)
	})
	mux.Handle("/api/artifacts", handler)
	mux.Handle("/api/artifacts/", handler)
	mux.Handle("/api/updates", handler)
	mux.Handle("/api/updates/", handler)
	mux.Handle("/api/restores", handler)
	mux.Handle("/api/restores/", handler)
}
