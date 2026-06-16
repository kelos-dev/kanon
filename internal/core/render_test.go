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

func TestRenderTreatsEmptyTargetsAsAllTargets(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "skills", "shared", "SKILL.md"), []byte("---\nname: shared\n---\n"))
	writeTestFile(t, filepath.Join(kanonHome, "skills", "empty", "SKILL.md"), []byte("---\nname: empty\n---\n"))
	writeTestFile(t, filepath.Join(kanonHome, "kanon.yaml"), []byte(`
version: 1
skills:
  - name: shared
  - name: empty
    targets: []
mcp:
  servers:
    shared:
      command: shared-mcp
    empty:
      command: empty-mcp
      targets: []
hooks:
  - name: shared-hook
    event: PostToolUse
    command: echo shared
  - name: empty-hook
    targets: []
    event: PostToolUse
    command: echo empty
`))

	cfg, _, err := LoadConfig(kanonHome, "")
	if err != nil {
		t.Fatal(err)
	}
	if !HasTarget(cfg.Skills[0].Targets, AgentCodex) {
		t.Fatalf("omitted targets should apply to codex: %#v", cfg.Skills[0].Targets)
	}
	if !HasTarget(cfg.Skills[1].Targets, AgentCodex) {
		t.Fatalf("empty targets should apply to codex: %#v", cfg.Skills[1].Targets)
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
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "shared", "SKILL.md")]; !ok {
		t.Fatalf("all-target skill was not rendered")
	}
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "empty", "SKILL.md")]; !ok {
		t.Fatalf("empty-target skill was not rendered")
	}
	config := string(byPath[filepath.Join(userHome, ".codex", "config.toml")].Content)
	if !strings.Contains(config, "shared-mcp") || !strings.Contains(config, "empty-mcp") {
		t.Fatalf("empty-target MCP server was not rendered:\n%s", config)
	}
	hooks := string(byPath[filepath.Join(userHome, ".codex", "hooks.json")].Content)
	if !strings.Contains(hooks, "echo shared") || !strings.Contains(hooks, "echo empty") {
		t.Fatalf("empty-target hook was not rendered:\n%s", hooks)
	}
}

func TestRenderSkipsDisabledUnnamedSkill(t *testing.T) {
	enabled := false
	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Enabled: &enabled,
		}},
	}, TargetOptions{KanonHome: t.TempDir(), UserHome: t.TempDir(), Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
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

func TestRenderGitSkillSourceIncludeFromDirectory(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "review", "notes.txt"), []byte("remote note\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "lint", "SKILL.md"), []byte("---\nname: lint\n---\n\nLint code.\n"))
	ref := commitTestRepo(t, repo, "add skills")

	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "shared",
			Git: &GitSkill{
				URL:    repo,
				Ref:    ref,
				Subdir: "packs",
			},
			Include: []string{"review"},
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
	remoteSkill := string(byPath[filepath.Join(userHome, ".agents", "skills", "shared:review", "SKILL.md")].Content)
	if remoteSkill == "" {
		t.Fatalf("remote skill was not rendered")
	}
	if !strings.Contains(remoteSkill, "name: shared:review") {
		t.Fatalf("remote skill frontmatter was not namespaced: %s", remoteSkill)
	}
	if string(byPath[filepath.Join(userHome, ".agents", "skills", "shared:review", "notes.txt")].Content) != "remote note\n" {
		t.Fatalf("remote skill extra file was not rendered")
	}
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "shared:lint", "SKILL.md")]; ok {
		t.Fatalf("excluded remote skill was rendered")
	}

	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex}); err != nil {
		t.Fatalf("expected cached remote skill after source repo removal: %v", err)
	}
}

func TestRenderRemoteSkillDirectoryFromGitSource(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := filepath.Join(t.TempDir(), "shared-skills.git")
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "review", "notes.txt"), []byte("review note\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "lint", "SKILL.md"), []byte("---\nname: lint\n---\n\nLint code.\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "README.md"), []byte("skill directory docs\n"))
	ref := commitTestRepo(t, repo, "add skill directory")

	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Git: &GitSkill{
				URL:    repo,
				Ref:    ref,
				Subdir: "packs",
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
	if string(byPath[filepath.Join(userHome, ".agents", "skills", "shared-skills:review", "notes.txt")].Content) != "review note\n" {
		t.Fatalf("remote skill directory review files were not rendered")
	}
	if string(byPath[filepath.Join(userHome, ".agents", "skills", "shared-skills:lint", "SKILL.md")].Content) == "" {
		t.Fatalf("remote skill directory lint skill was not rendered")
	}

	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex}); err != nil {
		t.Fatalf("expected cached remote skill directory after source repo removal: %v", err)
	}
}

func TestValidateGitSkillSource(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Git: &GitSkill{
				Subdir: "../bad",
			},
			Include: []string{"review", "review", "skip"},
			Exclude: []string{"skip", ""},
		}, {
			Name: "bad",
			Git:  &GitSkill{URL: "https://example.invalid/repo.git", Ref: "abc123"},
		}, {
			Name: "bad",
			Git:  &GitSkill{URL: "https://example.invalid/repo.git", Ref: "abc123"},
		}},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	joined := errorsText(errs)
	for _, want := range []string{
		"requires name",
		`git skill provider "bad" is duplicated`,
		"requires url",
		"requires ref",
		"invalid subdir",
		"include has duplicate skill",
		"exclude cannot contain an empty skill name",
		"cannot both include and exclude skill",
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
			Git: &GitSkill{
				URL: "https://example.invalid/${KANON_TEST_MISSING_TOKEN}/repo.git",
				Ref: "abc123",
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
			Name: "missing",
			Git:  &GitSkill{URL: repo, Ref: ref},
		}},
	}, TargetOptions{KanonHome: kanonHome, UserHome: t.TempDir(), Agent: AgentCodex})
	if err == nil || !strings.Contains(err.Error(), "contains no skill directories") {
		t.Fatalf("expected empty skill source error, got: %v", err)
	}
}

