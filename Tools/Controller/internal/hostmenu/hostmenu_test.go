package hostmenu

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

func testMenuConfig() appconfig.HostMenuConfig {
	return appconfig.HostMenuConfig{
		DefaultMenu: "root", RequestGesture: "door-hold-k4",
		DisplayDurationMS: 1500, SessionTimeoutMS: 120000,
		Menus: []appconfig.HostMenu{
			{ID: "root", Label: "ROOT", Title: "Root", Items: []appconfig.HostMenuItem{
				{ID: "level", Label: "LEVL", Title: "Level", Type: "number", Value: "10", Min: 0, Max: 20, Step: 5, WriteAction: "level"},
				{ID: "mode", Label: "MODE", Title: "Mode", Type: "select", Value: "a", Options: []appconfig.HostMenuOption{{Label: "Alpha", Value: "a"}, {Label: "Beta", Value: "b"}}, WriteAction: "mode"},
				{ID: "more", Label: "MORE", Title: "More", Type: "submenu", Submenu: "more"},
			}},
			{ID: "more", Label: "MORE", Title: "More", Items: []appconfig.HostMenuItem{
				{ID: "danger", Label: "LOCK", Title: "Lock", Type: "action", WriteAction: "os.lock", Guarded: true},
			}},
		},
	}
}

func TestDirectoryRejectsCombinedEntryCountOverflow(t *testing.T) {
	config := appconfig.HostMenuConfig{
		Menus:            make([]appconfig.HostMenu, native.HostMenuMaximumEntries),
		BuiltinOverrides: []appconfig.BuiltinMenuOverride{{}},
	}
	if _, err := New(config, Callbacks{}).Directory(); err == nil {
		t.Fatal("host-menu manager accepted more than the wire-directory capacity")
	}
}

func TestDefinitionChangesCoverActiveInactiveAndHiddenNode(t *testing.T) {
	config := appconfig.Defaults().HostMenus
	changes := make(chan DefinitionChange, 8)
	manager := New(config, Callbacks{DefinitionChanged: func(change DefinitionChange) { changes <- change }})
	if err := manager.Open("pc-settings"); err != nil {
		t.Fatal(err)
	}
	updated := cloneConfig(config)
	updated.Menus[1].Label = "EDIT"
	updated.Menus[1].Title = "Edited Settings"
	updated.Menus[1].Content = "Applied live"
	manager.UpdateConfig(updated)
	change := <-changes
	if change.Kind != "menu.content.changed" || !change.Active || change.NodeID != 0x81 ||
		change.Snapshot.Panel.Segments != "EDIT" || change.Snapshot.Panel.LCDLine1 != "Edited Settings" || change.Snapshot.Panel.LCDLine2 != "Applied live" {
		t.Fatalf("active definition change=%+v", change)
	}

	manager.Close("test")
	updated.Menus[2].Content = "Inactive edit"
	manager.UpdateConfig(updated)
	change = <-changes
	if change.NodeID != 0x82 || change.Active || change.Kind != "menu.content.changed" {
		t.Fatalf("inactive definition change=%+v", change)
	}

	if err := manager.Open("system-actions"); err != nil {
		t.Fatal(err)
	}
	updated.Menus[2].Flags.Visible = false
	manager.UpdateConfig(updated)
	change = <-changes
	if manager.Snapshot().Active || !strings.Contains(manager.Snapshot().Status, "hidden") {
		t.Fatalf("hidden active node did not fall back/close: %+v change=%+v", manager.Snapshot(), change)
	}
	directory, err := manager.Directory()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range directory.Entries {
		if entry.ID == 0x82 {
			t.Fatal("hidden node remained in advertised directory")
		}
	}
}

