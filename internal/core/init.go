package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type InitOptions struct {
	Home  string
	Force bool
	Git   bool
}

func InitHome(opts InitOptions) error {
	if opts.Home == "" {
		return errors.New("home is required")
	}
	for _, dir := range []string{"instructions", "skills", "hooks", ".kanon"} {
		if err := os.MkdirAll(filepath.Join(opts.Home, dir), 0o755); err != nil {
			return err
		}
	}
	configPath := filepath.Join(opts.Home, "kanon.yaml")
	if _, err := os.Stat(configPath); err == nil && !opts.Force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", configPath)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if opts.Force || missing(configPath) {
		if err := os.WriteFile(configPath, []byte(sampleConfig), 0o644); err != nil {
			return err
		}
	}
	sharedPath := filepath.Join(opts.Home, "instructions", "shared.md")
	if opts.Force || missing(sharedPath) {
		if err := os.WriteFile(sharedPath, []byte("# Shared Agent Instructions\n\nKeep changes focused and explain tradeoffs clearly.\n"), 0o644); err != nil {
			return err
		}
	}
	gitignorePath := filepath.Join(opts.Home, ".gitignore")
	if opts.Force || missing(gitignorePath) {
		if err := os.WriteFile(gitignorePath, []byte(".kanon/\n"), 0o644); err != nil {
			return err
		}
	}
	if opts.Git && missing(filepath.Join(opts.Home, ".git")) {
		cmd := exec.Command("git", "-C", opts.Home, "init")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %w: %s", err, string(output))
		}
	}
	return nil
}

func missing(path string) bool {
	_, err := os.Stat(path)
	return errors.Is(err, os.ErrNotExist)
}

const sampleConfig = `version: 1
instructions:
  files:
    - instructions/shared.md
skills: []
mcp:
  servers: {}
hooks: []
permissions: {}
`
