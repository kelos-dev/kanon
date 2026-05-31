package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kelos-dev/kanon/internal/core"
)

func TestValidateUsesHomeForRelativeAssets(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "validate"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate failed: %v\n%s", err, out.String())
	}
}

func TestRenderPrintsTargetState(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "render"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("render failed: %v\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{"==> [claude]", "CLAUDE.md", "==> [codex]", "AGENTS.md", "Shared Agent Instructions"} {
		if !strings.Contains(got, want) {
			t.Fatalf("render output missing %q\n%s", want, got)
		}
	}
}

func TestUpdatePullsAndApplies(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	// Build a source repository to act as the remote upstream.
	remote := t.TempDir()
	gitRun(t, remote, "init")
	gitRun(t, remote, "config", "user.email", "test@example.com")
	gitRun(t, remote, "config", "user.name", "Test")
	writeFile(t, filepath.Join(remote, "kanon.yaml"), "version: 1\ninstructions:\n  files:\n    - instructions/shared.md\n")
	writeFile(t, filepath.Join(remote, "instructions", "shared.md"), "# Shared Agent Instructions\n\nBe careful.\n")
	gitRun(t, remote, "add", ".")
	gitRun(t, remote, "commit", "-m", "initial")

	// Clone it into the Kanon home so `git pull --ff-only` has an upstream.
	home := filepath.Join(t.TempDir(), "home")
	gitRun(t, "", "clone", remote, home)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "update", "--yes"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update failed: %v\n%s", err, out.String())
	}

	// The apply step should have written the rendered instruction files.
	for _, rel := range []string{".codex/AGENTS.md", ".claude/CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(userHome, rel)); err != nil {
			t.Fatalf("update did not apply %s: %v\n%s", rel, err, out.String())
		}
	}
	if !strings.Contains(out.String(), "Applied") {
		t.Fatalf("expected apply confirmation, got: %s", out.String())
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