func TestRemoteSkillDirectoryChildMissingSkillFileReportsClearly(t *testing.T) {
	kanonHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "broken", "README.md"), []byte("not a skill\n"))
	ref := commitTestRepo(t, repo, "add broken skill directory")

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name: "bundle",
			Git:  &GitSkill{URL: repo, Ref: ref, Subdir: "packs"},
		}},
	}, TargetOptions{KanonHome: kanonHome, UserHome: t.TempDir(), Agent: AgentCodex})
	if err == nil || !strings.Contains(err.Error(), `git skill provider "bundle" child "broken"`) || !strings.Contains(err.Error(), "missing SKILL.md") {
		t.Fatalf("expected broken child skill error, got: %v", err)
	}
}

func TestRemoteSkillDirectoryUsesProviderNamespace(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "skills", "review", "SKILL.md"), []byte("---\nname: review\n---\n"))
	writeTestFile(t, filepath.Join(repo, "packs", "review", "SKILL.md"), []byte("---\nname: review\n---\n"))
	ref := commitTestRepo(t, repo, "add duplicate skill")

	files, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{
			{Name: "review"},
			{Name: "bundle", Git: &GitSkill{URL: repo, Ref: ref, Subdir: "packs"}},
		},
	}, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	byPath := renderedByPath(files)
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "review", "SKILL.md")]; !ok {
		t.Fatalf("local skill was not rendered")
	}
	if _, ok := byPath[filepath.Join(userHome, ".agents", "skills", "bundle:review", "SKILL.md")]; !ok {
		t.Fatalf("remote skill was not rendered with provider namespace")
	}
}

func TestRemoteSkillRootSourceDoesNotCacheGitMetadata(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "root", "SKILL.md"), []byte("---\nname: root\n---\n"))
	ref := commitTestRepo(t, repo, "add root skill directory")

	cfg := &Config{
		Version: 1,
		Skills: []Skill{{
			Name: "root",
			Git:  &GitSkill{URL: repo, Ref: ref},
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
	if _, err := os.Stat(filepath.Join(cachePath, "root", "SKILL.md")); err != nil {
		t.Fatalf("expected materialized skill file in cache: %v", err)
	}
}

func TestRemoteSkillRejectsSymlinkedSubdir(t *testing.T) {
	kanonHome := t.TempDir()
	repo := t.TempDir()
	writeTestFile(t, filepath.Join(repo, "actual", "skill", "SKILL.md"), []byte("---\nname: actual\n---\n"))
	if err := os.Symlink("actual", filepath.Join(repo, "linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ref := commitTestRepo(t, repo, "add symlinked skill")

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name: "linked",
			Git:  &GitSkill{URL: repo, Ref: ref, Subdir: "linked"},
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
	writeTestFile(t, filepath.Join(repo, "skills", "skill", "SKILL.md"), []byte("---\nname: skill\n---\n"))
	if err := os.Symlink(outside, filepath.Join(repo, "skills", "skill", "notes.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ref := commitTestRepo(t, repo, "add symlinked file")

	_, err := RenderAll(&Config{
		Version: 1,
		Skills: []Skill{{
			Name: "skill",
			Git:  &GitSkill{URL: repo, Ref: ref, Subdir: "skills"},
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
	_, err := installMaterializedSkillSource("named", filepath.Join(root, "missing"), filepath.Join(root, "cache"))
	if err == nil {
		t.Fatal("expected install error")
	}
	if !strings.Contains(err.Error(), `git skill provider "named" source cache`) {
		t.Fatalf("expected skill name in install error, got: %v", err)
	}
}

func TestRemoteSkillInstallKeepsConcurrentCacheWinner(t *testing.T) {
	root := t.TempDir()
	cachePath := filepath.Join(root, "cache")
	sourcePath := filepath.Join(root, "source")
	writeTestFile(t, filepath.Join(cachePath, "winner", "SKILL.md"), []byte("winner\n"))
	writeTestFile(t, filepath.Join(sourcePath, "loser", "SKILL.md"), []byte("loser\n"))

	got, err := installMaterializedSkillSource("race", sourcePath, cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != cachePath {
		t.Fatalf("expected cache path %q, got %q", cachePath, got)
	}
	if content := string(readTestFile(t, filepath.Join(cachePath, "winner", "SKILL.md"))); content != "winner\n" {
		t.Fatalf("expected existing cache to win, got %q", content)
	}
}

func TestHashMaterializedSkillFramesBinaryContent(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		writeTestFile(t, filepath.Join(dir, "SKILL.md"), []byte("---\nname: hash\n---\n"))
	}
	writeTestFile(t, filepath.Join(first, "a"), []byte{0, 'b', 0})
	writeTestFile(t, filepath.Join(second, "a"), nil)
	writeTestFile(t, filepath.Join(second, "b"), nil)

	firstHash, err := hashMaterializedSkill("hash", first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := hashMaterializedSkill("hash", second)
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == secondHash {
		t.Fatalf("hash collision for differently framed trees: %s", firstHash)
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
