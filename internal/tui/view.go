package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/provoke"
)

// View implements tea.Model. It is pure: no provider calls, no writes, no
// mutation of m. Every side effect is a tea.Cmd fired by a keystroke.
func (m *Model) View() string {
	switch m.view {
	case viewError:
		return bad.Render("Something went wrong") + "\n\n" +
			wrap(m.errMsg, m.textWidth()) + "\n\n" +
			dim.Render("Your workspace is unchanged. Any key to go back.")
	case viewHelp:
		return m.viewHelp()
	case viewPrompt:
		return bold.Render(m.promptTitle) + "\n" +
			dim.Render(m.promptHint) + "\n\n  > " +
			string(m.promptValue) + "█\n\n" +
			dim.Render("enter to save · esc to cancel")
	case viewRead:
		return m.viewRead()
	default:
		return m.viewList()
	}
}

func (m *Model) viewList() string {
	var b strings.Builder
	b.WriteString(m.header())

	rows := m.rows()
	if len(rows) == 0 {
		b.WriteString(dim.Render(
			"No documents.\n\n"+
				"The terminal front end reads what is already in the workspace.\n"+
				"To paste a document in, use the browser UI:\n") + "\n")
		b.WriteString("    whetstone -web\n\n")
		b.WriteString(dim.Render("[q] quit  [?] help\n"))
		return b.String()
	}

	cursor := clamp(m.cursor, 0, len(rows)-1)
	for i, r := range rows {
		mark := dim.Render("○")
		if m.ws.IsRead(r.docID, r.sectionID) {
			mark = good.Render("●")
		}
		flag := " "
		if m.hasOpen(r) {
			flag = warn.Render("!")
		}
		line := fmt.Sprintf("%s%s %s", mark, flag, r.title)
		if i == cursor {
			line = pick.Render("> " + line)
		} else {
			line = "  " + line
		}
		b.WriteString(truncate(line, m.textWidth()) + "\n")
	}

	// The lens reading of the selected section, when one exists.
	if r, ok := m.current(); ok {
		if sum, found := m.ws.Summary(r.docID, r.sectionID, m.ws.ActiveLens); found {
			b.WriteString("\n" + dim.Render(wrap(sum.Text, m.textWidth())) + "\n")
			for _, kp := range sum.KeyPoints {
				b.WriteString(dim.Render(truncate("  · "+kp, m.textWidth())) + "\n")
			}
		} else {
			b.WriteString("\n" + dim.Render("[a] apply the "+m.lensName()+" lens") + "\n")
		}
	}

	b.WriteString(m.provocationBlock())
	b.WriteString("\n" + m.footer("[enter] read  [a] lens  [p] provoke  [L] switch lens  [?] help"))
	return b.String()
}

func (m *Model) viewRead() string {
	r, ok := m.current()
	if !ok {
		return "no section"
	}
	d, ok := m.ws.Document(r.docID)
	if !ok {
		return "no document"
	}
	sec, ok := d.Section(r.sectionID)
	if !ok {
		return "no section"
	}

	var b strings.Builder
	b.WriteString(bold.Render(sec.Title) + "\n")
	b.WriteString(dim.Render(fmt.Sprintf("%s · %d words", d.Title, sec.WordCount())) + "\n\n")

	lines := strings.Split(wrap(sec.Body, m.textWidth()-2), "\n")
	height := max(m.height-12, 5)
	start := clamp(m.scroll, 0, max(len(lines)-1, 0))
	end := min(start+height, len(lines))
	for _, l := range lines[start:end] {
		b.WriteString("  " + l + "\n")
	}
	if end < len(lines) {
		b.WriteString(dim.Render(fmt.Sprintf("\n  … %d more lines", len(lines)-end)) + "\n")
	}

	b.WriteString(m.provocationBlock())
	b.WriteString("\n" + m.footer("[j/k] scroll  [a] lens  [p] provoke  [esc] back"))
	return b.String()
}

// provocationBlock renders the provocations for the selected section. It is
// shared by the list and reader views so an objection is never out of sight of
// the passage it attacks.
func (m *Model) provocationBlock() string {
	list := m.provocations()
	if len(list) == 0 {
		return ""
	}
	idx := clamp(m.provIdx, 0, len(list)-1)

	var b strings.Builder
	b.WriteString("\n")
	for i, p := range list {
		badge := warn.Render("open")
		switch p.Status {
		case provoke.StatusEngaged:
			badge = good.Render("engaged")
		case provoke.StatusDismissed:
			badge = dim.Render("dismissed")
		}
		head := brand.Render(p.Kind.Label()) + "  " + badge
		if i == idx {
			head = pick.Render("> ") + head
		} else {
			head = "  " + head
		}
		b.WriteString(head + "\n")
		b.WriteString(dim.Render(wrap("    "+p.Text, m.textWidth())) + "\n")
		if p.Response != "" {
			b.WriteString(dim.Render(truncate("    you: "+p.Response, m.textWidth())) + "\n")
		}
	}
	if len(list) > 1 {
		b.WriteString(dim.Render(fmt.Sprintf("  [n] next (%d/%d)", idx+1, len(list))) + "\n")
	}
	return b.String()
}

func (m *Model) header() string {
	q := m.ws.Question
	if q == "" {
		q = dim.Render("no question set")
	}
	line := bold.Render("Whetstone") + "  " + q
	right := dim.Render("lens: " + m.lensName())
	return fit(line, right, m.textWidth()) + "\n\n"
}

func (m *Model) footer(hint string) string {
	left := m.status
	if m.busy != "" {
		left = warn.Render("… " + m.busy)
	}
	if left == "" {
		left = dim.Render(hint)
	}
	e := m.ws.Engagement()
	return fit(left, dim.Render(e.Summary()), m.textWidth()) + "\n"
}

func (m *Model) viewHelp() string {
	return bold.Render("Whetstone — terminal") + "\n\n" +
		dim.Render("Reading and navigating happen here. Writing happens in the\n"+
			"browser UI, where a textarea does the job properly:\n") + "\n" +
		"    whetstone -web\n\n" +
		bold.Render("Keys") + "\n" +
		"  j k        move between sections\n" +
		"  enter      read the section (marks it read)\n" +
		"  a          apply the active lens\n" +
		"  L          switch lens\n" +
		"  p          ask for objections to this passage\n" +
		"  n          next provocation\n" +
		"  y / x      engage / dismiss  (both need a reason)\n" +
		"  s          save\n" +
		"  q          quit (saves)\n" +
		"  ?          this help\n\n" +
		dim.Render("Any key to go back.")
}

func (m *Model) lensName() string {
	if l, ok := lens.ByID(m.ws.ActiveLens); ok {
		return l.Name
	}
	return "none"
}

func (m *Model) hasOpen(r row) bool {
	for _, p := range m.provocationsFor(r) {
		if !p.Resolved() {
			return true
		}
	}
	return false
}

func (m *Model) provocationsFor(r row) []provoke.Provocation {
	return m.ws.ProvocationsFor(provoke.AnchorSection, sectionKey(r))
}

func (m *Model) textWidth() int {
	if m.width <= 0 {
		return 80
	}
	return m.width
}

// --- text helpers ---

// truncate shortens s to w display columns, measured with lipgloss.Width so
// styled and wide characters count correctly rather than by byte.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := words[0]
		for _, w := range words[1:] {
			if lipgloss.Width(line)+1+lipgloss.Width(w) > width {
				out = append(out, line)
				line = w
				continue
			}
			line += " " + w
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func fit(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return truncate(left, max(width, 1))
	}
	return left + strings.Repeat(" ", gap) + right
}
