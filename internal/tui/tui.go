// Package tui is the terminal front end: read, lens, provoke, resolve.
//
// Deliberately narrower than the browser UI. A terminal is good at reading a
// passage and firing an action, bad at editing a paragraph, so long-form writing
// belongs in `whetstone -web`.
//
// Both front ends share the same packages and the same workspace file.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/provider"
	"github.com/tanisha327/whetstone/internal/provoke"
	"github.com/tanisha327/whetstone/internal/workspace"
)

const requestTimeout = 2 * time.Minute

var (
	bold  = lipgloss.NewStyle().Bold(true)
	dim   = lipgloss.NewStyle().Faint(true)
	pick  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	warn  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	good  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	bad   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	brand = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
)

// view is which screen is showing.
type view int

const (
	viewList view = iota
	viewRead
	viewPrompt
	viewHelp
	viewError
)

// row is one line of the source list: every section of every document.
type row struct {
	docID     string
	sectionID int
	title     string
}

// Model is the bubbletea model.
type Model struct {
	ws   *workspace.Workspace
	prov provider.Provider

	view          view
	cursor        int
	scroll        int
	provIdx       int
	width, height int

	// prompt state. The terminal only ever asks for one line, which is all a
	// provocation response or a lens switch needs.
	promptTitle string
	promptHint  string
	promptValue []rune
	promptSave  func(string) tea.Cmd

	status string
	busy   string
	errMsg string
}

// New returns a Model bound to a workspace and provider.
func New(ws *workspace.Workspace, prov provider.Provider) *Model {
	if ws.ActiveLens == "" && len(lens.Builtin) > 0 {
		ws.ActiveLens = lens.Builtin[0].ID
	}
	return &Model{ws: ws, prov: prov}
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

type summaryMsg struct {
	docID string
	sum   lens.Summary
	err   error
}
type provokedMsg struct {
	items []provoke.Provocation
	err   error
}
type savedMsg struct{ err error }
type statusMsg string

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case summaryMsg:
		m.busy = ""
		if msg.err != nil {
			return m, m.fail(msg.err)
		}
		m.ws.PutSummary(msg.docID, msg.sum)
		m.status = "lens applied"

	case provokedMsg:
		m.busy = ""
		if msg.err != nil {
			return m, m.fail(msg.err)
		}
		n := m.ws.AddProvocations(msg.items)
		m.provIdx = 0
		if n == 0 {
			m.status = "no new provocations"
		} else {
			m.status = fmt.Sprintf("%d new — [y] engage, [x] dismiss", n)
		}

	case savedMsg:
		if msg.err != nil {
			return m, m.fail(msg.err)
		}
		m.status = "saved"

	case statusMsg:
		m.status = string(msg)

	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m *Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch m.view {
	case viewPrompt:
		return m.onPromptKey(msg)
	case viewHelp:
		m.view = viewList
		return m, nil
	case viewError:
		m.view = viewList
		m.errMsg = ""
		return m, nil
	case viewRead:
		return m.onReadKey(msg)
	default:
		return m.onListKey(msg)
	}
}

func (m *Model) onListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.rows()
	switch msg.String() {
	case "q":
		return m, tea.Sequence(m.save(), tea.Quit)
	case "?":
		m.view = viewHelp
	case "j", "down":
		m.cursor = clamp(m.cursor+1, 0, len(rows)-1)
		m.provIdx = 0
	case "k", "up":
		m.cursor = clamp(m.cursor-1, 0, len(rows)-1)
		m.provIdx = 0
	case "g":
		m.cursor = 0
	case "G":
		m.cursor = max(len(rows)-1, 0)
	case "enter", "l":
		if r, ok := m.current(); ok {
			m.ws.MarkRead(r.docID, r.sectionID)
			m.view = viewRead
			m.scroll = 0
		}
	case "L":
		return m, m.cycleLens()
	case "a":
		return m, m.applyLens()
	case "p":
		return m, m.provoke()
	case "n":
		return m, m.nextProvocation()
	case "y":
		return m, m.resolve(true)
	case "x":
		return m, m.resolve(false)
	case "s":
		return m, m.save()
	}
	return m, nil
}

func (m *Model) onReadKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "h":
		m.view = viewList
		m.scroll = 0
	case "j", "down":
		m.scroll++
	case "k", "up":
		m.scroll = max(m.scroll-1, 0)
	case "g":
		m.scroll = 0
	case "a":
		return m, m.applyLens()
	case "p":
		return m, m.provoke()
	case "?":
		m.view = viewHelp
	}
	return m, nil
}

// onPromptKey drives the single-line input.
//
// Rune-based, not byte-based: byte indexing splits multi-byte characters on
// backspace and drops non-ASCII keystrokes.
func (m *Model) onPromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewList
		m.status = "cancelled"
	case tea.KeyEnter:
		value := strings.TrimSpace(string(m.promptValue))
		fn := m.promptSave
		m.view = viewList
		if fn != nil {
			return m, fn(value)
		}
	case tea.KeyBackspace:
		if n := len(m.promptValue); n > 0 {
			m.promptValue = m.promptValue[:n-1]
		}
	case tea.KeySpace:
		m.promptValue = append(m.promptValue, ' ')
	case tea.KeyRunes:
		// The whole batch: a paste arrives as one message.
		m.promptValue = append(m.promptValue, msg.Runes...)
	}
	return m, nil
}

