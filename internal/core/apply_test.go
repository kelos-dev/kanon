package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyUpdatesExistingRenderedFileWithoutAdoptAndIgnoresOldState(t *testing.T) {
	kanonHome := t.TempDir()
	target := filepath.Join(t.TempDir(), "AGENTS.md")
	statePath := oldStatePath(kanonHome)
	writeTestFile(t, statePath, []byte("{not-json"))
	writeTestFile(t, target, []byte("hand edited\n"))
	file := RenderedFile{
		Agent:   AgentCodex,
		Path:    target,
		Content: []byte("generated\n"),
		Mode:    0o644,
	}

	plan, err := PlanFiles([]RenderedFile{file}, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 || len(plan.Changes) != 1 || plan.Changes[0].Action != "update" {
		t.Fatalf("expected direct update, got changes=%#v conflicts=%#v", plan.Changes, plan.Conflicts)
	}
	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated\n" {
		t.Fatalf("target was not written: %q", data)
	}
	state := readTestFile(t, statePath)
	if string(state) != "{not-json" {
		t.Fatalf("old state file was rewritten: %q", state)
	}
}

func TestApplyDoesNotCreateStateFile(t *testing.T) {
	kanonHome := t.TempDir()
	target := filepath.Join(t.TempDir(), "AGENTS.md")
	file := RenderedFile{
		Agent:   AgentCodex,
		Path:    target,
		Content: []byte("generated\n"),
		Mode:    0o644,
	}

	applyAll(t, []RenderedFile{file}, ApplyOptions{KanonHome: kanonHome})

	if _, err := os.Stat(oldStatePath(kanonHome)); !os.IsNotExist(err) {
		t.Fatalf("apply created state file (err=%v)", err)
	}
}

func TestApplyDoesNotPruneStoppedRenderingFiles(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	root := filepath.Join(dest, "skills")
	keep := filepath.Join(root, "keep", "SKILL.md")
	drop := filepath.Join(root, "drop", "SKILL.md")
	files := []RenderedFile{
		{Agent: AgentClaude, Path: keep, Content: []byte("keep\n"), Mode: 0o644},
		{Agent: AgentClaude, Path: drop, Content: []byte("drop\n"), Mode: 0o644},
	}
	opts := ApplyOptions{KanonHome: kanonHome}
	applyAll(t, files, opts)

	plan, err := PlanFiles(files[:1], opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("stopped-rendering file should not be deleted, got %#v", plan.Changes)
	}
	if err := ApplyFiles(plan, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(drop); err != nil {
		t.Fatalf("stopped-rendering file was removed: %v", err)
	}
}

func TestCodexConfigMergePreservesExistingFieldsAndServers(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	configPath := filepath.Join(userHome, ".codex", "config.toml")
	writeTestFile(t, configPath, []byte(`
approval_policy = "on-request"

[mcp_servers.github]
command = "old-github"

[mcp_servers.private]
command = "private-mcp"
`))
	cfg := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"github": {Command: "github-mcp", Args: []string{"stdio"}, Targets: []string{AgentCodex}},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}
	merged := string(readTestFile(t, configPath))
	for _, want := range []string{"approval_policy", "on-request", "github-mcp", "private-mcp"} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged Codex config missing %q:\n%s", want, merged)
		}
	}
	if strings.Contains(merged, "old-github") {
		t.Fatalf("merged Codex config kept stale generated server value:\n%s", merged)
	}
}

