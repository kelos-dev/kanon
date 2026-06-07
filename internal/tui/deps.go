package tui

import "github.com/kelos-dev/kanon/internal/core"

type SelectedChange struct {
	Path         string
	Action       string
	ExistingHash string
	DesiredHash  string
}

// Deps is everything the TUI needs from the cli layer: data captured at launch
// plus callbacks that run the existing core operations. Passing closures keeps
// the model free of cobra and the cli options struct, so it can be unit-tested
// with stub dependencies.
type Deps struct {
	Home     string
	UserHome string
	Agent    string // "all", "codex", or "claude"
	Project  string
	DryRun   bool

	// Plan renders the source and plans it against the destination.
	Plan func() (*core.ApplyPlan, error)

	// ApplySelected validates the currently selected paths against a fresh plan
	// before writing them. dryRun returns the validated count without writing.
	ApplySelected func(selected map[string]SelectedChange, dryRun bool) (int, error)

	// GitStatus returns `git status --short` output for the source repo.
	GitStatus func() ([]byte, error)

	// Git runs a git subcommand in the source repo, returning combined output.
	Git func(args ...string) (string, error)
}