func (m *Model) promptFor(title, hint string, save func(string) tea.Cmd) {
	m.promptTitle = title
	m.promptHint = hint
	m.promptValue = nil
	m.promptSave = save
	m.view = viewPrompt
}

// --- commands ---

func (m *Model) fail(err error) tea.Cmd {
	m.view = viewError
	m.errMsg = err.Error()
	return nil
}

func (m *Model) save() tea.Cmd {
	ws := m.ws
	return func() tea.Msg { return savedMsg{err: ws.Save()} }
}

func (m *Model) cycleLens() tea.Cmd {
	if len(lens.Builtin) == 0 {
		return nil
	}
	i := 0
	for j, l := range lens.Builtin {
		if l.ID == m.ws.ActiveLens {
			i = (j + 1) % len(lens.Builtin)
			break
		}
	}
	m.ws.ActiveLens = lens.Builtin[i].ID
	name := lens.Builtin[i].Name
	return func() tea.Msg { return statusMsg("lens: " + name) }
}

func (m *Model) applyLens() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	d, ok := m.ws.Document(r.docID)
	if !ok {
		return nil
	}
	sec, ok := d.Section(r.sectionID)
	if !ok {
		return nil
	}
	l, ok := lens.ByID(m.ws.ActiveLens)
	if !ok {
		return nil
	}

	m.busy = "applying " + l.Name + " lens"
	p, docID := m.prov, d.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		sum, err := lens.ApplySection(ctx, p, l, sec)
		return summaryMsg{docID: docID, sum: sum, err: err}
	}
}

func (m *Model) provoke() tea.Cmd {
	r, ok := m.current()
	if !ok {
		return nil
	}
	d, ok := m.ws.Document(r.docID)
	if !ok {
		return nil
	}
	sec, ok := d.Section(r.sectionID)
	if !ok {
		return nil
	}

	m.busy = "generating provocations"
	p := m.prov
	in := provoke.Input{
		AnchorKind: provoke.AnchorSection,
		AnchorID:   workspace.SectionKey(d.ID, sec.ID),
		Subject:    sec.Body,
		Context:    m.questionContext(),
		Max:        2,
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
		defer cancel()
		items, err := provoke.Generate(ctx, p, in)
		return provokedMsg{items: items, err: err}
	}
}

func (m *Model) nextProvocation() tea.Cmd {
	list := m.provocations()
	if len(list) == 0 {
		return nil
	}
	m.provIdx = (m.provIdx + 1) % len(list)
	return nil
}

// resolve collects the mandatory reason. There is no way to clear a
// provocation without typing one.
func (m *Model) resolve(engaged bool) tea.Cmd {
	list := m.provocations()
	if len(list) == 0 {
		return func() tea.Msg { return statusMsg("no provocation selected") }
	}
	id := list[clamp(m.provIdx, 0, len(list)-1)].ID

	title, hint := "Dismiss", "Why does it not apply?"
	if engaged {
		title, hint = "Engage", "What did you change or decide?"
	}
	m.promptFor(title, hint, func(v string) tea.Cmd {
		p := m.ws.Provocation(id)
		if p == nil {
			return nil
		}
		var err error
		if engaged {
			err = p.Engage(v, time.Now())
		} else {
			err = p.Dismiss(v, time.Now())
		}
		if err != nil {
			return func() tea.Msg { return statusMsg(err.Error()) }
		}
		return func() tea.Msg { return statusMsg("recorded") }
	})
	return nil
}

// --- selection ---

func (m *Model) rows() []row {
	var out []row
	for _, d := range m.ws.Documents {
		for _, s := range d.Sections {
			out = append(out, row{docID: d.ID, sectionID: s.ID, title: s.Title})
		}
	}
	return out
}

func (m *Model) current() (row, bool) {
	rows := m.rows()
	if len(rows) == 0 {
		return row{}, false
	}
	return rows[clamp(m.cursor, 0, len(rows)-1)], true
}

// provocations returns those for the selected section, unresolved first.
func (m *Model) provocations() []provoke.Provocation {
	r, ok := m.current()
	if !ok {
		return nil
	}
	all := m.ws.ProvocationsFor(provoke.AnchorSection, workspace.SectionKey(r.docID, r.sectionID))
	open := make([]provoke.Provocation, 0, len(all))
	done := make([]provoke.Provocation, 0, len(all))
	for _, p := range all {
		if p.Resolved() {
			done = append(done, p)
		} else {
			open = append(open, p)
		}
	}
	return append(open, done...)
}

func sectionKey(r row) string { return workspace.SectionKey(r.docID, r.sectionID) }

func (m *Model) questionContext() string {
	if m.ws.Question == "" {
		return ""
	}
	return "The author is trying to answer: " + m.ws.Question
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
