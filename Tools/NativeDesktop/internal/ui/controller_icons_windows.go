//go:build windows

package ui

import (
	"github.com/lxn/walk"
	d "github.com/lxn/walk/declarative"
	"pccontroller.local/desktop/internal/dialogs"
	"strings"
)

type controllerIconChoice struct{ id, name, glyph, keywords string }

var controllerIconChoices = []controllerIconChoice{
	{"", "Automatic", "\uE713", "default reset"},
	{"lightbulb", "Light bulb", "\uE781", "light lighting"},
	{"lamp-ceiling", "Chandelier / ceiling lamp", "\uE781", "loostr lustr lustre ceiling"},
	{"lamp-floor", "Floor lamp", "\uE781", "standing lantern"},
	{"lamp-desk", "Desk lamp", "\uE781", "table study"},
	{"lamp-wall-up", "Wall light up", "\uE781", "sconce"},
	{"lamp-wall-down", "Wall light down", "\uE781", "sconce"},
	{"sun", "Sun / stage light", "\uE706", "bright lighting spotlight"},
	{"sun-dim", "Dim light", "\uE706", "brightness"},
	{"moon", "Moon", "\uE708", "night"},
	{"power", "Power", "\uE7E8", "on off switch"},
	{"plug", "Plug", "\uE945", "socket outlet"},
	{"zap", "Lightning", "\uE945", "relay electricity"},
	{"fan", "Fan", "\uE9CA", "cooling air ventilation"},
	{"monitor", "Monitor", "\uE7F4", "screen display pc"},
	{"tv", "Television", "\uE714", "screen cinema"},
	{"projector", "Projector", "\uE95D", "cinema screen movie"},
	{"panel-top", "Window", "\uE737", "blind blinds shade curtain"},
	{"door-open", "Open door", "\uE8F4", "entrance gate"},
	{"door-closed", "Closed door", "\uE8F4", "entrance gate"},
	{"trees", "Garden / trees", "\uE8BE", "outdoor yard"},
	{"flower", "Flower", "\uE8BE", "garden plant"},
	{"leaf", "Leaf", "\uE8BE", "garden eco plant"},
	{"volume-2", "Speaker", "\uE767", "audio sound buzzer"},
	{"music", "Music", "\uE8D6", "melody tone"},
	{"bell", "Bell", "\uE7ED", "alert alarm buzzer"},
	{"thermometer", "Temperature", "\uE9CA", "sensor heat"},
	{"droplets", "Water", "\uE9CA", "pump irrigation"},
	{"house", "House", "\uE80F", "home room"},
	{"bed", "Bedroom", "\uE913", "bed sleep room"},
	{"sofa", "Sofa / seat", "\uE8B0", "chair cinema seating"},
	{"cog", "Motor", "\uE713", "gear machine"},
	{"cpu", "Controller", "\uE950", "board hardware chip"},
	{"wifi", "Wireless", "\uE701", "radio rf network"},
	{"bluetooth", "Bluetooth", "\uE702", "wireless"},
	{"camera", "Camera", "\uE722", "video security"},
	{"lock", "Lock", "\uE72E", "security"},
	{"battery", "Battery", "\uE850", "power voltage"},
	{"palette", "Color", "\uE790", "rgb led strip"},
	{"sparkles", "Effect", "\uE734", "animation lighting"},
}

func filterControllerIcons(query string, choices []controllerIconChoice) []controllerIconChoice {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	var result []controllerIconChoice
	for _, entry := range choices {
		haystack := strings.ToLower(entry.id + " " + entry.name + " " + entry.keywords)
		match := true
		for _, word := range words {
			match = match && strings.Contains(haystack, word)
		}
		if match {
			result = append(result, entry)
		}
	}
	return result
}

type controllerIconModel struct {
	walk.TableModelBase
	rows []controllerIconChoice
}

func (m *controllerIconModel) RowCount() int { return len(m.rows) }
func (m *controllerIconModel) Value(row, col int) any {
	if col == 0 {
		return m.rows[row].glyph
	}
	return m.rows[row].name
}

