package main

import (
	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/hostui"
	"pccontroller.local/controller/internal/productidentity"
)

// webDesktopIntegrationOptions derives the native identity from the same
// presentation configuration used by the active host. Keeping it in one place
// prevents the notification identity, URI handler, shortcut, and tray from
// drifting to different product names.
func webDesktopIntegrationOptions(store *appconfig.Store) hostui.DesktopIntegrationOptions {
	title := ""
	if store != nil {
		title = store.Current().UI.AppTitle
	}
	return hostui.DesktopIntegrationOptions{
		AppID:       productidentity.StableAppID,
		DisplayName: productidentity.Title(title),
	}
}
