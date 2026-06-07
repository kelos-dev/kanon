package core

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// DiffLineKind classifies a single line in a unified diff.
type DiffLineKind int

const (
	DiffContext DiffLineKind = iota
	DiffAdd
	DiffDelete
)

// DiffLine is one line of a hunk. Text keeps the original line content,
// including its trailing newline when the source line had one; the leading
// +/-/space marker is supplied by the renderer, not stored here.
type DiffLine struct {
	Kind DiffLineKind
	Text string
}

// DiffHunk is a contiguous run of changes plus surrounding context, matching
// the @@ -OldStart,OldLines +NewStart,NewLines @@ shape of a unified diff. A
// Start of 0 means the side contributes no lines (a pure insertion or deletion).
type DiffHunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []DiffLine
}

// DiffFile is the structured diff between two versions of a file. The TUI
// colorizes it line by line; RenderUnified turns it into plain diff text.
type DiffFile struct {
	Path           string
	Hunks          []DiffHunk
	Binary         bool
	LineEndingOnly bool
	IsCreate       bool // old side absent
	IsDelete       bool // new side absent
}

// diffContextLines is the number of unchanged lines kept around each change,
// matching git's default of 3.
const diffContextLines = 3

// diffOp is an intermediate per-line record carrying the old/new line numbers
// used to compute hunk headers. oldNo/newNo are 1-based, or 0 when the line is
// absent on that side (adds have no old number, deletes no new number).
type diffOp struct {
	kind  DiffLineKind
	text  string
	oldNo int
	newNo int
}

// DiffHunks computes a git-style unified diff between oldData and newData,
// grouped into hunks with diffContextLines of context. A nil side means that
// file side is absent; an empty but non-nil slice means the file exists and is
// empty.
func DiffHunks(path string, oldData, newData []byte) DiffFile {
	df := DiffFile{
		Path:     path,
		IsCreate: oldData == nil,
		IsDelete: newData == nil,
	}
	if isBinary(oldData) || isBinary(newData) {
		df.Binary = true
		return df
	}
	ops := diffOps(splitLines(oldData), splitLines(newData))
	df.Hunks = groupHunks(ops, diffContextLines)
	if len(df.Hunks) == 0 && !bytes.Equal(oldData, newData) {
		df.LineEndingOnly = true
	}
	return df
}

// DiffFileForChange preserves the plan action while preparing a structured
// diff. DiffHunks uses nil to mean "absent", so empty file contents must be
// normalized to non-nil slices for update/create paths.
func DiffFileForChange(change FileChange) DiffFile {
	oldData := change.Existing
	newData := change.File.Content
	switch change.Action {
	case "create":
		oldData = nil
		if newData == nil {
			newData = []byte{}
		}
	case "delete":
		if oldData == nil {
			oldData = []byte{}
		}
		newData = nil
	default:
		if oldData == nil {
			oldData = []byte{}
		}
		if newData == nil {
			newData = []byte{}
		}
	}
	return DiffHunks(change.Path, oldData, newData)
}

// diffOps walks the line LCS to produce a flat stream of context/add/delete
// ops with their line numbers, mirroring unifiedDiff's traversal order.
func diffOps(oldLines, newLines []string) []diffOp {
	var ops []diffOp
	i, j := 0, 0
	for _, pair := range lineLCS(oldLines, newLines) {
		for i < pair[0] {
			ops = append(ops, diffOp{kind: DiffDelete, text: oldLines[i], oldNo: i + 1})
			i++
		}
		for j < pair[1] {
			ops = append(ops, diffOp{kind: DiffAdd, text: newLines[j], newNo: j + 1})
			j++
		}
		ops = append(ops, diffOp{kind: DiffContext, text: oldLines[i], oldNo: i + 1, newNo: j + 1})
		i++
		j++
	}
	for i < len(oldLines) {
		ops = append(ops, diffOp{kind: DiffDelete, text: oldLines[i], oldNo: i + 1})
		i++
	}
	for j < len(newLines) {
		ops = append(ops, diffOp{kind: DiffAdd, text: newLines[j], newNo: j + 1})
		j++
	}
	return ops
}

