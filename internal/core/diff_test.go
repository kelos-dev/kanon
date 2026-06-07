package core

import (
	"strings"
	"testing"
)

func TestFormatPlanDiffShowsLineEndingOnlyChange(t *testing.T) {
	plan := &ApplyPlan{Changes: []FileChange{{
		Agent:    AgentCodex,
		Path:     "/tmp/AGENTS.md",
		Action:   "update",
		Existing: []byte("one\r\ntwo\r\n"),
		File: RenderedFile{
			Agent:   AgentCodex,
			Path:    "/tmp/AGENTS.md",
			Content: []byte("one\ntwo\n"),
			Mode:    0o644,
		},
	}}}

	diff := FormatPlanDiff(plan)
	if strings.Contains(diff, "No changes.") {
		t.Fatalf("line-ending-only change was hidden:\n%s", diff)
	}
	for _, want := range []string{"/tmp/AGENTS.md", "content differs only by line endings"} {
		if !strings.Contains(diff, want) {
			t.Fatalf("diff missing %q:\n%s", want, diff)
		}
	}
}
