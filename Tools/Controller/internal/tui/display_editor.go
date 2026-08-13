package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pccontroller.local/controller/internal/control"
	"pccontroller.local/controller/internal/native"
	"pccontroller.local/controller/internal/shell"
)

type displayEditor struct {
	Text        string
	Targets     []string
	Target      int
	SpeedMS     int
	DurationMS  int
	Repeat      control.DisplayRepeat
	IntervalMS  int
	ForceScroll bool
	Cursor      int
}

func displayTargetsFor(snapshot control.Snapshot) []string {
	capabilities := snapshot.Hello.Capabilities
	haveSegments := snapshot.Connected && capabilities&native.CapabilityScheduledSegments != 0
	haveLCD := snapshot.Connected && snapshot.HaveStatus && snapshot.Status.LCDAddress != 0 &&
		capabilities&native.CapabilityLCD != 0
	result := make([]string, 0, 3)
	if haveSegments {
		result = append(result, "segments")
	}
	if haveLCD {
		result = append(result, "lcd")
	}
	if haveSegments && haveLCD {
		result = append(result, "both")
	}
	return result
}

func (model Model) beginDisplayEditor() (Model, tea.Cmd, bool) {
	targets := displayTargetsFor(model.snapshot())
	if len(targets) == 0 {
		model.setNotice("Display message unavailable: the connected board did not advertise LCD or scheduled segments")
		return model, nil, true
	}
	scroll := model.uiValue.SegmentScroll
	speed := scroll.SpeedMS
	if speed < 80 || speed > 5000 {
		speed = 220
	}
	interval := scroll.IntervalSeconds * 1000
	if interval < 1000 || interval > 255000 {
		interval = 30000
	}
	repeat := control.DisplayRepeat(scroll.Repeat)
	if repeat != control.DisplayRepeatOnce && repeat != control.DisplayRepeatLoop && repeat != control.DisplayRepeatInterval {
		repeat = control.DisplayRepeatOnce
	}
	model.displayEditor = &displayEditor{
		Targets: targets, SpeedMS: speed, DurationMS: 5000,
		Repeat: repeat, IntervalMS: interval,
	}
	return model, nil, true
}

func (model Model) handleDisplayEditorKey(message tea.KeyMsg) (Model, tea.Cmd, bool) {
	editor := model.displayEditor
	if editor == nil {
		return model, nil, false
	}
	if message.Type == tea.KeyRunes {
		for _, character := range message.Runes {
			if character >= 0x20 && character <= 0x7e && len(editor.Text)+1 <= 40 {
				editor.Text += string(character)
			}
		}
		return model, nil, true
	}
	switch message.String() {
	case "esc":
		model.displayEditor = nil
		model.setNotice("Display message discarded")
		return model, nil, true
	case "up", "shift+tab":
		editor.Cursor = wrapInt(editor.Cursor, -1, 7)
		return model, nil, true
	case "down", "tab":
		editor.Cursor = wrapInt(editor.Cursor, 1, 7)
		return model, nil, true
	case "left":
		model.adjustDisplayEditor(-1)
		return model, nil, true
	case "right", " ":
		model.adjustDisplayEditor(1)
		return model, nil, true
	case "backspace":
		runes := []rune(editor.Text)
		if len(runes) != 0 {
			editor.Text = string(runes[:len(runes)-1])
		}
		return model, nil, true
	case "enter":
		line := displayEditorCommand(editor)
		model.displayEditor = nil
		return model.dispatchLine(line)
	}
	return model, nil, true
}

func (model *Model) adjustDisplayEditor(delta int) {
	editor := model.displayEditor
	if editor == nil || delta == 0 {
		return
	}
	switch editor.Cursor {
	case 1:
		editor.Target = wrapInt(editor.Target, delta, len(editor.Targets))
	case 2:
		editor.SpeedMS = clampDisplayValue(editor.SpeedMS+delta*20, 80, 5000)
	case 3:
		editor.DurationMS = clampDisplayValue(editor.DurationMS+delta*500, 80, 65535)
	case 4:
		repeats := []control.DisplayRepeat{control.DisplayRepeatOnce, control.DisplayRepeatLoop, control.DisplayRepeatInterval}
		index := 0
		for candidate, repeat := range repeats {
			if repeat == editor.Repeat {
				index = candidate
				break
			}
		}
		editor.Repeat = repeats[wrapInt(index, delta, len(repeats))]
	case 5:
		editor.IntervalMS = clampDisplayValue(editor.IntervalMS+delta*1000, 1000, 255000)
	case 6:
		editor.ForceScroll = !editor.ForceScroll
	}
}

func clampDisplayValue(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func displayEditorCommand(editor *displayEditor) string {
	if editor == nil || len(editor.Targets) == 0 {
		return ""
	}
	target := editor.Targets[editor.Target%len(editor.Targets)]
	arguments := []string{
		"display", target,
		"--speed-ms", fmt.Sprint(editor.SpeedMS),
		"--duration-ms", fmt.Sprint(editor.DurationMS),
		"--repeat", string(editor.Repeat),
	}
	if editor.Repeat == control.DisplayRepeatInterval {
		arguments = append(arguments, "--interval-ms", fmt.Sprint(editor.IntervalMS))
	}
	if editor.ForceScroll {
		arguments = append(arguments, "--scroll")
	}
	arguments = append(arguments, "--", editor.Text)
	return shell.Join(arguments)
}

func renderDisplayEditor(editor *displayEditor, width int) string {
	if editor == nil || len(editor.Targets) == 0 {
		return ""
	}
	dialogWidth := min(max(width-12, 52), 82)
	target := editor.Targets[editor.Target%len(editor.Targets)]
	scrolling := editor.ForceScroll || ((target == "segments" || target == "both") && len(editor.Text) > 4)
	rows := [][]string{
		{"Message", editor.Text + "▏"},
		{"Target", target},
		{"Scroll speed", fmt.Sprintf("%d ms", editor.SpeedMS)},
		{"Hold duration", fmt.Sprintf("%d ms", editor.DurationMS)},
		{"Repeat", string(editor.Repeat)},
		{"Repeat interval", fmt.Sprintf("%d ms", editor.IntervalMS)},
		{"Force marquee", boolWord(editor.ForceScroll, "YES", "NO")},
	}
	columns := []dataColumn{
		{Title: "PARAMETER", Width: 20, Align: lipgloss.Left},
		{Title: "VALUE", Width: dialogWidth - 27, Align: lipgloss.Left},
	}
	marqueeState := "off"
	if editor.ForceScroll {
		marqueeState = "forced"
	} else if scrolling {
		marqueeState = "automatic"
	}
	lines := []string{
		sectionHeader(dialogWidth, "SEND DISPLAY MESSAGE", ""),
		renderDataTable(dialogWidth-2, len(rows), editor.Cursor, columns, rows),
		labelStyle.Render("Marquee · " + marqueeState),
	}
	body := strings.Join(lines, "\n")
	return lipgloss.Place(width, max(14, lipgloss.Height(body)+2), lipgloss.Center, lipgloss.Center,
		cardStyle.Copy().Padding(1, 2).Render(body))
}
