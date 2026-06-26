package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/codex"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// View implements tea.Model.
func (m Model) View() string {
	if m.showHelp {
		return m.renderHelp()
	}
	switch m.state {
	case stateConfirmClean:
		return m.renderConfirmClean()
	case stateCleaning:
		return m.renderWorking("Tidying Codex logs aside…")
	case stateRestoreList:
		return m.renderRestoreList()
	case stateConfirmRestore:
		return m.renderConfirmRestore()
	case stateRestoring:
		return m.renderWorking("Restoring…")
	case stateResult:
		return m.renderResult()
	case stateBlocked:
		return m.renderBlocked()
	default:
		return m.renderDashboard()
	}
}

func (m Model) renderDashboard() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)

	if m.loadErr != nil {
		fmt.Fprintf(&b, "Could not read Codex's folder: %v\n\n", m.loadErr)
		fmt.Fprintln(&b, m.footer())
		return b.String()
	}

	fmt.Fprintf(&b, "Codex folder: %s\n", m.report.CodexDir)
	if !m.report.DirExists {
		fmt.Fprintln(&b, "  (not found — Codex may not have run yet)")
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, m.footer())
		return b.String()
	}

	fmt.Fprintln(&b, "Codex logs:")
	for _, f := range m.report.Files {
		if f.Exists {
			fmt.Fprintf(&b, "  %-20s %10s\n", f.Name, codex.HumanBytes(f.Size))
		}
	}
	fmt.Fprintf(&b, "  %-20s %10s\n\n", "Total", codex.HumanBytes(m.report.TotalBytes))

	if m.deadweight() {
		fmt.Fprintf(&b, "⚠  %s of Codex logs are sitting here — worth tidying.\n", codex.HumanBytes(m.report.TotalBytes))
	} else {
		fmt.Fprintln(&b, "Nothing alarming right now.")
	}

	switch {
	case !m.supported:
		fmt.Fprintln(&b, "(This platform can't check whether Codex is running.)")
	case m.running:
		fmt.Fprintln(&b, "Codex appears to be running.")
	default:
		fmt.Fprintln(&b, "Codex doesn't appear to be running.")
	}

	if t, ok := m.lastTidy(); ok {
		fmt.Fprintf(&b, "Recoverable backups: %d (last tidy %s)\n", len(m.backups), t.Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintln(&b, "Recoverable backups: none")
	}

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

func (m Model) renderConfirmClean() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Tidy Codex logs"))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Move %s of Codex's own logs into a recoverable bin?\n", codex.HumanBytes(m.report.TotalBytes))
	fmt.Fprintln(&b, "Nothing is deleted — you can restore them any time.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "y yes · n no")
	return b.String()
}

func (m Model) renderWorking(label string) string {
	return titleStyle.Render("CodexSSD") + "\n\n" + label + "\n"
}

func (m Model) renderResult() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("CodexSSD"))
	fmt.Fprintln(&b)
	if m.resultErr != nil {
		fmt.Fprintf(&b, "Something went wrong: %v\n", m.resultErr)
	} else {
		fmt.Fprintln(&b, m.resultMsg)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "enter return to dashboard")
	return b.String()
}

func (m Model) renderRestoreList() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Restore a backup"))
	fmt.Fprintln(&b)
	for i, bk := range m.backups {
		cursor := "  "
		if i == m.selected {
			cursor = "> "
		}
		var total int64
		for _, it := range bk.Manifest.Items {
			total += it.Size
		}
		fmt.Fprintf(&b, "%s%-18s %10s\n", cursor, filepathBase(bk.Dir), codex.HumanBytes(total))
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "↑/↓ choose · enter select · esc back")
	return b.String()
}

func (m Model) renderConfirmRestore() string {
	var b strings.Builder
	id := filepathBase(m.backups[m.selected].Dir)
	fmt.Fprintln(&b, titleStyle.Render("Restore backup"))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Move the logs in backup %s back to your Codex folder?\n", id)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "y yes · n no")
	return b.String()
}

func (m Model) renderBlocked() string {
	var b strings.Builder
	fmt.Fprintln(&b, titleStyle.Render("Can't do that right now"))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, m.blockedReason)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "enter return to dashboard")
	return b.String()
}
