package core

import (
	"regexp"
	"strings"
	"testing"
)

func hunkHeaders(df DiffFile) []string {
	var headers []string
	for _, h := range df.Hunks {
		headers = append(headers, HunkHeader(h))
	}
	return headers
}

func countKinds(df DiffFile) (add, del, ctx int) {
	for _, h := range df.Hunks {
		for _, ln := range h.Lines {
			switch ln.Kind {
			case DiffAdd:
				add++
			case DiffDelete:
				del++
			case DiffContext:
				ctx++
			}
		}
	}
	return add, del, ctx
}

func TestDiffHunksCreateEmitsSingleAddHunk(t *testing.T) {
	df := DiffHunks("/x", nil, []byte("a\nb\nc\n"))
	if !df.IsCreate {
		t.Fatalf("expected IsCreate for empty old side")
	}
	if got := hunkHeaders(df); len(got) != 1 || got[0] != "@@ -0,0 +1,3 @@" {
		t.Fatalf("unexpected headers: %v", got)
	}
	add, del, ctx := countKinds(df)
	if add != 3 || del != 0 || ctx != 0 {
		t.Fatalf("expected 3 adds only, got add=%d del=%d ctx=%d", add, del, ctx)
	}
}

func TestDiffHunksDeleteAllRemoved(t *testing.T) {
	df := DiffHunks("/x", []byte("a\nb\nc\n"), nil)
	if !df.IsDelete {
		t.Fatalf("expected IsDelete for empty new side")
	}
	if got := hunkHeaders(df); len(got) != 1 || got[0] != "@@ -1,3 +0,0 @@" {
		t.Fatalf("unexpected headers: %v", got)
	}
	add, del, ctx := countKinds(df)
	if add != 0 || del != 3 || ctx != 0 {
		t.Fatalf("expected 3 deletes only, got add=%d del=%d ctx=%d", add, del, ctx)
	}
}

func TestDiffHunksContextWindow(t *testing.T) {
	// Twenty identical lines with a single mid-file edit should yield exactly
	// three context lines on each side of the change.
	var oldB, newB strings.Builder
	for i := 1; i <= 20; i++ {
		line := "line" + itoa(i) + "\n"
		oldB.WriteString(line)
		if i == 10 {
			newB.WriteString("CHANGED\n")
		} else {
			newB.WriteString(line)
		}
	}
	df := DiffHunks("/x", []byte(oldB.String()), []byte(newB.String()))
	if len(df.Hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(df.Hunks))
	}
	add, del, ctx := countKinds(df)
	if add != 1 || del != 1 {
		t.Fatalf("expected one add and one delete, got add=%d del=%d", add, del)
	}
	if ctx != 6 {
		t.Fatalf("expected 6 context lines (3 each side), got %d", ctx)
	}
	if got := hunkHeaders(df)[0]; got != "@@ -7,7 +7,7 @@" {
		t.Fatalf("unexpected header: %q", got)
	}
}

func TestDiffHunksMergesAdjacentChanges(t *testing.T) {
	// Two edits within 2*context lines of each other collapse into one hunk.
	old := "a\nb\nc\nd\ne\nf\ng\n"
	neu := "A\nb\nc\nd\ne\nf\nG\n"
	df := DiffHunks("/x", []byte(old), []byte(neu))
	if len(df.Hunks) != 1 {
		t.Fatalf("expected 1 merged hunk, got %d: %v", len(df.Hunks), hunkHeaders(df))
	}
}

func TestDiffHunksSplitsDistantChanges(t *testing.T) {
	// Two edits far apart produce two independent hunks.
	var oldB, newB strings.Builder
	for i := 1; i <= 40; i++ {
		line := "line" + itoa(i) + "\n"
		oldB.WriteString(line)
		switch i {
		case 5:
			newB.WriteString("FIRST\n")
		case 35:
			newB.WriteString("SECOND\n")
		default:
			newB.WriteString(line)
		}
	}
	df := DiffHunks("/x", []byte(oldB.String()), []byte(newB.String()))
	if len(df.Hunks) != 2 {
		t.Fatalf("expected 2 hunks, got %d: %v", len(df.Hunks), hunkHeaders(df))
	}
}

func TestDiffHunksBinaryDetected(t *testing.T) {
	df := DiffHunks("/x", []byte("text\n"), []byte("bin\x00ary"))
	if !df.Binary {
		t.Fatalf("expected Binary=true for NUL-containing data")
	}
	if len(df.Hunks) != 0 {
		t.Fatalf("expected no hunks for binary file, got %d", len(df.Hunks))
	}
	if got := RenderUnified(df); got != "Binary files /x differ\n" {
		t.Fatalf("unexpected binary render: %q", got)
	}
}

func TestDiffHunksNoChangeYieldsNoHunks(t *testing.T) {
	df := DiffHunks("/x", []byte("same\n"), []byte("same\n"))
	if len(df.Hunks) != 0 {
		t.Fatalf("expected no hunks for identical content, got %d", len(df.Hunks))
	}
}

func TestDiffHunksLineEndingOnlyChangeIsReported(t *testing.T) {
	df := DiffHunks("/x", []byte("a\r\nb\r\n"), []byte("a\nb\n"))
	if !df.LineEndingOnly {
		t.Fatalf("expected line-ending-only change")
	}
	if len(df.Hunks) != 0 {
		t.Fatalf("line-ending-only change should not emit text hunks, got %d", len(df.Hunks))
	}
	out := RenderUnified(df)
	if !strings.Contains(out, "Line endings differ") {
		t.Fatalf("expected line-ending notice in rendered diff:\n%s", out)
	}
}

