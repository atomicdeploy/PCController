package ipcjson

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"pccontroller.local/controller/internal/releaseplane"
)

// ReleaseDiscoveryService keeps release-plane routing optional and prevents
// the core IPC dispatcher from owning provider-specific GitHub/manifest logic.
type ReleaseDiscoveryService interface {
	DispatchRPC(context.Context, string, json.RawMessage) (any, bool, error)
	Handler() http.Handler
}

func registerReleaseDiscoveryHTTP(mux *http.ServeMux, service *Service) {
	if mux == nil || service == nil || service.ReleaseDiscovery == nil {
		return
	}
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		access := accessFromHTTPRequest(request, "rest")
		if access.Remote && strings.TrimSpace(service.currentAuthToken()) == "" {
			writeHTTPJSON(writer, http.StatusForbidden, map[string]string{
				"error": "remote release discovery requires a configured authentication token",
			})
			return
		}
		if !authorizeHTTPRequest(writer, request, service) {
			return
		}
		capability := capabilityRead
		if releaseplane.HTTPRequiresProgramming(request) {
			capability = capabilityProgramming
		}
		if !authorizeHTTPCapability(writer, request, service, capability) {
			return
		}
		service.ReleaseDiscovery.Handler().ServeHTTP(writer, request)
	})
	mux.Handle("/api/discovery", handler)
	mux.Handle("/api/discovery/", handler)
}
