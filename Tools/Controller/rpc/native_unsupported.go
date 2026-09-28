//go:build !windows && !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris

package rpc

import (
	"context"
	"errors"
	"net"
)

func DefaultNativeEndpoint(string) (Endpoint, error) {
	return Endpoint{}, errors.New("native local RPC is unavailable on this platform")
}

func dialNativeEndpoint(context.Context, Endpoint) (net.Conn, error) {
	return nil, errors.New("native local RPC is unavailable on this platform")
}

func listenNativeEndpoint(Endpoint, ListenOptions) (net.Listener, error) {
	return nil, errors.New("native local RPC is unavailable on this platform")
}
