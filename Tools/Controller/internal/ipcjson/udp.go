package ipcjson

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"reflect"
	"sync"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	udp "pccontroller.local/controller/transport/udp"
)

type udpRuntime struct {
	sync.Mutex
	endpoint *udp.Endpoint
	failure  string
}

// StartUDP owns one configurable endpoint per dispatcher, independent of the
// HTTP/native listeners. Configuration notifications revoke the previous socket.
func (s *Service) StartUDP(ctx context.Context) {
	s.udpOnce.Do(func() {
		go func() {
			var updates <-chan appconfig.Config
			if s.SubscribeHostConfig != nil {
				updates = s.SubscribeHostConfig(ctx)
			}
			current := s.hostConfig()
			var applied *udp.Config
			var allowRemote bool
			var cancel context.CancelFunc
			defer func() {
				if cancel != nil {
					cancel()
				}
			}()
			for {
				config := current.IPC.UDP
				if applied == nil || !reflect.DeepEqual(*applied, config) || allowRemote != current.IPC.AllowRemote {
					allowRemote = current.IPC.AllowRemote
					if cancel != nil {
						cancel()
					}
					s.udpState.Lock()
					if s.udpState.endpoint != nil {
						s.udpState.endpoint.Close()
					}
					s.udpState.endpoint = nil
					s.udpState.failure = ""
					s.udpState.Unlock()
					copy := config
					applied = &copy
					if config.Enabled {
						address, err := net.ResolveUDPAddr("udp4", config.Listen)
						if err == nil && !address.IP.IsLoopback() && !current.IPC.AllowRemote {
							err = errors.New("UDP LAN binding requires ipc.allow_remote")
						}
						if err == nil {
							var ep *udp.Endpoint
							endpointContext, stop := context.WithCancel(ctx)
							cancel = stop
							ep, err = udp.Start(endpointContext, config, map[string]any{"app": "pccontroller", "instance": s.HostInstanceID}, func(callContext context.Context, kind byte, value any, from *net.UDPAddr) any {
								access := accessFromAddress(from, "udp")
								if kind == udp.Subscribe || kind == udp.Unsubscribe {
									if err := s.authorizeCapability(access, "controller.subscribe", capabilityEvents); err != nil {
										return map[string]any{"error": err.Error()}
									}
									return map[string]any{"subscribed": kind == udp.Subscribe, "lease_ms": 30000}
								}
								encoded, _ := json.Marshal(value)
								var request Request
								if json.Unmarshal(encoded, &request) != nil {
									return Response{JSONRPC: Version, Error: &RPCError{Code: -32700, Message: "invalid binary RPC"}}
								}
								return s.dispatch(callContext, request, access)
							})
							if err == nil {
								s.udpState.Lock()
								s.udpState.endpoint = ep
								s.udpState.Unlock()
								if s.Client != nil {
									go s.publishUDPEvents(endpointContext, ep)
								}
							}
						}
						if err != nil {
							s.udpState.Lock()
							s.udpState.failure = err.Error()
							s.udpState.Unlock()
						}
					}
				}
				select {
				case <-ctx.Done():
					return
				case next, ok := <-updates:
					if !ok {
						return
					}
					current = next
				}
			}
		}()
	})
}

func (s *Service) publishUDPEvents(ctx context.Context, ep *udp.Endpoint) {
	for _, stream := range []string{"activity", "state", "telemetry", "debug"} {
		go func(stream string) {
			after := s.Client.LatestEventID()
			for {
				event, err := s.Client.NextEventStream(ctx, after, "", stream)
				if err != nil {
					return
				}
				after = event.ID
				if !ep.WantsEvents() {
					continue
				}
				ep.Publish(map[string]any{"method": "controller.event", "params": event})
				if stream == "state" || stream == "telemetry" {
					ep.Publish(map[string]any{"method": "controller.snapshot", "params": s.controllerSnapshot()})
				}
			}
		}(stream)
	}
}

func (s *Service) dispatchUDP(ctx context.Context, r Request, access Access) Response {
	response := Response{JSONRPC: Version, ID: r.ID}
	s.udpState.Lock()
	ep := s.udpState.endpoint
	failure := s.udpState.failure
	s.udpState.Unlock()
	fail := func(message string) Response {
		response.Error = &RPCError{Code: -32602, Message: message}
		return response
	}
	switch r.Method {
	case "controller.udp.config.get":
		response.Result = s.hostConfig().IPC.UDP
	case "controller.udp.status":
		if ep != nil {
			response.Result = ep.Status()
		} else {
			response.Result = map[string]any{"running": false, "error": failure}
		}
	case "controller.udp.events.get":
		if ep == nil {
			return fail("UDP transport is not running")
		}
		response.Result = ep.Received()
	case "controller.udp.config.set":
		if access.Remote {
			return fail("UDP provisioning requires local IPC")
		}
		if s.UpdateHostConfig == nil {
			return fail("persistent host configuration unavailable")
		}
		var config udp.Config
		if err := decodeParams(r.Params, &config); err != nil {
			return fail(err.Error())
		}
		if err := config.Validate(); err != nil {
			return fail(err.Error())
		}
		if err := s.UpdateHostConfig(func(c *appconfig.Config) error { c.IPC.UDP = config; return nil }); err != nil {
			return fail(err.Error())
		}
		response.Result = config
	case "controller.udp.call", "controller.udp.subscribe", "controller.udp.unsubscribe":
		if access.Remote {
			return fail("UDP outbound routing requires local IPC; remote pivots are not allowed")
		}
		if ep == nil {
			return fail("UDP transport is not running")
		}
		var params struct {
			Peer      string  `json:"peer"`
			Request   Request `json:"request"`
			TimeoutMS int     `json:"timeout_ms"`
		}
		if err := decodeParams(r.Params, &params); err != nil {
			return fail(err.Error())
		}
		if params.TimeoutMS == 0 {
			params.TimeoutMS = 5000
		}
		if params.TimeoutMS < 100 || params.TimeoutMS > 30000 {
			return fail("timeout_ms must be 100..30000")
		}
		kind := udp.Request
		var value any = params.Request
		if r.Method == "controller.udp.subscribe" {
			kind = udp.Subscribe
			value = map[string]any{}
		}
		if r.Method == "controller.udp.unsubscribe" {
			kind = udp.Unsubscribe
			value = map[string]any{}
		}
		callContext, cancel := context.WithTimeout(ctx, time.Duration(params.TimeoutMS)*time.Millisecond)
		defer cancel()
		result, err := ep.Call(callContext, params.Peer, kind, value)
		if err != nil {
			response.Error = &RPCError{Code: -32000, Message: err.Error()}
		} else {
			response.Result = result
		}
	default:
		response.Error = &RPCError{Code: -32601, Message: "unknown UDP method"}
	}
	return response
}
