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
	force             bool
	write             bool
	gitInit           bool
	secretPolicy      string
	instructionPolicy string
}

func NewRootCommand() *cobra.Command {
	opts := &options{agent: core.AgentAll, gitInit: true}
	cmd := &cobra.Command{
		Use:           "kanon",
		Short:         "Manage coding-agent settings from a shared Kanon repository",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.PersistentFlags().StringVar(&opts.home, "home", "", "Kanon settings repository path (defaults to KANON_HOME or ~/.config/kanon)")
	cmd.PersistentFlags().StringVar(&opts.configPath, "config", "", "config file path (defaults to <home>/kanon.yaml)")
	cmd.PersistentFlags().StringVar(&opts.project, "project", "", "render project-scoped agent settings into this repository")
	cmd.PersistentFlags().StringVar(&opts.agent, "agent", core.AgentAll, "agent to manage: all, codex, or claude")

	cmd.AddCommand(initCommand(opts))
	cmd.AddCommand(validateCommand(opts))
	cmd.AddCommand(diffCommand(opts))
	cmd.AddCommand(applyCommand(opts))
	cmd.AddCommand(statusCommand(opts))
	cmd.AddCommand(importCommand(opts))
	cmd.AddCommand(gitCommand(opts, "pull", "pull", []string{"pull", "--ff-only"}))
	cmd.AddCommand(gitCommand(opts, "push", "push", []string{"push"}))
	return cmd
}

func initCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a Kanon settings repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
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
		Short: "Validate kanon.yaml and referenced assets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, home, err := opts.loadConfig()
			if err != nil {
				return err
			}
			if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "kanon.yaml is valid.")
			return nil
		},
	}
}

func diffCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "Preview generated agent file changes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, _, err := opts.plan(false)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), core.FormatPlanDiff(plan))
			return nil
		},
	}
}

func applyCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply generated agent file changes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, state, err := opts.plan(opts.adopt)
			if err != nil {
				return err
			}
			if len(plan.Conflicts) > 0 {
				fmt.Fprint(cmd.OutOrStdout(), core.FormatPlanDiff(plan))
				return errors.New("resolve conflicts or pass --adopt")
			}
			if len(plan.Changes) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No changes.")
				return nil
			}
			if !opts.yes {
				fmt.Fprint(cmd.OutOrStdout(), core.FormatPlanDiff(plan))
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
			if err := core.ApplyFiles(plan, state, core.ApplyOptions{KanonHome: home, Adopt: opts.adopt}); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Applied %d file change(s).\n", len(plan.Changes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&opts.yes, "yes", false, "apply without prompting")
	cmd.Flags().BoolVar(&opts.adopt, "adopt", false, "overwrite unmanaged or externally changed files")
	return cmd
}

func statusCommand(opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Kanon git status and rendered file drift",
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
			plan, _, err := opts.plan(false)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), core.DriftSummary(plan))
			return nil
		},
	}
}

func importCommand(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Preview or write a Kanon config from existing agent settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
	cmd.Flags().BoolVar(&opts.write, "write", false, "write imported config and files into the Kanon home")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite an existing kanon.yaml during import")
	cmd.Flags().StringVar(&opts.secretPolicy, "secret-policy", string(core.SecretPolicyKeep), "secret handling during import: keep")
	cmd.Flags().StringVar(&opts.instructionPolicy, "instruction-policy", string(core.InstructionPolicyAuto), "instruction handling during import: auto, codex, claude, merge, or skip")
	return cmd
}

func gitCommand(opts *options, use, short string, args []string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short + " the Kanon settings repository",
		RunE: func(cmd *cobra.Command, _ []string) error {
			home, err := opts.resolvedHome()
			if err != nil {
				return err
			}
			return core.RunGit(home, cmd.OutOrStdout(), cmd.ErrOrStderr(), args...)
		},
	}
}

func (opts *options) plan(adopt bool) (*core.ApplyPlan, *core.State, error) {
	cfg, home, err := opts.loadConfig()
	if err != nil {
		return nil, nil, err
	}
	if err := validationError(core.ValidateConfig(cfg, home)); err != nil {
		return nil, nil, err
	}
	target, err := opts.targetOptions()
	if err != nil {
		return nil, nil, err
	}
	files, err := core.RenderAll(cfg, target)
	if err != nil {
		return nil, nil, err
	}
	return core.PlanFiles(files, core.ApplyOptions{KanonHome: home, Adopt: adopt})
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
	if opts.agent != core.AgentAll && opts.agent != core.AgentCodex && opts.agent != core.AgentClaude {
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
