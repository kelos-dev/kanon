package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.resize()
		m.refreshDiff()
		return m, nil

	case planLoadedMsg:
		return m.onPlanLoaded(msg), nil

	case gitStatusMsg:
		m.gitStatus = msg.status
		return m, nil

	case appliedMsg:
		m.state = stateLoading
		m.status = fmt.Sprintf("%s %d change(s).", m.doneVerb(), msg.count)
		return m, tea.Batch(m.loadPlanCmd(), m.loadGitStatusCmd())

	case dryRunCheckedMsg:
		m.state = stateReady
		m.status = fmt.Sprintf("DRY-RUN: would %s %d change(s).", m.actionVerb(), msg.count)
		return m, nil

	case gitDoneMsg:
		m.status = fmt.Sprintf("git %s: %s", msg.label, firstLine(msg.output))
		if msg.label == "pull" {
			m.state = stateLoading
			return m, tea.Batch(m.loadPlanCmd(), m.loadGitStatusCmd())
		}
		m.state = stateReady
		return m, m.loadGitStatusCmd()

	case errMsg:
		m.state = stateError
		m.err = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleFilterKey(msg)
	}

	// In the error state, any key except quit dismisses the error and reloads.
	if m.state == stateError {
		switch {
		case key.Matches(msg, m.keys.Quit):
			return m, tea.Quit
		default:
			m.state = stateLoading
			m.err = nil
			m.status = ""
			return m, m.loadPlanCmd()
		}
	}

	// While reloading, ignore changes but still allow exit.
	if m.state == stateLoading {
		if key.Matches(msg, m.keys.Quit) {
			return m, tea.Quit
		}
		return m, nil
	}
	// Mutating commands must finish before exit; otherwise the process can stop
	// while files, state, or git state are only partially updated.
	if m.state == stateApplying || m.state == stateGitRunning {
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.showHelp = !m.showHelp
		return m, nil

	case key.Matches(msg, m.keys.NextPane):
		m.focus = (m.focus + 1) % 3
		return m, nil

	case key.Matches(msg, m.keys.PrevPane):
		m.focus = (m.focus + 2) % 3
		return m, nil

	case key.Matches(msg, m.keys.Diff):
		m.focus = focusDiff
		return m, nil

	case key.Matches(msg, m.keys.Up):
		return m.moveCursor(-1)

	case key.Matches(msg, m.keys.Down):
		return m.moveCursor(1)

	case key.Matches(msg, m.keys.Toggle):
		if it, ok := m.currentItem(); ok && it.selectable {
			m.selected[it.path] = !m.selected[it.path]
		}
		return m, nil

	case key.Matches(msg, m.keys.SelectAll):
		m.setAllSelected(true)
		return m, nil

	case key.Matches(msg, m.keys.SelectNone):
		m.setAllSelected(false)
		return m, nil

	case key.Matches(msg, m.keys.DryRun):
		m.dryRun = !m.dryRun
		return m, nil

	case key.Matches(msg, m.keys.Mode):
		return m.switchMode()

	case key.Matches(msg, m.keys.Apply):
		return m.apply()

	case key.Matches(msg, m.keys.Reload):
		m.state = stateLoading
		m.status = ""
		return m, tea.Batch(m.loadPlanCmd(), m.loadGitStatusCmd())

	case key.Matches(msg, m.keys.Pull):
		m.state = stateGitRunning
		m.status = "git pull…"
		return m, m.gitCmd("pull", "pull", "--ff-only")

	case key.Matches(msg, m.keys.Push):
		m.state = stateGitRunning
		m.status = "git push…"
		return m, m.gitCmd("push", "push")

	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.filterInput.SetValue(m.filter)
		m.filterInput.Focus()
		return m, nil
	}

	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter = ""
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.clampCursor()
		m.refreshDiff()
		return m, nil
	case "enter":
		m.filtering = false
		m.filterInput.Blur()
		m.clampCursor()
		m.refreshDiff()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filter = m.filterInput.Value()
	m.cursor = 0
	m.refreshDiff()
	return m, cmd
}

