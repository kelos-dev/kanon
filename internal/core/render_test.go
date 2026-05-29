package core

import (
	"os"
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
		Permissions: Permissions{
			ApprovalPolicy: "on-request",
			SandboxMode:    "workspace-write",
			Allow:          []string{"Bash(git status:*)"},
		},
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
	if !strings.Contains(codexConfig, "approval_policy = 'on-request'") && !strings.Contains(codexConfig, "approval_policy = \"on-request\"") {
		t.Fatalf("codex config missing approval policy: %s", codexConfig)
	}
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
