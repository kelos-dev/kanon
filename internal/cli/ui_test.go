package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/kelos-dev/kanon/internal/core"
	"github.com/kelos-dev/kanon/internal/tui"
)

const (
	uiPathA        = "/home/u/.claude/CLAUDE.md"
	uiPathB        = "/home/u/.codex/AGENTS.md"
	uiConflictPath = "/home/u/.claude/skills/review/SKILL.md"
)

func TestUICommandRefusesNonTTY(t *testing.T) {
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"ui"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected ui to refuse a non-TTY stdout")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("expected a terminal-related error, got %v", err)
	}
}

func TestSelectedPlanFiltersFreshPlanToSelection(t *testing.T) {
	plan := &core.ApplyPlan{
		Changes: []core.FileChange{
			{Path: uiPathA, Action: "update", ExistingHash: "old-a", DesiredHash: "new-a"},
			{Path: uiPathB, Action: "update", ExistingHash: "old-b", DesiredHash: "new-b"},
		},
	}
	filtered, err := selectedPlan(plan, map[string]tui.SelectedChange{
		uiPathB: {Path: uiPathB, Action: "update", ExistingHash: "old-b", DesiredHash: "new-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Changes) != 1 || filtered.Changes[0].Path != uiPathB {
		t.Fatalf("expected only selected fresh change, got %#v", filtered.Changes)
	}
}

func TestSelectedPlanBlocksSelectedConflict(t *testing.T) {
	plan := &core.ApplyPlan{
		Changes:   []core.FileChange{{Path: uiPathA, Action: "update"}},
		Conflicts: []core.FileConflict{{Path: uiConflictPath, Reason: "changed outside kanon"}},
	}
	filtered, err := selectedPlan(plan, map[string]tui.SelectedChange{
		uiConflictPath: {Path: uiConflictPath, Action: "update"},
	})
	if err == nil {
		t.Fatalf("expected selected conflict to fail")
	}
	if len(filtered.Conflicts) != 1 || filtered.Conflicts[0].Path != uiConflictPath {
		t.Fatalf("expected selected conflict in filtered plan, got %#v", filtered.Conflicts)
	}
}

func TestSelectedPlanBlocksChangedReviewedHash(t *testing.T) {
	plan := &core.ApplyPlan{
		Changes: []core.FileChange{{
			Path:         uiPathA,
			Action:       "update",
			ExistingHash: "new-external-edit",
			DesiredHash:  "desired",
		}},
	}
	_, err := selectedPlan(plan, map[string]tui.SelectedChange{
		uiPathA: {Path: uiPathA, Action: "update", ExistingHash: "reviewed", DesiredHash: "desired"},
	})
	if err == nil {
		t.Fatalf("expected changed reviewed hash to fail")
	}
	if !strings.Contains(err.Error(), "changed since it was reviewed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSelectedPlanBlocksMissingSelectedPath(t *testing.T) {
	plan := &core.ApplyPlan{
		Changes: []core.FileChange{
			{Path: uiPathA, Action: "update", ExistingHash: "old-a", DesiredHash: "new-a"},
		},
	}
	_, err := selectedPlan(plan, map[string]tui.SelectedChange{
		uiPathA: {Path: uiPathA, Action: "update", ExistingHash: "old-a", DesiredHash: "new-a"},
		uiPathB: {Path: uiPathB, Action: "update", ExistingHash: "old-b", DesiredHash: "new-b"},
	})
	if err == nil {
		t.Fatalf("expected missing selected path to fail")
	}
	if !strings.Contains(err.Error(), "no longer has a pending change") {
		t.Fatalf("unexpected error: %v", err)
	}
}
