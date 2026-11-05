//go:build windows && !no_native_gui

package desktop

import (
	"context"
	"pccontroller.local/desktop/internal/ui"
)

func Run(ctx context.Context, options Options) error {
	return ui.RunController(ctx, options)
}