func TestDiffHunksSingleLineCountOmitted(t *testing.T) {
	// A one-line range drops the ",count" suffix, matching git.
	df := DiffHunks("/x", []byte("only\n"), []byte("changed\n"))
	if got := hunkHeaders(df)[0]; got != "@@ -1 +1 @@" {
		t.Fatalf("unexpected header: %q", got)
	}
}

func TestRenderUnifiedMatchesGitShape(t *testing.T) {
	df := DiffHunks("/path/to/file", []byte("a\nb\nc\n"), []byte("a\nB\nc\n"))
	out := RenderUnified(df)
	if !strings.HasPrefix(out, "--- /path/to/file\n+++ /path/to/file\n") {
		t.Fatalf("missing file headers: %q", out)
	}
	header := regexp.MustCompile(`(?m)^@@ -\d+(,\d+)? \+\d+(,\d+)? @@$`)
	if !header.MatchString(out) {
		t.Fatalf("no valid @@ header in:\n%s", out)
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		switch line[:1] {
		case "-", "+", " ", "@":
			// ok
		default:
			t.Fatalf("unexpected line prefix in diff: %q", line)
		}
	}
}

func TestRenderUnifiedCreateUsesDevNull(t *testing.T) {
	out := RenderUnified(DiffHunks("/x", nil, []byte("hi\n")))
	if !strings.HasPrefix(out, "--- /dev/null\n+++ /x\n") {
		t.Fatalf("expected /dev/null old label for create: %q", out)
	}
}

func TestRenderUnifiedEmptyFileUpdateUsesPathLabels(t *testing.T) {
	out := RenderUnified(DiffHunks("/x", []byte{}, []byte("hi\n")))
	if !strings.HasPrefix(out, "--- /x\n+++ /x\n") {
		t.Fatalf("expected path labels for empty existing file update: %q", out)
	}
	if strings.Contains(out, "/dev/null") {
		t.Fatalf("empty existing file must not be rendered as absent: %q", out)
	}
	if !strings.Contains(out, "@@ -0,0 +1 @@") {
		t.Fatalf("expected empty-old hunk header: %q", out)
	}
}

func TestDiffFileForChangeKeepsEmptyUpdateDistinctFromDelete(t *testing.T) {
	df := DiffFileForChange(FileChange{
		Path:     "/x",
		Action:   "update",
		Existing: []byte("hi\n"),
		File:     RenderedFile{Content: nil},
	})
	out := RenderUnified(df)
	if !strings.HasPrefix(out, "--- /x\n+++ /x\n") {
		t.Fatalf("expected path labels for truncate update: %q", out)
	}
	if strings.Contains(out, "/dev/null") {
		t.Fatalf("empty desired file must not be rendered as absent for update: %q", out)
	}
	if !strings.Contains(out, "@@ -1 +0,0 @@") {
		t.Fatalf("expected empty-new hunk header: %q", out)
	}
}

func TestFormatPlanDiffUsesHunks(t *testing.T) {
	plan := &ApplyPlan{
		Changes: []FileChange{{
			Agent:    "claude",
			Path:     "/home/u/.claude/CLAUDE.md",
			Action:   "update",
			Existing: []byte("old line\n"),
			File:     RenderedFile{Content: []byte("new line\n")},
		}},
	}
	out := FormatPlanDiff(plan)
	if !strings.Contains(out, "@@ -1 +1 @@") {
		t.Fatalf("expected real hunk header in FormatPlanDiff output:\n%s", out)
	}
	if !strings.Contains(out, "-old line") || !strings.Contains(out, "+new line") {
		t.Fatalf("expected +/- lines in output:\n%s", out)
	}
}

func TestFormatPlanDiffReportsLineEndingOnlyChange(t *testing.T) {
	plan := &ApplyPlan{
		Changes: []FileChange{{
			Agent:    "claude",
			Path:     "/home/u/.claude/CLAUDE.md",
			Action:   "update",
			Existing: []byte("a\r\nb\r\n"),
			File:     RenderedFile{Content: []byte("a\nb\n")},
		}},
	}
	out := FormatPlanDiff(plan)
	if !strings.Contains(out, "Line endings differ") {
		t.Fatalf("expected line-ending notice in plan diff:\n%s", out)
	}
	if strings.Contains(out, "No changes") {
		t.Fatalf("line-ending-only update must not be reported as no changes:\n%s", out)
	}
}

func TestFormatPlanDiffColoredAddsAnsi(t *testing.T) {
	plan := &ApplyPlan{
		Changes: []FileChange{{
			Agent:    "claude",
			Path:     "/home/u/.claude/CLAUDE.md",
			Action:   "update",
			Existing: []byte("old line\n"),
			File:     RenderedFile{Content: []byte("new line\n")},
		}},
		Conflicts: []FileConflict{{Agent: "codex", Path: "/x", Reason: "unmanaged"}},
	}

	colored := FormatPlanDiffColored(plan)
	if !strings.Contains(colored, "\x1b[32m+new line\x1b[0m") {
		t.Fatalf("expected green-wrapped add line in colored output:\n%q", colored)
	}
	if !strings.Contains(colored, "\x1b[31m-old line\x1b[0m") {
		t.Fatalf("expected red-wrapped delete line in colored output:\n%q", colored)
	}
	if !strings.Contains(colored, "\x1b[1;31mCONFLICT") {
		t.Fatalf("expected colored conflict line:\n%q", colored)
	}

	plain := FormatPlanDiff(plan)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain output must contain no ANSI escapes:\n%q", plain)
	}
	if !strings.Contains(plain, "+new line") || !strings.Contains(plain, "-old line") {
		t.Fatalf("plain output missing diff lines:\n%q", plain)
	}
}

// itoa avoids importing strconv just for test fixtures.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
