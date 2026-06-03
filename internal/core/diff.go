package core

import (
	"bytes"
	"fmt"
	"strings"
)

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
	out.WriteString(fmt.Sprintf("--- %s\n+++ %s\n@@\n", path, path))
	oldLines := splitLines(oldData)
	newLines := splitLines(newData)
	lcs := lineLCS(oldLines, newLines)
	i, j := 0, 0
	for _, pair := range lcs {
		for i < pair[0] {
			out.WriteString("-")
			out.WriteString(oldLines[i])
			i++
		}
		for j < pair[1] {
			out.WriteString("+")
			out.WriteString(newLines[j])
			j++
		}
		out.WriteString(" ")
		out.WriteString(oldLines[i])
		i++
		j++
	}
	for i < len(oldLines) {
		out.WriteString("-")
		out.WriteString(oldLines[i])
		i++
	}
	for j < len(newLines) {
		out.WriteString("+")
		out.WriteString(newLines[j])
		j++
	}
	if !strings.HasSuffix(out.String(), "\n") {
		out.WriteByte('\n')
	}
	return out.String()
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
