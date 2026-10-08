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
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	controller "pccontroller.local/controller"
	hostapi "pccontroller.local/controller/host"
	"pccontroller.local/controller/internal/envfile"
	"pccontroller.local/controller/rpc"
)

type libraryRequest struct {
	Operation   string             `json:"operation"`
	Handle      uint64             `json:"handle,omitempty"`
	TimeoutMS   int                `json:"timeout_ms,omitempty"`
	Port        string             `json:"port,omitempty"`
	Command     string             `json:"command,omitempty"`
	Method      string             `json:"method,omitempty"`
	ClientID    string             `json:"client_id,omitempty"`
	Params      json.RawMessage    `json:"params,omitempty"`
	AfterID     uint64             `json:"after_id,omitempty"`
	Kind        string             `json:"kind,omitempty"`
	Rescan      bool               `json:"rescan,omitempty"`
	Options     controller.Options `json:"options,omitempty"`
	HostOptions libraryHostOptions `json:"host_options,omitempty"`
}

type libraryHostOptions struct {
	ConfigPath         string              `json:"config_path,omitempty"`
	DataRoot           string              `json:"data_root,omitempty"`
	AppID              string              `json:"app_id,omitempty"`
	AppName            string              `json:"app_name,omitempty"`
	Tagline            string              `json:"tagline,omitempty"`
	BuildVersion       string              `json:"build_version,omitempty"`
	BuildSourceHash    string              `json:"build_source_hash,omitempty"`
	BuildTime          string              `json:"build_time,omitempty"`
	ControllerOptions  *controller.Options `json:"controller_options,omitempty"`
	DisableAutoConnect bool                `json:"disable_auto_connect,omitempty"`
	DisableNative      bool                `json:"disable_native,omitempty"`
	EnableIntegrations bool                `json:"enable_integrations,omitempty"`
	HTTPAddress        string              `json:"http_address,omitempty"`
	HTTPAllowRemote    bool                `json:"http_allow_remote,omitempty"`
	HTTPWebSocketPath  string              `json:"http_websocket_path,omitempty"`
}

type libraryResponse struct {
	OK       bool          `json:"ok"`
	Handle   uint64        `json:"handle,omitempty"`
	Result   any           `json:"result,omitempty"`
	Error    string        `json:"error,omitempty"`
	RPCError *rpc.RPCError `json:"rpc_error,omitempty"`
}

type libraryClient struct {
	mu     sync.Mutex
	client *controller.Client
}

type libraryHost struct {
	mu   sync.Mutex
	host *hostapi.Host
}

var (
	nextHandle     atomic.Uint64
	clientsMu      sync.RWMutex
	clients        = make(map[uint64]*libraryClient)
	hostsMu        sync.RWMutex
	hosts          = make(map[uint64]*libraryHost)
	environmentErr error
	shutdownClient = func(client *controller.Client) error { return client.Shutdown() }
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
	case "host_create":
		return createHost(request.HostOptions)
	}
	if isHostOperation(request.Operation) {
		return invokeHost(request)
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
		if err := shutdownClient(entry.client); err != nil {
			return response(map[string]bool{"destroyed": false}, err)
		}
		clientsMu.Lock()
		delete(clients, request.Handle)
		clientsMu.Unlock()
		return response(map[string]bool{"destroyed": true}, nil)
	default:
		return libraryResponse{Error: "unknown operation " + request.Operation}
	}
}

