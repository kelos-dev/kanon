package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up         key.Binding
	Down       key.Binding
	NextPane   key.Binding
	PrevPane   key.Binding
	Toggle     key.Binding
	SelectAll  key.Binding
	SelectNone key.Binding
	Diff       key.Binding
	Apply      key.Binding
	Reload     key.Binding
	Pull       key.Binding
	Push       key.Binding
	Filter     key.Binding
	DryRun     key.Binding
	Mode       key.Binding
	Help       key.Binding
	Quit       key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k")),
		Down:       key.NewBinding(key.WithKeys("down", "j")),
		NextPane:   key.NewBinding(key.WithKeys("tab")),
		PrevPane:   key.NewBinding(key.WithKeys("shift+tab")),
		Toggle:     key.NewBinding(key.WithKeys(" ", "x")),
		SelectAll:  key.NewBinding(key.WithKeys("A")),
		SelectNone: key.NewBinding(key.WithKeys("N")),
		Diff:       key.NewBinding(key.WithKeys("d")),
		Apply:      key.NewBinding(key.WithKeys("a")),
		Reload:     key.NewBinding(key.WithKeys("r")),
		Pull:       key.NewBinding(key.WithKeys("p")),
		Push:       key.NewBinding(key.WithKeys("P")),
		Filter:     key.NewBinding(key.WithKeys("/")),
		DryRun:     key.NewBinding(key.WithKeys("t")),
		Mode:       key.NewBinding(key.WithKeys("m")),
		Help:       key.NewBinding(key.WithKeys("?")),
		Quit:       key.NewBinding(key.WithKeys("q", "ctrl+c")),
	}
}
