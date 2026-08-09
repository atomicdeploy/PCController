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
