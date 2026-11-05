//go:build windows && !no_native_gui

package desktop

import (
	"context"
	"rayanlamp.local/app/internal/ui"
)

func Run(ctx context.Context, options Options) error {
	return ui.RunController(ctx, options)
}
