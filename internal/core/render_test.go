package core

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderUserScopedOutputs(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "shared.md"), []byte("Shared rules\n"))
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "codex.md"), []byte("Codex rules\n"))
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "claude.md"), []byte("Claude rules\n"))
	writeTestFile(t, filepath.Join(kanonHome, "skills", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))

	cfg := &Config{
		Version: 1,
		Instructions: Instructions{
			Files: []string{
				"instructions/shared.md",
				"instructions/codex.md",
				"instructions/claude.md",
			},
		},
		Skills: []Skill{{Name: "review"}},
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"context": {
				Command: "context-server",
				Args:    []string{"--stdio"},
				Env:     map[string]string{"TOKEN": "${CONTEXT_TOKEN:-unset}"},
			},
		}},
		Hooks: []Hook{{
			Name:    "fmt",
			Event:   "PostToolUse",
			Matcher: "Write",
			Command: "gofmt",
			Args:    []string{"-w", "$FILE"},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{
		KanonHome: kanonHome,
		UserHome:  userHome,
		Agent:     AgentAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	byPath := renderedByPath(files)
	codexAgents := string(byPath[filepath.Join(userHome, ".codex", "AGENTS.md")].Content)
	if !strings.Contains(codexAgents, "Shared rules") || !strings.Contains(codexAgents, "Codex rules") || !strings.Contains(codexAgents, "Claude rules") {
		t.Fatalf("codex instructions were not combined: %q", codexAgents)
	}
	claudeAgents := string(byPath[filepath.Join(userHome, ".claude", "CLAUDE.md")].Content)
	if !strings.Contains(claudeAgents, "Shared rules") || !strings.Contains(claudeAgents, "Codex rules") || !strings.Contains(claudeAgents, "Claude rules") {
		t.Fatalf("claude instructions were not combined: %q", claudeAgents)
	}
	codexConfig := string(byPath[filepath.Join(userHome, ".codex", "config.toml")].Content)
	if !strings.Contains(codexConfig, "context-server") {
		t.Fatalf("codex config missing MCP server: %s", codexConfig)
	}
	claudeJSON := string(byPath[filepath.Join(userHome, ".claude.json")].Content)
	if !strings.Contains(claudeJSON, `"mcpServers"`) || !strings.Contains(claudeJSON, `"context"`) {
		t.Fatalf("claude mcp output missing server: %s", claudeJSON)
	}
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "review", "SKILL.md")]; !ok {
		t.Fatalf("codex skill was not rendered")
	}
	if _, ok := byPath[filepath.Join(userHome, ".claude", "skills", "review", "SKILL.md")]; !ok {
		t.Fatalf("claude skill was not rendered")
	}
}

func TestValidateEnvRefs(t *testing.T) {
	cfg := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"bad": {Command: "server", Env: map[string]string{"TOKEN": "${KANON_TEST_MISSING_TOKEN}"}},
			"ok":  {Command: "server", Env: map[string]string{"TOKEN": "${KANON_TEST_MISSING_TOKEN:-default}"}},
		}},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	if len(errs) == 0 {
		t.Fatal("expected missing env reference validation error")
	}
	if !strings.Contains(errs[0].Error(), "KANON_TEST_MISSING_TOKEN") {
		t.Fatalf("unexpected validation error: %v", errs[0])
	}
}

func TestRenderRemoteSkillFromGitSource(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "review", "notes.txt"), []byte("remote note\n"))
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "-c", "user.name=Kanon Test", "-c", "user.email=kanon@example.test", "commit", "-m", "add skill")
	ref := strings.TrimSpace(string(runTestGit(t, repo, "rev-parse", "HEAD")))

	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "review",
			Source: &RemoteSource{
				Type:   "git",
				URL:    repo,
				Ref:    ref,
				Subdir: "packs/review",
			},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{
		KanonHome: kanonHome,
		UserHome:  userHome,
		Agent:     AgentCodex,
	})
	if err != nil {
		t.Fatal(err)
	}
	byPath := renderedByPath(files)
	if string(byPath[filepath.Join(userHome, ".agents", "skills", "review", "SKILL.md")].Content) == "" {
		t.Fatalf("remote skill was not rendered")
	}
	if string(byPath[filepath.Join(userHome, ".agents", "skills", "review", "notes.txt")].Content) != "remote note\n" {
		t.Fatalf("remote skill extra file was not rendered")
	}

	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex}); err != nil {
		t.Fatalf("expected cached remote skill after source repo removal: %v", err)
	}
}