func createHost(options libraryHostOptions) libraryResponse {
	var httpOptions *hostapi.HTTPOptions
	if options.HTTPAddress != "" {
		httpOptions = &hostapi.HTTPOptions{
			Address:       options.HTTPAddress,
			AllowRemote:   options.HTTPAllowRemote,
			WebSocketPath: options.HTTPWebSocketPath,
		}
	}
	embedded, err := hostapi.New(hostapi.Options{
		ConfigPath: options.ConfigPath,
		DataRoot:   options.DataRoot,
		Branding: hostapi.Branding{
			AppID: options.AppID, AppName: options.AppName, Tagline: options.Tagline,
		},
		Build: hostapi.BuildInfo{
			Version: options.BuildVersion, SourceHash: options.BuildSourceHash,
			BuildTime: options.BuildTime,
		},
		ControllerOptions:  options.ControllerOptions,
		DisableAutoConnect: options.DisableAutoConnect,
		DisableNative:      options.DisableNative,
		HTTP:               httpOptions,
		EnableIntegrations: options.EnableIntegrations,
	})
	if err != nil {
		return response(nil, err)
	}
	handle := nextHandle.Add(1)
	hostsMu.Lock()
	hosts[handle] = &libraryHost{host: embedded}
	hostsMu.Unlock()
	return libraryResponse{OK: true, Handle: handle}
}

func isHostOperation(operation string) bool {
	switch operation {
	case "host_start", "host_call", "host_endpoints", "host_stop", "host_destroy":
		return true
	default:
		return false
	}
}

func invokeHost(request libraryRequest) libraryResponse {
	entry := getHost(request.Handle)
	if entry == nil {
		return libraryResponse{Error: fmt.Sprintf("unknown host handle %d", request.Handle)}
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
	case "host_start":
		if err := startLibraryHost(ctx, entry.host); err != nil {
			return response(nil, err)
		}
		return response(map[string]any{"endpoints": entry.host.Endpoints()}, nil)
	case "host_call":
		client, err := entry.host.RPC()
		if err != nil {
			return response(nil, err)
		}
		result, err := client.Call(ctx, rpc.Request{
			Method: request.Method, Params: request.Params, ClientID: request.ClientID,
		})
		if err != nil {
			if result.Error != nil {
				return libraryResponse{Error: err.Error(), RPCError: result.Error}
			}
			return response(nil, err)
		}
		return response(result.Result, nil)
	case "host_endpoints":
		return response(entry.host.Endpoints(), nil)
	case "host_stop":
		return response(map[string]bool{"stopped": true}, entry.host.Stop(ctx))
	case "host_destroy":
		err := entry.host.Stop(ctx)
		if errors.Is(err, hostapi.ErrNotStarted) {
			err = nil
		}
		hostsMu.Lock()
		delete(hosts, request.Handle)
		hostsMu.Unlock()
		return response(map[string]bool{"destroyed": true}, err)
	default:
		return libraryResponse{Error: "unknown operation " + request.Operation}
	}
}

func startLibraryHost(operationContext context.Context, embedded *hostapi.Host) error {
	// The Host needs a lifetime context that remains live after this operation,
	// while timeout_ms must still cancel and roll back synchronous startup.
	lifetimeContext, cancelLifetime := context.WithCancel(context.Background())
	startupComplete := make(chan struct{})
	watcherExited := make(chan struct{})
	go func() {
		defer close(watcherExited)
		select {
		case <-operationContext.Done():
			cancelLifetime()
		case <-startupComplete:
		}
	}()

	err := embedded.Start(lifetimeContext)
	close(startupComplete)
	<-watcherExited
	if operationErr := operationContext.Err(); operationErr != nil {
		cancelLifetime()
		rollbackContext, rollbackCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer rollbackCancel()
		if stopErr := embedded.Stop(rollbackContext); stopErr != nil && !errors.Is(stopErr, hostapi.ErrNotStarted) {
			return errors.Join(operationErr, stopErr)
		}
		return operationErr
	}
	if err != nil {
		cancelLifetime()
	}
	return err
}

func getClient(handle uint64) *libraryClient {
	clientsMu.RLock()
	defer clientsMu.RUnlock()
	return clients[handle]
}

func getHost(handle uint64) *libraryHost {
	hostsMu.RLock()
	defer hostsMu.RUnlock()
	return hosts[handle]
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
