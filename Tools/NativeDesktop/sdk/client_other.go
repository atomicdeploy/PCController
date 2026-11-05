//go:build !windows

package sdk

import (
	"context"

	"rayanlamp.local/app/internal/ipc"
)

func platformCall(
	context.Context,
	ipc.Request,
) (ipc.Response, error) {
	return ipc.Response{}, ErrUnsupported
}