func TestValidateRemoteSkillSource(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "bad",
			Path: "skills/bad",
			Source: &RemoteSource{
				Type:   "http",
				Subdir: "../bad",
			},
		}},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	joined := errorsText(errs)
	for _, want := range []string{
		"cannot be used with path",
		"unsupported type",
		"requires url",
		"requires ref",
		"invalid subdir",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected validation error containing %q, got: %s", want, joined)
		}
	}
}

func TestValidateRemoteSkillURLUsesEnvRefs(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "remote",
			Source: &RemoteSource{
				Type: "git",
				URL:  "https://example.invalid/${KANON_TEST_MISSING_TOKEN}/repo.git",
				Ref:  "abc123",
			},
		}},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	if !strings.Contains(errorsText(errs), "KANON_TEST_MISSING_TOKEN") {
		t.Fatalf("expected missing env reference validation error, got: %v", errs)
	}
}

func TestRemoteSkillMissingSkillFileReportsClearly(t *testing.T) {
	kanonHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "README.md"), []byte("not a skill\n"))
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "add", ".")
	runTestGit(t, repo, "-c", "user.name=Kanon Test", "-c", "user.email=kanon@example.test", "commit", "-m", "add readme")
	ref := strings.TrimSpace(string(runTestGit(t, repo, "rev-parse", "HEAD")))

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name:   "missing",
			Source: &RemoteSource{Type: "git", URL: repo, Ref: ref},
		}},
	}, TargetOptions{KanonHome: kanonHome, UserHome: t.TempDir(), Agent: AgentCodex})
	if err == nil || !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("expected missing SKILL.md error, got: %v", err)
	}
}

func TestRemoteSkillRootSourceDoesNotCacheGitMetadata(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "SKILL.md"), []byte("---\nname: root\n---\n"))
	ref := commitTestRepo(t, repo, "add root skill")

	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name:   "root",
			Source: &RemoteSource{Type: "git", URL: repo, Ref: ref},
		}},
	}
	if _, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex}); err != nil {
		t.Fatal(err)
	}

	cachePath := remoteSourceCachePath(kanonHome, RemoteSource{Type: "git", URL: repo, Ref: ref})
	if _, err := os.Stat(filepath.Join(cachePath, ".git", "config")); err == nil {
		t.Fatalf("remote skill cache retained git config at %s", filepath.Join(cachePath, ".git", "config"))
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat cached git config: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cachePath, "SKILL.md")); err != nil {
		t.Fatalf("expected materialized skill file in cache: %v", err)
	}
}

func TestRemoteSkillRejectsSymlinkedSubdir(t *testing.T) {
	kanonHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "actual", "SKILL.md"), []byte("---\nname: actual\n---\n"))
	if err := os.Symlink("actual", filepath.Join(repo, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ref := commitTestRepo(t, repo, "add symlinked skill")

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name:   "linked",
			Source: &RemoteSource{Type: "git", URL: repo, Ref: ref, Subdir: "linked"},
		}},
	}, TargetOptions{KanonHome: kanonHome, UserHome: t.TempDir(), Agent: AgentCodex})
	if err == nil || !strings.Contains(err.Error(), `source subdir "linked" is not a directory`) {
		t.Fatalf("expected symlinked subdir rejection, got: %v", err)
	}
}

