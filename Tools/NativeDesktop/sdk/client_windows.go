//go:build windows

package sdk

import (
	"context"

	"pccontroller.local/desktop/internal/ipc"
)

func platformCall(
	ctx context.Context,
	request ipc.Request,
) (ipc.Response, error) {
	return ipc.Call(ctx, request)
}
