package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/0xdefence/codexssd/internal/codex"
)

// Update implements tea.Model. It is a pure function over (Model, Msg) and is
// the testable core of the app.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case loadedMsg:
		m.report = msg.report
		m.running = msg.running
		m.supported = msg.supported
		m.runErr = msg.runErr
		m.loadErr = msg.loadErr
		m.plan = msg.plan
		m.backups = msg.backups
		return m, nil
	case cleanResultMsg:
		m.state = stateResult
		if msg.err != nil {
			m.resultErr = msg.err
			m.resultMsg = ""
		} else if msg.dest == "" {
			m.resultErr = nil
			m.resultMsg = "Nothing to tidy — no Codex logs are present."
		} else {
			m.resultErr = nil
			m.resultMsg = fmt.Sprintf("Tidied %s of Codex logs aside.\nBackup: %s\nNothing was deleted — restore any time.", codex.HumanBytes(msg.movedBytes), msg.dest)
		}
		return m, nil
	case blockedMsg:
		m.state = stateBlocked
		m.blockedReason = msg.reason
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keypress. Global keys first, then per-state keys.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
			m.state = stateCleaning
			return m, cleanCmd
		case "n", "esc":
			m.state = stateDashboard
			return m, nil
		}
	case stateResult, stateBlocked, stateError:
		switch msg.String() {
		case "enter", "esc":
			m.state = stateDashboard
			return m, loadCmd // refresh after returning
		}
	}
	return m, nil
}

// handleDashboardKey handles keys on the main screen.
func (m Model) handleDashboardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		// Refuse up-front if we already know Codex is running / unsupported.
		if !m.supported {
			m.state = stateBlocked
			m.blockedReason = "This platform can't verify Codex is closed, so tidying is disabled here."
			return m, nil
		}
		if m.running {
			m.state = stateBlocked
			m.blockedReason = "Codex appears to be running. Close it first, then try again."
			return m, nil
		}
		if m.plan.TotalBytes == 0 {
			m.state = stateResult
			m.resultMsg = "Nothing to tidy — no Codex logs are present."
			m.resultErr = nil
			return m, nil
		}
		m.state = stateConfirmClean
		return m, nil
	}
	return m, nil
}
