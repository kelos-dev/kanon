package core

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestImportKeepsSecretsByDefault(t *testing.T) {
	t.Setenv("TOKEN", "available")
	t.Setenv("API_KEY", "available")
	t.Setenv("ACCESS_TOKEN", "available")
	t.Setenv("AUTHORIZATION", "available")

	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(userHome, ".codex", "config.toml"), []byte(`
[mcp_servers.github]
command = "github-mcp"

[mcp_servers.github.env]
TOKEN = "ghp_secretvalue"
`))
	writeTestFile(t, filepath.Join(userHome, ".claude", "settings.json"), []byte(`{"api_key":"sk-secretvalue","accessToken":"github_pat_secretvalue","claudeCodeFirstTokenDate":"2026-05-30"}`))
	writeTestFile(t, filepath.Join(userHome, ".claude.json"), []byte(`{"mcpServers":{"private":{"type":"http","url":"https://mcp.example.com","headers":{"Authorization":"Bearer secretvalue"}}}}`))

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentAll,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := ImportPreview(result)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "ghp_secretvalue") || !strings.Contains(out, "Bearer secretvalue") {
		t.Fatalf("keep policy did not preserve plaintext secrets: %s", out)
	}
	if strings.Contains(out, redactedSecret) {
		t.Fatalf("import preview used redaction marker: %s", out)
	}
	if strings.Contains(out, legacyRedactedSecret) {
		t.Fatalf("import preview used legacy redaction marker: %s", out)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, "codex.config.mcp_servers.github.env.TOKEN") {
		t.Fatalf("missing codex secret warning: %v", result.Warnings)
	}
	if !strings.Contains(warnings, "claude.settings.api_key") {
		t.Fatalf("missing claude secret warning: %v", result.Warnings)
	}
	if strings.Contains(warnings, "kept possible plaintext secret at claude.settings.claudeCodeFirstTokenDate") {
		t.Fatalf("non-secret token metadata was treated as secret: %v", result.Warnings)
	}
	if errs := ValidateConfig(result.Config, t.TempDir()); len(errs) > 0 {
		t.Fatalf("imported config failed validation: %v", errs)
	}
}

func TestImportSecretPolicyKeep(t *testing.T) {
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(userHome, ".claude.json"), []byte(`{"mcpServers":{"private":{"type":"stdio","command":"private-mcp","env":{"API_KEY":"sk-secretvalue"}}}}`))

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentClaude,
		},
		SecretPolicy: SecretPolicyKeep,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := ImportPreview(result)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "sk-secretvalue") {
		t.Fatalf("keep policy did not preserve plaintext secret: %s", out)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "kept possible plaintext secret") {
		t.Fatalf("expected plaintext keep warning, got %v", result.Warnings)
	}
}

func TestImportRejectsUnsupportedSecretPolicy(t *testing.T) {
	_, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  t.TempDir(),
			Agent:     AgentClaude,
		},
		SecretPolicy: SecretPolicy("omit"),
	})
	if err == nil || !strings.Contains(err.Error(), "only \"keep\" is implemented") {
		t.Fatalf("expected unsupported secret policy error, got %v", err)
	}
}

func TestWriteImportRequiresForceForExistingConfig(t *testing.T) {
	home := t.TempDir()
	result := &ImportResult{
		Config: &Config{
			Version:      1,
			Instructions: Instructions{Files: []string{"instructions/imported.md"}},
		},
		Files: map[string][]byte{
			"instructions/imported.md": []byte("first\n"),
		},
	}
	if err := WriteImport(home, result, false); err != nil {
		t.Fatal(err)
	}

	result.Files["instructions/imported.md"] = []byte("second\n")
	err := WriteImport(home, result, false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected force error, got %v", err)
	}
	data := readTestFile(t, filepath.Join(home, "instructions", "imported.md"))
	if string(data) != "first\n" {
		t.Fatalf("import rewrote file without force: %q", data)
	}

	if err := WriteImport(home, result, true); err != nil {
		t.Fatal(err)
	}
	data = readTestFile(t, filepath.Join(home, "instructions", "imported.md"))
	if string(data) != "second\n" {
		t.Fatalf("forced import did not rewrite file: %q", data)
	}
}

func TestImportInstructionPolicy(t *testing.T) {
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(userHome, ".codex", "AGENTS.md"), []byte("Codex rules\n"))
	writeTestFile(t, filepath.Join(userHome, ".claude", "CLAUDE.md"), []byte("Claude rules\n"))

	_, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentAll,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "--instruction-policy") {
		t.Fatalf("expected instruction conflict error, got %v", err)
	}

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentAll,
		},
		InstructionPolicy: InstructionPolicyMerge,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Config.Instructions.Files, []string{"instructions/imported.md"}) {
		t.Fatalf("unexpected imported instruction files: %#v", result.Config.Instructions.Files)
	}
	content := string(result.Files[filepath.Join("instructions", "imported.md")])
	if !strings.Contains(content, "Codex rules") || !strings.Contains(content, "Claude rules") {
		t.Fatalf("merged instruction content missing source data: %q", content)
	}
}

