package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/codex"
	"github.com/0xdefence/codexssd/internal/monitor"
)

// Update implements tea.Model. It is a pure function over (Model, Msg) and is
// the testable core of the app.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case loadedMsg:
		m.report = msg.report
		m.running = msg.running
		m.supported = msg.supported
		m.runErr = msg.runErr
		m.loadErr = msg.loadErr
		m.plan = msg.plan
		m.backups = msg.backups
		m.memBytes = msg.memBytes
		m.processes = msg.processes
		// Capture the previously-displayed level before it's overwritten below —
		// this is the only place old and new assessments are both in scope, so
		// the escalation check (for the notification) happens right here.
		last := m.assessment.Level
		s := monitor.Sample{At: msg.at, TotalBytes: msg.report.TotalBytes, WALBytes: walBytes(msg.report), MemBytes: msg.memBytes}
		m.samples = monitor.AppendSample(m.samples, s, maxSamples)
		m.assessment = monitor.Evaluate(m.samples, m.running, m.cfg.MonitorThresholds())
		// Track session peaks for the receipt written on quit. startedAt is the
		// first successful load; peaks only ever ratchet up, so they survive a
		// later de-escalation.
		if m.startedAt.IsZero() && !msg.at.IsZero() {
			m.startedAt = msg.at
			m.startBytes = msg.report.TotalBytes
		}
		if m.assessment.RateMBPerMin > m.peakRate {
			m.peakRate = m.assessment.RateMBPerMin
		}
		if m.assessment.Level > m.peakRisk {
			m.peakRisk = m.assessment.Level
		}
		// Best-effort desktop notification on escalation into HIGH/CRITICAL,
		// gated on config (same field `watch --no-notify` respects). This is a
		// background side-effect only — no visual change on the dashboard itself.
		if m.cfg.Notifications && escalatedToAlarming(last, m.assessment.Level) {
			return m, notifyCmd(m.assessment)
		}
		return m, nil
	case tickMsg:
		// Re-check both tools' status and schedule the next tick. Does not touch m.state.
		return m, tea.Batch(loadCmd, loadClaudeCmd(m.cfg.StaleAfter()), tickCmd(m.cfg.PollInterval()))
	case loadedClaudeMsg:
		m.claudeLoaded = true
		m.claudeDir = msg.dir
		m.claudeLoadErr = msg.loadErr
		m.claudeRunning = msg.running
		m.claudeSupported = msg.supported
		m.claudeRunErr = msg.runErr
		m.claudeCleanable = msg.cleanable
		m.claudePlan = msg.plan
		m.claudeBackups = msg.backups
		m.claudeProcesses = msg.processes
		return m, nil
	case cleanResultMsg:
		m.state = stateResult
		// returnState carries which tool this clean was for, so the wording
		// matches even though cleanResultMsg itself is generic (dest/bytes/err
		// only). Defaults to the Codex wording (stateDashboard is the zero
		// value) for any dispatch site that doesn't explicitly set it.
		emptyMsg := "Nothing to tidy — no Codex logs are present."
		successFmt := "Tidied %s of Codex logs aside.\nBackup: %s\nNothing was deleted — restore any time."
		if m.returnState == stateClaude {
			emptyMsg = "Nothing to tidy — no stale Claude Code files are present."
			successFmt = "Tidied %s of Claude Code's stale files aside.\nBackup: %s\nNothing was deleted — restore any time."
		}
		if msg.err != nil {
			m.resultErr = msg.err
			m.resultMsg = ""
		} else if msg.dest == "" {
			m.resultErr = nil
			m.resultMsg = emptyMsg
		} else {
			m.resultErr = nil
			m.resultMsg = fmt.Sprintf(successFmt, codex.HumanBytes(msg.movedBytes), msg.dest)
		}
		return m, nil
	case restoreResultMsg:
		m.state = stateResult
		toolName := "Codex"
		if m.returnState == stateClaude {
			toolName = "Claude Code"
		}
		if msg.err != nil {
			m.resultErr = msg.err
			m.resultMsg = ""
		} else {
			m.resultErr = nil
			m.resultMsg = fmt.Sprintf("Restored backup %s to your %s folder.", msg.id, toolName)
		}
		return m, nil
	case releasedMsg:
		if len(msg.ids) > 0 {
			m.releaseNote = fmt.Sprintf("released %d backup(s)", len(msg.ids))
			if msg.trashDir != "" {
				m.releaseNote += " → " + shortenHome(msg.trashDir)
			}
		}
		return m, loadCmd // refresh the backups list after releasing
	case blockedMsg:
		m.state = stateBlocked
		m.blockedReason = msg.reason
		return m, nil
	case infoMsg:
		m.infoLoaded = true
		m.selfReport = msg.self
		m.selfErr = msg.selfErr
		m.diskReport = msg.disk
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keypress. Global keys first, then per-state keys.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Close help overlay with esc before any other routing.
	if m.showHelp && msg.String() == "esc" {
		m.showHelp = false
		return m, nil
	}

	// Global keys.
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}

	switch m.state {
	case stateDashboard:
		return m.handleDashboardKey(msg)
	case stateConfirmClean:
		switch msg.String() {
		case "y":
			// Existing Codex dispatch site: returnState/workingLabel set
			// explicitly to reproduce today's behavior byte-for-byte now that
			// both fields are threaded through the shared working/result screens.
			m.returnState = stateDashboard
			m.workingLabel = "Tidying Codex logs aside…"
			m.state = stateCleaning
			return m, cleanCmd(m.cfg.BinHold())
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateRestoreList:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.backups)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			m.state = stateConfirmRestore
			return m, nil
		case "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateConfirmRestore:
		switch msg.String() {
		case "y":
			// Existing Codex dispatch site: same byte-for-byte reproduction as
			// stateConfirmClean's "y" case above.
			m.returnState = stateDashboard
			m.workingLabel = "Restoring…"
			m.state = stateRestoring
			return m, restoreCmd(m.backups[m.selected].Dir)
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateResult, stateBlocked, stateError:
		switch msg.String() {
		case "enter", "esc":
			// returnState defaults to stateDashboard (the zero value) for any
			// path that never set it, so this reproduces the old unconditional
			// "back to stateDashboard + loadCmd" behavior exactly for Codex.
			m.state = m.returnState
			if m.returnState == stateClaude {
				return m, loadClaudeCmd(m.cfg.StaleAfter())
			}
			return m, loadCmd // refresh after returning
		}
	case stateInfo:
		switch msg.String() {
		case "esc":
			m.state = stateDashboard
			return m, loadCmd // refresh after returning
		}
	case stateClaude:
		return m.handleClaudeKey(msg)
	case stateClaudeConfirmClean:
		switch msg.String() {
		case "y":
			// Authoritative gate: claudeCleanCmd re-checks isClaudeRunning()
			// itself right before acting, never trusting the state captured
			// when the Claude screen was entered.
			m.returnState = stateClaude
			m.workingLabel = "Tidying Claude Code's stale files aside…"
			m.state = stateCleaning
			return m, claudeCleanCmd(m.cfg.BinHold(), m.cfg.StaleAfter())
		case "n", "esc":
			m.state = stateClaude
			return m, nil
		}
	case stateClaudeRestoreList:
		switch msg.String() {
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.claudeBackups)-1 {
				m.selected++
			}
			return m, nil
		case "enter":
			m.state = stateClaudeConfirmRestore
			return m, nil
		case "esc":
			m.state = stateClaude
			return m, nil
		}
	case stateClaudeConfirmRestore:
		switch msg.String() {
		case "y":
			// Authoritative gate: claudeRestoreCmd re-checks isClaudeRunning()
			// itself right before acting — same pattern as the tidy flow above.
			m.returnState = stateClaude
			m.workingLabel = "Restoring…"
			m.state = stateRestoring
			return m, claudeRestoreCmd(m.claudeBackups[m.selected].Dir)
		case "n", "esc":
			m.state = stateClaude
			return m, nil
		}
	}
	return m, nil
}