type controllerIconPicker struct {
	dialog *walk.Dialog
	search *walk.LineEdit
	table  *walk.TableView
	accept *walk.PushButton
	model  *controllerIconModel
	font   *walk.Font
	theme  *dialogs.ThemeSession
}

func (p *controllerIconPicker) dispose() {
	p.theme.Dispose()
	p.dialog.Dispose()
	p.font.Dispose()
}

func (p *controllerIconPicker) selection() (string, bool) {
	index := p.table.CurrentIndex()
	if p.dialog.Result() == walk.DlgCmdOK && index >= 0 && index < len(p.model.rows) {
		return p.model.rows[index].id, true
	}
	return "", false
}

func (s *Shell) chooseControllerIcon(current string) (string, bool) {
	p, err := s.newControllerIconPicker(current)
	if err != nil {
		s.showError("Choose icon", err)
		return current, false
	}
	defer p.dispose()
	p.dialog.Run()
	if value, ok := p.selection(); ok {
		return value, true
	}
	return current, false
}

func (s *Shell) newControllerIconPicker(current string) (*controllerIconPicker, error) {
	choices := append([]controllerIconChoice(nil), controllerIconChoices...)
	known := false
	for _, entry := range choices {
		known = known || entry.id == strings.TrimPrefix(current, "lucide:")
	}
	if !known {
		choices = append(choices, controllerIconChoice{current, "Current: " + current, controllerChannelGlyph(current, "\uE713"), current})
	}
	model := &controllerIconModel{rows: choices}
	var dialog *walk.Dialog
	var search *walk.LineEdit
	var table *walk.TableView
	var accept, cancel *walk.PushButton
	font, err := walk.NewFont("Segoe MDL2 Assets", 12, 0)
	if err != nil {
		return nil, err
	}
	spec := d.Dialog{AssignTo: &dialog, Title: "Channel icon", Font: d.Font{Family: s.preferredFontFamily(), PointSize: 10}, Size: d.Size{Width: 380, Height: 430}, MinSize: d.Size{Width: 320, Height: 300}, Layout: d.VBox{Margins: d.Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}, Spacing: 12}, DefaultButton: &accept, CancelButton: &cancel, Children: []d.Widget{
		d.LineEdit{AssignTo: &search, CueBanner: "Search icons…", OnTextChanged: func() {
			model.rows = filterControllerIcons(search.Text(), choices)
			model.PublishRowsReset()
			table.SetCurrentIndex(-1)
			accept.SetEnabled(false)
		}},
		d.TableView{AssignTo: &table, Model: model, HeaderHidden: true, LastColumnStretched: true, Columns: []d.TableViewColumn{{Title: "Icon", Width: 44}, {Title: "Name", Width: 270}}, StyleCell: func(style *walk.CellStyle) {
			if style.Col() == 0 {
				style.Font = font
			}
		}, OnCurrentIndexChanged: func() {
			if accept != nil {
				accept.SetEnabled(table.CurrentIndex() >= 0)
			}
		}, OnItemActivated: func() {
			if table.CurrentIndex() >= 0 {
				dialog.Accept()
			}
		}},
		d.Composite{Layout: d.HBox{MarginsZero: true}, Children: []d.Widget{d.HSpacer{}, d.PushButton{AssignTo: &accept, Text: "Use icon", Enabled: false, OnClicked: func() { dialog.Accept() }}, d.PushButton{AssignTo: &cancel, Text: "Cancel", OnClicked: func() { dialog.Cancel() }}}},
	}}
	if err := spec.Create(s.mw); err != nil {
		font.Dispose()
		if dialog != nil {
			dialog.Dispose()
		}
		return nil, err
	}
	theme := dialogs.NewThemeSession(dialog)
	theme.Apply(s.Config().UI.Theme, s.motionEnabled)
	if s.appIcon != nil {
		dialog.SetIcon(s.appIcon)
	}
	for i, entry := range choices {
		if entry.id == strings.TrimPrefix(current, "lucide:") {
			table.SetCurrentIndex(i)
			break
		}
	}
	search.SetFocus()
	return &controllerIconPicker{dialog: dialog, search: search, table: table, accept: accept, model: model, font: font, theme: theme}, nil
}
