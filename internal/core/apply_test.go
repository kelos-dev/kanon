package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanApplyBlocksUnmanagedFileUnlessAdopted(t *testing.T) {
	kanonHome := t.TempDir()
	target := filepath.Join(t.TempDir(), "AGENTS.md")
	writeTestFile(t, target, []byte("hand edited\n"))
	file := RenderedFile{
		Agent:   AgentCodex,
		Path:    target,
		Content: []byte("generated\n"),
		Mode:    0o644,
	}

	plan, _, err := PlanFiles([]RenderedFile{file}, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 {
		t.Fatalf("expected unmanaged-file conflict, got %#v", plan.Conflicts)
	}

	plan, state, err := PlanFiles([]RenderedFile{file}, ApplyOptions{KanonHome: kanonHome, Adopt: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 || len(plan.Changes) != 1 {
		t.Fatalf("expected adopted update, got changes=%d conflicts=%d", len(plan.Changes), len(plan.Conflicts))
	}
	if err := ApplyFiles(plan, state, ApplyOptions{KanonHome: kanonHome, Adopt: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "generated\n" {
		t.Fatalf("target was not written: %q", data)
	}
	state, err = LoadState(kanonHome)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Files[target].Hash; got != HashBytes(file.Content) {
		t.Fatalf("state hash mismatch: %q", got)
	}
	plan, _, err = PlanFiles([]RenderedFile{file}, ApplyOptions{KanonHome: kanonHome})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 || len(plan.Conflicts) != 0 {
		t.Fatalf("expected idempotent re-plan, got changes=%d conflicts=%d", len(plan.Changes), len(plan.Conflicts))
	}
}

func TestApplyPrunesOrphanedManagedFiles(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	keep := filepath.Join(dest, "keep.md")
	drop := filepath.Join(dest, "drop.md")
	files := []RenderedFile{
		{Agent: AgentClaude, Path: keep, Content: []byte("keep\n"), Mode: 0o644, Prunable: true},
		{Agent: AgentClaude, Path: drop, Content: []byte("drop\n"), Mode: 0o644, Prunable: true},
	}
	opts := ApplyOptions{KanonHome: kanonHome, Agent: AgentAll}
	applyAll(t, files, opts)

	// Re-render without drop.md: it should be planned for deletion and removed.
	plan, state, err := PlanFiles(files[:1], opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Action != "delete" || plan.Changes[0].Path != drop {
		t.Fatalf("expected a single delete for %s, got %#v", drop, plan.Changes)
	}
	if err := ApplyFiles(plan, state, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(drop); !os.IsNotExist(err) {
		t.Fatalf("orphaned file was not pruned: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("kept file was removed: %v", err)
	}
	if _, ok := state.Files[drop]; ok {
		t.Fatalf("pruned file still recorded in state")
	}
}

func TestApplyDoesNotPruneCoOwnedFiles(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	config := filepath.Join(dest, "settings.json")
	files := []RenderedFile{
		{Agent: AgentClaude, Path: config, Content: []byte("{}\n"), Mode: 0o644, Prunable: false},
	}
	opts := ApplyOptions{KanonHome: kanonHome, Agent: AgentAll}
	applyAll(t, files, opts)

	// Render nothing: a non-prunable (co-owned) file must be left in place.
	plan, _, err := PlanFiles(nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("co-owned file should not be pruned, got %#v", plan.Changes)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("co-owned file was removed: %v", err)
	}
}

func TestApplyPruneRespectsAgentScope(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	codexFile := filepath.Join(dest, "codex.md")
	claudeFile := filepath.Join(dest, "claude.md")
	files := []RenderedFile{
		{Agent: AgentCodex, Path: codexFile, Content: []byte("codex\n"), Mode: 0o644, Prunable: true},
		{Agent: AgentClaude, Path: claudeFile, Content: []byte("claude\n"), Mode: 0o644, Prunable: true},
	}
	applyAll(t, files, ApplyOptions{KanonHome: kanonHome, Agent: AgentAll})

	// Apply scoped to claude with no claude files: the codex file is out of
	// scope and must survive even though it is not rendered.
	plan, state, err := PlanFiles(nil, ApplyOptions{KanonHome: kanonHome, Agent: AgentClaude})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != claudeFile {
		t.Fatalf("expected only the claude file pruned, got %#v", plan.Changes)
	}
	if err := ApplyFiles(plan, state, ApplyOptions{KanonHome: kanonHome, Agent: AgentClaude}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(codexFile); err != nil {
		t.Fatalf("out-of-scope codex file was pruned: %v", err)
	}
	if _, err := os.Stat(claudeFile); !os.IsNotExist(err) {
		t.Fatalf("in-scope claude file was not pruned: %v", err)
	}
}

func TestApplyPruneRespectsProjectScope(t *testing.T) {
	kanonHome := t.TempDir()
	projectA := t.TempDir()
	projectB := t.TempDir()
	fileA := filepath.Join(projectA, "AGENTS.md")
	fileB := filepath.Join(projectB, "AGENTS.md")
	applyAll(t, []RenderedFile{{Agent: AgentClaude, Path: fileA, Content: []byte("a\n"), Mode: 0o644, Prunable: true}},
		ApplyOptions{KanonHome: kanonHome, Agent: AgentAll, Project: projectA})
	applyAll(t, []RenderedFile{{Agent: AgentClaude, Path: fileB, Content: []byte("b\n"), Mode: 0o644, Prunable: true}},
		ApplyOptions{KanonHome: kanonHome, Agent: AgentAll, Project: projectB})

	// Apply scoped to projectA with nothing rendered: projectB's file is out of
	// scope and must survive even though it is not rendered for this apply.
	opts := ApplyOptions{KanonHome: kanonHome, Agent: AgentAll, Project: projectA}
	plan, state, err := PlanFiles(nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Changes) != 1 || plan.Changes[0].Path != fileA {
		t.Fatalf("expected only projectA file pruned, got %#v", plan.Changes)
	}
	if err := ApplyFiles(plan, state, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fileB); err != nil {
		t.Fatalf("out-of-scope project file was pruned: %v", err)
	}
	if _, err := os.Stat(fileA); !os.IsNotExist(err) {
		t.Fatalf("in-scope project file was not pruned: %v", err)
	}
}

func TestApplyPruneGuardsExternallyModifiedFile(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	orphan := filepath.Join(dest, "AGENTS.md")
	files := []RenderedFile{{Agent: AgentClaude, Path: orphan, Content: []byte("rendered\n"), Mode: 0o644, Prunable: true}}
	opts := ApplyOptions{KanonHome: kanonHome, Agent: AgentAll}
	applyAll(t, files, opts)

	// Edit the managed file outside kanon, then stop rendering it.
	writeTestFile(t, orphan, []byte("hand edited\n"))

	// Without --adopt the external change blocks the prune: it is a conflict and
	// the file is left untouched.
	plan, _, err := PlanFiles(nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Path != orphan {
		t.Fatalf("expected a prune conflict for %s, got conflicts=%#v changes=%#v", orphan, plan.Conflicts, plan.Changes)
	}
	if len(plan.Changes) != 0 {
		t.Fatalf("externally modified orphan should not be deleted without --adopt, got %#v", plan.Changes)
	}

	// With --adopt the conflict becomes a delete and the file is removed.
	adoptOpts := ApplyOptions{KanonHome: kanonHome, Agent: AgentAll, Adopt: true}
	plan, state, err := PlanFiles(nil, adoptOpts)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Conflicts) != 0 || len(plan.Changes) != 1 || plan.Changes[0].Action != "delete" {
		t.Fatalf("expected an adopted delete, got changes=%#v conflicts=%#v", plan.Changes, plan.Conflicts)
	}
	if err := ApplyFiles(plan, state, adoptOpts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("adopted orphan was not pruned: %v", err)
	}
}

func TestApplyPruneRemovesEmptySkillDirectory(t *testing.T) {
	kanonHome := t.TempDir()
	dest := t.TempDir()
	skillFile := filepath.Join(dest, "skills", "demo", "SKILL.md")
	files := []RenderedFile{{Agent: AgentClaude, Path: skillFile, Content: []byte("skill\n"), Mode: 0o644, Prunable: true}}
	opts := ApplyOptions{KanonHome: kanonHome, UserHome: dest, Agent: AgentAll}
	applyAll(t, files, opts)

	// Re-render without the skill: its file is pruned and the now-empty
	// skills/demo directory is cleaned up, leaving the destination root intact.
	plan, state, err := PlanFiles(nil, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, state, opts); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skills", "demo")); !os.IsNotExist(err) {
		t.Fatalf("empty skill directory was not removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "skills")); !os.IsNotExist(err) {
		t.Fatalf("empty skills parent directory was not removed: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("protected destination root was removed: %v", err)
	}
}

func applyAll(t *testing.T, files []RenderedFile, opts ApplyOptions) {
	t.Helper()
	plan, state, err := PlanFiles(files, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyFiles(plan, state, opts); err != nil {
		t.Fatal(err)
	}
}
