package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/kelos-dev/kanon/internal/core"
)

const (
	minWidth  = 60
	minHeight = 16
)

func (m Model) View() string {
	if !m.ready {
		return "Loading…"
	}
	if m.width < minWidth || m.height < minHeight {
		return m.styles.DiffMeta.Render(
			fmt.Sprintf("Terminal too small — need at least %d×%d, have %d×%d.",
				minWidth, minHeight, m.width, m.height))
	}

	l := m.computeLayout()

	left := lipgloss.JoinVertical(lipgloss.Left,
		m.panel("Agents", m.renderAgents(), l.leftW, l.topH, m.focus == focusAgents),
		m.panel("Source", m.renderSource(), l.leftW, l.bottomH, false),
	)
	right := lipgloss.JoinVertical(lipgloss.Left,
		m.panel(m.changesTitle(), m.renderChanges(), l.rightW, l.topH, m.focus == focusChanges),
		m.panel("Diff", m.viewport.View(), l.rightW, l.bottomH, m.focus == focusDiff),
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return lipgloss.JoinVertical(lipgloss.Left, body, m.statusLineView(), m.hintView())
}

func (m Model) panel(title, body string, width, height int, focused bool) string {
	style := m.styles.PanelBlurred
	if focused {
		style = m.styles.PanelFocused
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, m.styles.PanelTitle.Render(title), body)
	return style.Width(width - 2).Height(height - 2).MaxHeight(height).Render(inner)
}

func (m Model) changesTitle() string {
	if m.plan == nil {
		return "Changes"
	}
	return fmt.Sprintf("Changes (%d changed · %d conflict · %d selected)",
		len(m.plan.Changes), len(m.plan.Conflicts), m.selectedCount())
}

func (m Model) renderAgents() string {
	counts := map[string]int{}
	if m.plan != nil {
		for i := range m.plan.Changes {
			counts[m.plan.Changes[i].Agent]++
		}
	}
	var b strings.Builder
	for _, a := range []string{core.AgentClaude, core.AgentCodex} {
		active := m.deps.Agent == core.AgentAll || m.deps.Agent == a
		label := fmt.Sprintf("%s (%d)", a, counts[a])
		if active {
			b.WriteString(m.styles.AgentActive.Render("● " + label))
		} else {
			b.WriteString(m.styles.AgentInactive.Render("○ " + label))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderSource() string {
	status := m.gitStatus
	if status == "" {
		return m.styles.DiffMeta.Render("loading…")
	}
	if status == "clean" {
		return m.styles.OkText.Render("✓ clean")
	}
	if status == "unavailable" {
		return m.styles.DiffMeta.Render("git unavailable")
	}
	return m.styles.StatusUpdate.Render("● dirty") + "\n" + status
}

func (m Model) renderChanges() string {
	items := m.visibleItems()
	if len(items) == 0 {
		if m.filter != "" {
			return m.styles.DiffMeta.Render("No files match filter.")
		}
		return m.styles.DiffMeta.Render("No changes.")
	}
	l := m.computeLayout()
	capacity := l.listRows
	if capacity < 1 {
		capacity = 1
	}
	start := 0
	if m.cursor >= capacity {
		start = m.cursor - capacity + 1
	}
	end := start + capacity
	if end > len(items) {
		end = len(items)
	}
	rowWidth := max(1, l.rightW-2)
	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(m.renderRow(items[i], i == m.cursor, rowWidth))
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderRow(it changeItem, cursor bool, width int) string {
	box := "   "
	if it.kind == kindChange {
		mark := " "
		if m.selected[it.path] {
			mark = "x"
		}
		box = "[" + mark + "]"
	}
	sym, symStyle := m.symbol(it)
	prefix := "  "
	if cursor {
		prefix = "> "
	}
	lead := fmt.Sprintf("%s%s %s ", prefix, box, symStyle.Render(sym))
	path := truncate(m.displayPath(it.path), width-lipgloss.Width(lead))
	row := lead + path
	if cursor {
		return m.styles.CursorRow.Render(row)
	}
	return row
}

func (m Model) symbol(it changeItem) (string, lipgloss.Style) {
	if it.kind == kindConflict {
		return "!", m.styles.StatusBang
	}
	switch it.action {
	case "create":
		return "+", m.styles.StatusCreate
	case "delete":
		return "D", m.styles.StatusDelete
	default:
		return "M", m.styles.StatusUpdate
	}
}

func (m Model) displayPath(p string) string {
	home := m.deps.UserHome
	if home == "" {
		return p
	}
	rel, err := filepath.Rel(home, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	if rel == "." {
		return "~"
	}
	return filepath.Join("~", rel)
}

func (m Model) statusLineView() string {
	var left string
	switch m.state {
	case stateLoading:
		left = "Loading…"
	case stateApplying:
		left = "Applying…"
	case stateGitRunning:
		left = "Running git…"
	case stateError:
		left = m.styles.ErrorText.Render("error: " + firstLine(m.err.Error()))
	default:
		if m.status != "" {
			left = m.status
		} else {
			left = m.summary()
		}
	}
	if m.dryRun {
		left = m.styles.Badge.Render(" DRY-RUN ") + " " + left
	}
	return m.styles.StatusLine.Width(m.width).Render(truncate(left, m.width))
}

func (m Model) summary() string {
	if m.plan == nil {
		return ""
	}
	if len(m.plan.Changes) == 0 && len(m.plan.Conflicts) == 0 {
		return "No changes."
	}
	return fmt.Sprintf("%d change(s), %d conflict(s), %d selected",
		len(m.plan.Changes), len(m.plan.Conflicts), m.selectedCount())
}

func (m Model) hintView() string {
	if m.filtering {
		return m.styles.HintBar.Width(m.width).Render(m.filterInput.View())
	}
	hint := "[a]pply [space]toggle [A]ll [N]one [d]iff [r]eload [p]ull [P]push [/]filter [q]uit"
	if m.showHelp {
		hint = "↑/k up · ↓/j down · tab focus · space toggle · A all · N none · t dry-run · " +
			"d diff · a apply · r reload · p pull · P push · / filter · q quit"
	}
	return m.styles.HintBar.Width(m.width).Render(truncate(hint, m.width))
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if s == "" {
		return "done"
	}
	return s
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
