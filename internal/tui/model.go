package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/kelos-dev/kanon/internal/core"
)

type focusArea int

const (
	focusAgents focusArea = iota
	focusChanges
	focusDiff
)

type loadState int

const (
	stateLoading loadState = iota
	stateReady
	stateApplying
	stateGitRunning
	stateError
)

type itemKind int

const (
	kindChange itemKind = iota
	kindConflict
)

// changeItem is a view-model row in the Changes list, wrapping either a planned
// FileChange or a FileConflict.
type changeItem struct {
	kind       itemKind
	path       string
	agent      string
	action     string // create|update|delete (kindChange only)
	reason     string // kindConflict only
	selectable bool
	change     *core.FileChange // non-nil for kindChange
}

type Model struct {
	deps   Deps
	keys   keyMap
	styles Styles

	width, height int
	ready         bool

	focus    focusArea
	state    loadState
	err      error
	status   string
	showHelp bool
	dryRun   bool
	mode     Mode

	gitStatus string

	plan   *core.ApplyPlan
	items  []changeItem
	cursor int

	selected map[string]bool

	viewport  viewport.Model
	diffCache map[string]string

	filtering   bool
	filter      string
	filterInput textinput.Model
}

// New builds a Model from its dependencies. The first WindowSizeMsg sizes the
// panels; Init kicks off the initial plan + git-status loads.
func New(deps Deps) Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter by path"
	mode := deps.Mode
	if mode == "" {
		mode = ModeApply
	}
	return Model{
		deps:        deps,
		keys:        defaultKeys(),
		styles:      DefaultStyles(),
		focus:       focusChanges,
		state:       stateLoading,
		dryRun:      deps.DryRun,
		mode:        mode,
		selected:    map[string]bool{},
		diffCache:   map[string]string{},
		viewport:    viewport.New(0, 0),
		filterInput: ti,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadPlanCmd(), m.loadGitStatusCmd())
}

// --- command factories: every core call runs off the Update loop ---

func (m Model) loadPlanCmd() tea.Cmd {
	plan := m.planFunc()
	return func() tea.Msg {
		p, err := plan()
		if err != nil {
			return errMsg{err}
		}
		return planLoadedMsg{plan: p}
	}
}

func (m Model) loadGitStatusCmd() tea.Cmd {
	gitStatus := m.deps.GitStatus
	return func() tea.Msg {
		b, err := gitStatus()
		if err != nil {
			return gitStatusMsg{status: "unavailable"}
		}
		s := strings.TrimRight(string(b), "\n")
		if s == "" {
			s = "clean"
		}
		return gitStatusMsg{status: s}
	}
}

func (m Model) applyCmd(selected map[string]SelectedChange, dryRun bool) tea.Cmd {
	apply := m.writeFunc()
	return func() tea.Msg {
		count, err := apply(selected, dryRun)
		if err != nil {
			return errMsg{err}
		}
		if dryRun {
			return dryRunCheckedMsg{count: count}
		}
		return appliedMsg{count: count}
	}
}

func (m Model) planFunc() func() (*core.ApplyPlan, error) {
	if m.mode == ModeImport && m.deps.ImportPlan != nil {
		return m.deps.ImportPlan
	}
	return m.deps.Plan
}

func (m Model) writeFunc() func(map[string]SelectedChange, bool) (int, error) {
	if m.mode == ModeImport && m.deps.ImportSelected != nil {
		return m.deps.ImportSelected
	}
	return m.deps.ApplySelected
}

func (m Model) gitCmd(label string, args ...string) tea.Cmd {
	git := m.deps.Git
	return func() tea.Msg {
		out, err := git(args...)
		if err != nil {
			if strings.TrimSpace(out) != "" {
				err = fmt.Errorf("%w: %s", err, firstLine(out))
			}
			return errMsg{err}
		}
		return gitDoneMsg{label: label, output: out}
	}
}

// --- plan/selection helpers ---

func buildItems(plan *core.ApplyPlan) []changeItem {
	if plan == nil {
		return nil
	}
	items := make([]changeItem, 0, len(plan.Conflicts)+len(plan.Changes))
	for i := range plan.Conflicts {
		c := plan.Conflicts[i]
		items = append(items, changeItem{
			kind:   kindConflict,
			path:   c.Path,
			agent:  c.Agent,
			reason: c.Reason,
		})
	}
	for i := range plan.Changes {
		ch := &plan.Changes[i]
		items = append(items, changeItem{
			kind:       kindChange,
			path:       ch.Path,
			agent:      ch.Agent,
			action:     ch.Action,
			selectable: true,
			change:     ch,
		})
	}
	return items
}

// visibleItems applies the active path filter. Selection is keyed by path, so
// it is unaffected by filtering.
func (m Model) visibleItems() []changeItem {
	if m.filter == "" {
		return m.items
	}
	var out []changeItem
	for _, it := range m.items {
		if m.matchesFilter(it.path) {
			out = append(out, it)
		}
	}
	return out
}

func (m Model) matchesFilter(path string) bool {
	return strings.Contains(path, m.filter) || strings.Contains(m.displayPath(path), m.filter)
}

func (m Model) currentItem() (changeItem, bool) {
	items := m.visibleItems()
	if len(items) == 0 || m.cursor < 0 || m.cursor >= len(items) {
		return changeItem{}, false
	}
	return items[m.cursor], true
}

func (m Model) selectedCount() int {
	n := 0
	if m.plan == nil {
		return 0
	}
	for i := range m.plan.Changes {
		if m.selected[m.plan.Changes[i].Path] {
			n++
		}
	}
	return n
}

func (m Model) selectedChanges() map[string]SelectedChange {
	selected := make(map[string]SelectedChange, len(m.selected))
	if m.plan == nil {
		return selected
	}
	for _, change := range m.plan.Changes {
		if m.selected[change.Path] {
			selected[change.Path] = SelectedChange{
				Path:         change.Path,
				Action:       change.Action,
				ExistingHash: change.ExistingHash,
				DesiredHash:  change.DesiredHash,
			}
		}
	}
	return selected
}

// --- layout ---

type layout struct {
	leftW    int
	rightW   int
	topH     int
	bottomH  int
	listRows int
}

func (m Model) computeLayout() layout {
	leftW := clamp(m.width/4, 22, 40)
	rightW := m.width - leftW
	bodyH := m.height - 2 // status line + hint bar
	if bodyH < 6 {
		bodyH = 6
	}
	topH := clamp(bodyH/3, 4, bodyH-4)
	bottomH := bodyH - topH
	return layout{
		leftW:    leftW,
		rightW:   rightW,
		topH:     topH,
		bottomH:  bottomH,
		listRows: topH - 3, // border (2) + title (1)
	}
}

func (m *Model) resize() {
	l := m.computeLayout()
	m.viewport.Width = max(1, l.rightW-2)
	m.viewport.Height = max(1, l.bottomH-3)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
