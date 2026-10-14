package tui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

func (model Model) rfLearningPreferencesPage() string {
	p := model.rfValue.Learning
	return strings.Join([]string{sectionHeader(model.width, "RF learning settings", ""),
		kv("T Timed learning", fmt.Sprint(p.Timed)), kv("Timeout", fmt.Sprintf("%d s", p.TimeoutSeconds)),
		kv("C Continuous learning", fmt.Sprint(p.Continuous)), "", "− / + Timeout · S Save · Esc Cancel"}, "\n")
}
func (model Model) handleRFLearningPreferencesKey(key string) (Model, tea.Cmd, bool) {
	switch key {
	case "esc":
		model.rfPreferencesOpen = false
		if model.rfConfig != nil {
			model.rfValue = model.rfConfig()
		}
	case "t":
		model.rfValue.Learning.Timed = !model.rfValue.Learning.Timed
	case "c":
		model.rfValue.Learning.Continuous = !model.rfValue.Learning.Continuous
	case "+", "=":
		if model.rfValue.Learning.TimeoutSeconds < 120 {
			model.rfValue.Learning.TimeoutSeconds++
		}
	case "-":
		if model.rfValue.Learning.TimeoutSeconds > 1 {
			model.rfValue.Learning.TimeoutSeconds--
		}
	case "s":
		model.persistRFConfig("RF learning settings saved")
		model.rfPreferencesOpen = false
	}
	return model, nil, true
}
