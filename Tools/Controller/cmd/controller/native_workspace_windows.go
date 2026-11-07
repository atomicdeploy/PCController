//go:build windows && !no_native_gui

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
	"pccontroller.local/desktop/desktop"
)

func nativeNumber(key, label string, value, minimum, maximum int) desktop.Field {
	return desktop.Field{Key: key, Label: label, Value: strconv.Itoa(value), Numeric: true, Min: float64(minimum), Max: float64(maximum)}
}
func nativeBool(key, label string, value bool) desktop.Field {
	return choice(key, label, strconv.FormatBool(value), "false", "true")
}
func nativeDigest(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
func nativeSelectionAction(id, label string, enabled bool) desktop.PageAction {
	return desktop.PageAction{ID: id, Label: label, Enabled: enabled, SelectionRequired: true}
}

func (p nativePages) loadWorkspacePage(ctx context.Context, key string, state control.Snapshot) (desktop.Page, error) {
	ready := !p.preview && state.Connected && !state.EmergencyStop.Active && !state.Programming.Active
	switch key {
	case "board":
		page := desktop.Page{Title: "Board settings"}
		if !state.HaveSettings {
			if p.preview {
				return page, nil
			}
			if _, err := p.command(ctx, "settings"); err != nil {
				return page, err
			}
			if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
				return page, err
			}
			if !state.HaveSettings {
				return page, errors.New("Board settings have not been read; reconnect or refresh.")
			}
		}
		s := state.Settings
		page.Fields = []desktop.Field{
			{Key: "revision", Value: nativeDigest(s), Hidden: true},
			{Key: "generation", Value: strconv.FormatUint(state.ConnectionGeneration, 10), Hidden: true},
			nativeBool("silent", "Silent buzzer", s.Flags&native.SettingsSilent != 0),
			nativeNumber("light_mode", "Illumination mode", int(s.LightMode), 0, 2),
			nativeNumber("on_brightness", "Door-open brightness", int(s.OnBrightness), 0, 255),
			nativeNumber("off_brightness", "Door-closed brightness", int(s.OffBrightness), 0, 255),
			nativeNumber("display_brightness", "Display · door open", int(s.DisplayBrightness), 0, 7),
			nativeNumber("display_closed", "Display · door closed", int(s.DisplayClosedBrightness), 0, 7),
			nativeNumber("status_brightness", "Status LED brightness", int(s.StatusBrightness), 0, 255),
			nativeNumber("status_color", "Status color", int(s.StatusColor()), 0, 4),
			nativeNumber("output_persistence", "Restore outputs bit mask", int(s.OutputPersistence), 0, int(native.OutputPersistenceMask)),
			nativeNumber("relay_restore", "Relay restore bit mask", int(s.RelayRestoreMask), 0, 255),
			nativeNumber("stream_period", "Measurement period (ms)", int(s.StreamPeriodMS), 0, 65535),
			nativeNumber("voltage_decimals", "Voltage decimals", int(s.VoltageDecimals()), 0, 3),
			nativeNumber("current_decimals", "Current decimals", int(s.CurrentDecimals()), 0, 3),
			nativeNumber("default_page", "Default menu page", int(s.DefaultPage), 0, 255),
			nativeBool("save_last", "Remember last menu", s.SaveLastPage()),
			nativeNumber("motion_door", "Door policy · always/closed/open/never", int(s.MotionDoorPolicy()), 0, 3),
			nativeNumber("motion_hold", "Motion exit hold (s)", int(s.MotionExitHoldSeconds), 1, 31),
			nativeNumber("motion_break", "Direction dead-time (ms)", int(s.MotionBreakMS()), 1, 255),
			nativeBool("door_audio", "Door buzzer cues", s.DoorAudioEnabled()),
			nativeBool("relay_audio", "Relay buzzer cues", s.RelayAudioEnabled()),
		}
		for index := range page.Fields {
			page.Fields[index].Section = "EEPROM preferences"
			f := &page.Fields[index]
			switch f.Key {
			case "light_mode":
				f.Numeric = false
				f.Choices = []string{"0", "1", "2"}
				f.ChoiceLabels = []string{"Off", "On", "Automatic"}
			case "status_color":
				f.Numeric = false
				f.Choices = []string{"0", "1", "2", "3", "4"}
				f.ChoiceLabels = []string{"Red", "Blue", "Violet", "Green", "White"}
			case "motion_door":
				f.Numeric = false
				f.Choices = []string{"0", "1", "2", "3"}
				f.ChoiceLabels = []string{"Always allowed", "Only while door closed", "Only while door open", "Never allowed"}
			}
		}
		page.Actions = []desktop.PageAction{{ID: "save", Label: "Save to board", Enabled: ready, Confirm: true}}
		return page, nil
	case "menus":
		page := desktop.Page{Title: "Board menus", Columns: []string{"Page", "Name", "Display label", "Visible"}, RowValues: map[string]map[string]string{}}
		var catalog control.MenuCatalog
		if !p.preview {
			if err := p.call(ctx, "controller.menu.list", map[string]any{}, &catalog); err != nil {
				return page, err
			}
		}
		for _, menu := range catalog.Pages {
			id := strconv.Itoa(int(menu.ID))
			visible := catalog.Layout.VisibleMask&(1<<menu.ID) != 0
			page.Rows = append(page.Rows, []string{id, menu.Name, menu.Label, strconv.FormatBool(visible)})
			page.RowValues[id] = map[string]string{"label": menu.Label, "visible": strconv.FormatBool(visible)}
		}
		order := make([]string, len(catalog.Layout.Order))
		for i, id := range catalog.Layout.Order {
			order[i] = strconv.Itoa(int(id))
		}
		page.Fields = []desktop.Field{field("order", "Page order (IDs)", strings.Join(order, ", ")), nativeBool("visible", "Selected page visible", true), field("label", "Selected display label", "")}
		page.Actions = []desktop.PageAction{nativeSelectionAction("jump", "Open page", ready), nativeSelectionAction("visibility", "Apply visibility", ready && catalog.Layout.Supported), nativeSelectionAction("label", "Save display label", ready), pageAction("order", "Save order", ready && catalog.Layout.Supported)}
		return page, nil
	case "terminal":
		page := desktop.Page{Title: "Command workbench", Columns: []string{"Command", "Group", "Usage", "Summary"}}
		var commands []shell.CommandDescriptor
		if !p.preview {
			if err := p.call(ctx, "controller.command.catalog", map[string]any{}, &commands); err != nil {
				return page, err
			}
		}
		for _, command := range commands {
			page.Rows = append(page.Rows, []string{command.Name, command.Group, command.Usage, command.Summary})
		}
		page.Fields = []desktop.Field{field("command", "Command", "")}
		page.Actions = []desktop.PageAction{pageAction("execute", "Execute", !p.preview)}
		return page, nil
	case "diagnostics":
		page := desktop.Page{Title: "Diagnostics", Columns: []string{"Property", "Value"}}
		for _, row := range [][]string{{"Connected", strconv.FormatBool(state.Connected)}, {"Port", state.Port.Name}, {"Generation", strconv.FormatUint(state.ConnectionGeneration, 10)}, {"Connection detail", state.ConnectionReason}, {"Emergency stop", strconv.FormatBool(state.EmergencyStop.Active)}, {"Programming", strconv.FormatBool(state.Programming.Active)}} {
			if row[1] != "" {
				page.Rows = append(page.Rows, row)
			}
		}
		page.Actions = []desktop.PageAction{pageAction("identity", "Board identity", ready), pageAction("status", "Refresh status", ready), pageAction("temperatures", "Temperature identities", ready), pageAction("i2c", "Scan I²C bus", ready), pageAction("toolchain", "Toolchain profile", !p.preview), pageAction("owner", "Serial ownership", !p.preview)}
		return page, nil
	case "host":
		return p.loadHostPreferences(ctx)
	case "updates":
		return p.loadNativeArtifacts(ctx, state)
	case "integration-settings", "discovery-settings", "udp-settings", "os-policy", "status-policy":
		return p.loadNativeConfig(ctx, key)
	case "pwm-settings":
		return p.loadNativePWMPreferences(ctx)
	case "data":
		return p.loadNativeDataPage(), nil
	case "board-automations":
		return p.loadNativeBoardAutomations(ctx, state)
	case "rf-bindings":
		return p.loadNativeRules(ctx, true)
	case "host-automations":
		return p.loadNativeRules(ctx, false)
	case "melodies":
		return p.loadNativeMelodies(ctx)
	case "lcd":
		return p.loadNativeLCD(ctx, state)
	case "peripherals":
		return p.loadNativePeripherals(ctx)
	case "folders":
		return p.loadNativeFolders(ctx)
	case "releases":
		return p.loadNativeReleases(), nil
	case "profiles":
		return p.loadNativeProfiles(ctx)
	case "sequence":
		return p.loadNativeSequence(ctx)
	case "effect-library":
		return p.loadNativeEffectLibrary(ctx)
	case "peers":
		return p.loadNativePeers(ctx)
	case "hotkeys":
		return p.loadNativeHotkeys(ctx)
	default:
		return p.loadNativeTools(key, state)
	}
}