// handleClaudeKey handles keys on the Claude screen (entered via the l key).
func (m Model) handleClaudeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		// Refuse up-front if we already know Claude Code is running / unsupported.
		if !m.claudeSupported {
			m.returnState = stateClaude
			m.state = stateBlocked
			m.blockedReason = "This platform can't verify Claude Code is closed, so tidying is disabled here."
			return m, nil
		}
		if m.claudeRunning {
			m.returnState = stateClaude
			m.state = stateBlocked
			m.blockedReason = "Claude Code appears to be running. Close it first, then try again."
			return m, nil
		}
		if m.claudePlan.Empty() {
			m.returnState = stateClaude
			m.state = stateResult
			m.resultMsg = "Nothing to tidy — no stale Claude Code files are present."
			m.resultErr = nil
			return m, nil
		}
		m.state = stateClaudeConfirmClean
		return m, nil
	case "r":
		if len(m.claudeBackups) == 0 {
			m.returnState = stateClaude
			m.state = stateResult
			m.resultMsg = "No Claude Code backups to restore — nothing has been tidied yet."
			m.resultErr = nil
			return m, nil
		}
		m.selected = 0
		m.state = stateClaudeRestoreList
		return m, nil
	case "esc":
		// This screen never mutates anything itself, so there is nothing to
		// guard on the way out — esc always returns to the dashboard.
		m.state = stateDashboard
		return m, nil
	}
	return m, nil
}

// handleDashboardKey handles keys on the main screen.
func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		// Refuse up-front if we already know Codex is running / unsupported.
		// returnState is set explicitly here (even though stateDashboard is its
		// zero value) so this reproduces today's behavior byte-for-byte now
		// that the shared result/blocked screens read returnState.
		if !m.supported {
			m.returnState = stateDashboard
			m.state = stateBlocked
			m.blockedReason = "This platform can't verify Codex is closed, so tidying is disabled here."
			return m, nil
		}
		if m.running {
			m.returnState = stateDashboard
			m.state = stateBlocked
			m.blockedReason = "Codex appears to be running. Close it first, then try again."
			return m, nil
		}
		if m.plan.Empty() {
			m.returnState = stateDashboard
			m.state = stateResult
			m.resultMsg = "Nothing to tidy — no Codex logs are present."
			m.resultErr = nil
			return m, nil
		}
		m.state = stateConfirmClean
		return m, nil
	case "r":
		if len(m.backups) == 0 {
			m.returnState = stateDashboard
			m.state = stateResult
			m.resultMsg = "No backups to restore — nothing has been tidied yet."
			m.resultErr = nil
			return m, nil
		}
		m.selected = 0
		m.state = stateRestoreList
		return m, nil
	case "i":
		m.state = stateInfo
		m.infoLoaded = false
		return m, infoCmd(m.cfg.StaleAfter())
	case "l":
		m.state = stateClaude
		m.claudeLoaded = false
		return m, loadClaudeCmd(m.cfg.StaleAfter())
	}
	return m, nil
}
