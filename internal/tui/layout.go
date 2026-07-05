package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// truncateCells shortens s to at most max terminal cells, measuring display
// width (so wide runes / ANSI are handled correctly). This is the guard that
// keeps panel rows and the status bar to exactly their declared width: real
// report text (long ~/.codex paths, long risk messages) must be cut to fit
// rather than overflow and break the one-line, fixed-width contract.
func truncateCells(s string, max int) string {
	if max <= 0 {
		return ""
	}
	return ansi.Truncate(s, max, "")
}

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
	// The label is "─ "+title+" " (title width + 3 cells), so the title is
	// capped to inner-3 cells to guarantee the top border never exceeds width.
	label := ""
	if title != "" {
		title = truncateCells(title, inner-3)
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
		// Truncate to the inner content width (inner-1, accounting for the 1
		// leading space) BEFORE padding, so a wide line can never overflow the
		// right border.
		line = truncateCells(line, inner-1)
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
	if width < 1 {
		width = 1
	}
	// Truncate content so the assembled line is never wider than width: if it
	// were, statusBarStyle.Width(width).Render would word-WRAP the overflow into
	// extra lines, silently turning the fixed one-row bar into 2+ rows and
	// misaligning the last row of later screens. Status (the shorter right-hand
	// hint) is preserved first; keys yields the space. When status alone fills
	// the whole width, keys is dropped and there is no gap.
	if lipgloss.Width(status) >= width {
		status = truncateCells(status, width)
		keys = ""
	} else {
		keys = truncateCells(keys, width-lipgloss.Width(status)-1)
	}
	gap := width - lipgloss.Width(keys) - lipgloss.Width(status)
	// gap is >= 1 whenever keys is present (keys was capped to leave room) and
	// exactly fills the remainder otherwise; clamp defensively so the line width
	// equals width and Render never wraps.
	if gap < 0 {
		gap = 0
	}
	line := keys + strings.Repeat(" ", gap) + status
	return statusBarStyle.Width(width).Render(line)
}
