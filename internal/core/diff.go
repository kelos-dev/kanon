package core

import (
	"bytes"
	"fmt"
	"strings"
)

const diffContextLines = 3

func FormatPlanDiff(plan *ApplyPlan) string {
	var out strings.Builder
	for _, conflict := range plan.Conflicts {
		out.WriteString(fmt.Sprintf("CONFLICT %s (%s): %s\n", conflict.Path, conflict.Agent, conflict.Reason))
	}
	for _, change := range plan.Changes {
		if change.Action == "delete" {
			out.WriteString(fmt.Sprintf("DELETE %s (%s)\n", change.Path, change.Agent))
			continue
		}
		existing := change.Existing
		if change.Action == "create" {
			existing = nil
		}
		out.WriteString(unifiedDiff(change.Path, existing, change.File.Content))
		if !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
	}
	if out.Len() == 0 {
		return "No changes.\n"
	}
	return out.String()
}

func DriftSummary(plan *ApplyPlan) string {
	if len(plan.Changes) == 0 && len(plan.Conflicts) == 0 {
		return "No rendered file drift.\n"
	}
	return fmt.Sprintf("%d change(s), %d conflict(s).\n", len(plan.Changes), len(plan.Conflicts))
}

func unifiedDiff(path string, oldData, newData []byte) string {
	var out strings.Builder
	oldLines := splitLines(oldData)
	newLines := splitLines(newData)
	lines := diffLines(oldLines, newLines)
	hunks := diffHunks(lines, diffContextLines)
	if len(hunks) == 0 {
		return ""
	}
	out.WriteString(fmt.Sprintf("--- %s\n+++ %s\n", path, path))
	for _, hunk := range hunks {
		out.WriteString(fmt.Sprintf("@@ -%s +%s @@\n", diffRange(hunk.oldStart, hunk.oldCount), diffRange(hunk.newStart, hunk.newCount)))
		for _, line := range lines[hunk.start:hunk.end] {
			out.WriteByte(line.kind)
			out.WriteString(line.text)
			if !strings.HasSuffix(line.text, "\n") {
				out.WriteByte('\n')
			}
		}
	}
	if !strings.HasSuffix(out.String(), "\n") {
		out.WriteByte('\n')
	}
	return out.String()
}

type diffLine struct {
	kind    byte
	text    string
	oldLine int
	newLine int
}

type diffHunk struct {
	start    int
	end      int
	oldStart int
	oldCount int
	newStart int
	newCount int
}

func diffLines(oldLines, newLines []string) []diffLine {
	var lines []diffLine
	lcs := lineLCS(oldLines, newLines)
	i, j := 0, 0
	for _, pair := range lcs {
		for i < pair[0] {
			lines = append(lines, diffLine{kind: '-', text: oldLines[i], oldLine: i + 1, newLine: j + 1})
			i++
		}
		for j < pair[1] {
			lines = append(lines, diffLine{kind: '+', text: newLines[j], oldLine: i + 1, newLine: j + 1})
			j++
		}
		lines = append(lines, diffLine{kind: ' ', text: oldLines[i], oldLine: i + 1, newLine: j + 1})
		i++
		j++
	}
	for i < len(oldLines) {
		lines = append(lines, diffLine{kind: '-', text: oldLines[i], oldLine: i + 1, newLine: j + 1})
		i++
	}
	for j < len(newLines) {
		lines = append(lines, diffLine{kind: '+', text: newLines[j], oldLine: i + 1, newLine: j + 1})
		j++
	}
	return lines
}

func diffHunks(lines []diffLine, context int) []diffHunk {
	var changed []int
	for i, line := range lines {
		if line.kind != ' ' {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	var hunks []diffHunk
	start := max(changed[0]-context, 0)
	end := min(changed[0]+context+1, len(lines))
	for _, index := range changed[1:] {
		nextStart := max(index-context, 0)
		nextEnd := min(index+context+1, len(lines))
		if nextStart <= end {
			end = max(end, nextEnd)
			continue
		}
		hunks = append(hunks, newDiffHunk(lines, start, end))
		start, end = nextStart, nextEnd
	}
	hunks = append(hunks, newDiffHunk(lines, start, end))
	return hunks
}

func newDiffHunk(lines []diffLine, start, end int) diffHunk {
	hunk := diffHunk{start: start, end: end}
	for _, line := range lines[start:end] {
		if line.kind != '+' {
			hunk.oldCount++
			if hunk.oldStart == 0 {
				hunk.oldStart = line.oldLine
			}
		}
		if line.kind != '-' {
			hunk.newCount++
			if hunk.newStart == 0 {
				hunk.newStart = line.newLine
			}
		}
	}
	if hunk.oldStart == 0 {
		hunk.oldStart = insertionStart(lines[start:end], true)
	}
	if hunk.newStart == 0 {
		hunk.newStart = insertionStart(lines[start:end], false)
	}
	return hunk
}

func insertionStart(lines []diffLine, old bool) int {
	if len(lines) == 0 {
		return 1
	}
	if old {
		return max(lines[0].oldLine-1, 0)
	}
	return max(lines[0].newLine-1, 0)
}

func diffRange(start, count int) string {
	if count == 1 {
		return fmt.Sprint(start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	raw := strings.SplitAfter(string(data), "\n")
	if raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	return raw
}

func lineLCS(a, b []string) [][2]int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var pairs [][2]int
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] == b[j] {
			pairs = append(pairs, [2]int{i, j})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			i++
		} else {
			j++
		}
	}
	return pairs
}
