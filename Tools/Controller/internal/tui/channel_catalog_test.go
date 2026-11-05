package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/ipcjson"
)

func testChannelCatalog(t *testing.T, name, revision string) ipcjson.PeripheralSettings {
	t.Helper()
	var value ipcjson.PeripheralSettings
	if err := json.Unmarshal([]byte(`{"board_profile":{"board_identity":"test-board","configured":true,"attached":true,"revision":"`+revision+`"},"controls":[{"key":"relay.5","name":"`+name+`","icon":"lamp"},{"key":"motion.a","name":"Lift","icon":"seat"},{"key":"pwm.11","name":"Cabinet","icon":"door"}],"peripherals":[{"key":"relay.5","name":"`+name+`","icon":"lamp"}]}`), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestChannelCatalogEventRefreshCoalescesAndKeepsDraft(t *testing.T) {
	model := readyModel(t, PageOutputs)
	value := testChannelCatalog(t, "Aisle", "one")
	reads := 0
	model.channels.source.Read = func(context.Context) (ipcjson.PeripheralSettings, error) { reads++; return value, nil }
	model.input.SetValue("Uncommitted caption")
	command := model.refreshChannelCatalog()
	stale := command().(channelCatalogResultMsg)
	updated, _ := model.Update(runtimeEventReplayMsg(control.Event{Kind: "peripherals.changed"}))
	model = updated.(Model)
	value = testChannelCatalog(t, "Entrance", "two")
	command = model.acceptChannelCatalog(stale)
	if command == nil {
		t.Fatal("event during refresh was lost")
	}
	if model.channels.value.BoardProfile.Revision != "" {
		t.Fatal("stale response was installed")
	}
	if next := model.acceptChannelCatalog(command().(channelCatalogResultMsg)); next != nil {
		t.Fatal("unchanged catalog scheduled polling")
	}
	if reads != 2 || model.peripheralName("relay.5", "fallback") != "Entrance" || model.input.Value() != "Uncommitted caption" {
		t.Fatal("catalog did not converge without replacing the draft")
	}
	rows := model.controlTableRows(model.snapshot(), 8)
	found := 0
	for _, row := range rows {
		if row.PeripheralKey == "relay.5" || row.PeripheralKey == "motion.a" || row.PeripheralKey == "pwm.11" {
			if row.Icon == "" {
				t.Fatal("missing profile icon")
			}
			found++
		}
	}
	if found != 5 {
		t.Fatalf("missing named relay, motion or enclosure rows: %d", found)
	}
}

func TestChannelCatalogFailureRetainsStateAndBoardChangeFencesIt(t *testing.T) {
	model := readyModel(t, PageOutputs)
	model.channels.source.Read = func(context.Context) (ipcjson.PeripheralSettings, error) {
		return ipcjson.PeripheralSettings{}, errors.New("offline")
	}
	model.channels.value = testChannelCatalog(t, "Aisle", "one")
	model.channels.owner = model.channelCatalogOwner()
	command := model.refreshChannelCatalog()
	model.acceptChannelCatalog(command().(channelCatalogResultMsg))
	if model.peripheralName("relay.5", "fallback") != "Aisle" {
		t.Fatal("failed refresh erased last good state")
	}
	model.channels.owner = "another board"
	if model.peripheralName("relay.5", "fallback") != "fallback" {
		t.Fatal("presentation leaked across board owners")
	}
}

func TestChannelCatalogRenameUsesRevisionAndNeverLocalNames(t *testing.T) {
	model := readyModel(t, PageOutputs)
	model.channels.value = testChannelCatalog(t, "Aisle", "one")
	model.channels.owner = model.channelCatalogOwner()
	model.channels.source.Read = func(context.Context) (ipcjson.PeripheralSettings, error) {
		t.Fatal("save must use ACK, not an extra read")
		return ipcjson.PeripheralSettings{}, nil
	}
	model.channels.renameRevision = "one"
	model.saveUI = func(appconfig.UI) error { t.Fatal("channel metadata saved into local UI settings"); return nil }
	model.channels.source.Rename = func(_ context.Context, key, name, revision string) (ipcjson.PresentationUpdateResult, error) {
		if key != "relay.5" || name != "Entrance" || revision != "one" {
			t.Fatalf("incorrect mutation %s %s %s", key, name, revision)
		}
		var ack ipcjson.PresentationUpdateResult
		_ = json.Unmarshal([]byte(`{"board_profile":{"board_identity":"test-board","configured":true,"attached":true,"revision":"two"},"peripheral":{"key":"relay.5","name":"Entrance","icon":"lamp"},"controls":[{"key":"relay.5","name":"Entrance","icon":"lamp"}]}`), &ack)
		return ack, nil
	}
	descriptor, _ := peripheralDescriptorByKey("relay.5")
	next, name, _, err := model.savePeripheralName(descriptor, "Entrance")
	if err != nil || name != "Entrance" || next.channels.value.BoardProfile.Revision != "two" {
		t.Fatalf("rename failed %s %v", name, err)
	}
	if model.channels.value.Peripherals[0].Name != "Aisle" {
		t.Fatal("ACK mutated the prior model")
	}
	next.channels.source.Rename = func(context.Context, string, string, string) (ipcjson.PresentationUpdateResult, error) {
		return ipcjson.PresentationUpdateResult{}, errors.New("revision conflict")
	}
	failed, _, _, err := next.savePeripheralName(descriptor, "New draft")
	if err == nil || failed.peripheralName("relay.5", "") != "Entrance" {
		t.Fatal("failed mutation changed authoritative name")
	}
}

func TestChannelCatalogIconsKeepUnknownIdentity(t *testing.T) {
	if terminalChannelIcon("lamp") == "" || terminalChannelIcon("seat") == terminalChannelIcon("lamp") {
		t.Fatal("configured icons collapsed")
	}
	if !strings.Contains(terminalChannelIcon("unmapped-symbol"), "unmapped-symbol") {
		t.Fatal("unknown icon identity was lost")
	}
}

func TestChannelCatalogCinemaUsesAdvertisedSeatIdentity(t *testing.T) {
	model := readyModel(t, PageOutputs)
	value := testChannelCatalog(t, "Aisle", "one")
	value.Peripherals, value.Controls = appconfig.ProfileDescriptors(appconfig.BoardModeCinemaSeatMotion, false, nil, map[string]appconfig.PeripheralPresentation{
		"seat.a": {Name: "Left seats", Icon: "seat"},
		"seat.b": {Name: "Right seats", Icon: "car"},
	})
	model.channels.source.Read = func(context.Context) (ipcjson.PeripheralSettings, error) { return value, nil }
	command := model.refreshChannelCatalog()
	model.acceptChannelCatalog(command().(channelCatalogResultMsg))
	rows := model.controlTableRows(model.snapshot(), 8)
	found := false
	for index, row := range rows {
		if row.PeripheralKey == "relay.1" || row.PeripheralKey == "motion.a" {
			t.Fatal("raw or generic motion control escaped profile")
		}
		if row.PeripheralKey == "seat.a" {
			found = true
			if !strings.Contains(row.Name, "Left seats") || row.Icon != "seat" {
				t.Fatal("cinema presentation lost")
			}
			model.cursor = index
			renaming, _, _ := model.beginPeripheralRename()
			if renaming.renameTarget != "seat.a" || renaming.input.Value() != "Left seats" {
				t.Fatal("rename targets generic motion instead of the advertised seat")
			}
		}
	}
	if !found {
		t.Fatal("seat rows missing")
	}
	model.page = PageAppSettings
	model.cursor = peripheralSettingRowIndex(model.appSettingRows(), "seat.a")
	if model.cursor < 0 {
		t.Fatal("seat settings missing")
	}
	editor, handled := model.beginSettingEditor()
	if !handled || editor.settingEditor == nil || editor.settingEditor.Text != "Left seats" || editor.channels.renameRevision != "one" {
		t.Fatal("seat editor lost name or revision")
	}
}
