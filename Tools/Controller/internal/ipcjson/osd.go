package ipcjson

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/productidentity"
)

type DesktopOSDService interface {
	Show(context.Context, hostui.OSDRequest) (hostui.OSDStatus, error)
	Dismiss(context.Context, string) (hostui.OSDStatus, error)
	Status() hostui.OSDStatus
}
type defaultDesktopOSD struct{}

func (defaultDesktopOSD) Show(ctx context.Context, r hostui.OSDRequest) (hostui.OSDStatus, error) {
	return hostui.ShowOSD(ctx, r)
}
func (defaultDesktopOSD) Dismiss(ctx context.Context, id string) (hostui.OSDStatus, error) {
	return hostui.DismissOSD(ctx, id)
}
func (defaultDesktopOSD) Status() hostui.OSDStatus { return hostui.DesktopOSDStatus() }

func (s *Service) desktopOSDCall(ctx context.Context, request Request) (any, error) {
	surface := s.DesktopOSD
	if surface == nil {
		surface = defaultDesktopOSD{}
	}
	switch request.Method {
	case "controller.osd.status":
		return surface.Status(), nil
	case "controller.osd.show":
		var params hostui.OSDRequest
		if err := decodeStrictParams(request.Params, &params); err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		if s.HostConfig != nil {
			config := s.hostConfig()
			if params.Title == "" {
				params.Title = productidentity.Title(config.UI.AppTitle)
			}
			if config.UI.Appearance.ReduceMotion {
				params.ReduceMotion = true
			}
		}
		params, err := hostui.ValidateOSD(params)
		if err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		if s.HostConfig != nil && s.hostConfig().UI.Appearance.ReduceMotion {
			params.ReduceMotion = true
		}
		return surface.Show(ctx, params)
	default:
		var params struct {
			ID string `json:"id,omitempty"`
		}
		if err := decodeStrictParams(request.Params, &params); err != nil {
			return nil, &RPCError{Code: -32602, Message: err.Error()}
		}
		return surface.Dismiss(ctx, params.ID)
	}
}

func registerDesktopOSDHTTP(mux *http.ServeMux, service *Service) {
	mux.HandleFunc("/api/osd", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeHTTPRequest(w, r, service) {
			return
		}
		method, capability := "controller.osd.show", capabilityMessages
		switch r.Method {
		case "GET":
			method, capability = "controller.osd.status", capabilityRead
		case "POST":
		case "DELETE":
			method = "controller.osd.dismiss"
		default:
			w.Header().Set("Allow", "GET, POST, DELETE")
			http.Error(w, "method not allowed", 405)
			return
		}
		if !authorizeHTTPCapability(w, r, service, capability) {
			return
		}
		params := json.RawMessage(`{}`)
		if r.Method != "GET" && !(r.Method == "DELETE" && r.ContentLength == 0) {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768))
			if err := decoder.Decode(&params); err != nil {
				writeHTTPJSON(w, 400, map[string]string{"error": err.Error()})
				return
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF {
				writeHTTPJSON(w, 400, map[string]string{"error": "OSD requires exactly one JSON object"})
				return
			}
		}
		result, err := service.desktopOSDCall(r.Context(), Request{Method: method, Params: params})
		w.Header().Set("Cache-Control", "no-store")
		if err != nil {
			writeHTTPJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(w, 200, result)
	})
}
