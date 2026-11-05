//go:build windows

package ui

import "rayanlamp.local/app/internal/config"

func controllerStartupConfig(cfg config.Config, firstRun bool) config.Config {
	if firstRun {
		cfg.UI.ViewMode = config.ViewDashboard
		cfg.UI.WindowBounds = config.WindowBounds{X: -1, Y: -1, Width: 900, Height: 640}
		cfg.UI.TitleBarMode = config.TitleBarCustom
		cfg.UI.Backdrop = config.BackdropAcrylic
		cfg.UI.MagneticSnap = true
		cfg.Hotkeys = config.HotkeysConfig{}
		cfg.Notifications.Enabled, cfg.Notifications.Sound = false, false
	}
	// The donor core is never started, but keep its dormant endpoint inert too.
	cfg.Lamp.BaseURL = "http://127.0.0.1:1"
	return cfg
}

// View changes own dimensions, never the live window position. Keep the last
// dashboard rectangle so expanding is reversible, including Settings previews.
func controllerViewConfig(previous, next config.Config) config.Config {
	if next.UI.ControllerExpandedBounds.Width == 0 {
		next.UI.ControllerExpandedBounds = previous.UI.ControllerExpandedBounds
	}
	if previous.UI.ViewMode == next.UI.ViewMode {
		return next
	}
	if next.UI.ViewMode == config.ViewCompact {
		next.UI.ControllerExpandedBounds = previous.UI.WindowBounds
		next.UI.WindowBounds.Width, next.UI.WindowBounds.Height = 400, 340
	} else {
		bounds := previous.UI.ControllerExpandedBounds
		if bounds.Width < config.DashboardWindowMinWidth || bounds.Height < config.DashboardWindowMinHeight {
			bounds.Width, bounds.Height = 900, 760
		}
		next.UI.WindowBounds.Width, next.UI.WindowBounds.Height = bounds.Width, bounds.Height
	}
	return next
}
