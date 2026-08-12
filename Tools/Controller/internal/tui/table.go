package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
)

// dataColumn describes one terminal-cell-aware column rendered by Bubbles.
type dataColumn struct {
	Title string
	Width int
	Align lipgloss.Position
}

type controlValueTone int

const (
	controlToneNeutral controlValueTone = iota
	controlToneOn
	controlToneOff
	controlToneAction
	controlToneLevel
)

type controlTableRow struct {
	Group string
	Name  string
	Value string
	Tone  controlValueTone
}

type controlTableLine struct {
	Logical int
	Group   string
}

func controlTableLines(rows []controlTableRow) []controlTableLine {
	lines := make([]controlTableLine, 0, len(rows)+3)
	for logical, row := range rows {
		if row.Group != "" {
			lines = append(lines, controlTableLine{Logical: -1, Group: row.Group})
		}
		lines = append(lines, controlTableLine{Logical: logical})
	}
	return lines
}

func visibleControlTableLines(rows []controlTableRow, visibleRows, cursor int) []controlTableLine {
	lines := controlTableLines(rows)
	if len(lines) == 0 {
		return nil
	}
	selected := 0
	for index, line := range lines {
		if line.Logical == cursor {
			selected = index
			break
		}
	}
	start, end := tableWindow(len(lines), visibleRows, selected)
	return lines[start:end]
}

func controlTableLogicalAt(rows []controlTableRow, visibleRows, cursor, visibleLine int) (int, bool) {
	lines := visibleControlTableLines(rows, visibleRows, cursor)
	if visibleLine < 0 || visibleLine >= len(lines) || lines[visibleLine].Logical < 0 {
		return 0, false
	}
	return lines[visibleLine].Logical, true
}

func renderControlTable(
	width, visibleRows, cursor int,
	columns []dataColumn,
	rows []controlTableRow,
	colorValues bool,
) string {
	if width < 8 {
		width = 8
	}
	innerWidth := width - 2
	if len(columns) != 2 || columns[0].Width+columns[1].Width != innerWidth {
		return ""
	}
	borderStyle := lipgloss.NewStyle().Foreground(colorPanel)
	border := func(left, fill, right string) string {
		return borderStyle.Render(left + strings.Repeat(fill, innerWidth) + right)
	}
	header := alignCell(columns[0].Title, columns[0].Width, lipgloss.Center) +
		alignCell(columns[1].Title, columns[1].Width, lipgloss.Center)
	header = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Background(colorPanel).Render(header)

	lines := []string{border("╭", "─", "╮"), borderStyle.Render("│") + header + borderStyle.Render("│")}
	for _, line := range visibleControlTableLines(rows, visibleRows, cursor) {
		if line.Logical < 0 {
			label := "─ " + line.Group + " "
			separator := label + strings.Repeat("─", max(0, innerWidth-lipgloss.Width(label)))
			lines = append(lines, borderStyle.Render("│")+labelStyle.Render(separator)+borderStyle.Render("│"))
			continue
		}
		row := rows[line.Logical]
		selected := line.Logical == cursor
		marker := "  "
		if selected {
			marker = "▸ "
		}
		name := alignCell(marker+row.Name, columns[0].Width, lipgloss.Left)
		value := alignCell(row.Value, columns[1].Width, lipgloss.Left)
		nameStyle := lipgloss.NewStyle().Foreground(colorBright)
		valueStyle := lipgloss.NewStyle().Foreground(colorBright)
		if colorValues {
			switch row.Tone {
			case controlToneOn:
				valueStyle = valueStyle.Foreground(colorGood)
			case controlToneOff:
				valueStyle = valueStyle.Foreground(colorBad)
			case controlToneAction:
				valueStyle = valueStyle.Foreground(colorWarn)
			case controlToneLevel:
				valueStyle = valueStyle.Foreground(colorAccent)
			}
		}
		if selected {
			nameStyle = nameStyle.Background(colorPanel).Bold(true)
			valueStyle = valueStyle.Background(colorPanel).Bold(true)
		}
		lines = append(lines, borderStyle.Render("│")+nameStyle.Render(name)+valueStyle.Render(value)+borderStyle.Render("│"))
	}
	lines = append(lines, border("╰", "─", "╯"))
	return strings.Join(lines, "\n")
}

