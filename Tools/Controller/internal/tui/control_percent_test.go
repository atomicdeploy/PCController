package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestControlPercentReadoutNeverTruncates(t *testing.T) {
	for _, width := range []int{34, 40, 56, 80, 118, 160} {
		for _, percent := range []int{0, 9, 10, 99, 100} {
			for _, selected := range []int{-1, 0} {
				t.Run(fmt.Sprintf("width%d/percent%d/selected%d", width, percent, selected), func(t *testing.T) {
					columns := outputTableColumns(width)
					value := sliderPercentPlain(percent, controlLevelWidth(columns[1].Width)) + fmt.Sprintf(" %3d%%", percent)
					row := controlTableRow{Name: "PWM", Kind: "pwm", Value: value, Detail: "applied 4095/4095 · policy 4095/4095", Tone: controlToneLevel}
					for _, colors := range []bool{false, true} {
						plain := ansi.Strip(renderControlTable(width, 1, selected, columns, []controlTableRow{row}, colors))
						if !strings.Contains(plain, value) || strings.Contains(plain, "…") {
							t.Fatalf("measurement truncated:\n%s", plain)
						}
						for _, line := range strings.Split(plain, "\n") {
							if lipgloss.Width(line) != width {
								t.Fatalf("line width %d, want %d: %q", lipgloss.Width(line), width, line)
							}
						}
					}
				})
			}
		}
	}
}

func TestControlPWMRowsKeepDiagnosticsSeparate(t *testing.T) {
	model := readyModel(t, PageOutputs)
	model.havePWMValues = true
	for channel := range model.pwmValues {
		model.pwmValues[channel] = 4095
	}
	columns := outputTableColumns(118)
	rows := model.controlTableRows(model.snapshot(), controlLevelWidth(columns[1].Width))
	count := 0
	for _, row := range rows {
		if row.Kind != "pwm" {
			continue
		}
		count++
		if !strings.HasSuffix(row.Value, "100%") || lipgloss.Width(row.Value) != columns[1].Width {
			t.Fatalf("PWM %d has invalid measurement %q", row.Index, row.Value)
		}
		plain := ansi.Strip(renderControlTable(118, 2, 0, columns, []controlTableRow{row}, true))
		if !strings.Contains(plain, row.Value) {
			t.Fatalf("PWM %d lost measurement:\n%s", row.Index, plain)
		}
	}
	if count != 12 {
		t.Fatalf("checked %d PWM channels, want 12", count)
	}
}
