//go:build controllerlib

package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	controller "pccontroller.local/controller"
	"pccontroller.local/controller/internal/envfile"
)

type libraryRequest struct {
	Operation string             `json:"operation"`
	Handle    uint64             `json:"handle,omitempty"`
	TimeoutMS int                `json:"timeout_ms,omitempty"`
	Port      string             `json:"port,omitempty"`
	Command   string             `json:"command,omitempty"`
	AfterID   uint64             `json:"after_id,omitempty"`
	Kind      string             `json:"kind,omitempty"`
	Rescan    bool               `json:"rescan,omitempty"`
	Options   controller.Options `json:"options,omitempty"`
}

type libraryResponse struct {
	OK     bool   `json:"ok"`
	Handle uint64 `json:"handle,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

type libraryClient struct {
	mu     sync.Mutex
	client *controller.Client
}

var (
	nextHandle     atomic.Uint64
	clientsMu      sync.RWMutex
	clients        = make(map[uint64]*libraryClient)
	environmentErr error
)

func init() {
	_, environmentErr = envfile.LoadProcess()
}

func main() {}

// PCControllerInvoke accepts a UTF-8 JSON request and returns a newly allocated
// UTF-8 JSON response. Release the result with PCControllerFree.
//
//export PCControllerInvoke
func PCControllerInvoke(input *C.char) *C.char {
	if input == nil {
		return encodeCString(libraryResponse{Error: "request pointer is null"})
	}
	if environmentErr != nil {
		return encodeCString(libraryResponse{Error: "environment: " + environmentErr.Error()})
	}
	var request libraryRequest
	if err := json.Unmarshal([]byte(C.GoString(input)), &request); err != nil {
		return encodeCString(libraryResponse{Error: "decode request: " + err.Error()})
	}
	response := invoke(request)
	return encodeCString(response)
}

// PCControllerFree releases a string returned by PCControllerInvoke.
//
//export PCControllerFree
func PCControllerFree(value *C.char) {
	C.free(unsafe.Pointer(value))
}

func invoke(request libraryRequest) libraryResponse {
	switch request.Operation {
	case "create":
		client := controller.New(request.Options)
		handle := nextHandle.Add(1)
		clientsMu.Lock()
		clients[handle] = &libraryClient{client: client}
		clientsMu.Unlock()
		return libraryResponse{OK: true, Handle: handle}
	case "ports":
		ports, err := controller.ListPorts()
		return response(ports, err)
	}
	entry := getClient(request.Handle)
	if entry == nil {
		return libraryResponse{Error: fmt.Sprintf("unknown handle %d", request.Handle)}
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()

	timeout := time.Duration(request.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	switch request.Operation {
	case "connect":
		var err error
		if request.Port == "" {
			err = entry.client.Connect(ctx)
		} else {
			err = entry.client.Open(ctx, request.Port)
		}
		return response(entry.client.Snapshot(), err)
	case "execute":
		result, err := entry.client.Execute(ctx, request.Command)
		return response(map[string]string{"output": result}, err)
	case "commands":
		return response(entry.client.CommandCatalog(), nil)
	case "status":
		result, err := entry.client.Status(ctx)
		return response(result, err)
	case "temperatures":
		result, err := entry.client.Temperatures(ctx, request.Rescan)
		return response(result, err)
	case "event_next":
		result, err := entry.client.NextEvent(ctx, request.AfterID, request.Kind)
		return response(result, err)
	case "snapshot":
		return response(entry.client.Snapshot(), nil)
	case "rf_list":
		result, err := entry.client.ListLearned(ctx)
		return response(result, err)
	case "close":
		return response(map[string]bool{"closed": true}, entry.client.Close())
	case "destroy":
		err := entry.client.Shutdown()
		clientsMu.Lock()
		delete(clients, request.Handle)
		clientsMu.Unlock()
		return response(map[string]bool{"destroyed": true}, err)
	default:
		return libraryResponse{Error: "unknown operation " + request.Operation}
	}
}

func getClient(handle uint64) *libraryClient {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	return clients[handle]
}

func response(result any, err error) libraryResponse {
	if err != nil {
		return libraryResponse{Error: err.Error()}
	}
	return libraryResponse{OK: true, Result: result}
}

func encodeCString(response libraryResponse) *C.char {
	data, err := json.Marshal(response)
	if err != nil {
		data = []byte(`{"ok":false,"error":"encode response"}`)
	}
	return C.CString(string(data))
}