func TestRemoteSkillRejectsSymlinkedFiles(t *testing.T) {
	kanonHome := t.TempDir()
	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeTestFile(t, outside, []byte("secret\n"))
	writeTestFile(t, filepath.Join(repo, "skill", "SKILL.md"), []byte("---\nname: skill\n---\n"))
	if err := os.Symlink(outside, filepath.Join(repo, "skill", "notes.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ref := commitTestRepo(t, repo, "add symlinked file")

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name:   "skill",
			Source: &RemoteSource{Type: "git", URL: repo, Ref: ref, Subdir: "skill"},
		}},
	}, TargetOptions{KanonHome: kanonHome, UserHome: t.TempDir(), Agent: AgentCodex})
	if err == nil || !strings.Contains(err.Error(), `source contains symlink "notes.txt"`) {
		t.Fatalf("expected symlinked file rejection, got: %v", err)
	}
}

func TestRemoteSourceGitErrorRedactsCredentials(t *testing.T) {
	repo := "https://carol:s3cr3t-token@example.invalid/repo.git"
	output := "fatal: unable to access https://carol:s3cr3t-token@example.invalid/repo.git for carol:s3cr3t-token"
	got := redactSourceGitOutput(repo, output)
	for _, leak := range []string{"carol", "s3cr3t-token", repo} {
		if strings.Contains(got, leak) {
			t.Fatalf("git output leaked %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, "redacted") {
		t.Fatalf("expected redacted output, got: %s", got)
	}
}

func TestRemoteSourceGitErrorRedactsExpandedURLQueryCredentials(t *testing.T) {
	t.Setenv("KANON_TEST_REMOTE_TOKEN", "s3cr3t/query-token")
	repo := expandEnvRefs("https://example.invalid/repo.git?token=${KANON_TEST_REMOTE_TOKEN}")
	output := "fatal: unable to access " + repo + " token=s3cr3t/query-token escaped=" + url.QueryEscape("s3cr3t/query-token")
	got := redactSourceGitOutput(repo, output)
	for _, leak := range []string{"s3cr3t/query-token", url.QueryEscape("s3cr3t/query-token"), repo} {
		if strings.Contains(got, leak) {
			t.Fatalf("git output leaked %q: %s", leak, got)
		}
	}
	if !strings.Contains(got, "redacted") {
		t.Fatalf("expected redacted output, got: %s", got)
	}
}

func TestRemoteSkillInstallErrorIncludesSkillName(t *testing.T) {
	root := t.TempDir()
	_, err := installMaterializedSkill("named", filepath.Join(root, "missing"), filepath.Join(root, "cache"))
	if err == nil {
		t.Fatal("expected install error")
	}
	if !strings.Contains(err.Error(), `skill "named" source cache`) {
		t.Fatalf("expected skill name in install error, got: %v", err)
	}
}

func TestRemoteSkillInstallKeepsConcurrentCacheWinner(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, "cache")
	sourcePath := filepath.Join(root, "source")
	writeTestFile(t, filepath.Join(cachePath, "SKILL.md"), []byte("winner\n"))
	writeTestFile(t, filepath.Join(sourcePath, "SKILL.md"), []byte("loser\n"))

	got, err := installMaterializedSkill("race", sourcePath, cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != cachePath {
		t.Fatalf("expected cache path %q, got %q", cachePath, got)
	}
	if content := string(readTestFile(t, filepath.Join(cachePath, "SKILL.md"))); content != "winner\n" {
		t.Fatalf("expected existing cache to win, got %q", content)
	}
}

func renderedByPath(files []RenderedFile) map[string]RenderedFile {
	out := map[string]RenderedFile{}
	for _, file := range files {
		out[file.Path] = file
	}
	return out
}

func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func runTestGit(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, string(output))
	}
	return output
}

func commitTestRepo(t *testing.T, dir, message string) string {
	t.Helper()
	runTestGit(t, dir, "init")
	runTestGit(t, dir, "add", ".")
	runTestGit(t, dir, "-c", "user.name=Kanon Test", "-c", "user.email=kanon@example.test", "commit", "-m", message)
	return strings.TrimSpace(string(runTestGit(t, dir, "rev-parse", "HEAD")))
}

func errorsText(errs []error) string {
	parts := make([]string, 0, len(errs))
	for _, err := range errs {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "\n")
}
