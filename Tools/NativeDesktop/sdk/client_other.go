//go:build !windows

package sdk

import (
	"context"

	"pccontroller.local/desktop/internal/ipc"
)

func platformCall(
	context.Context,
	ipc.Request,
) (ipc.Response, error) {
	return ipc.Response{}, ErrUnsupported
}
