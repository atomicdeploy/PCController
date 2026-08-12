package main

import (
	"path/filepath"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/productidentity"
)

func TestWebDesktopIntegrationOptionsUseCurrentPresentation(t *testing.T) {
	store, err := appconfig.Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(func(configured *appconfig.Config) error {
		configured.UI.AppTitle = "Workshop Controller"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	options := webDesktopIntegrationOptions(store)
	if options.AppID != productidentity.StableAppID {
		t.Fatalf("AppID=%q want %q", options.AppID, productidentity.StableAppID)
	}
	if options.DisplayName != "Workshop Controller" {
		t.Fatalf("DisplayName=%q", options.DisplayName)
	}
}
