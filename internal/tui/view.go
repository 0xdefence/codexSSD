package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
)

var titleStyle = lipgloss.NewStyle().Bold(true)

// banner classifies what the dashboard's deadweight line should say.
type banner int

const (
	bannerCalm          banner = iota // nothing worth tidying
	bannerActionable                  // deadweight + Codex idle → offer to tidy now
	bannerInformational               // deadweight + Codex active → can't act yet
)

// bannerState is a pure function of the current load: it never tracks history.
func (m Model) bannerState() banner {
	concern := m.deadweight() || m.assessment.Level >= monitor.RiskMedium
	if !concern {
		return bannerCalm
	}
	if m.supported && !m.running {
		return bannerActionable
	}
	return bannerInformational
}

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
	w := effectiveWidth(m)
	var sections []string
	sections = append(sections, renderLogo(w), "")

	if m.loadErr != nil {
		sections = append(sections,
			panel("Codex folder", fmt.Sprintf("Could not read Codex's folder: %v", m.loadErr), w),
			"",
			statusBar(m.footer(), "watching ~/.codex", w),
		)
		return strings.Join(sections, "\n")
	}

	// Left panel: the Codex folder + log sizes.
	var logs strings.Builder
	fmt.Fprintf(&logs, "%s\n", m.report.CodexDir)
	if !m.report.DirExists {
		fmt.Fprint(&logs, "(not found — Codex may not have run yet)")
	} else {
		for _, f := range m.report.Files {
			if f.Exists {
				fmt.Fprintf(&logs, "%-20s %10s\n", f.Name, codex.HumanBytes(f.Size))
			}
		}
		fmt.Fprintf(&logs, "%-20s %10s", "Total", codex.HumanBytes(m.report.TotalBytes))
	}

	// Right panel: risk + process + memory.
	var risk strings.Builder
	lvl := m.assessment.Level
	fmt.Fprintf(&risk, "%s %s\n", riskStyle(lvl).Render(riskGlyph(lvl)), riskStyle(lvl).Render(lvl.String()))
	if lvl >= monitor.RiskMedium {
		reason := ""
		if len(m.assessment.Reasons) > 0 {
			reason = " · " + m.assessment.Reasons[0]
		}
		fmt.Fprintf(&risk, "%.0f MB/min · WAL %s%s\n", m.assessment.RateMBPerMin, codex.HumanBytes(m.assessment.WALBytes), reason)
	}
	switch {
	case !m.supported:
		fmt.Fprint(&risk, "Codex: can't check")
	case m.running:
		fmt.Fprint(&risk, "Codex: running")
	default:
		fmt.Fprint(&risk, "Codex: not running")
	}
	if m.running && m.memBytes > 0 {
		fmt.Fprintf(&risk, "\nmemory: %s", codex.HumanBytes(m.memBytes))
	}

	// Compose the two panels: side by side when wide, stacked when narrow.
	const twoColMin = 72
	if w >= twoColMin {
		leftW := (w - 2) / 2
		rightW := w - 2 - leftW
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			panel("Codex folder", logs.String(), leftW), "  ", panel("Risk", risk.String(), rightW))
		sections = append(sections, row)
	} else {
		sections = append(sections, panel("Codex folder", logs.String(), w), panel("Risk", risk.String(), w))
	}

	// Recycling bin (full width).
	bin := "empty"
	if t, ok := m.lastTidy(); ok {
		bin = fmt.Sprintf("%d backup(s) · last tidy %s", len(m.backups), t.Format("2006-01-02 15:04"))
		if s, ok := m.soonestRelease(); ok {
			bin += fmt.Sprintf(" · next release %s", s.Format("2006-01-02"))
		}
	}
	sections = append(sections, panel("Recycling bin", bin, w))

	// Banner line (unchanged logic).
	switch m.bannerState() {
	case bannerActionable:
		sections = append(sections, headerStyle.Render(fmt.Sprintf("⚠  %s of Codex logs piled up — press c to tidy.", codex.HumanBytes(m.report.TotalBytes))))
	case bannerInformational:
		sections = append(sections, mutedTextStyle.Render(fmt.Sprintf("⚠  %s piling up — I'll offer to tidy when Codex is closed.", codex.HumanBytes(m.report.TotalBytes))))
	default:
		sections = append(sections, mutedTextStyle.Render("Nothing alarming right now."))
	}
	if m.releaseNote != "" {
		sections = append(sections, mutedTextStyle.Render(m.releaseNote))
	}

	sections = append(sections, "", statusBar(m.footer(), "watching ~/.codex · updates every 30s", w))
	return strings.Join(sections, "\n")
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
		fmt.Fprintf(&b, "%s%-18s %10s   releases %s\n", cursor, filepathBase(bk.Dir), codex.HumanBytes(total), bk.Manifest.HoldUntil.Format("2006-01-02"))
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
