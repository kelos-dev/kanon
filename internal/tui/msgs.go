package tui

import "github.com/kelos-dev/kanon/internal/core"

// planLoadedMsg carries a freshly rendered plan.
type planLoadedMsg struct {
	plan *core.ApplyPlan
}

// gitStatusMsg carries the rendered source git status ("clean", "unavailable",
// or short status text).
type gitStatusMsg struct{ status string }

// appliedMsg reports a successful apply of count file changes.
type appliedMsg struct{ count int }

// dryRunCheckedMsg reports how many selected changes still validate after a
// fresh plan.
type dryRunCheckedMsg struct{ count int }

// gitDoneMsg reports a finished git subcommand (pull/push) and its output.
type gitDoneMsg struct {
	label  string
	output string
}

// errMsg wraps an error from any async command.
type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }
