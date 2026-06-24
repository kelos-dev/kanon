package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kelos-dev/kanon/internal/core"
	"github.com/spf13/cobra"
)

type options struct {
	home              string
	configPath        string
	project           string
	agent             string
	yes               bool
	adopt             bool
	dryRun            bool
	force             bool
	write             bool
	ui                bool
	uiMode            string
	gitInit           bool
	secretPolicy      string
	instructionPolicy string
}

func NewRootCommand() *cobra.Command {
	opts := &options{agent: core.AgentAll, gitInit: true}
	cmd := &cobra.Command{
		Use:   "kanon",
		Short: "Manage coding-agent settings from a shared Kanon repository",
		Long: `Kanon compiles one neutral settings spec into the native files that each
coding agent expects, and keeps those files in sync across machines.

It works in three states, mirroring chezmoi:

  source state       kanon.yaml plus instructions/ skills/ hooks/ — the single
                     source of truth, tracked in git
  target state       the agent-native files compiled from the source by the
                     per-agent adapters (codex, claude, gemini)
  destination state  the real files on this machine (AGENTS.md, CLAUDE.md,
                     ~/.codex, ~/.claude, and project directories)

Commands move data between these states: render (source to target), diff and
apply (target to destination), import (destination back to source), lock git
skill provider pins, and pull/push/update to sync the source with a remote.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.home, "home", "", "Kanon source repository path (defaults to KANON_HOME or ~/.config/kanon)")
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "config file path (defaults to <home>/kanon.yaml)")
	cmd.PersistentFlags().StringVar(&opts.project, "project", "", "render project-scoped agent settings into this repository")
	cmd.PersistentFlags().StringVar(&opts.agent, "agent", core.AgentAll, "agent to manage: all, codex, claude, or gemini")

	cmd.AddCommand(initCommand(opts))
	cmd.AddCommand(validateCommand(opts))
	cmd.AddCommand(renderCommand(opts))
	cmd.AddCommand(diffCommand(opts))
	cmd.AddCommand(applyCommand(opts))
	cmd.AddCommand(statusCommand(opts))
	cmd.AddCommand(lockCommand(opts))
	cmd.AddCommand(sourceCommand(opts))
	cmd.AddCommand(importCommand(opts))
	cmd.AddCommand(updateCommand(opts))
	cmd.AddCommand(uiCommand(opts))
	cmd.AddCommand(gitCommand(opts, "pull", "pull", []string{"pull", "--ff-only"}))
	cmd.AddCommand(gitCommand(opts, "push", "push", []string{"push"}))
	return cmd
}

func initCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [repo]",
		Short: "Create a new Kanon source repository, or clone one from a remote",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
			}
			if len(args) == 1 {
				if err := core.CloneHome(args[0], home); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Cloned %s into %s\n", args[0], home)
				return nil
			}
			if err := core.InitHome(core.InitOptions{Home: home, Force: opts.force, Git: opts.gitInit}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Initialized %s\n", home)
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite existing starter files")
	cmd.Flags().BoolVar(&opts.gitInit, "git", true, "run git init in the Kanon home")
	return cmd
}

func validateCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate the source state (kanon.yaml and referenced assets)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, home, err := opts.loadConfig()
			if err != nil {
				return err
			}
			if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
				return err
			}
			lock, _, err := core.LoadSourceLock(home)
			if err != nil {
				return err
			}
			for _, warning := range core.SourceLockWarnings(cfg, lock) {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "kanon.yaml is valid.")
			return nil
		},
	}
}

func renderCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "render",
		Short: "Render the target state (agent-native files) from the source state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, _, err := opts.render()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), core.FormatRender(files))
			return nil
		},
	}
}

func diffCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Diff the target state against files on disk (destination)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := opts.plan()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), planDiff(cmd, plan))
			return nil
		},
	}
}

func applyCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the target state to disk (destination)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return opts.runApply(cmd, !opts.yes)
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "apply without prompting")
	cmd.Flags().BoolVar(&opts.adopt, "adopt", false, "deprecated; has no effect")
	cmd.Flags().BoolVarP(&opts.dryRun, "dry-run", "n", false, "show what apply would change without writing")
	return cmd
}

func statusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show source git status and destination drift",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
			}
			if status, err := core.GitStatus(home); err == nil {
				if len(status) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Git status: clean")
				} else {
					fmt.Fprint(cmd.OutOrStdout(), string(status))
				}
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "Git status unavailable: %v\n", err)
			}
			plan, err := opts.plan()
			if err != nil {
				if isMissingConfigError(home, opts.configPath, err) {
					return nil
				}
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), core.DriftSummary(plan))
			return nil
		},
	}
}

func lockCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Create or repair kanon.lock git skill provider pins",
		Args:  cobra.NoArgs,
		RunE:  runLockCommand(opts),
	}
	cmd.AddCommand(lockCheckCommand(opts))
	cmd.AddCommand(lockUpdateCommand(opts))
	return cmd
}

func sourceCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "source",
		Short:  "Manage remote source locks",
		Hidden: true,
	}
	cmd.AddCommand(sourceLockCommand(opts))
	cmd.AddCommand(sourceCheckCommand(opts))
	cmd.AddCommand(sourceUpdateCommand(opts))
	return cmd
}

func runLockCommand(opts *options) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cfg, home, err := opts.loadConfig()
		if err != nil {
			return err
		}
		if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
			return err
		}
		lock, err := core.LockRemoteSkillSources(cfg, home)
		if err != nil {
			return err
		}
		path, err := core.WriteSourceLock(home, lock)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Locked %d git skill provider(s) in %s\n", len(lock.Sources), path)
		return nil
	}
}

func sourceLockCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "lock",
		Short: "Resolve git skill providers and write kanon.lock",
		Args:  cobra.NoArgs,
		RunE:  runLockCommand(opts),
	}
}

func lockCheckCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "check",
		Short: "Verify git skill providers against kanon.lock",
		Args:  cobra.NoArgs,
		RunE:  runLockCheckCommand(opts, func() bool { return true }),
	}
}

func sourceCheckCommand(opts *options) *cobra.Command {
	var locked bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify git skill providers against kanon.lock",
		Args:  cobra.NoArgs,
		RunE:  runLockCheckCommand(opts, func() bool { return locked }),
	}
	cmd.Flags().BoolVar(&locked, "locked", false, "require every git skill provider to match kanon.lock")
	return cmd
}

func runLockCheckCommand(opts *options, requireLocked func() bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cfg, home, err := opts.loadConfig()
		if err != nil {
			return err
		}
		if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
			return err
		}
		if err := validationError(core.CheckRemoteSkillSources(cfg, home, requireLocked())); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "kanon.lock is valid.")
		return nil
	}
}

func lockUpdateCommand(opts *options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [provider-name ...]",
		Short: "Re-resolve git skill providers and update kanon.lock",
		RunE:  runLockUpdateCommand(opts, &all, "lock update"),
	}
	cmd.Flags().BoolVar(&all, "all", false, "update every enabled git skill provider")
	return cmd
}

func sourceUpdateCommand(opts *options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "update [provider-name ...]",
		Short: "Re-resolve git skill providers and update kanon.lock",
		RunE:  runLockUpdateCommand(opts, &all, "source update"),
	}
	cmd.Flags().BoolVar(&all, "all", false, "update every enabled git skill provider")
	return cmd
}

func runLockUpdateCommand(opts *options, all *bool, commandName string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if *all && len(args) > 0 {
			return fmt.Errorf("use either %s --all or named git skill providers, not both", commandName)
		}
		if !*all && len(args) == 0 {
			return fmt.Errorf("%s requires a git skill provider name or --all", commandName)
		}
		cfg, home, err := opts.loadConfig()
		if err != nil {
			return err
		}
		if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
			return err
		}
		lock, err := core.UpdateRemoteSkillSources(cfg, home, args, *all)
		if err != nil {
			return err
		}
		path, err := core.WriteSourceLock(home, lock)
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Updated %s\n", path)
		return nil
	}
}

func importCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "import",
		Aliases: []string{"add"},
		Short:   "Capture existing agent files into the source state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.ui {
				if opts.write {
					return errors.New("use only one of --ui or --write")
				}
				return opts.runUI(cmd, "import")
			}
			target, err := opts.targetOptions()
			if err != nil {
				return err
			}
			result, err := core.ImportAll(core.ImportOptions{
				TargetOptions:     target,
				SecretPolicy:      core.SecretPolicy(opts.secretPolicy),
				InstructionPolicy: core.InstructionPolicy(opts.instructionPolicy),
			})
			if err != nil {
				return err
			}
			for _, warning := range result.Warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", warning)
			}
			if opts.write {
				if err := core.WriteImport(target.KanonHome, result, opts.force); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote imported config to %s\n", filepath.Join(target.KanonHome, "kanon.yaml"))
				return nil
			}
			data, err := core.ImportPreview(result)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.ui, "ui", false, "review and select imports in the interactive TUI")
	cmd.Flags().BoolVar(&opts.write, "write", false, "write imported config and files into the Kanon home")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite an existing kanon.yaml during import")
	cmd.Flags().StringVar(&opts.secretPolicy, "secret-policy", string(core.SecretPolicyKeep), "secret handling during import: keep")
	cmd.Flags().StringVar(&opts.instructionPolicy, "instruction-policy", string(core.InstructionPolicyAuto), "instruction handling during import: auto, codex, claude, merge, or skip")
	return cmd
}

func updateCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Pull the source repository, then render and apply (remote to destination)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
			}
			if err := core.RunGit(home, cmd.OutOrStdout(), cmd.ErrOrStderr(), "pull", "--ff-only"); err != nil {
				return err
			}
			return opts.runApply(cmd, !opts.yes)
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "apply without prompting")
	cmd.Flags().BoolVar(&opts.adopt, "adopt", false, "deprecated; has no effect")
	cmd.Flags().BoolVarP(&opts.dryRun, "dry-run", "n", false, "pull, then show what apply would change without writing")
	return cmd
}

func gitCommand(opts *options, use, short string, args []string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short + " the Kanon source repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
			}
			return core.RunGit(home, cmd.OutOrStdout(), cmd.ErrOrStderr(), args...)
		},
	}
}

// render compiles the source state into the target state: it loads and
// validates the config, then runs the per-agent adapters. It returns the
// rendered files and the resolved Kanon home.
func (opts *options) render() ([]core.RenderedFile, string, error) {
	cfg, home, err := opts.loadConfig()
	if err != nil {
		return nil, "", err
	}
	if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
		return nil, "", err
	}
	lock, _, err := core.LoadSourceLock(home)
	if err != nil {
		return nil, "", err
	}
	target, err := opts.targetOptions()
	if err != nil {
		return nil, "", err
	}
	target.SourceLock = lock
	files, err := core.RenderAll(cfg, target)
	if err != nil {
		return nil, "", err
	}
	return files, home, nil
}

func (opts *options) plan() (*core.ApplyPlan, error) {
	files, home, err := opts.render()
	if err != nil {
		return nil, err
	}
	target, err := opts.targetOptions()
	if err != nil {
		return nil, err
	}
	return core.PlanFiles(files, core.ApplyOptions{
		KanonHome: home,
		UserHome:  target.UserHome,
		Adopt:     opts.adopt,
		Agent:     target.Agent,
		Project:   target.Project,
	})
}

// runApply plans the target state against the destination and writes the
// changes. When confirmFirst is true it shows the diff and prompts before
// writing. It is shared by the apply and update commands.
func (opts *options) runApply(cmd *cobra.Command, confirmFirst bool) error {
	plan, err := opts.plan()
	if err != nil {
		return err
	}
	if len(plan.Conflicts) > 0 {
		fmt.Fprint(cmd.OutOrStdout(), planDiff(cmd, plan))
		return errors.New("resolve conflicts")
	}
	if len(plan.Changes) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No changes.")
		return nil
	}
	if opts.dryRun {
		fmt.Fprint(cmd.OutOrStdout(), planDiff(cmd, plan))
		fmt.Fprintf(cmd.OutOrStdout(), "Dry run: would apply %d file change(s); nothing written.\n", len(plan.Changes))
		return nil
	}
	if confirmFirst {
		fmt.Fprint(cmd.OutOrStdout(), planDiff(cmd, plan))
		ok, err := confirm(cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(cmd.OutOrStdout(), "Apply cancelled.")
			return nil
		}
	}
	home, err := opts.resolvedHome()
	if err != nil {
		return err
	}
	target, err := opts.targetOptions()
	if err != nil {
		return err
	}
	if err := core.ApplyFiles(plan, core.ApplyOptions{
		KanonHome: home,
		UserHome:  target.UserHome,
		Adopt:     opts.adopt,
		Agent:     target.Agent,
		Project:   target.Project,
	}); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Applied %d file change(s).\n", len(plan.Changes))
	return nil
}

func (opts *options) loadConfig() (*core.Config, string, error) {
	home, err := opts.resolvedHome()
	if err != nil {
		return nil, "", err
	}
	cfg, _, err := core.LoadConfig(home, opts.configPath)
	return cfg, home, err
}

func (opts *options) targetOptions() (core.TargetOptions, error) {
	home, err := opts.resolvedHome()
	if err != nil {
		return core.TargetOptions{}, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return core.TargetOptions{}, err
	}
	project := opts.project
	if project != "" {
		project, err = filepath.Abs(project)
		if err != nil {
			return core.TargetOptions{}, err
		}
	}
	if opts.agent != core.AgentAll && !core.IsAgent(opts.agent) {
		return core.TargetOptions{}, fmt.Errorf("unsupported agent %q", opts.agent)
	}
	return core.TargetOptions{
		KanonHome: home,
		UserHome:  userHome,
		Project:   project,
		Agent:     opts.agent,
	}, nil
}

func (opts *options) resolvedHome() (string, error) {
	if opts.home == "" {
		return core.DefaultHome()
	}
	home := core.ResolvePath("", opts.home)
	return filepath.Abs(home)
}

func validationError(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func isMissingConfigError(home, configPath string, err error) bool {
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, os.ErrNotExist) {
		return false
	}
	if configPath == "" {
		configPath = filepath.Join(home, "kanon.yaml")
	}
	return filepath.Clean(pathErr.Path) == filepath.Clean(configPath)
}

func confirm(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "Apply these changes? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