func (p nativePages) submitWorkspacePage(ctx context.Context, key, action string, values map[string]string) (desktop.Page, error) {
	if p.preview {
		return desktop.Page{}, errors.New("preview cannot change controller state")
	}
	var output string
	var err error
	command := func(words ...string) error { output, err = p.command(ctx, shell.Join(words)); return err }
	switch key {
	case "board":
		if action != "save" {
			return desktop.Page{}, errors.New("unknown board action")
		}
		err = p.saveBoardPreferences(ctx, values)
	case "menus":
		switch action {
		case "jump":
			err = command("menu", "page", values["selected"])
		case "visibility":
			verb := "hide"
			if values["visible"] == "true" {
				verb = "show"
			}
			err = command("menu", verb, values["selected"])
		case "label":
			err = command("host-menu", "set", "builtin:"+values["selected"], values["label"])
		case "order":
			ids := strings.Fields(strings.ReplaceAll(values["order"], ",", " "))
			err = command(append([]string{"menu", "order"}, ids...)...)
		default:
			err = errors.New("unknown menu action")
		}
	case "terminal":
		if action != "execute" || strings.TrimSpace(values["command"]) == "" {
			return desktop.Page{}, errors.New("enter a command")
		}
		output, err = p.command(ctx, values["command"])
	case "diagnostics":
		commands := map[string]string{"identity": "hello", "status": "status", "temperatures": "temp scan", "i2c": "i2c scan", "toolchain": "toolchain profile", "owner": "port-process"}
		line, ok := commands[action]
		if !ok {
			return desktop.Page{}, errors.New("unknown diagnostic action")
		}
		output, err = p.command(ctx, line)
	case "host":
		err = p.saveHostPreferences(ctx, values)
	case "updates":
		return p.submitNativeArtifacts(ctx, action, values)
	case "integration-settings", "discovery-settings", "udp-settings", "os-policy", "status-policy":
		return p.submitNativeConfig(ctx, key, action, values)
	case "pwm-settings":
		if action != "save" {
			return desktop.Page{}, errors.New("unknown PWM preference action")
		}
		return p.submitNativePWMPreferences(ctx, values)
	case "data":
		return p.submitNativeData(ctx, action, values)
	case "board-automations":
		return p.submitNativeBoardAutomations(ctx, action, values)
	case "rf-bindings":
		return p.submitNativeRules(ctx, action, values, true)
	case "host-automations":
		return p.submitNativeRules(ctx, action, values, false)
	case "melodies":
		return p.submitNativeMelodies(ctx, action, values)
	case "lcd":
		return p.submitNativeLCD(ctx, action, values)
	case "peripherals":
		return p.submitNativePeripherals(ctx, action, values)
	case "folders":
		return p.submitNativeFolders(ctx, action, values)
	case "releases":
		return p.submitNativeReleases(ctx, action, values)
	case "profiles":
		return p.submitNativeProfiles(ctx, action, values)
	case "sequence":
		return p.submitNativeSequence(ctx, action, values)
	case "effect-library":
		return p.submitNativeEffectLibrary(ctx, action, values)
	case "peers":
		return p.submitNativePeers(ctx, action, values)
	case "hotkeys":
		return p.submitNativeHotkeys(ctx, action, values)
	default:
		return p.submitNativeTools(ctx, key, action, values)
	}
	if err != nil {
		return desktop.Page{}, err
	}
	var state control.Snapshot
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return desktop.Page{}, fmt.Errorf("operation acknowledged; refresh failed: %w", err)
	}
	page, err := p.loadWorkspacePage(ctx, key, state)
	page.Output = output
	return page, err
}

