package rpc

import (
	"fmt"
	"net"
	"strings"
)

type ListenOptions struct {
	AllowRemote bool
	// RecoverStaleNative acquires an adjacent cross-process ownership lock and
	// may then remove an unresponsive Unix socket after identity revalidation.
	RecoverStaleNative bool
}

func Listen(endpoint Endpoint, options ListenOptions) (net.Listener, error) {
	if err := endpoint.Validate(); err != nil {
		return nil, err
	}
	switch endpoint.Transport {
	case TransportTCP:
		address, err := validateTCPListenAddress(endpoint.Address, options.AllowRemote)
		if err != nil {
			return nil, err
		}
		return net.Listen("tcp", address)
	case TransportNamedPipe, TransportUnix:
		return listenNativeEndpoint(endpoint, options)
	default:
		return nil, fmt.Errorf("RPC transport %q cannot listen", endpoint.Transport)
	}
}

func validateTCPListenAddress(address string, allowRemote bool) (string, error) {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return "", err
	}
	host = strings.Trim(host, "[]")
	if !allowRemote && !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return "", fmt.Errorf("refusing non-loopback RPC address %q without explicit remote policy", address)
		}
	}
	return address, nil
}
