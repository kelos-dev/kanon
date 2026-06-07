package core

import (
	"bytes"
	"fmt"
	"strings"
)

// diffPalette holds the ANSI escapes used to colorize diff output. The zero
// value (plainPalette) emits no escapes; the caller decides when to colorize
// (typically only when writing to a terminal), so core never inspects the
// terminal itself.
type diffPalette struct {
	add        string
	del        string
	context    string
	hunk       string
	fileHeader string
	conflict   string
	reset      string
}

func (p diffPalette) paint(code, s string) string {
	if code == "" {
		return s
	}
	return code + s + p.reset
}

var plainPalette = diffPalette{}

var colorPalette = diffPalette{
	add:        "\x1b[32m",   // green
	del:        "\x1b[31m",   // red
	hunk:       "\x1b[36m",   // cyan
	fileHeader: "\x1b[1m",    // bold
	conflict:   "\x1b[1;31m", // bold red
	reset:      "\x1b[0m",
}

// FormatPlanDiff renders a plan as plain unified-diff text.
func FormatPlanDiff(plan *ApplyPlan) string {
	return formatPlanDiff(plan, plainPalette)
}

// FormatPlanDiffColored renders a plan as unified-diff text with ANSI color,
// for output to a terminal.
func FormatPlanDiffColored(plan *ApplyPlan) string {
	return formatPlanDiff(plan, colorPalette)
}

func formatPlanDiff(plan *ApplyPlan, p diffPalette) string {
	var out strings.Builder
	for _, conflict := range plan.Conflicts {
		out.WriteString(p.paint(p.conflict, fmt.Sprintf("CONFLICT %s (%s): %s", conflict.Path, conflict.Agent, conflict.Reason)))
		out.WriteByte('\n')
	}
	for _, change := range plan.Changes {
		if change.Action == "delete" {
			out.WriteString(p.paint(p.del, fmt.Sprintf("DELETE %s (%s)", change.Path, change.Agent)))
			out.WriteByte('\n')
			continue
		}
		out.WriteString(renderUnified(DiffFileForChange(change), p))
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
