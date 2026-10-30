package ipcjson

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"runtime/pprof"
	"runtime/trace"
	"sync/atomic"
	"time"
)

var cpuProfileBusy atomic.Bool

// A bounded, explicitly requested application profile, never an OS profiler
// or a public pprof/debug listener. No files, configuration or board commands.
func (service *Service) serveCPUProfile(writer http.ResponseWriter, request *http.Request) {
	service.serveApplicationProfile(writer, request, pprof.StartCPUProfile, pprof.StopCPUProfile, "controller-cpu.pprof")
}

func (service *Service) serveExecutionTrace(writer http.ResponseWriter, request *http.Request) {
	service.serveApplicationProfile(writer, request, trace.Start, trace.Stop, "controller-execution.trace")
}

// CPU and execution captures share exclusion and the same five-second bound.
func (service *Service) serveApplicationProfile(writer http.ResponseWriter, request *http.Request, start func(io.Writer) error, stop func(), filename string) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeHTTPJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() {
		writeHTTPJSON(writer, http.StatusForbidden, map[string]string{"error": "application profiling requires a local caller"})
		return
	}
	if !authorizeHTTPRequest(writer, request, service) {
		return
	}
	if request.ContentLength != 0 || request.URL.RawQuery != "" {
		writeHTTPJSON(writer, http.StatusBadRequest, map[string]string{"error": "profile capture accepts no parameters or body"})
		return
	}
	if !cpuProfileBusy.CompareAndSwap(false, true) {
		writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": "application profile already active"})
		return
	}
	defer cpuProfileBusy.Store(false)
	var profile bytes.Buffer
	if err := start(&profile); err != nil {
		writeHTTPJSON(writer, http.StatusConflict, map[string]string{"error": "application profiler unavailable"})
		return
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-request.Context().Done():
	}
	stop()
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Disposition", "attachment; filename="+filename)
	_, _ = writer.Write(profile.Bytes())
}
