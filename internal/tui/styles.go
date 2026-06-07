package tui

import "github.com/charmbracelet/lipgloss"

// Styles is the Lipgloss palette for the TUI. Colors use ANSI indices so they
// adapt to the user's terminal theme.
type Styles struct {
	PanelFocused lipgloss.Style
	PanelBlurred lipgloss.Style
	PanelTitle   lipgloss.Style

	AgentActive   lipgloss.Style
	AgentInactive lipgloss.Style

	StatusUpdate lipgloss.Style
	StatusCreate lipgloss.Style
	StatusDelete lipgloss.Style
	StatusBang   lipgloss.Style
	CursorRow    lipgloss.Style

	DiffFileHeader lipgloss.Style
	DiffHunkHeader lipgloss.Style
	DiffAdd        lipgloss.Style
	DiffDelete     lipgloss.Style
	DiffContext    lipgloss.Style
	DiffMeta       lipgloss.Style

	HintBar    lipgloss.Style
	StatusLine lipgloss.Style
	ErrorText  lipgloss.Style
	OkText     lipgloss.Style
	Badge      lipgloss.Style
}

const (
	colRed     = lipgloss.Color("1")
	colGreen   = lipgloss.Color("2")
	colYellow  = lipgloss.Color("3")
	colCyan    = lipgloss.Color("6")
	colGray    = lipgloss.Color("8")
	colBrightR = lipgloss.Color("9")
)

func DefaultStyles() Styles {
	return Styles{
		PanelFocused: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colCyan),
		PanelBlurred: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colGray),
		PanelTitle:   lipgloss.NewStyle().Bold(true).Foreground(colCyan),

		AgentActive:   lipgloss.NewStyle().Bold(true),
		AgentInactive: lipgloss.NewStyle().Faint(true),

		StatusUpdate: lipgloss.NewStyle().Foreground(colYellow).Bold(true),
		StatusCreate: lipgloss.NewStyle().Foreground(colGreen).Bold(true),
		StatusDelete: lipgloss.NewStyle().Foreground(colRed).Bold(true),
		StatusBang:   lipgloss.NewStyle().Foreground(colBrightR).Bold(true),
		CursorRow:    lipgloss.NewStyle().Bold(true),

		DiffFileHeader: lipgloss.NewStyle().Bold(true),
		DiffHunkHeader: lipgloss.NewStyle().Foreground(colCyan),
		DiffAdd:        lipgloss.NewStyle().Foreground(colGreen),
		DiffDelete:     lipgloss.NewStyle().Foreground(colRed),
		DiffContext:    lipgloss.NewStyle().Faint(true),
		DiffMeta:       lipgloss.NewStyle().Faint(true).Italic(true),

		HintBar:    lipgloss.NewStyle().Faint(true),
		StatusLine: lipgloss.NewStyle(),
		ErrorText:  lipgloss.NewStyle().Foreground(colRed).Bold(true),
		OkText:     lipgloss.NewStyle().Foreground(colGreen),
		Badge:      lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colYellow).Bold(true),
	}
}
