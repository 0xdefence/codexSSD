package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/monitor"
)

// Palette. Adaptive colors pick a legible shade for light or dark terminals;
// under NO_COLOR / non-color terminals Lip Gloss drops color automatically.
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#0e7490", Dark: "#22d3ee"} // cyan/teal brand
	mutedColor  = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"} // secondary text

	// Risk colors are semantic and deliberately distinct from the brand accent.
	riskGreenColor  = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#22c55e"}
	riskYellowColor = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#eab308"}
	riskOrangeColor = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#f97316"}
	riskRedColor    = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#ef4444"}
)

// Shared styles. Centralized so every screen reads the same.
var (
	logoStyle     = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	subtitleStyle = lipgloss.NewStyle().Foreground(mutedColor)
	headerStyle   = lipgloss.NewStyle().Foreground(accentColor).Bold(true)

	panelTitleStyle  = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	panelBorderStyle = lipgloss.NewStyle().Foreground(accentColor)

	// The status bar fills its width with the accent as a background; text on it
	// stays high-contrast. Foreground("0")/Background(accent) reads as dark-on-cyan.
	statusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)

	// Selected restore-list row: inverse-style highlight.
	selectedRowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(accentColor)

	mutedTextStyle = lipgloss.NewStyle().Foreground(mutedColor)
)

// riskColor maps a risk level to its semantic color.
func riskColor(level monitor.Risk) lipgloss.TerminalColor {
	switch level {
	case monitor.RiskCritical:
		return riskRedColor
	case monitor.RiskHigh:
		return riskOrangeColor
	case monitor.RiskMedium:
		return riskYellowColor
	default:
		return riskGreenColor
	}
}

// riskStyle is the bold, colored style for a risk level's label and glyph.
func riskStyle(level monitor.Risk) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(riskColor(level)).Bold(true)
}

// riskGlyph is the status dot shown beside a risk level. Always present (even at
// LOW) so the layout does not shift as risk changes.
func riskGlyph(monitor.Risk) string { return "●" }