func TestWatchedConfigHotReloadPushesActiveDefinitionAndEmitsEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controller.json")
	if err := appconfig.Write(path, appconfig.Defaults()); err != nil {
		t.Fatal(err)
	}
	store, err := appconfig.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	manager := New(store.Current().HostMenus, Callbacks{})
	if err := manager.Open("host"); err != nil {
		t.Fatal(err)
	}
	pushed := make(chan Snapshot, 4)
	events := make(chan DefinitionChange, 4)
	manager.SetDefinitionChanged(func(change DefinitionChange) {
		events <- change
		if change.Active {
			pushed <- change.Snapshot
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for value := range store.Subscribe(ctx) {
			manager.UpdateConfig(value.HostMenus)
		}
	}()
	go store.Watch(ctx, 10*time.Millisecond, nil, func(err error) { t.Errorf("watch error: %v", err) })
	value := store.Current()
	value.HostMenus.Menus[0].Label = "LIVE"
	value.HostMenus.Menus[0].Title = "Watched Menu"
	value.HostMenus.Menus[0].Content = "Updated now"
	if err := appconfig.Write(path, value); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-events:
		if change.Kind != "menu.content.changed" || !change.Active {
			t.Fatalf("watch event=%+v", change)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watched host-menu edit emitted no normalized event")
	}
	select {
	case panel := <-pushed:
		if panel.Panel.Segments != "LIVE" || panel.Panel.LCDLine1 != "Watched Menu" || panel.Panel.LCDLine2 != "Updated now" {
			t.Fatalf("watched active preview=%+v", panel)
		}
	case <-time.After(time.Second):
		t.Fatal("watched active host-menu edit was not pushed")
	}
}

func TestManagerMirrorsNavigationRolloverSubmenuAndGuard(t *testing.T) {
	writes := make(map[string]string)
	var executed []string
	manager := New(testMenuConfig(), Callbacks{
		Write: func(_ context.Context, action, value string) (string, error) {
			writes[action] = value
			return "saved", nil
		},
		Execute: func(_ context.Context, action string) (string, error) {
			executed = append(executed, action)
			return "done", nil
		},
	})
	if err := manager.Open(""); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.HandleKey(context.Background(), 4, "press")
	if err != nil || snapshot.Value != "15" || writes["level"] != "15" || snapshot.Panel.Segments != "LEVL" {
		t.Fatalf("number step snapshot=%+v writes=%v err=%v", snapshot, writes, err)
	}
	_, _ = manager.HandleKey(context.Background(), 4, "press")
	snapshot, _ = manager.HandleKey(context.Background(), 4, "press")
	if snapshot.Value != "0" { // 15 -> 20 -> rollover 0
		t.Fatalf("number rollover=%q", snapshot.Value)
	}
	_, _ = manager.HandleKey(context.Background(), 2, "press")
	snapshot, _ = manager.HandleKey(context.Background(), 4, "press")
	if snapshot.Value != "b" || writes["mode"] != "b" {
		t.Fatalf("select step snapshot=%+v writes=%v", snapshot, writes)
	}
	_, _ = manager.HandleKey(context.Background(), 2, "press")
	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil || snapshot.MenuID != "more" || snapshot.Depth != 2 {
		t.Fatalf("submenu snapshot=%+v err=%v", snapshot, err)
	}
	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil || !snapshot.GuardPending || len(executed) != 0 {
		t.Fatalf("first guarded press snapshot=%+v executed=%v err=%v", snapshot, executed, err)
	}
	snapshot, err = manager.HandleKey(context.Background(), 4, "hold")
	if err != nil || snapshot.GuardPending || strings.Join(executed, ",") != "os.lock" {
		t.Fatalf("guard confirmation snapshot=%+v executed=%v err=%v", snapshot, executed, err)
	}
	snapshot, _ = manager.HandleKey(context.Background(), 3, "hold")
	if snapshot.MenuID != "root" || snapshot.Depth != 1 {
		t.Fatalf("hold K3 back snapshot=%+v", snapshot)
	}
}

func TestDisabledGuardedOSActionCannotExecute(t *testing.T) {
	config := testMenuConfig()
	config.DefaultMenu = "more"
	config.Menus[1].Items[0].Disabled = true
	called := false
	manager := New(config, Callbacks{Execute: func(context.Context, string) (string, error) {
		called = true
		return "", nil
	}})
	_ = manager.Open("")
	if _, err := manager.HandleKey(context.Background(), 4, "hold"); err == nil || called {
		t.Fatalf("disabled action err=%v called=%t", err, called)
	}
}

func TestReadOnlyK4DoesNotRefreshMutateOrAdvanceRevision(t *testing.T) {
	config := appconfig.DefaultHostMenus()
	reads := 0
	var interaction InteractionEvent
	manager := New(config, Callbacks{
		Read: func(context.Context, string) (string, error) {
			reads++
			return "changed", nil
		},
		Interaction: func(event InteractionEvent) { interaction = event },
	})
	if err := manager.Open(""); err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	after, err := manager.HandleKey(context.Background(), 4, "press")
	if !errors.Is(err, ErrInteractionDenied) {
		t.Fatalf("read-only K4 err=%v", err)
	}
	if reads != 0 || after.Value != before.Value || after.Revision != before.Revision {
		t.Fatalf("read-only K4 mutated state: reads=%d before=%+v after=%+v", reads, before, after)
	}
	if interaction.Kind != "menu.action.denied" || interaction.Key != 4 || interaction.ItemID != before.ItemID {
		t.Fatalf("denied interaction=%+v", interaction)
	}
}

func TestHostMenuShellCommandUsesSameManager(t *testing.T) {
	manager := New(testMenuConfig(), Callbacks{})
	engine := shell.New(10)
	if err := RegisterCommands(engine, manager); err != nil {
		t.Fatal(err)
	}
	output, err := engine.Execute(context.Background(), "host-menu open root")
	if err != nil || !strings.Contains(output, "host menu root") || !manager.Snapshot().Active {
		t.Fatalf("open output=%q err=%v snapshot=%+v", output, err, manager.Snapshot())
	}
	output, err = engine.Execute(context.Background(), "host-menu list")
	if err != nil || !strings.Contains(output, "root") || !strings.Contains(output, "more") {
		t.Fatalf("list output=%q err=%v", output, err)
	}
}

func TestHostMenuShellOpenRefreshesLiveReadValue(t *testing.T) {
	reads := 0
	manager := New(appconfig.DefaultHostMenus(), Callbacks{
		Read: func(_ context.Context, action string) (string, error) {
			reads++
			if action != "host.status" {
				t.Fatalf("unexpected initial read action %q", action)
			}
			return "PC online - device connected", nil
		},
	})
	engine := shell.New(10)
	if err := RegisterCommands(engine, manager); err != nil {
		t.Fatal(err)
	}

	output, err := engine.Execute(context.Background(), "host-menu open host")
	if err != nil || reads != 1 || !strings.Contains(output, "PC online - device connected") {
		t.Fatalf("open output=%q reads=%d err=%v", output, reads, err)
	}
}

func TestDefaultHostDateAndTimeItemsRenderLiveValues(t *testing.T) {
	manager := New(appconfig.DefaultHostMenus(), Callbacks{
		Read: func(_ context.Context, action string) (string, error) {
			switch action {
			case "host.date":
				return "2026-08-02", nil
			case "host.time":
				return "10:08:04", nil
			default:
				return "ready", nil
			}
		},
	})
	if err := manager.Open("host"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HandleKey(context.Background(), 2, "press"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HandleKey(context.Background(), 2, "press"); err != nil {
		t.Fatal(err)
	}
	date, err := manager.Refresh(context.Background())
	if err != nil || date.Panel.Segments != "DATE" || date.Value != "2026-08-02" ||
		date.Panel.LCDLine1 != "Current date" || date.Panel.LCDLine2 != "2026-08-02" {
		t.Fatalf("date snapshot=%+v err=%v", date, err)
	}
	if _, err := manager.HandleKey(context.Background(), 2, "press"); err != nil {
		t.Fatal(err)
	}
	clock, err := manager.Refresh(context.Background())
	if err != nil || clock.Panel.Segments != "TIME" || clock.Value != "10:08:04" ||
		clock.Panel.LCDLine1 != "Current time" || clock.Panel.LCDLine2 != "10:08:04" {
		t.Fatalf("time snapshot=%+v err=%v", clock, err)
	}
}

func TestDynamicMacroOptionsNavigateExecuteAndRenderResult(t *testing.T) {
	writes := make(map[string]string)
	var executed string
	manager := New(appconfig.DefaultHostMenus(), Callbacks{
		Read: func(_ context.Context, action string) (string, error) {
			if action == "host.macro.selection" {
				return "2", nil
			}
			return "Idle", nil
		},
		Write: func(_ context.Context, action, value string) (string, error) {
			writes[action] = value
			return "Selected " + value, nil
		},
		Execute: func(_ context.Context, action string) (string, error) {
			executed = action
			return "PLAY 7 Demo", nil
		},
	})
	if err := manager.UpdateSelectOptions("macro.library", []appconfig.HostMenuOption{
		{Label: "2 Door alert", Value: "2"},
		{Label: "7 Output demo", Value: "7"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Open("macro-library"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := manager.Refresh(context.Background())
	if err != nil || snapshot.Value != "2" || !strings.Contains(snapshot.Panel.LCDLine2, "Door") {
		t.Fatalf("initial macro selector=%+v err=%v", snapshot, err)
	}
	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil || snapshot.Value != "7" || writes["host.macro.selection"] != "7" ||
		!strings.Contains(snapshot.Panel.LCDLine2, "Output") {
		t.Fatalf("advanced macro selector=%+v writes=%v err=%v", snapshot, writes, err)
	}
	_, _ = manager.HandleKey(context.Background(), 2, "press") // details
	_, _ = manager.HandleKey(context.Background(), 2, "press") // play
	snapshot, err = manager.HandleKey(context.Background(), 4, "press")
	if err != nil || !snapshot.GuardPending || executed != "" {
		t.Fatalf("play guard=%+v executed=%q err=%v", snapshot, executed, err)
	}
	snapshot, err = manager.HandleKey(context.Background(), 4, "hold")
	if err != nil || executed != "host.macro.play" || snapshot.Value != "PLAY 7 Demo" ||
		!strings.Contains(snapshot.Panel.LCDLine2, "PLAY") {
		t.Fatalf("play result=%+v executed=%q err=%v", snapshot, executed, err)
	}
}

func TestDynamicSelectWithEmptyLibraryFailsWithoutMutation(t *testing.T) {
	manager := New(appconfig.DefaultHostMenus(), Callbacks{})
	if err := manager.UpdateSelectOptions("macro.library", nil); err != nil {
		t.Fatal(err)
	}
	if err := manager.Open("macro-library"); err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	after, err := manager.HandleKey(context.Background(), 4, "press")
	if err == nil || after.Value != before.Value {
		t.Fatalf("empty selector after=%+v before=%+v err=%v", after, before, err)
	}
}

func TestPanelChangedTracksOpenKeyRefreshAndClose(t *testing.T) {
	panels := make([]Snapshot, 0, 4)
	manager := New(testMenuConfig(), Callbacks{
		Read: func(context.Context, string) (string, error) { return "fresh", nil },
		PanelChanged: func(snapshot Snapshot) {
			panels = append(panels, snapshot)
		},
	})
	if err := manager.Open("root"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.HandleKey(context.Background(), 2, "press"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	manager.Close("test complete")

	if len(panels) != 4 {
		t.Fatalf("panel callback count=%d snapshots=%+v", len(panels), panels)
	}
	if !panels[0].Active || panels[0].MenuID != "root" || panels[0].Cursor != 0 {
		t.Fatalf("open panel=%+v", panels[0])
	}
	if !panels[1].Active || panels[1].ItemID != "mode" || panels[1].Cursor != 1 {
		t.Fatalf("key panel=%+v", panels[1])
	}
	if !panels[2].Active || panels[2].Revision < panels[1].Revision {
		t.Fatalf("refresh panel=%+v previous=%+v", panels[2], panels[1])
	}
	if panels[3].Active || panels[3].Status != "test complete" {
		t.Fatalf("close panel=%+v", panels[3])
	}
}
