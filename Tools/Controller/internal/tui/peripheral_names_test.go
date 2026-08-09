package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"pccontroller.local/controller/internal/appconfig"
)

func peripheralSettingRowIndex(rows []settingRow, key string) int {
	want := peripheralNameSettingKey(key)
	for index, row := range rows {
		if row.Key == want {
			return index
		}
	}
	return -1
}

func TestAppSettingsPeripheralRowsCoverRegistryAndRenderCustomNames(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	descriptors := appconfig.PeripheralDescriptors()
	model.uiValue.PeripheralNames = make(map[string]string, len(descriptors))
	for _, descriptor := range descriptors {
		model.uiValue.PeripheralNames[descriptor.Key] = "Named " + descriptor.Key
	}

	rows := model.appSettingRows()
	seen := make(map[string]bool, len(descriptors))
	counts := make(map[string]int)
	for index, row := range rows {
		descriptor, ok := peripheralDescriptorForSettingKey(row.Key)
		if !ok {
			continue
		}
		if seen[descriptor.Key] {
			t.Fatalf("peripheral %q appears more than once in App Settings", descriptor.Key)
		}
		seen[descriptor.Key] = true
		counts[descriptor.Kind]++
		wantName := model.uiValue.PeripheralNames[descriptor.Key]
		if !row.Editable || !strings.Contains(row.Value, wantName) || !strings.Contains(row.Value, "custom") {
			t.Fatalf("peripheral row %q is not editable or does not render its custom name: %+v", descriptor.Key, row)
		}

		model.cursor = index
		updated, handled := model.beginSettingEditor()
		if !handled || updated.settingEditor == nil || !updated.settingEditor.IsText || updated.settingEditor.Text != wantName {
			t.Fatalf("peripheral row %q did not open a populated text editor: %#v", descriptor.Key, updated.settingEditor)
		}
	}

	if len(seen) != len(descriptors) || len(seen) != 34 {
		t.Fatalf("App Settings peripheral coverage=%d, registry=%d", len(seen), len(descriptors))
	}
	for kind, want := range map[string]int{"relay": 8, "motion": 2, "pwm": 16, "display": 2, "sensor": 6} {
		if counts[kind] != want {
			t.Fatalf("App Settings %s rows=%d, want %d", kind, counts[kind], want)
		}
	}

	for _, key := range []string{"pwm.15", "display.lcd", "sensor.power"} {
		model.cursor = peripheralSettingRowIndex(rows, key)
		if model.cursor < 0 {
			t.Fatalf("missing App Settings row for %s", key)
		}
		rendered := ansi.Strip(model.appSettingsPage())
		if want := model.uiValue.PeripheralNames[key]; !strings.Contains(rendered, want) {
			t.Fatalf("App Settings did not render %q for %s:\n%s", want, key, rendered)
		}
	}
}

func TestF2AppSettingsRestoresEveryRegistryPeripheralWithoutBoardCommand(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	ui := appconfig.Defaults().UI
	model.saveUI = func(value appconfig.UI) error {
		ui = value
		return nil
	}

	for _, descriptor := range appconfig.PeripheralDescriptors() {
		if ui.PeripheralNames == nil {
			ui.PeripheralNames = make(map[string]string)
		}
		ui.PeripheralNames[descriptor.Key] = "Temporary " + descriptor.Key
		model.uiValue = ui
		model.page = PageAppSettings
		model.cursor = peripheralSettingRowIndex(model.appSettingRows(), descriptor.Key)
		if model.cursor < 0 {
			t.Fatalf("missing App Settings row for %s", descriptor.Key)
		}

		updated, command := model.Update(tea.KeyMsg{Type: tea.KeyF2})
		model = updated.(Model)
		if command != nil || model.renameTarget != descriptor.Key {
			t.Fatalf("F2 for %s dispatched a board command or selected %q", descriptor.Key, model.renameTarget)
		}
		model.input.SetValue("")
		updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
		model = updated.(Model)
		if command != nil {
			t.Fatalf("restoring %s dispatched a board command", descriptor.Key)
		}
		if _, exists := ui.PeripheralNames[descriptor.Key]; exists || model.renameTarget != "" {
			t.Fatalf("F2 did not restore %s to its registry default: %#v", descriptor.Key, ui.PeripheralNames)
		}
	}
}

func TestPeripheralSettingsModalSavesAndRestoresRoleSpecificPWMName(t *testing.T) {
	model := readyModel(t, PageAppSettings)
	ui := appconfig.Defaults().UI
	model.saveUI = func(value appconfig.UI) error {
		ui = value
		return nil
	}
	model.cursor = peripheralSettingRowIndex(model.appSettingRows(), "pwm.15")
	if model.cursor < 0 {
		t.Fatal("missing role-specific PWM row")
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command != nil || model.settingEditor == nil || !model.settingEditor.IsText {
		t.Fatalf("role-specific PWM editor=%#v command=%v", model.settingEditor, command)
	}
	model.settingEditor.Text = "Service status blue"
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command != nil || ui.PeripheralNames["pwm.15"] != "Service status blue" {
		t.Fatalf("modal save dispatched a board command or saved %#v", ui.PeripheralNames)
	}
	model.cursor = peripheralSettingRowIndex(model.appSettingRows(), "pwm.15")
	if rendered := ansi.Strip(model.appSettingsPage()); !strings.Contains(rendered, "Service status blue") {
		t.Fatalf("saved role-specific PWM name is not visible:\n%s", rendered)
	}

	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	model.settingEditor.Text = ""
	updated, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(Model)
	if command != nil {
		t.Fatal("modal default restoration dispatched a board command")
	}
	if _, exists := ui.PeripheralNames["pwm.15"]; exists {
		t.Fatalf("modal did not restore role-specific PWM default: %s", fmt.Sprint(ui.PeripheralNames))
	}
}