func TestClaudeSettingsMergePreservesExistingFields(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	settingsPath := filepath.Join(userHome, ".claude", "settings.json")
	writeTestFile(t, settingsPath, []byte(`{"permissions":{"allow":["Read(**)"]},"theme":"dark","hooks":{"Old":[]}}`))
	cfg := &Config{
		Version: 1,
		Hooks: []Hook{{
			Name:    "fmt",
			Targets: []string{AgentClaude},
			Event:   "PostToolUse",
			Matcher: "Write",
			Command: "gofmt",
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(readTestFile(t, settingsPath), &merged); err != nil {
		t.Fatal(err)
	}
	if _, ok := merged["permissions"]; !ok {
		t.Fatalf("merged settings dropped permissions: %#v", merged)
	}
	if merged["theme"] != "dark" {
		t.Fatalf("merged settings dropped theme: %#v", merged)
	}
	hooks := merged["hooks"].(map[string]any)
	if _, ok := hooks["PostToolUse"]; !ok {
		t.Fatalf("merged settings did not install rendered hooks: %#v", hooks)
	}
	if _, ok := hooks["Old"]; ok {
		t.Fatalf("merged settings kept old hook section: %#v", hooks)
	}
}

func TestClaudeSettingsMergeDoesNotHTMLEscapeShellRedirection(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	settingsPath := filepath.Join(userHome, ".claude", "settings.json")
	writeTestFile(t, settingsPath, []byte(`{"theme":"dark"}`))
	cfg := &Config{
		Version: 1,
		Hooks: []Hook{{
			Name:    "notify",
			Targets: []string{AgentClaude},
			Event:   "Notification",
			Command: "bash -c 'echo ok' 2>>/tmp/claude-log",
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}
	merged := string(readTestFile(t, settingsPath))
	if !strings.Contains(merged, "2>>/tmp/claude-log") {
		t.Fatalf("merged settings escaped shell redirection:\n%s", merged)
	}
	if strings.Contains(merged, `\u003e`) {
		t.Fatalf("merged settings contains HTML escape:\n%s", merged)
	}
}

func TestClaudeSettingsMergeSkipsSemanticallyEqualHooks(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	settingsPath := filepath.Join(userHome, ".claude", "settings.json")
	existing := `{
  "theme": "dark",
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "command": "bash -c 'echo ok' 2>>/tmp/claude-log",
            "type": "command"
          }
        ]
      }
    ]
  }
}
`
	writeTestFile(t, settingsPath, []byte(existing))
	cfg := &Config{
		Version: 1,
		Hooks: []Hook{{
			Name:    "notify",
			Targets: []string{AgentClaude},
			Event:   "Notification",
			Command: "bash -c 'echo ok' 2>>/tmp/claude-log",
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("semantically equal hooks should not produce a change: %#v", plan.Changes)
	}
}

func TestClaudeSettingsDiffOnlyShowsChangedHookCommand(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	settingsPath := filepath.Join(userHome, ".claude", "settings.json")
	existing := `{
  "permissions": {
    "allow": [
      "Read(**)"
    ]
  },
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "command": "bash -c 'echo notify' 2>>/tmp/claude-log",
            "type": "command"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "command": "bash -c 'echo stop' 2>>/tmp/claude-log",
            "type": "command"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "command": "bash -c 'echo pre' 2>>/tmp/claude-log",
            "type": "command"
          }
        ],
        "matcher": "Write"
      }
    ]
  },
  "theme": "dark"
}
`
	writeTestFile(t, settingsPath, []byte(existing))
	cfg := &Config{
		Version: 1,
		Hooks: []Hook{
			{
				Name:    "notify",
				Targets: []string{AgentClaude},
				Event:   "Notification",
				Command: "bash -c 'echo notify' 2>>/tmp/claude-log",
			},
			{
				Name:    "stop",
				Targets: []string{AgentClaude},
				Event:   "Stop",
				Command: "bash -c 'echo stop' 2>>/tmp/claude-log-test",
			},
			{
				Name:    "pre",
				Targets: []string{AgentClaude},
				Event:   "PreToolUse",
				Matcher: "Write",
				Command: "bash -c 'echo pre' 2>>/tmp/claude-log",
			},
		},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 {
		t.Fatalf("expected one settings update, got %#v", plan.Changes)
	}
	diff := FormatPlanDiff(plan)
	var changed []string
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "-") || strings.HasPrefix(line, "+") {
			changed = append(changed, line)
		}
	}
	if len(changed) != 2 {
		t.Fatalf("expected only the changed command line in diff, got %d changed lines:\n%s", len(changed), diff)
	}
	if !strings.Contains(diff, `-            "command": "bash -c 'echo stop' 2>>/tmp/claude-log",`) {
		t.Fatalf("diff missing old stop command:\n%s", diff)
	}
	if !strings.Contains(diff, `+            "command": "bash -c 'echo stop' 2>>/tmp/claude-log-test",`) {
		t.Fatalf("diff missing new stop command:\n%s", diff)
	}
	for _, noise := range []string{"permissions", "theme", "echo notify", "echo pre"} {
		if strings.Contains(diff, noise) {
			t.Fatalf("diff includes unrelated setting %q:\n%s", noise, diff)
		}
	}
}

func TestClaudeMCPMergePreservesExistingFieldsAndServers(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	claudePath := filepath.Join(userHome, ".claude.json")
	writeTestFile(t, claudePath, []byte(`{
  "autoUpdates": false,
  "mcpServers": {
    "github": {"type":"stdio","command":"old-github"},
    "private": {"type":"stdio","command":"private-mcp"}
  }
}`))
	cfg := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"github": {Command: "github-mcp", Targets: []string{AgentClaude}},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}
	var merged map[string]any
	if err := json.Unmarshal(readTestFile(t, claudePath), &merged); err != nil {
		t.Fatal(err)
	}
	if merged["autoUpdates"] != false {
		t.Fatalf("merged Claude config dropped autoUpdates: %#v", merged)
	}
	servers := merged["mcpServers"].(map[string]any)
	if _, ok := servers["private"]; !ok {
		t.Fatalf("merged Claude config dropped private server: %#v", servers)
	}
	github := servers["github"].(map[string]any)
	if github["command"] != "github-mcp" {
		t.Fatalf("merged Claude config did not replace github server: %#v", github)
	}
}

func TestCoOwnedConfigMergeRejectsInvalidExistingFile(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()
	configPath := filepath.Join(userHome, ".codex", "config.toml")
	writeTestFile(t, configPath, []byte("mcp_servers = [\n"))
	cfg := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"github": {Command: "github-mcp", Targets: []string{AgentCodex}},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentCodex})
	if err != nil {
		t.Fatal(err)
	}
	_, err = PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Fatalf("expected parse error for invalid existing config, got %v", err)
	}
	if string(readTestFile(t, configPath)) != "mcp_servers = [\n" {
		t.Fatalf("invalid config was modified")
	}
}

func TestClaudeSettingsAndMCPMergeWithoutExistingKeys(t *testing.T) {
	kanonHome := t.TempDir()
	userHome := t.TempDir()

	// Write settings.json without hooks field
	settingsPath := filepath.Join(userHome, ".claude", "settings.json")
	existingSettings := `{
  "permissions": {
    "allow": [
      "Read(**)"
    ]
  },
  "theme": "dark"
}
`
	writeTestFile(t, settingsPath, []byte(existingSettings))

	// Write .claude.json without mcpServers field
	claudePath := filepath.Join(userHome, ".claude.json")
	existingClaude := `{
  "autoUpdates": false,
  "theme": "light"
}
`
	writeTestFile(t, claudePath, []byte(existingClaude))

	cfg := &Config{
		Version: 1,
		Hooks: []Hook{{
			Name:    "notify",
			Targets: []string{AgentClaude},
			Event:   "Notification",
			Command: "bash -c 'echo notify'",
		}},
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"github": {Command: "github-mcp", Targets: []string{AgentClaude}},
		}},
	}

	files, err := RenderAll(cfg, TargetOptions{KanonHome: kanonHome, UserHome: userHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}

	plan, err := PlanFiles(files, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}

	diff := FormatPlanDiff(plan)

	// The diff should NOT contain the key "theme" as a deleted/re-added line (meaningless diff)
	// it should only show adding the hooks and mcpServers fields.
	if strings.Contains(diff, "-  \"theme\":") || strings.Contains(diff, "-  \"permissions\":") {
		t.Fatalf("diff includes unrelated changes to existing fields:\n%s", diff)
	}

	if err := ApplyFiles(plan, ApplyOptions{KanonHome: kanonHome}); err != nil {
		t.Fatal(err)
	}

	// Verify settings.json merged nicely
	mergedSettings := string(readTestFile(t, settingsPath))
	if !strings.Contains(mergedSettings, `"theme": "dark"`) {
		t.Fatalf("theme was modified/lost in settings.json:\n%s", mergedSettings)
	}
	if !strings.Contains(mergedSettings, `"hooks"`) {
		t.Fatalf("hooks were not merged into settings.json:\n%s", mergedSettings)
	}

	// Verify .claude.json merged nicely
	mergedClaude := string(readTestFile(t, claudePath))
	if !strings.Contains(mergedClaude, `"theme": "light"`) {
		t.Fatalf("theme was modified/lost in .claude.json:\n%s", mergedClaude)
	}
	if !strings.Contains(mergedClaude, `"mcpServers"`) {
		t.Fatalf("mcpServers were not merged into .claude.json:\n%s", mergedClaude)
	}
}

func applyAll(t *testing.T, files []RenderedFile, opts ApplyOptions) {
	t.Helper()
	plan, err := PlanFiles(files, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, opts); err != nil {
		t.Fatal(err)
	}
}

func oldStatePath(home string) string {
	return filepath.Join(home, ".kanon", "state.json")
}
