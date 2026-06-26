package tui

import tea "github.com/charmbracelet/bubbletea"

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
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a keypress. Global keys first, then per-state keys (added in
// later tasks).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.showHelp = !m.showHelp
		return m, nil
	}
	return m, nil
}
