package core

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// CloneHome clones an existing Kanon source repository from repo into home.
// home must be missing or empty; git clone populates it.
func CloneHome(repo, home string) error {
	if repo == "" {
		return errors.New("repo is required")
	}
	if home == "" {
		return errors.New("home is required")
	}
	if entries, err := os.ReadDir(home); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s already exists and is not empty; remove it or choose another --home", home)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(home), 0o755); err != nil {
		return err
	}
	// Terminate option parsing so a repo value beginning with "-" cannot be
	// interpreted by git as a flag (argument injection).
	cmd := exec.Command("git", "clone", "--", repo, home)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Repo URLs may embed credentials (https://user:token@host/...); keep
		// them out of the error, which can reach logs and support transcripts.
		clean := strings.TrimSpace(redactCredentials(repo, string(output)))
		return fmt.Errorf("git clone %s: %w: %s", redactURL(repo), err, clean)
	}
	return nil
}

// redactURL strips any userinfo (user:password) from a URL so embedded
// credentials are not leaked. Non-URL repo values are returned unchanged.
func redactURL(repo string) string {
	u, err := url.Parse(repo)
	if err != nil || u.User == nil {
		return repo
	}
	u.User = nil
	return u.String()
}

// redactCredentials replaces any userinfo from repo (the username, password, or
// the whole user:password pair) wherever git echoed it back in output.
func redactCredentials(repo, output string) string {
	u, err := url.Parse(repo)
	if err != nil || u.User == nil {
		return output
	}
	if password, ok := u.User.Password(); ok && password != "" {
		output = strings.ReplaceAll(output, password, "redacted")
	}
	return strings.ReplaceAll(output, u.User.String(), "redacted")
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
`
