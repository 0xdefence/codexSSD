package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
	"github.com/0xdefence/codexssd/internal/tool"
)

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
	case stateCleaning, stateRestoring:
		// workingLabel is set by whichever dispatch site (Codex or Claude) put
		// us in this state, so this single case covers both tools.
		return m.renderWorking(m.workingLabel)
	case stateRestoreList:
		return m.renderRestoreList()
	case stateConfirmRestore:
		return m.renderConfirmRestore()
	case stateResult:
		return m.renderResult()
	case stateBlocked:
		return m.renderBlocked()
	case stateInfo:
		return m.renderInfo()
	case stateClaude:
		return m.renderClaude()
	case stateClaudeConfirmClean:
		return m.renderClaudeConfirmClean()
	case stateClaudeRestoreList:
		return m.renderClaudeRestoreList()
	case stateClaudeConfirmRestore:
		return m.renderClaudeConfirmRestore()
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
			reason = " · " + strings.Join(m.assessment.Reasons, " · ")
		}
		fmt.Fprintf(&risk, "%.0f MB/min · WAL %s%s\n", m.assessment.RateMBPerMin, codex.HumanBytes(m.assessment.WALBytes), reason)
	}
	if !m.startedAt.IsZero() {
		fmt.Fprintf(&risk, "session peak: %s · %.0f MB/min\n", m.peakRisk.String(), m.peakRate)
	}
	switch {
	case !m.supported:
		fmt.Fprint(&risk, "Codex: can't check")
	case m.running && len(m.processes) > 0:
		fmt.Fprint(&risk, formatRunningProcesses("Codex", m.processes))
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

	// Claude Code (full width): dir + cleanable summary + running state, same
	// PID-inclusive style as the Codex Risk panel above.
	sections = append(sections, panel("Claude Code", m.claudePanelLine(), w))

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

	sections = append(sections, "", statusBar(m.footer(), "watching ~/.codex · updates every "+friendlyInterval(m.cfg.PollInterval()), w))
	return strings.Join(sections, "\n")
}

// formatRunningProcesses renders a "<label>: running" line with PID detail, or
// just "running (...)" with no prefix when label is empty (for panels whose
// title already names the tool, so repeating it inline would be redundant).
// Callers must only invoke this when len(procs) > 0 — a single process reads
// "Codex: running (PID 1234)" / "running (PID 1234)"; multiple read
// "Codex: running (2 processes, PIDs 1234, 5678)" / "running (2 processes,
// PIDs 1234, 5678)".
func formatRunningProcesses(label string, procs []tool.Process) string {
	prefix := ""
	if label != "" {
		prefix = label + ": "
	}
	if len(procs) == 1 {
		return fmt.Sprintf("%srunning (PID %d)", prefix, procs[0].PID)
	}
	pids := make([]string, len(procs))
	for i, p := range procs {
		pids[i] = strconv.Itoa(p.PID)
	}
	return fmt.Sprintf("%srunning (%d processes, PIDs %s)", prefix, len(procs), strings.Join(pids, ", "))
}

// claudePanelLine renders the dashboard's compact, full-width Claude Code
// summary: directory, cleanable summary, and running state — mirroring round
// 2's PID-inclusive style already established for the Codex Risk panel above.
func (m Model) claudePanelLine() string {
	if m.claudeLoadErr != nil {
		return fmt.Sprintf("Could not read Claude Code's folder: %v", m.claudeLoadErr)
	}
	if m.claudeDir == "" {
		return "loading…"
	}
	var total int64
	for _, f := range m.claudeCleanable {
		total += f.Size
	}
	line := fmt.Sprintf("%s · %d stale file(s), %s cleanable", m.claudeDir, len(m.claudeCleanable), codex.HumanBytes(total))
	switch {
	case !m.claudeSupported:
		line += " · can't check"
	case m.claudeRunning && len(m.claudeProcesses) > 0:
		line += " · " + formatRunningProcesses("", m.claudeProcesses)
	case m.claudeRunning:
		line += " · running"
	default:
		line += " · not running"
	}
	return line
}

func (m Model) footer() string {
	return "c tidy · r restore · i info · l claude · ? help · q quit"
}

// friendlyInterval formats a poll interval the way a non-technical user reads
// it: whole seconds under a minute as "30s", whole minutes as "1m", otherwise
// falling back to Duration's own String() (e.g. "1m30s").
func friendlyInterval(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d/time.Second))
	case d%time.Minute == 0:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return d.String()
	}
}

// screen frames a secondary view: compact logo header, an accent-titled card,
// and the shared status bar. keys is the status bar's left content.
func (m Model) screen(title, body, keys string) string {
	w := effectiveWidth(m)
	return strings.Join([]string{
		renderCompactLogo(w),
		"",
		panel(title, body, w),
		"",
		statusBar(keys, "watching ~/.codex", w),
	}, "\n")
}

func (m Model) renderConfirmClean() string {
	body := fmt.Sprintf("Move %s of Codex's own logs into a recoverable bin?\nNothing is deleted — you can restore them any time.",
		codex.HumanBytes(m.report.TotalBytes))
	return m.screen("Tidy Codex logs", body, "y yes · n no")
}

