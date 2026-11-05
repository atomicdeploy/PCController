//go:build !windows

package comserver

import (
	"context"

	"pccontroller.local/desktop/internal/ipc"
)

// Run is unavailable outside Windows.
func Run(context.Context, ipc.Handler) error {
	return ErrUnsupported
}