// groupHunks slices the op stream into hunks. Each changed op pulls in up to
// `context` neighbouring context lines; runs whose context windows touch merge
// into one hunk, and runs separated by more than 2*context context lines split
// into separate hunks.
func groupHunks(ops []diffOp, context int) []DiffHunk {
	n := len(ops)
	include := make([]bool, n)
	any := false
	for idx, op := range ops {
		if op.kind == DiffContext {
			continue
		}
		any = true
		lo := idx - context
		if lo < 0 {
			lo = 0
		}
		hi := idx + context
		if hi > n-1 {
			hi = n - 1
		}
		for k := lo; k <= hi; k++ {
			include[k] = true
		}
	}
	if !any {
		return nil
	}
	var hunks []DiffHunk
	for k := 0; k < n; {
		if !include[k] {
			k++
			continue
		}
		start := k
		for k < n && include[k] {
			k++
		}
		hunks = append(hunks, buildHunk(ops[start:k]))
	}
	return hunks
}

func buildHunk(ops []diffOp) DiffHunk {
	h := DiffHunk{Lines: make([]DiffLine, 0, len(ops))}
	for _, op := range ops {
		h.Lines = append(h.Lines, DiffLine{Kind: op.kind, Text: op.text})
		switch op.kind {
		case DiffContext:
			h.OldLines++
			h.NewLines++
			if h.OldStart == 0 {
				h.OldStart = op.oldNo
			}
			if h.NewStart == 0 {
				h.NewStart = op.newNo
			}
		case DiffDelete:
			h.OldLines++
			if h.OldStart == 0 {
				h.OldStart = op.oldNo
			}
		case DiffAdd:
			h.NewLines++
			if h.NewStart == 0 {
				h.NewStart = op.newNo
			}
		}
	}
	return h
}

// RenderUnified renders a DiffFile as plain unified-diff text, the way the CLI
// commands print it. The TUI uses the structured DiffFile directly instead.
func RenderUnified(df DiffFile) string {
	return renderUnified(df, plainPalette)
}

func renderUnified(df DiffFile, p diffPalette) string {
	var out strings.Builder
	if df.Binary {
		fmt.Fprintf(&out, "Binary files %s differ\n", df.Path)
		return out.String()
	}
	oldLabel, newLabel := df.Path, df.Path
	if df.IsCreate {
		oldLabel = "/dev/null"
	}
	if df.IsDelete {
		newLabel = "/dev/null"
	}
	out.WriteString(p.paint(p.fileHeader, "--- "+oldLabel) + "\n")
	out.WriteString(p.paint(p.fileHeader, "+++ "+newLabel) + "\n")
	if df.LineEndingOnly {
		out.WriteString(p.paint(p.hunk, "Line endings differ; content differs only by line endings.") + "\n")
		return out.String()
	}
	for _, h := range df.Hunks {
		out.WriteString(p.paint(p.hunk, HunkHeader(h)) + "\n")
		for _, ln := range h.Lines {
			text := string(lineMarker(ln.Kind)) + strings.TrimRight(ln.Text, "\n")
			switch ln.Kind {
			case DiffAdd:
				out.WriteString(p.paint(p.add, text))
			case DiffDelete:
				out.WriteString(p.paint(p.del, text))
			default:
				out.WriteString(p.paint(p.context, text))
			}
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// HunkHeader formats the @@ -l,s +l,s @@ line for a hunk.
func HunkHeader(h DiffHunk) string {
	return fmt.Sprintf("@@ -%s +%s @@", rangeSpec(h.OldStart, h.OldLines), rangeSpec(h.NewStart, h.NewLines))
}

// rangeSpec renders one side of a hunk header. Git omits the count when it is
// exactly one line (e.g. "-5" rather than "-5,1").
func rangeSpec(start, count int) string {
	if count == 1 {
		return strconv.Itoa(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func lineMarker(k DiffLineKind) byte {
	switch k {
	case DiffAdd:
		return '+'
	case DiffDelete:
		return '-'
	default:
		return ' '
	}
}

// isBinary reports whether data looks binary, using git's heuristic of a NUL
// byte within the first several kilobytes.
func isBinary(data []byte) bool {
	const sniff = 8000
	if len(data) > sniff {
		data = data[:sniff]
	}
	return bytes.IndexByte(data, 0) >= 0
}