func (m Model) renderWorking(label string) string {
	return m.screen("CodexSSD", label, "please wait…")
}

func (m Model) renderResult() string {
	body := m.resultMsg
	if m.resultErr != nil {
		body = fmt.Sprintf("Something went wrong: %v", m.resultErr)
	}
	return m.screen("CodexSSD", body, "enter return to dashboard")
}

func (m Model) renderBlocked() string {
	return m.screen("Can't do that right now", m.blockedReason, "enter return to dashboard")
}

func (m Model) renderConfirmRestore() string {
	bk := m.backups[m.selected]
	id := filepathBase(bk.Dir)
	var body strings.Builder
	fmt.Fprintf(&body, "Move the logs in backup %s back to your Codex folder?", id)
	for i, it := range bk.Manifest.Items {
		if i == 0 {
			body.WriteString("\n\n")
		} else {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "%-18s %10s  → %s", it.Name, codex.HumanBytes(it.Size), it.OriginalPath)
	}
	return m.screen("Restore backup", body.String(), "y yes · n no")
}

func (m Model) renderRestoreList() string {
	var body strings.Builder
	for i, bk := range m.backups {
		var total int64
		for _, it := range bk.Manifest.Items {
			total += it.Size
		}
		row := fmt.Sprintf("%-18s %10s   releases %s", filepathBase(bk.Dir), codex.HumanBytes(total), bk.Manifest.HoldUntil.Format("2006-01-02"))
		if i == m.selected {
			row = selectedRowStyle.Render(row)
		}
		if i > 0 {
			body.WriteString("\n")
		}
		body.WriteString(row)
	}
	return m.screen("Restore a backup", body.String(), "↑/↓ choose · enter select · esc back")
}

// renderClaude shows the Claude Code screen (entered via the l key): the
// cleanable-file listing and the recycling-bin summary, stacked full-width —
// matching the Info screen's stacked-panel style, not the dashboard's
// side-by-side treatment. Data is fetched lazily by loadClaudeCmd when the
// screen is entered; until it arrives, this shows "loading…" (same pattern as
// renderInfo, gated on claudeLoaded instead of infoLoaded).
func (m Model) renderClaude() string {
	w := effectiveWidth(m)
	if !m.claudeLoaded {
		return strings.Join([]string{
			renderCompactLogo(w),
			"",
			panel("Claude Code", "loading…", w),
			"",
			statusBar("esc back", "watching ~/.codex", w),
		}, "\n")
	}

	var cleanable strings.Builder
	fmt.Fprintf(&cleanable, "%s\n", m.claudeDir)
	if len(m.claudeCleanable) == 0 {
		fmt.Fprint(&cleanable, "Nothing stale to report right now — no cleanable Claude Code files were found.")
	} else {
		var total int64
		for _, f := range m.claudeCleanable {
			fmt.Fprintf(&cleanable, "%-40s %10s\n", f.Rel, codex.HumanBytes(f.Size))
			total += f.Size
		}
		fmt.Fprintf(&cleanable, "%-40s %10s\n", "Total", codex.HumanBytes(total))
	}
	fmt.Fprint(&cleanable, "\nFresh Claude Code session files aren't listed here on purpose — they're still in use.")

	bin := "empty"
	if t, ok := m.claudeLastTidy(); ok {
		bin = fmt.Sprintf("%d backup(s) · last tidy %s", len(m.claudeBackups), t.Format("2006-01-02 15:04"))
		if s, ok := m.claudeSoonestRelease(); ok {
			bin += fmt.Sprintf(" · next release %s", s.Format("2006-01-02"))
		}
	}

	sections := []string{
		renderCompactLogo(w), "",
		panel("Claude Code", cleanable.String(), w), "",
		panel("Recycling bin", bin, w), "",
		statusBar("c tidy · r restore · esc back", "watching ~/.codex", w),
	}
	return strings.Join(sections, "\n")
}

// renderClaudeConfirmClean mirrors renderConfirmClean for Claude Code.
func (m Model) renderClaudeConfirmClean() string {
	body := fmt.Sprintf("Move %s of Claude Code's stale session files into a recoverable bin?\nNothing is deleted — you can restore them any time.",
		codex.HumanBytes(m.claudePlan.TotalBytes))
	return m.screen("Tidy Claude Code files", body, "y yes · n no")
}

// renderClaudeRestoreList mirrors renderRestoreList for Claude Code's own backups.
func (m Model) renderClaudeRestoreList() string {
	var body strings.Builder
	for i, bk := range m.claudeBackups {
		var total int64
		for _, it := range bk.Manifest.Items {
			total += it.Size
		}
		row := fmt.Sprintf("%-18s %10s   releases %s", filepathBase(bk.Dir), codex.HumanBytes(total), bk.Manifest.HoldUntil.Format("2006-01-02"))
		if i == m.selected {
			row = selectedRowStyle.Render(row)
		}
		if i > 0 {
			body.WriteString("\n")
		}
		body.WriteString(row)
	}
	return m.screen("Restore a Claude Code backup", body.String(), "↑/↓ choose · enter select · esc back")
}

