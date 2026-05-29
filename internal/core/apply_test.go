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
