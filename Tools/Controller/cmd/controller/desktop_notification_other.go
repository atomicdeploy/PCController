//go:build !windows

package main

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
)

func ensureWebDesktopIntegration(*appconfig.Store) (hostui.DesktopIntegrationStatus, error) {
	return hostui.DesktopIntegrationStatus{}, nil
}
