//go:build windows

package dialogs

import (
	decl "github.com/lxn/walk/declarative"
	"rayanlamp.local/app/internal/config"
)

func settingsControllerIntegrationPage(controls *settingsControls, cfg config.Config, actions SettingsActions) decl.TabPage {
	return decl.TabPage{Title: "Integration", Layout: decl.VBox{Margins: decl.Margins{Left: 12, Top: 14, Right: 12, Bottom: 12}, Spacing: 12}, Children: []decl.Widget{
		decl.CheckBox{AssignTo: &controls.startWithWindows, Text: "Start PCController desktop with Windows", Checked: cfg.Integration.StartWithWindows},
		decl.PushButton{Text: "Host preferences…", Enabled: actions.OpenHostSettings != nil, OnClicked: func() {
			if actions.OpenHostSettings != nil {
				actions.OpenHostSettings()
			}
		}},
		decl.PushButton{Text: "Open Web UI", Enabled: actions.OpenWebUI != nil, OnClicked: func() {
			if actions.OpenWebUI != nil {
				actions.OpenWebUI()
			}
		}},
		decl.VSpacer{},
	}}
}
