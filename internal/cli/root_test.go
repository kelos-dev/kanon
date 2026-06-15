package cli

import (
	"bytes"
	"errors"
	"fmt"
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

func TestApplyDryRunWritesNothing(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "apply", "-n"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("apply -n failed: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("expected dry-run notice, got: %s", out.String())
	}
	// The full plan diff — not just the summary line — must be printed.
	for _, want := range []string{"CLAUDE.md", "AGENTS.md", "+++ "} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dry run output missing %q; plan diff body not printed?\n%s", want, out.String())
		}
	}
	for _, rel := range []string{".codex/AGENTS.md", ".claude/CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(userHome, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry run wrote %s (err=%v)\n%s", rel, err, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".kanon", "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run recorded state (err=%v)", err)
	}
}

func TestUpdateDryRunPullsButWritesNothing(t *testing.T) {
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

	// Add a new commit upstream so the dry-run update has something to pull.
	writeFile(t, filepath.Join(remote, "instructions", "shared.md"), "# Shared Agent Instructions\n\nBe careful and kind.\n")
	gitRun(t, remote, "add", ".")
	gitRun(t, remote, "commit", "-m", "second")
	want := strings.TrimSpace(string(gitOutput(t, remote, "rev-parse", "HEAD")))

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "update", "-n"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update -n failed: %v\n%s", err, out.String())
	}

	if !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("expected dry-run notice, got: %s", out.String())
	}
	// The plan diff must be printed and reflect the pulled source: "kind" only
	// exists in the upstream commit fetched by the dry-run pull.
	for _, want := range []string{"CLAUDE.md", "AGENTS.md", "+++ ", "Be careful and kind"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("dry run output missing %q; plan diff body not printed or stale?\n%s", want, out.String())
		}
	}
	// The pull still runs in dry-run mode, so the source repo fast-forwards.
	if got := strings.TrimSpace(string(gitOutput(t, home, "rev-parse", "HEAD"))); got != want {
		t.Fatalf("update -n did not pull: home HEAD=%s want=%s\n%s", got, want, out.String())
	}
	// But the destination is left untouched and no state is recorded.
	for _, rel := range []string{".codex/AGENTS.md", ".claude/CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(userHome, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("dry run wrote %s (err=%v)\n%s", rel, err, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".kanon", "state.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry run recorded state (err=%v)", err)
	}
}

func TestStatusReportsRemoteSkillMaterializationErrors(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)

	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	writeFile(t, filepath.Join(repo, "README.md"), "not a skill\n")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "missing skill")
	ref := strings.TrimSpace(string(gitOutput(t, repo, "rev-parse", "HEAD")))

	home := t.TempDir()
	if err := core.WriteConfig(filepath.Join(home, "kanon.yaml"), &core.Config{
		Version: 1,
		Skills: []core.Skill{{
			Name:   "broken",
			Source: &core.RemoteSource{Type: "git", URL: repo, Ref: ref},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "status"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected status to report remote materialization error\n%s", out.String())
	}
	if !strings.Contains(err.Error(), `skill "broken" source missing SKILL.md`) {
		t.Fatalf("unexpected status error: %v\n%s", err, out.String())
	}
}

func TestLockCommandWritesLockfile(t *testing.T) {
	repo, ref := newRemoteSkillRepo(t, "version one\n")
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, repo, ref)

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "lock"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("lock failed: %v\n%s", err, out.String())
	}

	lock, _, err := core.LoadSourceLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Sources) != 1 {
		t.Fatalf("expected one lock entry, got %#v", lock.Sources)
	}
	entry := lock.Sources[0]
	if entry.Owner != "skill.remote-review" {
		t.Fatalf("unexpected owner %q", entry.Owner)
	}
	if entry.URL != repo {
		t.Fatalf("lock wrote url %q, want declared url %q", entry.URL, repo)
	}
	if entry.Ref != ref {
		t.Fatalf("lock wrote ref %q, want %q", entry.Ref, ref)
	}
	if entry.ResolvedRef == "" || entry.ResolvedRef == ref {
		t.Fatalf("lock did not record resolved commit sha: %#v", entry)
	}
	if !strings.HasPrefix(entry.ContentSHA256, "sha256:") {
		t.Fatalf("lock did not record content hash: %#v", entry)
	}
	if !strings.Contains(out.String(), "Locked 1 remote skill source(s)") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	checkOut := runKanon(t, home, "lock", "check")
	if !strings.Contains(checkOut, "kanon.lock is valid.") {
		t.Fatalf("unexpected check output: %s", checkOut)
	}
}

func TestLockCommandPreservesExistingPinAfterBranchMoves(t *testing.T) {
	repo, ref := newRemoteSkillRepo(t, "version one\n")
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, repo, ref)
	runKanon(t, home, "lock")

	lock, _, err := core.LoadSourceLock(home)
	if err != nil {
		t.Fatal(err)
	}
	initial := lock.Sources[0].ResolvedRef

	commitRemoteSkill(t, repo, "version two\n")
	runKanon(t, home, "lock")

	lock, _, err = core.LoadSourceLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Sources[0].ResolvedRef; got != initial {
		t.Fatalf("lock moved existing pin from %s to %s", initial, got)
	}
}

