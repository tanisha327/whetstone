// Package export renders a workspace as a document: Word or plain text.
//
// Exports also carry the objections you have not answered, so a finished
// document cannot quietly drop its open questions on the way to the printer.
package export

import (
	"fmt"
	"strings"

	"github.com/tanisha327/whetstone/internal/provoke"
	"github.com/tanisha327/whetstone/internal/workspace"
)

// Style classifies a block so a renderer can format it. The model is flat on
// purpose: adding a renderer should not mean learning a tree.
type Style int

const (
	// StyleTitle is the document title, once, at the top.
	StyleTitle Style = iota
	// StyleHeading is an outline point. Level 1..3 follows the point's depth.
	StyleHeading
	// StyleBody is ordinary prose.
	StyleBody
	// StyleQuote is a cited excerpt from a source.
	StyleQuote
	// StyleMeta is small print: engagement figures, the caveat, stale markers.
	StyleMeta
)

// Scope selects what goes into an export.
type Scope int

const (
	// ScopeAll exports the sources you read and the argument you built. This
	// is the default: a reader who has only the conclusions cannot check them.
	ScopeAll Scope = iota
	// ScopeArgument exports only your points, prose, and citations.
	ScopeArgument
	// ScopeSources exports only the source documents, as you have them now
	// (including any edits you made).
	ScopeSources
)

// ParseScope maps the query parameter onto a Scope, defaulting to ScopeAll.
func ParseScope(s string) Scope {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "argument":
		return ScopeArgument
	case "sources":
		return ScopeSources
	default:
		return ScopeAll
	}
}

// Block is one paragraph of the exported document.
type Block struct {
	Style Style
	// Level is the heading depth, 1-based. Ignored unless Style is StyleHeading.
	Level int
	Text  string
}

// Doc is a flat document ready to render.
type Doc struct {
	Title  string
	Blocks []Block
}

// FromWorkspace builds the exportable document for the given scope.
func FromWorkspace(ws *workspace.Workspace, scope Scope) Doc {
	title := strings.TrimSpace(ws.Question)
	if title == "" {
		title = strings.TrimSpace(ws.Name)
	}
	if title == "" {
		title = "Whetstone"
	}

	d := Doc{Title: title}
	add := func(style Style, level int, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		d.Blocks = append(d.Blocks, Block{Style: style, Level: level, Text: text})
	}

	add(StyleTitle, 0, title)

	if scope == ScopeAll || scope == ScopeArgument {
		addArgument(ws, add)
	}
	if scope == ScopeAll || scope == ScopeSources {
		addSources(ws, add)
	}

	if len(d.Blocks) == 1 {
		add(StyleBody, 0, "This workspace is empty.")
	}
	addOpenObjections(ws, add)
	addEngagement(ws, add)
	return d
}

// addArgument writes the points, prose, and citations.
func addArgument(ws *workspace.Workspace, add func(Style, int, string)) {
	if ws.Outline.Len() == 0 {
		return
	}
	add(StyleHeading, 1, "The argument")

	for _, f := range ws.Outline.Flatten() {
		n := f.Node
		add(StyleHeading, min(f.Depth+2, 3), n.Title)

		// The prose if there is any, otherwise the author's notes: an outline
		// that was never drafted should still export as something readable.
		if strings.TrimSpace(n.Draft) != "" {
			add(StyleBody, 0, n.Draft)
		} else if strings.TrimSpace(n.Notes) != "" {
			add(StyleBody, 0, n.Notes)
		}

		for _, g := range n.Grounding {
			add(StyleQuote, 0, fmt.Sprintf("%s — %s §%d", g.Excerpt, g.DocID, g.SectionID))
		}
	}
}

// addSources writes the source documents in full, so the finished document
// carries the material its claims rest on rather than only the conclusions.
func addSources(ws *workspace.Workspace, add func(Style, int, string)) {
	if len(ws.Documents) == 0 {
		return
	}
	add(StyleHeading, 1, "Sources")

	for _, doc := range ws.Documents {
		add(StyleHeading, 2, doc.Title)
		for _, sec := range doc.Sections {
			add(StyleHeading, 3, fmt.Sprintf("§%d  %s", sec.ID, sec.Title))
			add(StyleBody, 0, sec.Body)
		}
	}
}

// addOpenObjections carries the unanswered objections into the document.
func addOpenObjections(ws *workspace.Workspace, add func(Style, int, string)) {
	var open []provoke.Provocation
	for _, p := range ws.Provocations {
		if !p.Resolved() {
			open = append(open, p)
		}
	}
	if len(open) > 0 {
		add(StyleHeading, 1, "Objections still open")
		for _, p := range open {
			add(StyleBody, 0, p.Kind.Label()+": "+p.Text)
		}
	}
}

func addEngagement(ws *workspace.Workspace, add func(Style, int, string)) {
	e := ws.Engagement()
	add(StyleMeta, 0, fmt.Sprintf(
		"Sections opened %d of %d · objections %d resolved, %d open · %d%% of the words are the author's.",
		e.SectionsRead, e.SectionsTotal,
		e.ProvocationsEngaged+e.ProvocationsDismissed, e.ProvocationsOpen,
		int(e.AuthorshipFraction()*100+0.5)))
	add(StyleMeta, 0, workspace.Caveat)
}

// Filename returns a filesystem-safe name for the exported document, without
// an extension.
func (d Doc) Filename() string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(d.Title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	name := strings.Trim(b.String(), "-")
	if name == "" {
		return "whetstone"
	}
	if len(name) > 60 {
		name = strings.Trim(name[:60], "-")
	}
	return name
}

// Text renders the document as plain text. It is the reference rendering: if
// something is missing here, it is missing from the model, not the renderer.
func (d Doc) Text() string {
	var b strings.Builder
	for _, blk := range d.Blocks {
		switch blk.Style {
		case StyleTitle:
			b.WriteString(blk.Text + "\n")
			b.WriteString(strings.Repeat("=", len([]rune(blk.Text))) + "\n\n")
		case StyleHeading:
			b.WriteString("\n" + strings.Repeat("  ", blk.Level-1) + blk.Text + "\n\n")
		case StyleQuote:
			for _, line := range strings.Split(blk.Text, "\n") {
				b.WriteString("    > " + line + "\n")
			}
			b.WriteString("\n")
		case StyleMeta:
			b.WriteString("\n" + blk.Text + "\n")
		default:
			b.WriteString(blk.Text + "\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
