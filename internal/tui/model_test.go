package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kelos-dev/kanon/internal/core"
)

const (
	pathClaude   = "/home/u/.claude/CLAUDE.md"
	pathSkill    = "/home/u/.claude/skills/foo.md"
	pathOld      = "/home/u/.codex/old.md"
	pathConflict = "/home/u/.codex/AGENTS.md"
)

func testPlan() *core.ApplyPlan {
	return &core.ApplyPlan{
		Changes: []core.FileChange{
			{Agent: "claude", Path: pathClaude, Action: "update", Existing: []byte("a\n"), File: core.RenderedFile{Content: []byte("b\n")}},
			{Agent: "claude", Path: pathSkill, Action: "create", File: core.RenderedFile{Content: []byte("x\n")}},
			{Agent: "codex", Path: pathOld, Action: "delete", Existing: []byte("gone\n")},
		},
		Conflicts: []core.FileConflict{
			{Agent: "codex", Path: pathConflict, Reason: "existing file is not managed by kanon"},
		},
	}
}

func newTestModel(t *testing.T, plan *core.ApplyPlan) Model {
	t.Helper()
	deps := Deps{
		UserHome: "/home/u",
		Agent:    "all",
		Plan: func() (*core.ApplyPlan, error) {
			return plan, nil
		},
		ApplySelected: func(map[string]SelectedChange, bool) (int, error) { return 0, nil },
		GitStatus:     func() ([]byte, error) { return nil, nil },
		Git:           func(...string) (string, error) { return "", nil },
	}
	m := New(deps)
	m = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m = update(m, planLoadedMsg{plan: plan})
	return m
}

// update runs one Update cycle and returns the new model, discarding the cmd.
func update(m Model, msg tea.Msg) Model {
	next, _ := m.Update(msg)
	return next.(Model)
}

func updateCmd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestPlanLoadedDefaultsAllSelected(t *testing.T) {
	m := newTestModel(t, testPlan())
	for _, p := range []string{pathClaude, pathSkill, pathOld} {
		if !m.selected[p] {
			t.Fatalf("expected %s selected by default", p)
		}
	}
	if m.selected[pathConflict] {
		t.Fatalf("conflict path must not be selected")
	}
	if m.selectedCount() != 3 {
		t.Fatalf("expected 3 selected, got %d", m.selectedCount())
	}
}

func TestToggleSelectsAndDeselects(t *testing.T) {
	m := newTestModel(t, testPlan())
	// Cursor starts at 0 = the conflict row, which must not toggle.
	m = update(m, keyMsg(" "))
	if m.selected[pathConflict] {
		t.Fatalf("toggling a conflict row must be a no-op")
	}
	// Move to the first change row and toggle off, then on.
	m = update(m, keyMsg("down"))
	m = update(m, keyMsg(" "))
	if m.selected[pathClaude] {
		t.Fatalf("expected %s deselected after toggle", pathClaude)
	}
	m = update(m, keyMsg(" "))
	if !m.selected[pathClaude] {
		t.Fatalf("expected %s reselected after second toggle", pathClaude)
	}
}

func TestSelectedChangesOnlyIncludesSelectedChanges(t *testing.T) {
	m := newTestModel(t, testPlan())
	// Deselect the create row (index 2: conflict, claude, skill).
	m = update(m, keyMsg("down")) // -> claude
	m = update(m, keyMsg("down")) // -> skill
	m = update(m, keyMsg(" "))    // deselect skill
	selected := m.selectedChanges()
	if len(selected) != 2 {
		t.Fatalf("expected 2 selected paths, got %d", len(selected))
	}
	if _, ok := selected[pathSkill]; ok {
		t.Fatalf("deselected path %s must not be selected", pathSkill)
	}
	if _, ok := selected[pathConflict]; ok {
		t.Fatalf("conflict path %s must not be selected", pathConflict)
	}
}

func TestApplyWithNoSelectionIsNoop(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("N")) // select none
	if m.selectedCount() != 0 {
		t.Fatalf("expected 0 selected after select-none")
	}
	m, cmd := updateCmd(m, keyMsg("a"))
	if cmd != nil {
		t.Fatalf("apply with nothing selected must not issue a command")
	}
	if m.state == stateApplying {
		t.Fatalf("must not enter applying state with nothing selected")
	}
	if !strings.Contains(m.status, "Nothing selected") {
		t.Fatalf("expected 'Nothing selected' status, got %q", m.status)
	}
}