func TestLockUpdateAllRefreshesExistingPinAfterBranchMoves(t *testing.T) {
	repo, ref := newRemoteSkillRepo(t, "version one\n")
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, repo, ref)
	runKanon(t, home, "lock")

	lock, _, err := core.LoadSourceLock(home)
	if err != nil {
		t.Fatal(err)
	}
	initial := lock.Sources[0].ResolvedRef

	commitRemoteSkill(t, repo, "version two\n")
	runKanon(t, home, "lock", "update", "--all")

	lock, _, err = core.LoadSourceLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if got := lock.Sources[0].ResolvedRef; got == initial {
		t.Fatalf("lock update --all kept stale pin %s", got)
	}
}

func TestRenderUsesLockedRemoteSkillAfterBranchMoves(t *testing.T) {
	repo, ref := newRemoteSkillRepo(t, "version one\n")
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, repo, ref)
	runKanon(t, home, "lock")

	commitRemoteSkill(t, repo, "version two\n")
	if err := os.RemoveAll(filepath.Join(home, ".kanon", "cache")); err != nil {
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
	if !strings.Contains(out.String(), "version one") {
		t.Fatalf("render did not use locked content:\n%s", out.String())
	}
	if strings.Contains(out.String(), "version two") {
		t.Fatalf("render used moved branch content despite lock:\n%s", out.String())
	}
}

func TestLockCheckDetectsMovedRef(t *testing.T) {
	repo, ref := newRemoteSkillRepo(t, "version one\n")
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, repo, ref)
	runKanon(t, home, "lock")
	commitRemoteSkill(t, repo, "version two\n")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "lock", "check"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected lock check to fail after ref moved\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "resolves to") {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
}

func TestLockRejectsLiteralCredentials(t *testing.T) {
	home := t.TempDir()
	writeRemoteSkillConfig(t, home, "https://user:secret@example.com/acme/skills.git", "main")

	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--home", home, "lock"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected lock to reject literal credentials\n%s", out.String())
	}
	if !strings.Contains(err.Error(), "literal credentials") {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	if _, statErr := os.Stat(filepath.Join(home, "kanon.lock")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("lock wrote kanon.lock despite credential error (stat err=%v)", statErr)
	}
}

func TestRenderMergesExplicitOverlay(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	overlayDir := filepath.Join(project, ".kanon")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(overlayDir, "extra.md"), "Project-specific rule\n")
	writeFile(t, filepath.Join(overlayDir, "kanon.yaml"),
		"version: 1\ninstructions:\n  files:\n    - extra.md\n")

	out := runKanon(t, home, "--overlay", filepath.Join(overlayDir, "kanon.yaml"), "render")
	if !strings.Contains(out, "Project-specific rule") {
		t.Errorf("render output missing overlay instruction content\n%s", out)
	}
}

func TestRenderAutoDetectsProjectOverlay(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	overlayDir := filepath.Join(project, ".kanon")
	if err := os.MkdirAll(overlayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(overlayDir, "proj.md"), "Auto-detected project rule\n")
	writeFile(t, filepath.Join(overlayDir, "kanon.yaml"),
		"version: 1\ninstructions:\n  files:\n    - proj.md\n")

	out := runKanon(t, home, "--project", project, "render")
	if !strings.Contains(out, "Auto-detected project rule") {
		t.Errorf("render output missing auto-detected overlay content\n%s", out)
	}
}

func TestRenderNoOverlayWhenProjectLacksKanonDir(t *testing.T) {
	home := t.TempDir()
	if err := core.InitHome(core.InitOptions{Home: home}); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()

	// should succeed without any overlay
	out := runKanon(t, home, "--project", project, "render")
	if !strings.Contains(out, "Shared Agent Instructions") {
		t.Errorf("render missing base content\n%s", out)
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

func gitOutput(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
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

func newRemoteSkillRepo(t *testing.T, content string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	gitRun(t, repo, "init")
	gitRun(t, repo, "config", "user.email", "test@example.com")
	gitRun(t, repo, "config", "user.name", "Test")
	commitRemoteSkill(t, repo, content)
	ref := strings.TrimSpace(string(gitOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD")))
	return repo, ref
}

func commitRemoteSkill(t *testing.T, repo, content string) string {
	t.Helper()
	writeFile(t, filepath.Join(repo, "SKILL.md"), "# Remote Review\n\n"+content)
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-m", "update skill")
	return strings.TrimSpace(string(gitOutput(t, repo, "rev-parse", "HEAD")))
}

func writeRemoteSkillConfig(t *testing.T, home, repo, ref string) {
	t.Helper()
	writeFile(t, filepath.Join(home, "kanon.yaml"), fmt.Sprintf(`version: 1
skills:
  - name: remote-review
    source:
      type: git
      url: %s
      ref: %s
`, repo, ref))
}

func runKanon(t *testing.T, home string, args ...string) string {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--home", home}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("kanon %v failed: %v\n%s", args, err, out.String())
	}
	return out.String()
}
