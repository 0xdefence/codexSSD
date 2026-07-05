package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// effectiveWidth resolves the width to render at. Before the first WindowSizeMsg
// the model width is 0; assume a conventional 80-column terminal so the first
// paint is sane.
func effectiveWidth(m Model) int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

// panel wraps body in a rounded border of total outer `width`, with `title`
// spliced into the top border. Border runes are colored with the brand accent;
// the title with panelTitleStyle. Body lines are left-padded one space and
// filled to the inner width so the right border aligns.
func panel(title, body string, width int) string {
	if width < 4 {
		width = 4
	}
	inner := width - 2 // columns between the two vertical borders
	b := lipgloss.RoundedBorder()

	// Top border: "╭─ Title ──────╮" (or a plain top when title is empty).
	label := ""
	if title != "" {
		label = b.Top + " " + panelTitleStyle.Render(title) + " "
	}
	dashes := inner - lipgloss.Width(label)
	if dashes < 0 {
		dashes = 0
	}
	top := panelBorderStyle.Render(b.TopLeft) + label + panelBorderStyle.Render(strings.Repeat(b.Top, dashes)) + panelBorderStyle.Render(b.TopRight)
	bottom := panelBorderStyle.Render(b.BottomLeft + strings.Repeat(b.Top, inner) + b.BottomRight)

	left := panelBorderStyle.Render(b.Left)
	right := panelBorderStyle.Render(b.Right)

	var rows []string
	rows = append(rows, top)
	for _, line := range strings.Split(body, "\n") {
		pad := inner - 1 - lipgloss.Width(line) // 1 leading space
		if pad < 0 {
			pad = 0
		}
		rows = append(rows, left+" "+line+strings.Repeat(" ", pad)+right)
	}
	rows = append(rows, bottom)
	return strings.Join(rows, "\n")
}

// statusBar renders the full-width bottom bar: keys left, status right, filled
// with the accent background.
func statusBar(keys, status string, width int) string {
	gap := width - lipgloss.Width(keys) - lipgloss.Width(status)
	if gap < 1 {
		gap = 1
	}
	line := keys + strings.Repeat(" ", gap) + status
	return statusBarStyle.Width(width).Render(line)
}