func TestApplyIssuesCommandAndEntersApplying(t *testing.T) {
	m := newTestModel(t, testPlan())
	m, cmd := updateCmd(m, keyMsg("a"))
	if cmd == nil {
		t.Fatalf("apply with a selection must issue a command")
	}
	if m.state != stateApplying {
		t.Fatalf("expected applying state, got %v", m.state)
	}
}

func TestApplyUsesActualAppliedCount(t *testing.T) {
	m := New(Deps{
		ApplySelected: func(map[string]SelectedChange, bool) (int, error) { return 1, nil },
	})
	msg := m.applyCmd(map[string]SelectedChange{
		pathClaude: {Path: pathClaude},
		pathSkill:  {Path: pathSkill},
	}, false)()
	applied, ok := msg.(appliedMsg)
	if !ok {
		t.Fatalf("expected appliedMsg, got %T", msg)
	}
	if applied.count != 1 {
		t.Fatalf("expected actual applied count 1, got %d", applied.count)
	}
}

func TestLoadingStateIgnoresApplyWithStalePlan(t *testing.T) {
	m := newTestModel(t, testPlan())
	m.state = stateLoading
	m.status = "Reloading..."
	m, cmd := updateCmd(m, keyMsg("a"))
	if cmd != nil {
		t.Fatalf("loading state must not issue an apply command")
	}
	if m.state != stateLoading {
		t.Fatalf("expected loading state to remain active, got %v", m.state)
	}
	if m.status != "Reloading..." {
		t.Fatalf("loading key handling should not mutate status, got %q", m.status)
	}
}

func TestMutatingStatesIgnoreQuit(t *testing.T) {
	for name, state := range map[string]loadState{
		"apply": stateApplying,
		"git":   stateGitRunning,
	} {
		t.Run(name, func(t *testing.T) {
			m := newTestModel(t, testPlan())
			m.state = state
			m, cmd := updateCmd(m, keyMsg("q"))
			if cmd != nil {
				t.Fatalf("quit during %s must not issue a command", name)
			}
			if m.state != state {
				t.Fatalf("expected state %v to remain active, got %v", state, m.state)
			}
		})
	}
}

func TestDryRunApplyValidatesWithoutWriting(t *testing.T) {
	var gotDryRun bool
	m := newTestModel(t, testPlan())
	m.deps.ApplySelected = func(_ map[string]SelectedChange, dryRun bool) (int, error) {
		gotDryRun = dryRun
		return 2, nil
	}
	m = update(m, keyMsg("t")) // toggle dry-run on
	m, cmd := updateCmd(m, keyMsg("a"))
	if cmd == nil {
		t.Fatalf("dry-run apply must issue a validation command")
	}
	if m.state != stateApplying {
		t.Fatalf("expected dry-run validation state, got %v", m.state)
	}
	msg := cmd()
	checked, ok := msg.(dryRunCheckedMsg)
	if !ok {
		t.Fatalf("expected dryRunCheckedMsg, got %T", msg)
	}
	if checked.count != 2 {
		t.Fatalf("expected validated dry-run count 2, got %d", checked.count)
	}
	if !gotDryRun {
		t.Fatalf("expected dry-run flag passed to apply callback")
	}
	m = update(m, msg)
	if m.state != stateReady {
		t.Fatalf("expected ready state after dry-run validation, got %v", m.state)
	}
	if !strings.Contains(m.status, "DRY-RUN") {
		t.Fatalf("expected dry-run status, got %q", m.status)
	}
}

func TestSelectAllSelectNone(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("N"))
	if m.selectedCount() != 0 {
		t.Fatalf("expected 0 selected after N, got %d", m.selectedCount())
	}
	m = update(m, keyMsg("A"))
	if m.selectedCount() != 3 {
		t.Fatalf("expected 3 selected after A, got %d", m.selectedCount())
	}
}

func TestFocusCycles(t *testing.T) {
	m := newTestModel(t, testPlan())
	if m.focus != focusChanges {
		t.Fatalf("expected initial focus on changes")
	}
	m = update(m, keyMsg("tab"))
	if m.focus != focusDiff {
		t.Fatalf("expected focus diff after tab, got %v", m.focus)
	}
	m = update(m, keyMsg("tab"))
	if m.focus != focusAgents {
		t.Fatalf("expected focus agents after second tab, got %v", m.focus)
	}
	m = update(m, keyMsg("tab"))
	if m.focus != focusChanges {
		t.Fatalf("expected focus back to changes after third tab, got %v", m.focus)
	}
}

func TestDiffShortcutFocusesDiffPane(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("d"))
	if m.focus != focusDiff {
		t.Fatalf("expected d shortcut to focus diff pane, got %v", m.focus)
	}
}