func TestImportSkillsIntoNeutralTargets(t *testing.T) {
	userHome := t.TempDir()
	skill := []byte("---\nname: review\n---\n\nReview code.\n")
	writeTestFile(t, filepath.Join(userHome, ".agents", "skills", "review", "SKILL.md"), skill)
	writeTestFile(t, filepath.Join(userHome, ".claude", "skills", "review", "SKILL.md"), skill)

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentAll,
		},
		InstructionPolicy: InstructionPolicySkip,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Config.Skills) != 1 {
		t.Fatalf("expected one merged skill, got %#v", result.Config.Skills)
	}
	imported := result.Config.Skills[0]
	if imported.Name != "review" || !slices.Contains(imported.Targets, AgentCodex) || !slices.Contains(imported.Targets, AgentClaude) {
		t.Fatalf("skill was not imported with neutral targets: %#v", imported)
	}
	if string(result.Files[filepath.Join("skills", "review", "SKILL.md")]) != string(skill) {
		t.Fatalf("skill file was not imported: %#v", result.Files)
	}
}

func TestImportNormalizesSharedAgentConfiguration(t *testing.T) {
	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(userHome, ".codex", "config.toml"), []byte(`
approval_policy = "on-request"
sandbox_mode = "workspace-write"

[mcp_servers.github]
command = "github-mcp"
args = ["stdio"]
env_vars = ["GITHUB_TOKEN"]

[projects."/tmp/work"]
trust_level = "trusted"
`))
	writeTestFile(t, filepath.Join(userHome, ".codex", "hooks.json"), []byte(`{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          {"type": "command", "command": "gofmt", "args": ["-w", "$FILE"]}
        ]
      }
    ]
  }
}`))
	writeTestFile(t, filepath.Join(userHome, ".claude", "settings.json"), []byte(`{
  "permissions": {
    "allow": ["Read(**)"],
    "deny": ["Bash(rm:*)"],
    "defaultMode": "default"
  },
  "additionalDirectories": ["/tmp/work"],
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Write",
        "hooks": [
          {"type": "command", "command": "echo ok", "timeout": 3}
        ]
      }
    ]
  },
  "theme": "light"
}`))
	writeTestFile(t, filepath.Join(userHome, ".claude.json"), []byte(`{
  "mcpServers": {
    "github": {
      "type": "stdio",
      "command": "github-mcp",
      "args": ["stdio"],
      "env": {"GITHUB_TOKEN": "${GITHUB_TOKEN}"}
    }
  },
  "autoUpdates": false
}`))

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentAll,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Config.Permissions.ApprovalPolicy != "on-request" || result.Config.Permissions.SandboxMode != "workspace-write" {
		t.Fatalf("codex permissions were not normalized: %#v", result.Config.Permissions)
	}
	server := result.Config.MCP.Servers["github"]
	if server.Command != "github-mcp" || !slices.Contains(server.Targets, AgentCodex) || !slices.Contains(server.Targets, AgentClaude) {
		t.Fatalf("mcp server was not normalized and merged: %#v", server)
	}
	if server.Env["GITHUB_TOKEN"] != "${GITHUB_TOKEN}" {
		t.Fatalf("claude mcp env was not merged: %#v", server)
	}
	if len(result.Config.Hooks) != 2 {
		t.Fatalf("expected 2 normalized hooks, got %#v", result.Config.Hooks)
	}
	if result.Config.Permissions.Allow[0] != "Read(**)" || result.Config.Permissions.DefaultMode != "default" {
		t.Fatalf("claude permissions were not normalized: %#v", result.Config.Permissions)
	}
	warnings := strings.Join(result.Warnings, "\n")
	if !strings.Contains(warnings, `skipped unsupported codex config field "projects"`) {
		t.Fatalf("unmapped codex config was not reported: %v", result.Warnings)
	}
	if !strings.Contains(warnings, `skipped unsupported claude settings field "theme"`) {
		t.Fatalf("unmapped claude setting was not reported: %v", result.Warnings)
	}
	if !strings.Contains(warnings, `skipped unsupported claude json field "autoUpdates"`) {
		t.Fatalf("unmapped claude json was not reported: %v", result.Warnings)
	}
}

func TestImportNormalizesSecretHeadersAsEnvHeaders(t *testing.T) {
	t.Setenv("AUTHORIZATION", "available")

	userHome := t.TempDir()
	writeTestFile(t, filepath.Join(userHome, ".claude.json"), []byte(`{
  "mcpServers": {
    "private": {
      "type": "http",
      "url": "https://mcp.example.com",
      "headers": {
        "Authorization": "${AUTHORIZATION}",
        "X-Public": "public"
      }
    }
  }
}`))

	result, err := ImportAll(ImportOptions{
		TargetOptions: TargetOptions{
			KanonHome: t.TempDir(),
			UserHome:  userHome,
			Agent:     AgentClaude,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := result.Config.MCP.Servers["private"]
	if server.Headers["Authorization"] != "" {
		t.Fatalf("env authorization header remained literal: %#v", server.Headers)
	}
	if server.Headers["X-Public"] != "public" {
		t.Fatalf("public header was not preserved: %#v", server.Headers)
	}
	if server.EnvHeaders["Authorization"] != "AUTHORIZATION" {
		t.Fatalf("authorization header was not moved to env_headers: %#v", server.EnvHeaders)
	}
	if errs := ValidateConfig(result.Config, t.TempDir()); len(errs) > 0 {
		t.Fatalf("imported config failed validation: %v", errs)
	}
	files, err := RenderAll(result.Config, TargetOptions{
		KanonHome: t.TempDir(),
		UserHome:  t.TempDir(),
		Agent:     AgentClaude,
	})
	if err != nil {
		t.Fatal(err)
	}
	var rendered string
	for _, file := range files {
		if filepath.Base(file.Path) == ".claude.json" {
			rendered = string(file.Content)
		}
	}
	if !strings.Contains(rendered, `"Authorization": "${AUTHORIZATION}"`) {
		t.Fatalf("claude render did not convert env_headers to header env refs: %s", rendered)
	}
}
