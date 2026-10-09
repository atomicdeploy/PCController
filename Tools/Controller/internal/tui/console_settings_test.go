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

func TestCheckedSettingIntegerConversionsRejectOverflow(t *testing.T) {
	for _, value := range []int{-1, 256} {
		if _, err := checkedUint8(value); err == nil {
			t.Fatalf("checkedUint8(%d) accepted overflow", value)
		}
	}
	if value, err := checkedUint8(255); err != nil || value != 255 {
		t.Fatalf("checkedUint8 boundary=%d err=%v", value, err)
	}
	for _, value := range []int{-1, 65536} {
		if _, err := checkedUint16(value); err == nil {
			t.Fatalf("checkedUint16(%d) accepted overflow", value)
		}
	}
	if value, err := checkedUint16(65535); err != nil || value != 65535 {
		t.Fatalf("checkedUint16 boundary=%d err=%v", value, err)
	}
	for _, value := range []int{-32769, 32768} {
		if _, err := checkedInt16(value); err == nil {
			t.Fatalf("checkedInt16(%d) accepted overflow", value)
		}
	}
	if value, err := checkedInt16(-32768); err != nil || value != -32768 {
		t.Fatalf("checkedInt16 boundary=%d err=%v", value, err)
	}
}

func TestVisualPreviewRejectsOutOfRangeColor(t *testing.T) {
	editor := &settingEditor{
		Key: "led.visual.idle",
		Fields: []settingEditorField{
			{Key: "red", Value: 256},
			{Key: "green", Value: 20},
			{Key: "blue", Value: 30},
		},
	}
	if _, ok := editorVisualPreview(editor); ok {
		t.Fatal("visual preview accepted an overflowing color channel")
	}
	editor.Fields[0].Value = 255
	color, ok := editorVisualPreview(editor)
	if !ok || color.Red != 255 || color.Green != 20 || color.Blue != 30 {
		t.Fatalf("valid visual preview color=%#v ok=%t", color, ok)
	}
}