// tableWindow returns a stable viewport centered on the selected logical row.
// Keeping the window explicit also makes mouse row mapping deterministic.
func tableWindow(total, visible, cursor int) (start, end int) {
	if total <= 0 || visible <= 0 {
		return 0, 0
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= total {
		cursor = total - 1
	}
	if visible >= total {
		return 0, total
	}
	start = cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}

// renderDataTable renders aligned data through Charm's table component. Column
// headings and cells are padded by visible terminal width, not byte length.
func renderDataTable(
	width, visibleRows, cursor int,
	columns []dataColumn,
	rows [][]string,
) string {
	if width < 8 {
		width = 8
	}
	if visibleRows < 1 {
		visibleRows = 1
	}
	selection := cursor
	if cursor < 0 {
		cursor = 0
	}
	start, end := tableWindow(len(rows), visibleRows, cursor)
	visible := rows[start:end]
	tableRows := make([]table.Row, 0, len(visible))
	for _, source := range visible {
		row := make(table.Row, len(columns))
		for index, column := range columns {
			value := ""
			if index < len(source) {
				value = source[index]
			}
			row[index] = alignCell(value, column.Width, column.Align)
		}
		tableRows = append(tableRows, row)
	}

	tableColumns := make([]table.Column, len(columns))
	for index, column := range columns {
		tableColumns[index] = table.Column{
			Title: alignCell(column.Title, column.Width, lipgloss.Center),
			Width: column.Width,
		}
	}
	styles := table.DefaultStyles()
	styles.Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorAccent).
		Background(colorPanel)
	styles.Cell = lipgloss.NewStyle().Foreground(colorBright)
	styles.Selected = selectedStyle.Copy()
	if selection < 0 {
		styles.Selected = styles.Cell
	}

	widget := table.New(
		table.WithColumns(tableColumns),
		table.WithRows(tableRows),
		table.WithHeight(len(tableRows)+1),
		table.WithWidth(width),
		table.WithFocused(true),
		table.WithStyles(styles),
	)
	widget.SetCursor(cursor - start)

	frame := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPanel).
		Padding(0, 1)
	frame = frame.Width(max(1, width-frame.GetHorizontalFrameSize()))
	return frame.Render(widget.View())
}

func alignCell(value string, width int, alignment lipgloss.Position) string {
	value = truncateDisplayText(value, width)
	missing := width - lipgloss.Width(value)
	if missing <= 0 {
		return value
	}
	switch alignment {
	case lipgloss.Right:
		return strings.Repeat(" ", missing) + value
	case lipgloss.Center:
		left := missing / 2
		return strings.Repeat(" ", left) + value + strings.Repeat(" ", missing-left)
	default:
		return value + strings.Repeat(" ", missing)
	}
}

func tableBodyRows(contentHeight int) int {
	// Page header + help row + rounded border + table header consume five rows.
	if rows := contentHeight - 5; rows > 0 {
		return rows
	}
	return 1
}

func (model Model) presentationTableWidth(compactMaximum int) int {
	if strings.EqualFold(strings.TrimSpace(model.uiValue.TableLayout), "expanded") {
		return model.width
	}
	if compactMaximum < 56 {
		compactMaximum = 56
	}
	return min(model.width, compactMaximum)
}

func (model Model) centeredDataTable(
	width, visibleRows, cursor int,
	columns []dataColumn,
	rows [][]string,
) string {
	tableView := renderDataTable(width, visibleRows, cursor, columns, rows)
	return lipgloss.PlaceHorizontal(model.width, lipgloss.Center, tableView)
}
