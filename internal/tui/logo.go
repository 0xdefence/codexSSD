package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// blockLogo is the ANSI-shadow wordmark "CODEX". Its lines are equal width; do
// not reflow or trim — the trailing spaces keep the block rectangular.
const blockLogo = ` ██████╗ ██████╗ ██████╗ ███████╗██╗  ██╗
██╔════╝██╔═══██╗██╔══██╗██╔════╝╚██╗██╔╝
██║     ██║   ██║██║  ██║█████╗   ╚███╔╝ 
██║     ██║   ██║██║  ██║██╔══╝   ██╔██╗ 
╚██████╗╚██████╔╝██████╔╝███████╗██╔╝ ██╗
 ╚═════╝ ╚═════╝ ╚═════╝ ╚══════╝╚═╝  ╚═╝`

const logoSubtitle = "SSD · the disk watchdog"

// logoWidth is the natural rendered width of blockLogo (widest line).
func logoWidth() int {
	w := 0
	for _, line := range strings.Split(blockLogo, "\n") {
		if lw := lipgloss.Width(line); lw > w {
			w = lw
		}
	}
	return w
}

// renderCompactLogo is the one-line wordmark used as a header on secondary
// screens, where vertical space is at a premium. Always compact, never the block.
func renderCompactLogo(width int) string {
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return center.Render(logoStyle.Render("codexSSD")) + "\n" + center.Render(subtitleStyle.Render(logoSubtitle))
}

// renderLogo returns the centered, styled wordmark for a terminal of the given
// width. When width is too narrow for the block art it falls back to the compact
// form. Both include the subtitle. Callers pass a positive width.
func renderLogo(width int) string {
	if width < logoWidth() {
		return renderCompactLogo(width)
	}
	center := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)
	return center.Render(logoStyle.Render(blockLogo)) + "\n" + center.Render(subtitleStyle.Render(logoSubtitle))
}
