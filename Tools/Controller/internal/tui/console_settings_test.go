package tui

import (
	"errors"
	"strings"
	"testing"

	"pccontroller.local/controller/internal/appconfig"
	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/shell"
)

func TestLocalConsoleSettingAppliesBeforePersistence(t *testing.T) {
	ui := appconfig.Defaults().UI
	var applied appconfig.TUIConsole
	var saved appconfig.UI
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		UIConfig: func() appconfig.UI { return ui },
		ApplyTUIConsole: func(value appconfig.TUIConsole) error {
			applied = value
			return nil
		},
		SaveUI: func(value appconfig.UI) error {
			saved = value
			return nil
		},
		Preview:        func() *control.Snapshot { value := RichPreviewSnapshot(); return &value }(),
		DisableWelcome: true,
	})
	model.page = PageAppSettings
	for index, row := range model.appSettingRows() {
		if row.Key == "console.window" {
			model.cursor = index
			break
		}
	}
	var opened bool
	model, opened = model.beginSettingEditor()
	if !opened || model.settingEditor == nil {
		t.Fatal("console window editor did not open")
	}
	model.settingEditor.Fields[0].Value = 144
	model.settingEditor.Fields[1].Value = 44
	model, _, _ = model.commitAppSettingEditor()
	if applied.Columns != 144 || applied.Rows != 44 || saved.TUIConsole != applied {
		t.Fatalf("applied=%#v saved=%#v", applied, saved.TUIConsole)
	}
}

func TestUnavailableLocalConsoleDoesNotPersistRuntimeEdit(t *testing.T) {
	ui := appconfig.Defaults().UI
	saves := 0
	model := NewWithOptions(control.New(control.Options{}), shell.New(10), Options{
		UIConfig: func() appconfig.UI { return ui },
		ApplyTUIConsole: func(appconfig.TUIConsole) error {
			return errors.New("remote SSH terminal owns its window")
		},
		SaveUI:         func(appconfig.UI) error { saves++; return nil },
		Preview:        func() *control.Snapshot { value := RichPreviewSnapshot(); return &value }(),
		DisableWelcome: true,
	})
	model.settingEditor = &settingEditor{
		Page: PageAppSettings, Key: "console.font", IsText: true, Text: "Cascadia Mono",
	}
	model, _, _ = model.commitAppSettingEditor()
	if saves != 0 || model.settingEditor == nil || !strings.Contains(model.notice, "not applied or saved") {
		t.Fatalf("saves=%d editor=%#v notice=%q", saves, model.settingEditor, model.notice)
	}
}
