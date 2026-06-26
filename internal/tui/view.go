package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// View implements tea.Model.
func (m Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Starting…")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.footer())
	return b.String()
}

func (m Model) footer() string {
	return "c tidy · r restore · ? help · q quit"
}

func (m Model) renderHelp() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD — help"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "c    tidy Codex's logs aside (recoverable)")
	fmt.Fprintln(&b, "r    restore previously tidied logs")
	fmt.Fprintln(&b, "?    toggle this help")
	fmt.Fprintln(&b, "q    quit")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "press ? or esc to close")
	return b.String()
}
