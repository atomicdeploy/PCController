package main

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/hostmenu"
	"pccontroller.local/controller/internal/shell"
)

func TestMacroHostMenuUsesSortedWatchedLibraryAndSharedCommandEngine(t *testing.T) {
	config := appconfig.Defaults()
	config.Macros = []appconfig.Macro{
		{ID: 9, Name: "Long output demonstration"},
		{ID: 2, Name: "Door cue"},
	}
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := appconfig.Write(path, config); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runtime := control.New(control.Options{})
	engine := shell.New(8)
	var calls [][]string
	if err := engine.Register(shell.Command{Name: "macro", Run: func(_ context.Context, args []string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "ok", nil
	}}); err != nil {
		t.Fatal(err)
	}
	actions := newHostMacroMenuActions(store, runtime, engine)
	manager := hostmenu.New(config.HostMenus, hostmenu.Callbacks{})
	if err := actions.Sync(manager, config.Macros); err != nil {
		t.Fatal(err)
	}
	resolved := manager.Config()
	options := resolved.Menus[3].Items[0].Options
	if len(options) != 2 || options[0].Value != "2" || options[1].Value != "9" || len(options[1].Label) > 16 {
		t.Fatalf("resolved macro options=%+v", options)
	}
	if output, handled, err := actions.Write("host.macro.selection", "9"); err != nil || !handled || output == "" {
		t.Fatalf("select output=%q handled=%t err=%v", output, handled, err)
	}
	if output, handled, err := actions.Read("host.macro.selected"); err != nil || !handled || output != "9 Long output demonstration 0st" {
		t.Fatalf("details output=%q handled=%t err=%v", output, handled, err)
	}
	if output, handled, err := actions.Execute(context.Background(), "host.macro.play"); err != nil || !handled || output == "" {
		t.Fatalf("play output=%q handled=%t err=%v", output, handled, err)
	}
	if !reflect.DeepEqual(calls, [][]string{{"play", "9"}}) {
		t.Fatalf("macro command calls=%v", calls)
	}
}

func TestMacroHostMenuEmptyLibraryIsExplicit(t *testing.T) {
	config := appconfig.Defaults()
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := appconfig.Write(path, config); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	actions := newHostMacroMenuActions(store, control.New(control.Options{}), shell.New(1))
	if output, handled, err := actions.Read("host.macro.selected"); err != nil || !handled || output != "None" {
		t.Fatalf("empty details output=%q handled=%t err=%v", output, handled, err)
	}
	if _, handled, err := actions.Execute(context.Background(), "host.macro.play"); !handled || err == nil {
		t.Fatalf("empty play handled=%t err=%v", handled, err)
	}
}

func TestInactiveHostMenuPanelChangeReleasesFallbackCapture(t *testing.T) {
	manager := hostmenu.New(appconfig.DefaultHostMenus(), hostmenu.Callbacks{})
	bridge := newRecordingHostPanelBridge()
	if err := manager.Open("macro-library"); err != nil {
		t.Fatal(err)
	}
	if err := syncFallbackHostMenuOverlay(manager, bridge, nil); err != nil {
		t.Fatal(err)
	}
	waitHostPanelPush(t, bridge.pushes)

	manager.Close("test complete")
	if err := syncFallbackHostMenuOverlay(manager, bridge, nil); err != nil {
		t.Fatal(err)
	}
	waitHostPanelRelease(t, bridge.releases)
}
