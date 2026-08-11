//go:build windows

package main

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

// ensureWebDesktopIntegration gives unpackaged WinRT notifications a stable
// AppUserModelID and Start-menu shortcut, allowing Windows to resolve the APP
// resource from this executable instead of displaying a blank/generic toast
// identity.
func ensureWebDesktopIntegration(store *appconfig.Store) (hostui.DesktopIntegrationStatus, error) {
	return hostui.EnsureDesktopIntegration(webDesktopIntegrationOptions(store))
}
