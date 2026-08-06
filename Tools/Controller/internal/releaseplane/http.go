package releaseplane

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
)

func (service *Service) Handler() http.Handler { return http.HandlerFunc(service.serveHTTP) }

func (service *Service) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimSuffix(request.URL.Path, "/")
	switch {
	case path == "/api/discovery/github/workflow" && request.Method == http.MethodPost:
		var value GitHubWorkflowRequest
		if !decodeHTTP(writer, request, &value) {
			return
		}
		result, err := service.client.DiscoverWorkflow(request.Context(), value)
		writeResult(writer, http.StatusOK, result, err)
	case path == "/api/discovery/github/release" && request.Method == http.MethodPost:
		var value GitHubReleaseRequest
		if !decodeHTTP(writer, request, &value) {
			return
		}
		result, err := service.client.DiscoverRelease(request.Context(), value)
		writeResult(writer, http.StatusOK, result, err)
	case path == "/api/discovery/manifest" && request.Method == http.MethodPost:
		var value ManifestRequest
		if !decodeHTTP(writer, request, &value) {
			return
		}
		result, err := service.client.DiscoverManifest(request.Context(), value)
		writeResult(writer, http.StatusOK, result, err)
	case path == "/api/discovery/manifest" && request.Method == http.MethodGet:
		result, err := service.LocalManifest()
		writeResult(writer, http.StatusOK, result, err)
	case path == "/api/discovery/check" && request.Method == http.MethodPost:
		var value CheckRequest
		if !decodeHTTP(writer, request, &value) {
			return
		}
		result, err := CheckForUpdate(value)
		writeResult(writer, http.StatusOK, result, err)
	case path == "/api/discovery/stage" && request.Method == http.MethodPost:
		var value StageRequest
		if !decodeHTTP(writer, request, &value) {
			return
		}
		result, err := service.StartStage(value)
		writeResult(writer, http.StatusAccepted, result, err)
	case strings.HasPrefix(path, "/api/discovery/status") && request.Method == http.MethodGet:
		id := strings.TrimPrefix(strings.TrimPrefix(path, "/api/discovery/status"), "/")
		result, err := service.Status(id)
		status := http.StatusOK
		if errors.Is(err, os.ErrNotExist) {
			status = http.StatusNotFound
		}
		writeResult(writer, status, result, err)
	default:
		writer.Header().Set("Allow", discoveryAllowedMethods(path))
		writeHTTPJSON(writer, http.StatusNotFound, map[string]string{"error": "discovery endpoint not found"})
	}
}

func HTTPRequiresProgramming(request *http.Request) bool {
	return request != nil && request.Method == http.MethodPost && strings.HasSuffix(strings.TrimSuffix(request.URL.Path, "/"), "/stage")
}

func decodeHTTP(writer http.ResponseWriter, request *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, maxMetadataBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request contains trailing JSON")
		}
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return false
	}
	return true
}

func writeResult(writer http.ResponseWriter, status int, value any, err error) {
	if err != nil {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeHTTPJSON(writer, status, value)
}

func writeHTTPJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func discoveryAllowedMethods(path string) string {
	if strings.TrimSuffix(path, "/") == "/api/discovery/manifest" {
		return http.MethodGet + ", " + http.MethodPost
	}
	if strings.Contains(path, "/status") {
		return http.MethodGet
	}
	return http.MethodPost
}
