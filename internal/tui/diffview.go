package tui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/kelos-dev/kanon/internal/core"
)

// maxDiffLines caps how many diff lines are colorized for one file, so a huge
// file does not stall the render. The remainder is summarized.
const maxDiffLines = 5000

// maxDiffCells caps the line-comparison matrix used by the TUI preview before
// it asks core to compute an LCS diff.
const maxDiffCells = 2_000_000

const binarySniffBytes = 8000

func renderChangeDiff(st Styles, change core.FileChange) string {
	if oldLines, newLines, ok := hugeUpdateDiff(change); ok {
		df := core.DiffFile{Path: change.Path}
		msg := fmt.Sprintf("Diff too large to render interactively (%d x %d lines). Use `kanon diff` for full output.", oldLines, newLines)
		return diffHeader(st, df) + "\n" + st.DiffMeta.Render(msg)
	}
	return renderDiff(st, core.DiffFileForChange(change))
}

func hugeUpdateDiff(change core.FileChange) (int, int, bool) {
	if change.Action != "update" {
		return 0, 0, false
	}
	if looksBinary(change.Existing) || looksBinary(change.File.Content) {
		return 0, 0, false
	}
	oldLines := approxLineCount(change.Existing)
	newLines := approxLineCount(change.File.Content)
	if oldLines == 0 || newLines == 0 {
		return oldLines, newLines, false
	}
	return oldLines, newLines, oldLines > maxDiffCells/newLines
}

func looksBinary(data []byte) bool {
	if len(data) > binarySniffBytes {
		data = data[:binarySniffBytes]
	}
	return bytes.IndexByte(data, 0) >= 0
}

func approxLineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	lines := bytes.Count(data, []byte{'\n'})
	if data[len(data)-1] != '\n' {
		lines++
	}
	return lines
}

// renderDiff turns a structured DiffFile into colorized text for the viewport.
// Add/delete lines use foreground colors (not backgrounds) so they never bleed
// across the terminal width, and each line's trailing newline is stripped
// before styling for the same reason.
func renderDiff(st Styles, df core.DiffFile) string {
	if df.Binary {
		return st.DiffMeta.Render("Binary files differ")
	}
	if df.LineEndingOnly {
		return diffHeader(st, df) + "\n" + st.DiffMeta.Render("Line endings differ; content is otherwise unchanged.")
	}
	if len(df.Hunks) == 0 && df.IsCreate {
		return diffHeader(st, df) + "\n" + st.DiffMeta.Render("Empty file will be created.")
	}
	if len(df.Hunks) == 0 && df.IsDelete {
		return diffHeader(st, df) + "\n" + st.DiffMeta.Render("Empty file will be deleted.")
	}
	if len(df.Hunks) == 0 {
		return st.DiffMeta.Render("No content changes.")
	}

	var b strings.Builder
	b.WriteString(diffHeader(st, df) + "\n")

	total := countLines(df)
	lines := 0
	for _, h := range df.Hunks {
		b.WriteString(st.DiffHunkHeader.Render(core.HunkHeader(h)) + "\n")
		for _, ln := range h.Lines {
			if lines >= maxDiffLines {
				b.WriteString(st.DiffMeta.Render(fmt.Sprintf("… diff truncated (%d more lines)", total-lines)))
				return b.String()
			}
			text := strings.TrimRight(ln.Text, "\n")
			switch ln.Kind {
			case core.DiffAdd:
				b.WriteString(st.DiffAdd.Render("+" + text))
			case core.DiffDelete:
				b.WriteString(st.DiffDelete.Render("-" + text))
			default:
				b.WriteString(st.DiffContext.Render(" " + text))
			}
			b.WriteByte('\n')
			lines++
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func diffHeader(st Styles, df core.DiffFile) string {
	oldLabel, newLabel := df.Path, df.Path
	if df.IsCreate {
		oldLabel = "/dev/null"
	}
	if df.IsDelete {
		newLabel = "/dev/null"
	}
	return st.DiffFileHeader.Render("--- "+oldLabel) + "\n" + st.DiffFileHeader.Render("+++ "+newLabel)
}

func countLines(df core.DiffFile) int {
	n := 0
	for _, h := range df.Hunks {
		n += len(h.Lines)
	}
	return n
}
