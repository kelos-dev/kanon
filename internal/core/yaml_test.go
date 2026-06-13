package core

import (
	"strings"
	"testing"
)

func TestYAMLMarshalUsesTwoSpaceIndentation(t *testing.T) {
	data, err := yamlMarshal(&Config{
		Version: 1,
		Instructions: Instructions{
			Files: []string{"instructions/shared.md"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if !strings.Contains(out, "instructions:\n  files:\n    - instructions/shared.md") {
		t.Fatalf("yaml did not use 2-space indentation:\n%s", out)
	}
	if strings.Contains(out, "instructions:\n    files:") {
		t.Fatalf("yaml used 4-space indentation:\n%s", out)
	}
}

func TestYAMLMarshalOmitsEmptyTargetsByDefault(t *testing.T) {
	data, err := yamlMarshal(&Config{
		Version: 1,
		Skills: []Skill{
			{Name: "shared"},
			{Name: "empty", Targets: []string{}},
			{Name: "codex", Targets: []string{AgentCodex}},
		},
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"shared": {Command: "shared-mcp"},
			"empty":  {Command: "empty-mcp", Targets: []string{}},
			"codex":  {Command: "codex-mcp", Targets: []string{AgentCodex}},
		}},
		Hooks: []Hook{
			{Name: "shared-hook", Event: "PostToolUse", Command: "echo shared"},
			{Name: "empty-hook", Targets: []string{}, Event: "PostToolUse", Command: "echo empty"},
			{Name: "codex-hook", Targets: []string{AgentCodex}, Event: "PostToolUse", Command: "echo codex"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)
	if strings.Contains(out, "targets: []") {
		t.Fatalf("yaml included empty targets:\n%s", out)
	}
	if strings.Count(out, "targets:") != 3 {
		t.Fatalf("yaml did not preserve non-empty targets only:\n%s", out)
	}
	if !strings.Contains(out, "- codex") {
		t.Fatalf("yaml omitted non-empty target values:\n%s", out)
	}
}