func TestCursorClampOnReload(t *testing.T) {
	m := newTestModel(t, testPlan())
	// Move cursor to the last item.
	for i := 0; i < 5; i++ {
		m = update(m, keyMsg("down"))
	}
	small := &core.ApplyPlan{Changes: []core.FileChange{
		{Agent: "claude", Path: pathClaude, Action: "create", File: core.RenderedFile{Content: []byte("z\n")}},
	}}
	m = update(m, planLoadedMsg{plan: small})
	if m.cursor != 0 {
		t.Fatalf("expected cursor clamped to 0 after reload, got %d", m.cursor)
	}
}

func TestAppliedMsgReloads(t *testing.T) {
	m := newTestModel(t, testPlan())
	m, cmd := updateCmd(m, appliedMsg{count: 2})
	if cmd == nil {
		t.Fatalf("appliedMsg must trigger a reload command")
	}
	if m.state != stateLoading {
		t.Fatalf("expected loading state after apply, got %v", m.state)
	}
	if !strings.Contains(m.status, "Applied 2") {
		t.Fatalf("expected applied status, got %q", m.status)
	}
}

func TestGitCmdIncludesFailureOutput(t *testing.T) {
	m := New(Deps{
		Git: func(...string) (string, error) {
			return "fatal: not possible\nhint: details\n", errors.New("git pull failed")
		},
	})
	msg := m.gitCmd("pull", "pull")()
	err, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("expected errMsg, got %T", msg)
	}
	if !strings.Contains(err.Error(), "git pull failed") || !strings.Contains(err.Error(), "fatal: not possible") {
		t.Fatalf("expected git error and output, got %q", err.Error())
	}
}

func TestPlanLoadedClearsStaleStatus(t *testing.T) {
	m := newTestModel(t, testPlan())
	m.status = "Applying…"
	m.state = stateLoading
	m = update(m, planLoadedMsg{plan: testPlan()})
	if m.status != "" {
		t.Fatalf("expected stale status cleared after normal plan load, got %q", m.status)
	}
}

func TestErrMsgEntersErrorState(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, errMsg{errors.New("boom")})
	if m.state != stateError {
		t.Fatalf("expected error state")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("expected error text in view")
	}
}

func TestErrorStateAnyKeyReloads(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, errMsg{errors.New("boom")})
	m, cmd := updateCmd(m, keyMsg("r"))
	if cmd == nil {
		t.Fatalf("a key in error state must trigger a reload")
	}
	if m.state != stateLoading {
		t.Fatalf("expected loading state after dismissing error, got %v", m.state)
	}
}

func TestFilterNarrowsVisibleItems(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("/"))
	if !m.filtering {
		t.Fatalf("expected filtering mode after /")
	}
	for _, r := range "skills" {
		m = update(m, keyMsg(string(r)))
	}
	m = update(m, keyMsg("enter"))
	vis := m.visibleItems()
	if len(vis) != 1 || vis[0].path != pathSkill {
		t.Fatalf("expected only the skills path visible, got %d items", len(vis))
	}
	// Selection is keyed by path, so the other changes stay selected.
	if m.selectedCount() != 3 {
		t.Fatalf("filtering must not change selection, got %d", m.selectedCount())
	}
}

func TestFilterMatchesDisplayedHomeRelativePath(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("/"))
	for _, r := range "~/.claude/skills" {
		m = update(m, keyMsg(string(r)))
	}
	m = update(m, keyMsg("enter"))
	vis := m.visibleItems()
	if len(vis) != 1 || vis[0].path != pathSkill {
		t.Fatalf("expected displayed-path filter to match skills path, got %d items", len(vis))
	}
}

func TestFilterNoMatchesUpdatesDiffPane(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("/"))
	for _, r := range "missing" {
		m = update(m, keyMsg(string(r)))
	}
	if !strings.Contains(m.viewport.View(), "No files match filter") {
		t.Fatalf("expected no-match message in diff pane, got %q", m.viewport.View())
	}
	if strings.Contains(m.viewport.View(), "Source matches destination") {
		t.Fatalf("filtered no-match diff pane must not claim source matches destination: %q", m.viewport.View())
	}
}

func TestFilterModeCtrlCQuits(t *testing.T) {
	m := newTestModel(t, testPlan())
	m = update(m, keyMsg("/"))
	m, cmd := updateCmd(m, keyMsg("ctrl+c"))
	if cmd == nil {
		t.Fatalf("ctrl+c while filtering must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected quit command while filtering")
	}
	if !m.filtering {
		t.Fatalf("ctrl+c should quit without first mutating filter mode")
	}
}

