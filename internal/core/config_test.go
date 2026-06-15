package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOverlay(t *testing.T) {
	dir := t.TempDir()
	overlayYAML := `version: 1
instructions:
  files:
    - docs/extra.md
skills:
  - name: lint
`
	overlayPath := filepath.Join(dir, "kanon.yaml")
	if err := os.WriteFile(overlayPath, []byte(overlayYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, root, err := LoadConfigOverlay(overlayPath)
	if err != nil {
		t.Fatalf("LoadConfigOverlay: %v", err)
	}
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
	if len(cfg.Instructions.Files) != 1 {
		t.Fatalf("expected 1 instruction file, got %d", len(cfg.Instructions.Files))
	}
	want := filepath.Join(dir, "docs/extra.md")
	if cfg.Instructions.Files[0] != want {
		t.Errorf("instruction path = %q, want %q", cfg.Instructions.Files[0], want)
	}
	if len(cfg.Skills) != 1 || cfg.Skills[0].Name != "lint" {
		t.Errorf("unexpected skills: %+v", cfg.Skills)
	}
	// skill path should be rebased to <dir>/skills/lint
	wantSkillPath := filepath.Join(dir, "skills", "lint")
	if cfg.Skills[0].Path != wantSkillPath {
		t.Errorf("skill path = %q, want %q", cfg.Skills[0].Path, wantSkillPath)
	}
}

func TestLoadConfigOverlayAbsPathUnchanged(t *testing.T) {
	dir := t.TempDir()
	abs := "/absolute/path/to/file.md"
	overlayYAML := "version: 1\ninstructions:\n  files:\n    - " + abs + "\n"
	overlayPath := filepath.Join(dir, "kanon.yaml")
	if err := os.WriteFile(overlayPath, []byte(overlayYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := LoadConfigOverlay(overlayPath)
	if err != nil {
		t.Fatalf("LoadConfigOverlay: %v", err)
	}
	if cfg.Instructions.Files[0] != abs {
		t.Errorf("abs path was modified: got %q", cfg.Instructions.Files[0])
	}
}

func TestMergeConfigOverlayAppendsInstructions(t *testing.T) {
	base := &Config{
		Version: 1,
		Instructions: Instructions{Files: []string{"base.md"}},
	}
	overlay := &Config{
		Version: 1,
		Instructions: Instructions{Files: []string{"extra.md"}},
	}
	merged := MergeConfigOverlay(base, overlay)
	if len(merged.Instructions.Files) != 2 {
		t.Fatalf("expected 2 instruction files, got %d: %v", len(merged.Instructions.Files), merged.Instructions.Files)
	}
}

func TestMergeConfigOverlayMergesMCPServers(t *testing.T) {
	base := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"existing": {Command: "cmd-a"},
		}},
	}
	overlay := &Config{
		Version: 1,
		MCP: MCPConfig{Servers: map[string]MCPServer{
			"new": {Command: "cmd-b"},
		}},
	}
	merged := MergeConfigOverlay(base, overlay)
	if len(merged.MCP.Servers) != 2 {
		t.Fatalf("expected 2 MCP servers, got %d", len(merged.MCP.Servers))
	}
	if _, ok := merged.MCP.Servers["existing"]; !ok {
		t.Error("base MCP server 'existing' lost after merge")
	}
	if _, ok := merged.MCP.Servers["new"]; !ok {
		t.Error("overlay MCP server 'new' missing after merge")
	}
}

func TestMergeConfigOverlaySkillByName(t *testing.T) {
	base := &Config{
		Version: 1,
		Skills: []Skill{
			{Name: "review", Path: "/base/skills/review"},
			{Name: "lint", Path: "/base/skills/lint"},
		},
	}
	overlay := &Config{
		Version: 1,
		Skills: []Skill{
			{Name: "review", Path: "/overlay/skills/review"},
			{Name: "format", Path: "/overlay/skills/format"},
		},
	}
	merged := MergeConfigOverlay(base, overlay)
	if len(merged.Skills) != 3 {
		t.Fatalf("expected 3 skills, got %d: %+v", len(merged.Skills), merged.Skills)
	}
	// overlay wins for 'review'
	for _, s := range merged.Skills {
		if s.Name == "review" && s.Path != "/overlay/skills/review" {
			t.Errorf("review skill not overridden: path = %q", s.Path)
		}
	}
}

func TestMergeConfigOverlayNilHandling(t *testing.T) {
	base := &Config{Version: 1}
	if got := MergeConfigOverlay(nil, base); got != base {
		t.Error("MergeConfigOverlay(nil, x) should return x")
	}
	if got := MergeConfigOverlay(base, nil); got != base {
		t.Error("MergeConfigOverlay(x, nil) should return x")
	}
}