func (p nativePages) saveBoardPreferences(ctx context.Context, values map[string]string) error {
	var state control.Snapshot
	if _, err := p.command(ctx, "settings"); err != nil {
		return err
	}
	if err := p.call(ctx, "controller.snapshot", map[string]any{}, &state); err != nil {
		return err
	}
	if !state.Connected || !state.HaveSettings || state.Programming.Active || state.EmergencyStop.Active {
		return errors.New("board settings are not currently writable")
	}
	if values["generation"] != strconv.FormatUint(state.ConnectionGeneration, 10) || values["revision"] != nativeDigest(state.Settings) {
		return errors.New("Board settings changed; refresh before saving. Your draft was not applied.")
	}
	s := state.Settings
	if values["silent"] == "true" {
		s.Flags |= native.SettingsSilent
	} else {
		s.Flags &^= native.SettingsSilent
	}
	for _, item := range []struct {
		key      string
		target   *byte
		min, max int
	}{
		{"light_mode", &s.LightMode, 0, 2}, {"on_brightness", &s.OnBrightness, 0, 255}, {"off_brightness", &s.OffBrightness, 0, 255}, {"display_brightness", &s.DisplayBrightness, 0, 7}, {"display_closed", &s.DisplayClosedBrightness, 0, 7}, {"status_brightness", &s.StatusBrightness, 0, 255}, {"output_persistence", &s.OutputPersistence, 0, int(native.OutputPersistenceMask)}, {"relay_restore", &s.RelayRestoreMask, 0, 255}, {"default_page", &s.DefaultPage, 0, 255}, {"motion_hold", &s.MotionExitHoldSeconds, 1, 31},
	} {
		value, err := nativeFormInteger(values, item.key, item.min, item.max)
		if err != nil {
			return err
		}
		*item.target = byte(value)
	}
	period, err := nativeFormInteger(values, "stream_period", 0, 65535)
	if err != nil {
		return err
	}
	s.StreamPeriodMS = uint16(period)
	for _, item := range []struct {
		key string
		max int
		set func(byte) error
	}{{"status_color", 4, s.SetStatusColor}, {"voltage_decimals", 3, s.SetVoltageDecimals}, {"current_decimals", 3, s.SetCurrentDecimals}, {"motion_door", 3, s.SetMotionDoorPolicy}} {
		value, err := nativeFormInteger(values, item.key, 0, item.max)
		if err != nil {
			return err
		}
		if err := item.set(byte(value)); err != nil {
			return err
		}
	}
	deadtime, err := nativeFormInteger(values, "motion_break", 1, 255)
	if err != nil {
		return err
	}
	if err := s.SetMotionBreakMS(uint16(deadtime)); err != nil {
		return err
	}
	s.SetSaveLastPage(values["save_last"] == "true")
	s.SetDoorAudioEnabled(values["door_audio"] == "true")
	s.SetRelayAudioEnabled(values["relay_audio"] == "true")
	_, err = p.command(ctx, fmt.Sprintf("settings set %d %d %d %d %d %d %d %d %d %d %t %d %d %d %d %d %d", s.Flags, s.LightMode, s.OnBrightness, s.OffBrightness, s.DisplayBrightness, s.DisplayClosedBrightness, s.StatusBrightness, s.OutputPersistence, s.StreamPeriodMS, s.DefaultPage, s.SaveLastPage(), s.StatusColor(), s.VoltageDecimals(), s.CurrentDecimals(), s.MotionExitHoldSeconds, s.MotionBreakMS(), s.RelayRestoreMask))
	return err
}

func nativeFormInteger(values map[string]string, key string, minimum, maximum int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(values[key]))
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be %d–%d", key, minimum, maximum)
	}
	return value, nil
}
