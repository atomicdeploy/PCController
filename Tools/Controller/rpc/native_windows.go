//go:build windows

package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

func DefaultNativeEndpoint(appID string) (Endpoint, error) {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return Endpoint{}, errors.New("application identity is required")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return Endpoint{}, err
	}
	sid := user.User.Sid.String()
	digest := sha256.Sum256([]byte(appID + "\x00" + sid))
	return Endpoint{
		Transport: TransportNamedPipe,
		Address:   `\\.\pipe\pccontroller-` + hex.EncodeToString(digest[:8]),
		Scope:     "local",
		Priority:  10,
	}, nil
}

func dialNativeEndpoint(ctx context.Context, endpoint Endpoint) (net.Conn, error) {
	if endpoint.Transport != TransportNamedPipe {
		return nil, errors.New("Unix-domain sockets are unavailable on Windows")
	}
	return winio.DialPipeContext(ctx, endpoint.Address)
}

func listenNativeEndpoint(endpoint Endpoint, _ ListenOptions) (net.Listener, error) {
	if endpoint.Transport != TransportNamedPipe {
		return nil, errors.New("Unix-domain sockets are unavailable on Windows")
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	// FILE_PIPE_REJECT_REMOTE_CLIENTS is enforced by go-winio. The protected
	// DACL grants the current user and LocalSystem full access, and nobody else.
	sddl := "D:P(A;;GA;;;" + user.User.Sid.String() + ")(A;;GA;;;SY)"
	return winio.ListenPipe(endpoint.Address, &winio.PipeConfig{
		SecurityDescriptor: sddl,
		InputBufferSize:    64 * 1024,
		OutputBufferSize:   64 * 1024,
	})
}
