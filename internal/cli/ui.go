package cli

import (
	"bytes"
	"errors"
	"io"
	"os"

	"github.com/kelos-dev/kanon/internal/core"
	"github.com/kelos-dev/kanon/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func uiCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Launch the interactive full-screen TUI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.runUI(cmd, opts.uiMode)
		},
	}
	cmd.Flags().BoolVarP(&opts.dryRun, "dry-run", "n", false, "start in dry-run mode (preview applies without writing)")
	cmd.Flags().StringVar(&opts.uiMode, "mode", string(tui.ModeApply), "initial UI mode: apply or import")
	cmd.Flags().StringVar(&opts.secretPolicy, "secret-policy", string(core.SecretPolicyKeep), "secret handling during import mode: keep")
	cmd.Flags().StringVar(&opts.instructionPolicy, "instruction-policy", string(core.InstructionPolicyAuto), "instruction handling during import mode: auto, codex, claude, merge, or skip")
	return cmd
}

func (opts *options) runUI(cmd *cobra.Command, modeValue string) error {
	out, ok := cmd.OutOrStdout().(*os.File)
	if !ok || !term.IsTerminal(int(out.Fd())) {
		return errors.New("kanon ui requires an interactive terminal")
	}
	mode, err := parseUIMode(modeValue)
	if err != nil {
		return err
	}
	home, err := opts.resolvedHome()
	if err != nil {
		return err
	}
	target, err := opts.targetOptions()
	if err != nil {
		return err
	}
	importOptions := func() core.ImportOptions {
		return core.ImportOptions{
			TargetOptions:     target,
			SecretPolicy:      core.SecretPolicy(opts.secretPolicy),
			InstructionPolicy: core.InstructionPolicy(opts.instructionPolicy),
		}
	}
	deps := tui.Deps{
		Home:     home,
		UserHome: target.UserHome,
		Agent:    target.Agent,
		Project:  target.Project,
		DryRun:   opts.dryRun,
		Mode:     mode,
		Plan:     func() (*core.ApplyPlan, error) { return opts.plan() },
		ApplySelected: func(selected map[string]tui.SelectedChange, dryRun bool) (int, error) {
			plan, err := opts.plan()
			if err != nil {
				return 0, err
			}
			filtered, err := selectedPlan(plan, selected)
			if err != nil {
				return 0, err
			}
			if dryRun {
				return len(filtered.Changes), nil
			}
			if err := core.ApplyFiles(filtered, core.ApplyOptions{
				KanonHome: home,
				UserHome:  target.UserHome,
				Agent:     target.Agent,
				Project:   target.Project,
			}); err != nil {
				return 0, err
			}
			return len(filtered.Changes), nil
		},
		ImportPlan: func() (*core.ApplyPlan, error) {
			plan, err := core.PlanImport(importOptions())
			if err != nil {
				return nil, err
			}
			return plan.ApplyPlan, nil
		},
		ImportSelected: func(selected map[string]tui.SelectedChange, dryRun bool) (int, error) {
			plan, err := core.PlanImport(importOptions())
			if err != nil {
				return 0, err
			}
			filtered, err := selectedPlan(plan.ApplyPlan, selected)
			if err != nil {
				return 0, err
			}
			if dryRun {
				return len(filtered.Changes), nil
			}
			if err := core.WriteSelectedImport(plan, selectedPathSet(filtered), home); err != nil {
				return 0, err
			}
			return len(filtered.Changes), nil
		},
		GitStatus: func() ([]byte, error) { return core.GitStatus(home) },
		Git: func(args ...string) (string, error) {
			var buf bytes.Buffer
			err := core.RunGit(home, &buf, &buf, args...)
			return buf.String(), err
		},
	}
	return tui.Run(deps)
}

func parseUIMode(value string) (tui.Mode, error) {
	switch tui.Mode(value) {
	case tui.ModeApply, "":
		return tui.ModeApply, nil
	case tui.ModeImport:
		return tui.ModeImport, nil
	default:
		return "", errors.New("unsupported UI mode " + value)
	}
}

func selectedPlan(plan *core.ApplyPlan, selected map[string]tui.SelectedChange) (*core.ApplyPlan, error) {
	filtered := &core.ApplyPlan{}
	seen := make(map[string]bool, len(selected))
	for _, conflict := range plan.Conflicts {
		if _, ok := selected[conflict.Path]; ok {
			seen[conflict.Path] = true
			filtered.Conflicts = append(filtered.Conflicts, conflict)
		}
	}
	if len(filtered.Conflicts) > 0 {
		return filtered, errors.New("selected file now has conflicts; reload before applying")
	}
	for _, change := range plan.Changes {
		expected, ok := selected[change.Path]
		if !ok {
			continue
		}
		seen[change.Path] = true
		if !sameReviewedChange(change, expected) {
			return filtered, errors.New("selected file changed since it was reviewed; reload before applying")
		}
		filtered.Changes = append(filtered.Changes, change)
	}
	for path := range selected {
		if !seen[path] {
			return filtered, errors.New("selected file no longer has a pending change; reload before applying")
		}
	}
	return filtered, nil
}

func sameReviewedChange(change core.FileChange, expected tui.SelectedChange) bool {
	return change.Action == expected.Action &&
		change.ExistingHash == expected.ExistingHash &&
		change.DesiredHash == expected.DesiredHash
}

func selectedPathSet(plan *core.ApplyPlan) map[string]bool {
	selected := make(map[string]bool, len(plan.Changes))
	for _, change := range plan.Changes {
		selected[change.Path] = true
	}
	return selected
}

// planDiff renders a plan's diff, colorized when the command's output is a
// terminal (matching git's color.ui=auto) and plain otherwise.
func planDiff(cmd *cobra.Command, plan *core.ApplyPlan) string {
	if useColor(cmd.OutOrStdout()) {
		return core.FormatPlanDiffColored(plan)
	}
	return core.FormatPlanDiff(plan)
}

// useColor reports whether diff output to w should be colorized: only when w is
// a terminal and NO_COLOR is unset (https://no-color.org).
func useColor(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
