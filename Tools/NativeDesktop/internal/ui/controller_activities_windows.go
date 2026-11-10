//go:build windows

package ui

import (
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"pccontroller.local/desktop/internal/dialogs"
	"strings"
)

type controllerActivity struct{ key, group, title string }

var controllerActivities = []controllerActivity{
	{"connection", "Board", "Connection"}, {"board", "Board", "EEPROM settings"}, {"menus", "Board", "Menu editor"},
	{"peripherals", "Board", "Peripheral presentation"}, {"lcd", "Board", "LCD presentation"},
	{"folders", "Board", "Channel folders"},
	{"rf", "Board", "RF reception & assignments"}, {"display", "Board", "Display presentation"}, {"buzzer", "Board", "Buzzer & melodies"}, {"strip", "Board", "LED strip"},
	{"programs", "Authoring", "Programs & effects"},
	{"sequence", "Authoring", "Sequence editor"},
	{"melodies", "Authoring", "Melody library"},
	{"effect-library", "Authoring", "Effect library files & folders"},
	{"firmware", "Maintenance", "Firmware & backups"}, {"updates", "Maintenance", "Artifact transfers & updates"}, {"diagnostics", "Maintenance", "Diagnostics"}, {"events", "Maintenance", "Event history"},
	{"terminal", "Tools", "Command workbench"}, {"host", "Preferences", "Host preferences"}, {"settings", "Preferences", "Application identity"},
	{"safety", "Board", "Safety & recovery"}, {"status-lighting", "Board", "Status lighting"}, {"i2c", "Tools", "I²C workbench"},
	{"history", "Monitoring", "Telemetry history"}, {"network", "Connectivity", "Peers & discovery"}, {"local-device", "Integrations", "Local device"},
	{"host-tools", "Tools", "Host operating system"}, {"firmware-tools", "Maintenance", "Firmware & toolchain"},
	{"integration-settings", "Preferences", "Host integrations"}, {"discovery-settings", "Preferences", "Discovery protocols"}, {"udp-settings", "Preferences", "Binary UDP transport"},
	{"os-policy", "Preferences", "OS action policy"}, {"pwm-settings", "Preferences", "PWM channel calibration"},
	{"data", "Tools", "Data workspace"},
	{"board-automations", "Authoring", "Board-owned automations"}, {"rf-bindings", "Authoring", "RF host bindings"},
	{"releases", "Maintenance", "Release & workflow discovery"},
	{"motion", "Board", "Seat & motion"},
	{"status-policy", "Preferences", "Reactive status lighting"},
	{"profiles", "Board", "Board profile & routing"},
	{"peers", "Connectivity", "Persistent bridge peers"}, {"hotkeys", "Preferences", "Host hotkey bindings"},
	{"host-automations", "Authoring", "Host event automations"}, {"rf-transmit", "Board", "RF transmitter"},
}

func (s *Shell) openControllerActivities() {
	if s.controller.LoadPage == nil {
		return
	}
	var dialog *walk.Dialog
	var filter *walk.LineEdit
	var list *walk.ListBox
	var open, closeButton *walk.PushButton
	visible := append([]controllerActivity(nil), controllerActivities...)
	labels := func() []string {
		values := make([]string, len(visible))
		for i, item := range visible {
			values[i] = s.nativeText(item.group) + "  ·  " + s.nativeText(item.title)
		}
		return values
	}
	selected := ""
	accept := func() {
		index := list.CurrentIndex()
		if index >= 0 && index < len(visible) {
			selected = visible[index].key
			dialog.Accept()
		}
	}
	spec := d.Dialog{AssignTo: &dialog, Title: "Activities — PCController", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, MinSize: d.Size{Width: 440, Height: 360}, Size: d.Size{Width: 560, Height: 520}, Layout: d.VBox{Margins: d.Margins{Left: 18, Right: 18, Top: 18, Bottom: 18}, Spacing: 12}, DefaultButton: &open, CancelButton: &closeButton, Children: []d.Widget{
		d.Label{Text: "Activities", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 16, Bold: true}},
		d.LineEdit{AssignTo: &filter, CueBanner: "Find an activity…", OnTextChanged: func() {
			if filter == nil || list == nil || open == nil {
				return
			}
			query := strings.ToLower(strings.TrimSpace(filter.Text()))
			visible = nil
			for _, item := range controllerActivities {
				if strings.Contains(strings.ToLower(item.group+" "+item.title+" "+s.nativeText(item.group)+" "+s.nativeText(item.title)), query) {
					visible = append(visible, item)
				}
			}
			_ = list.SetModel(labels())
			_ = list.SetCurrentIndex(0)
			open.SetEnabled(len(visible) > 0)
		}},
		d.ListBox{AssignTo: &list, Model: labels(), CurrentIndex: 0, StretchFactor: 1, OnItemActivated: accept},
		d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{d.HSpacer{}, d.PushButton{AssignTo: &open, Text: "Open", OnClicked: accept}, d.PushButton{AssignTo: &closeButton, Text: "Close", OnClicked: func() { dialog.Cancel() }}}},
	}}
	if err := spec.Create(s.mw); err != nil {
		s.showError("Activities", err)
		return
	}
	_ = dialog.SetTitle(s.nativeText("Activities") + " — PCController")
	_ = dialog.SetRightToLeftLayout(s.isRTL())
	_ = dialog.SetRightToLeftReading(s.isRTL())
	_ = open.SetText(s.nativeText("Open"))
	_ = closeButton.SetText(s.nativeText("Close"))
	_ = filter.SetCueBanner(s.nativeText("Find an activity…"))
	theme := dialogs.NewThemeSession(dialog)
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	_ = theme.WatchSystemChanges(nil)
	_ = dialog.SetIcon(s.appIcon)
	dialog.Run()
	theme.Dispose()
	dialog.Dispose()
	if selected != "" && !s.closed.Load() {
		if selected == "motion" {
			s.openControllerMotion()
		} else {
			s.openControllerForm(selected)
		}
	}
}
