package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderDefaultsExcludeGemini guards the backward-compatibility contract:
// a config without an agents: key must keep rendering exactly codex+claude and
// never emit Gemini files.
func TestRenderDefaultsExcludeGemini(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "shared.md"), []byte("Shared rules\n"))

	cfg := &Config{
		Version:      1,
		Instructions: Instructions{Files: []string{"instructions/shared.md"}},
	}
	files, err := RenderAll(cfg, TargetOptions{
		KanonHome: kanonHome,
		UserHome:  userHome,
		Agent:     AgentAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Agent == AgentGemini {
			t.Fatalf("gemini file rendered without opt-in: %s", file.Path)
		}
	}
	byPath := renderedByPath(files)
	if _, ok := byPath[filepath.Join(userHome, ".codex", "AGENTS.md")]; !ok {
		t.Fatalf("codex instructions were not rendered by default")
	}
	if _, ok := byPath[filepath.Join(userHome, ".claude", "CLAUDE.md")]; !ok {
		t.Fatalf("claude instructions were not rendered by default")
	}
}

func TestRenderGeminiOptIn(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "shared.md"), []byte("Shared rules\n"))
	writeTestFile(t, filepath.Join(kanonHome, "skills", "review", "SKILL.md"), []byte("---\nname: review\n---\n\nReview code.\n"))

	cfg := &Config{
		Version:      1,
		Agents:       []string{AgentCodex, AgentClaude, AgentGemini},
		Instructions: Instructions{Files: []string{"instructions/shared.md"}},
		Skills:       []Skill{{Name: "review"}},
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"context": {
				Command: "context-server",
				Args:    []string{"--stdio"},
				Env:     map[string]string{"TOKEN": "${CONTEXT_TOKEN:-unset}"},
			},
			"remote": {
				Type: "http",
				URL:  "https://example.com/mcp",
			},
		}},
		Hooks: []Hook{{
			Name:    "fmt",
			Event:   "PostToolUse",
			Matcher: "write_file",
			Command: "gofmt -w \"$FILE\"",
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

	geminiMd := string(byPath[filepath.Join(userHome, ".gemini", "GEMINI.md")].Content)
	if !strings.Contains(geminiMd, "Shared rules") {
		t.Fatalf("gemini instructions missing: %q", geminiMd)
	}

	settingsData := byPath[filepath.Join(userHome, ".gemini", "settings.json")].Content
	settings := string(settingsData)
	if !strings.Contains(settings, `"mcpServers"`) || !strings.Contains(settings, `"context-server"`) {
		t.Fatalf("gemini settings missing stdio mcp server: %s", settings)
	}
	if !strings.Contains(settings, `"httpUrl": "https://example.com/mcp"`) {
		t.Fatalf("gemini settings missing http mcp server: %s", settings)
	}
	var parsed map[string]any
	if err := json.Unmarshal(settingsData, &parsed); err != nil {
		t.Fatal(err)
	}
	hooks := parsed["hooks"].(map[string]any)
	afterTool := hooks["AfterTool"].([]any)
	hookGroup := afterTool[0].(map[string]any)
	if hookGroup["matcher"] != "write_file" {
		t.Fatalf("gemini hook matcher was not rendered: %#v", hookGroup)
	}
	handler := hookGroup["hooks"].([]any)[0].(map[string]any)
	if handler["name"] != "fmt" || handler["type"] != "command" || handler["command"] != `gofmt -w "$FILE"` {
		t.Fatalf("gemini hook handler was not rendered correctly: %#v", handler)
	}
	if _, ok := handler["args"]; ok {
		t.Fatalf("gemini hook rendered unsupported args field: %#v", handler)
	}
	if _, ok := handler["async"]; ok {
		t.Fatalf("gemini hook rendered unsupported async field: %#v", handler)
	}
	if _, ok := byPath[filepath.Join(userHome, ".gemini", "skills", "review", "SKILL.md")]; !ok {
		t.Fatalf("gemini skill was not rendered")
	}
}

func TestRenderGeminiExplicitAgent(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(kanonHome, "instructions", "shared.md"), []byte("Shared rules\n"))

	cfg := &Config{
		Version:      1,
		Instructions: Instructions{Files: []string{"instructions/shared.md"}},
	}
	// --agent gemini selects the adapter explicitly, even without an opt-in list.
	files, err := RenderAll(cfg, TargetOptions{
		KanonHome: kanonHome,
		UserHome:  userHome,
		Agent:     AgentGemini,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.Agent != AgentGemini {
			t.Fatalf("expected only gemini files, got %s for %s", file.Agent, file.Path)
		}
	}
	byPath := renderedByPath(files)
	if _, ok := byPath[filepath.Join(userHome, ".gemini", "GEMINI.md")]; !ok {
		t.Fatalf("gemini instructions were not rendered")
	}
}

func TestValidateRejectsUnknownAgent(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Agents:  []string{AgentCodex, "windsurf"},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	if len(errs) == 0 {
		t.Fatal("expected validation error for unknown agent entry")
	}
	if !strings.Contains(errs[0].Error(), "windsurf") {
		t.Fatalf("unexpected validation error: %v", errs[0])
	}
}

func TestValidateRejectsTopLevelAgentAll(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Agents:  []string{AgentAll},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	if len(errs) == 0 {
		t.Fatal("expected validation error for agents all")
	}
	if !strings.Contains(errs[0].Error(), `agents cannot include "all"`) {
		t.Fatalf("unexpected validation error: %v", errs[0])
	}
}

func TestValidateRejectsInvalidGeminiHookWhenOptedIn(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Agents:  []string{AgentGemini},
		Hooks: []Hook{{
			Name:    "fmt",
			Event:   "PostToolUse",
			Command: "gofmt",
			Args:    []string{"-w", "$FILE"},
		}},
	}
	errs := ValidateConfig(cfg, t.TempDir())
	if len(errs) == 0 {
		t.Fatal("expected validation error for unsupported gemini hook args")
	}
	if !strings.Contains(errorsText(errs), "args are not supported") {
		t.Fatalf("unexpected validation error: %v", errs)
	}
}

func TestRenderGeminiRejectsUnsupportedHookEvent(t *testing.T) {
	_, err := RenderAll(&Config{
		Version: 1,
		Hooks: []Hook{{
			Name:    "stop",
			Targets: []string{AgentGemini},
			Event:   "Stop",
			Command: "echo stop",
		}},
	}, TargetOptions{UserHome: t.TempDir(), Agent: AgentGemini})
	if err == nil {
		t.Fatal("expected render error for unsupported gemini hook event")
	}
	if !strings.Contains(err.Error(), `unsupported event "Stop"`) {
		t.Fatalf("unexpected render error: %v", err)
	}
}