// renderClaudeConfirmRestore mirrors renderConfirmRestore for Claude Code.
func (m Model) renderClaudeConfirmRestore() string {
	bk := m.claudeBackups[m.selected]
	id := filepathBase(bk.Dir)
	var body strings.Builder
	fmt.Fprintf(&body, "Move the files in backup %s back to your Claude Code folder?", id)
	for i, it := range bk.Manifest.Items {
		if i == 0 {
			body.WriteString("\n\n")
		} else {
			body.WriteString("\n")
		}
		fmt.Fprintf(&body, "%-18s %10s  → %s", it.Name, codex.HumanBytes(it.Size), it.OriginalPath)
	}
	return m.screen("Restore backup", body.String(), "y yes · n no")
}

func (m Model) renderHelp() string {
	body := strings.Join([]string{
		"c    tidy Codex's logs aside (recoverable)",
		"r    restore previously tidied logs",
		"i    open the info screen (settings, self-footprint, disk report)",
		"l    open the Claude Code screen (tidy/restore its own stale files)",
		"?    toggle this help",
		"q    quit",
	}, "\n")
	return m.screen("CodexSSD — help", body, "? or esc to close")
}

// renderInfo shows the read-only Info screen: configured settings, CodexSSD's
// own footprint, and a full ~/.codex disk breakdown. Data is fetched lazily by
// infoCmd when the screen is entered; until it arrives, this shows "loading…"
// (same pattern as renderWorking).
func (m Model) renderInfo() string {
	w := effectiveWidth(m)
	if !m.infoLoaded {
		return strings.Join([]string{
			renderCompactLogo(w),
			"",
			panel("CodexSSD — info", "loading…", w),
			"",
			statusBar("esc back", "watching ~/.codex", w),
		}, "\n")
	}

	var settings strings.Builder
	fmt.Fprintf(&settings, "risk thresholds (MB/min)   medium %.0f · high %.0f · critical %.0f\n",
		m.cfg.MediumMBPerMin, m.cfg.HighMBPerMin, m.cfg.CriticalMBPerMin)
	fmt.Fprintf(&settings, "WAL size thresholds (MiB)  high %d · critical %d\n",
		m.cfg.HighWALSizeMB, m.cfg.CriticalWALSizeMB)
	fmt.Fprintf(&settings, "memory thresholds (MiB)    high %d · critical %d\n",
		m.cfg.HighMemMB, m.cfg.CriticalMemMB)
	fmt.Fprintf(&settings, "poll interval (seconds)    %d\n", m.cfg.PollIntervalSeconds)
	fmt.Fprintf(&settings, "recycling-bin hold (days)  %d\n", m.cfg.BinHoldDays)
	fmt.Fprintf(&settings, "stale after (days)         %d\n", m.cfg.StaleAfterDays)
	fmt.Fprintf(&settings, "notifications              %s", onOff(m.cfg.Notifications))

	var foot strings.Builder
	if m.selfErr != nil {
		fmt.Fprintf(&foot, "Could not measure CodexSSD's own footprint: %v", m.selfErr)
	} else {
		fmt.Fprintf(&foot, "mode          %s\n", m.selfReport.Mode)
		fmt.Fprintf(&foot, "state dir     %s\n", m.selfReport.StateDir)
		fmt.Fprintf(&foot, "history size  %s\n", codex.HumanBytes(m.selfReport.HistoryBytes))
		fmt.Fprintf(&foot, "records       %d\n", m.selfReport.Records)
		lastAction := m.selfReport.LastAction
		if lastAction == "" {
			lastAction = "(none yet)"
		}
		fmt.Fprintf(&foot, "last action   %s", lastAction)
	}

	var disk strings.Builder
	switch {
	case !m.diskReport.DirExists:
		fmt.Fprint(&disk, "~/.codex was not found — Codex may not have run yet.")
	case len(m.diskReport.Entries) == 0:
		fmt.Fprint(&disk, "(empty)")
	default:
		for i, e := range m.diskReport.Entries {
			if i > 0 {
				disk.WriteString("\n")
			}
			flags := ""
			if e.Stale {
				flags += " STALE"
			}
			if e.ReadError != "" {
				flags += " ⚠"
			}
			fmt.Fprintf(&disk, "%-24s %10s  %4d files  %s%s",
				e.Name, codex.HumanBytes(e.TotalBytes), e.FileCount, e.NewestMod.Format("2006-01-02"), flags)
		}
		fmt.Fprintf(&disk, "\nTotal %s", codex.HumanBytes(m.diskReport.TotalBytes))
	}

	sections := []string{
		renderCompactLogo(w), "",
		panel("Settings", settings.String(), w), "",
		panel("CodexSSD's own footprint", foot.String(), w), "",
		panel("Disk report (~/.codex)", disk.String(), w), "",
		statusBar("esc back", "watching ~/.codex", w),
	}
	return strings.Join(sections, "\n")
}

// onOff renders a bool as the plain-language settings words a non-technical
// user reads, rather than "true"/"false".
func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}
