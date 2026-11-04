package appconfig

import (
	"path/filepath"
	"testing"
)

func TestIntroModesAndRuntimeOverrideDoNotPersist(t *testing.T) {
	for _, tc := range []struct {
		mode           string
		complete, show bool
	}{
		{"auto", false, true}, {"auto", true, false}, {"", true, false},
		{"always", true, true}, {"skip", false, false},
	} {
		if got := (UI{IntroMode: tc.mode, SetupComplete: tc.complete}).ShouldShowIntro(); got != tc.show {
			t.Fatalf("%+v got=%t", tc, got)
		}
	}
	store, err := Open(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPresentationOverrides("", "", "always"); err != nil {
		t.Fatal(err)
	}
	if store.Current().UI.IntroMode != "always" || store.CurrentRuntime().UI.IntroMode != "always" {
		t.Fatal("process override missing")
	}
	ui := store.Current().UI
	ui.SetupComplete = true
	if _, err := store.UpdateUI(ui); err != nil {
		t.Fatal(err)
	}
	stored, _, err := Load(store.Path())
	if err != nil || stored.UI.IntroMode != "auto" || !stored.UI.SetupComplete {
		t.Fatalf("persisted override: %+v %v", stored.UI, err)
	}
	if err := store.SetPresentationOverrides("", "", "bad"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}
