package tui

import tea "github.com/charmbracelet/bubbletea"

// Run launches the TUI on the alternate screen and blocks until the user quits.
func Run(deps Deps) error {
	p := tea.NewProgram(New(deps), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