func (m Model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if m.focus == focusDiff {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(scrollKey(delta))
		return m, cmd
	}
	items := m.visibleItems()
	if len(items) == 0 {
		return m, nil
	}
	m.cursor += delta
	m.clampCursor()
	m.refreshDiff()
	return m, nil
}

// scrollKey maps a cursor delta to a viewport scroll key so a single helper can
// drive both up and down.
func scrollKey(delta int) tea.KeyMsg {
	if delta < 0 {
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyDown}
}

func (m Model) apply() (tea.Model, tea.Cmd) {
	n := m.selectedCount()
	if n == 0 {
		m.status = "Nothing selected."
		return m, nil
	}
	if m.writeFunc() == nil {
		m.status = fmt.Sprintf("%s mode is not configured.", m.mode)
		return m, nil
	}
	if m.dryRun {
		m.state = stateApplying
		m.status = "Checking dry run…"
		return m, m.applyCmd(m.selectedChanges(), true)
	}
	m.state = stateApplying
	m.status = m.activeVerb() + "…"
	return m, m.applyCmd(m.selectedChanges(), false)
}

func (m Model) switchMode() (tea.Model, tea.Cmd) {
	if m.deps.ImportPlan == nil || m.deps.ImportSelected == nil {
		m.status = "Import mode is not configured."
		return m, nil
	}
	if m.mode == ModeImport {
		m.mode = ModeApply
	} else {
		m.mode = ModeImport
	}
	m.state = stateLoading
	m.status = ""
	m.cursor = 0
	m.selected = map[string]bool{}
	m.diffCache = map[string]string{}
	m.filter = ""
	m.filtering = false
	m.filterInput.Blur()
	m.filterInput.SetValue("")
	return m, m.loadPlanCmd()
}

func (m Model) activeVerb() string {
	if m.mode == ModeImport {
		return "Importing"
	}
	return "Applying"
}

func (m Model) doneVerb() string {
	if m.mode == ModeImport {
		return "Imported"
	}
	return "Applied"
}

func (m Model) actionVerb() string {
	if m.mode == ModeImport {
		return "import"
	}
	return "apply"
}

func (m *Model) setAllSelected(v bool) {
	if m.plan == nil {
		return
	}
	for i := range m.plan.Changes {
		m.selected[m.plan.Changes[i].Path] = v
	}
}

func (m Model) onPlanLoaded(msg planLoadedMsg) Model {
	m.plan = msg.plan
	m.items = buildItems(msg.plan)
	m.diffCache = map[string]string{}
	m.selected = map[string]bool{}
	for i := range msg.plan.Changes {
		m.selected[msg.plan.Changes[i].Path] = true
	}
	m.state = stateReady
	m.err = nil
	m.status = ""
	m.clampCursor()
	m.refreshDiff()
	return m
}

func (m *Model) clampCursor() {
	n := len(m.visibleItems())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// refreshDiff updates the viewport to show the diff (or conflict detail) for
// the item under the cursor, caching colorized diffs per path.
func (m *Model) refreshDiff() {
	it, ok := m.currentItem()
	if !ok {
		if m.filter != "" {
			m.viewport.SetContent(m.styles.DiffMeta.Render("No files match filter."))
			m.viewport.GotoTop()
			return
		}
		m.viewport.SetContent(m.styles.DiffMeta.Render("No changes. Source matches destination."))
		m.viewport.GotoTop()
		return
	}
	if it.kind == kindConflict {
		body := m.styles.StatusBang.Render("CONFLICT ") + it.path + "\n\n" +
			m.styles.DiffMeta.Render(it.reason) + "\n\n" +
			m.styles.DiffMeta.Render("Resolve the destination file, then reload.")
		m.viewport.SetContent(body)
		m.viewport.GotoTop()
		return
	}
	if cached, ok := m.diffCache[it.path]; ok {
		m.viewport.SetContent(cached)
		m.viewport.GotoTop()
		return
	}
	content := renderChangeDiff(m.styles, *it.change)
	m.diffCache[it.path] = content
	m.viewport.SetContent(content)
	m.viewport.GotoTop()
}