func TestViewSmallTerminalNotice(t *testing.T) {
	m := New(Deps{Agent: "all"})
	m = update(m, tea.WindowSizeMsg{Width: 40, Height: 10})
	if !strings.Contains(m.View(), "too small") {
		t.Fatalf("expected too-small notice, got:\n%s", m.View())
	}
}

func TestViewRendersChangesAndDiff(t *testing.T) {
	m := newTestModel(t, testPlan())
	out := m.View()
	if !strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("expected a change path in the view")
	}
	if !strings.Contains(out, "Changes") {
		t.Fatalf("expected the Changes panel title")
	}
}

func TestRenderRowConstrainsLongPath(t *testing.T) {
	m := newTestModel(t, testPlan())
	longPath := "/home/u/.claude/skills/" + strings.Repeat("deeply-nested-", 20) + "SKILL.md"
	row := m.renderRow(changeItem{
		kind:       kindChange,
		path:       longPath,
		action:     "update",
		selectable: true,
	}, true, 38)
	if got := lipgloss.Width(row); got > 38 {
		t.Fatalf("row width = %d, want <= 38: %q", got, row)
	}
	if strings.Contains(row, "deeply-nested-deeply-nested-deeply-nested-deeply-nested") {
		t.Fatalf("row contains unbounded path: %q", row)
	}
}

func TestRenderDiffReportsLineEndingOnlyChange(t *testing.T) {
	out := renderDiff(DefaultStyles(), core.DiffHunks("/x", []byte("a\r\nb\r\n"), []byte("a\nb\n")))
	if !strings.Contains(out, "Line endings differ") {
		t.Fatalf("expected line-ending notice in TUI diff, got %q", out)
	}
	if strings.Contains(out, "No content changes") {
		t.Fatalf("line-ending-only update must not be reported as no content changes: %q", out)
	}
}

func TestRenderDiffReportsEmptyCreateAndDelete(t *testing.T) {
	for name, df := range map[string]core.DiffFile{
		"create": core.DiffHunks("/x", nil, []byte{}),
		"delete": core.DiffHunks("/x", []byte{}, nil),
	} {
		out := renderDiff(DefaultStyles(), df)
		if !strings.Contains(out, "Empty file will be") {
			t.Fatalf("%s: expected empty file notice, got %q", name, out)
		}
		if !strings.Contains(out, "/dev/null") {
			t.Fatalf("%s: expected /dev/null header, got %q", name, out)
		}
		if strings.Contains(out, "No content changes") {
			t.Fatalf("%s: empty file existence change must not be reported as no content changes: %q", name, out)
		}
	}
}

func TestRenderChangeDiffSkipsHugeUpdateDiff(t *testing.T) {
	change := core.FileChange{
		Path:     "/x",
		Action:   "update",
		Existing: []byte(repeatedLines("old", 1500)),
		File:     core.RenderedFile{Content: []byte(repeatedLines("new", 1500))},
	}
	out := renderChangeDiff(DefaultStyles(), change)
	if !strings.Contains(out, "Diff too large") {
		t.Fatalf("expected large diff notice, got %q", out)
	}
	if strings.Contains(out, "@@") {
		t.Fatalf("large update diff should skip hunk rendering, got %q", out)
	}
}

func TestRenderChangeDiffKeepsBinaryNotice(t *testing.T) {
	change := core.FileChange{
		Path:     "/x",
		Action:   "update",
		Existing: []byte(strings.Repeat("\x00\n", 1500)),
		File:     core.RenderedFile{Content: []byte(strings.Repeat("\x00\n", 1500))},
	}
	out := renderChangeDiff(DefaultStyles(), change)
	if !strings.Contains(out, "Binary files differ") {
		t.Fatalf("expected binary diff notice, got %q", out)
	}
	if strings.Contains(out, "Diff too large") {
		t.Fatalf("binary update must not be reported as a large text diff: %q", out)
	}
}

func TestDisplayPathOnlyShortensWithinUserHome(t *testing.T) {
	m := New(Deps{UserHome: "/home/u"})
	tests := map[string]string{
		"/home/u":              "~",
		"/home/u/.codex/a.md":  "~/.codex/a.md",
		"/home/u2/.codex/a.md": "/home/u2/.codex/a.md",
		"/home/u-project/a.md": "/home/u-project/a.md",
	}
	for in, want := range tests {
		if got := m.displayPath(in); got != want {
			t.Fatalf("displayPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func repeatedLines(prefix string, n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteString(prefix)
		b.WriteByte('\n')
	}
	return b.String()
}
