package web

import (
	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/provoke"
	"github.com/tanisha327/whetstone/internal/rewrite"
	"github.com/tanisha327/whetstone/internal/workspace"
)

// state is the whole workspace as the browser sees it.
//
// Every mutating endpoint returns one of these. Sending the complete state on
// each action costs a few kilobytes and removes an entire class of bug: the
// client cannot drift out of sync because it never merges anything.
type state struct {
	Question   string      `json:"question"`
	ActiveLens string      `json:"activeLens"`
	Lenses     []lensView  `json:"lenses"`
	Dimensions []lensView  `json:"dimensions"`
	Documents  []docView   `json:"documents"`
	Outline    []nodeView  `json:"outline"`
	Engagement engagement  `json:"engagement"`
	Provider   providerRef `json:"provider"`
	Path       string      `json:"path"`
}

type lensView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type docView struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Sections []sectionView `json:"sections"`
}

type sectionView struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body"`
	Words int    `json:"words"`
	Read  bool   `json:"read"`
	// Heading is true when this section came from a real markdown heading, and
	// false when the title was synthesised from the opening words of a
	// paragraph. The sidebar renders the two differently: otherwise a chunk of
	// prose looks exactly like a section title.
	Heading bool `json:"heading"`
	Level   int  `json:"level"`
	// Summary is the active lens's take on this section, if one has been
	// generated. Empty text means "not yet".
	Summary      summaryView       `json:"summary"`
	Provocations []provocationView `json:"provocations"`
}

type summaryView struct {
	Text      string   `json:"text"`
	KeyPoints []string `json:"keyPoints"`
	Relevance int      `json:"relevance"`
}

type nodeView struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	Notes        string            `json:"notes"`
	Draft        string            `json:"draft"`
	DraftEdited  bool              `json:"draftEdited"`
	Depth        int               `json:"depth"`
	Grounding    []groundingView   `json:"grounding"`
	Provocations []provocationView `json:"provocations"`
}

type groundingView struct {
	DocID     string `json:"docId"`
	SectionID int    `json:"sectionId"`
	Excerpt   string `json:"excerpt"`
	// Stale is true when the quoted text no longer appears in the section,
	// because the source was edited or the section deleted. The citation is
	// kept — what you quoted is still what you read — but it is flagged, so a
	// claim is never quietly left resting on evidence that has moved.
	Stale bool `json:"stale"`
}

type provocationView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Response string `json:"response"`
}

type engagement struct {
	SectionsRead  int     `json:"sectionsRead"`
	SectionsTotal int     `json:"sectionsTotal"`
	Open          int     `json:"open"`
	Resolved      int     `json:"resolved"`
	UserWords     int     `json:"userWords"`
	GenWords      int     `json:"genWords"`
	Authorship    float64 `json:"authorship"`
	Caveat        string  `json:"caveat"`
}

type providerRef struct {
	Name string `json:"name"`
}

// snapshot builds the client's view. Callers hold s.mu.
//
// Every slice is initialised empty, never left nil: a nil slice marshals to
// JSON null, and `state.outline.length` on null is a TypeError that kills
// whatever handler touched it.
func (s *Server) snapshot() state {
	ws := s.ws

	out := state{
		Question:   ws.Question,
		ActiveLens: ws.ActiveLens,
		Path:       ws.Path(),
		Provider:   providerRef{Name: s.prov.Name()},
		Lenses:     []lensView{},
		Dimensions: []lensView{},
		Documents:  []docView{},
		Outline:    []nodeView{},
	}

	for _, l := range lens.Builtin {
		out.Lenses = append(out.Lenses, lensView{ID: l.ID, Name: l.Name, Description: l.Description})
	}
	for _, d := range rewrite.Builtin {
		out.Dimensions = append(out.Dimensions, lensView{ID: d.ID, Name: d.Name, Description: d.Description})
	}

	for _, d := range ws.Documents {
		dv := docView{ID: d.ID, Title: d.Title, Sections: []sectionView{}}
		for _, sec := range d.Sections {
			sv := sectionView{
				ID:      sec.ID,
				Title:   sec.Title,
				Body:    sec.Body,
				Words:   sec.WordCount(),
				Read:    ws.IsRead(d.ID, sec.ID),
				Heading: sec.Level > 0,
				Level:   sec.Level,
				Summary: summaryView{KeyPoints: []string{}},
			}
			if sum, ok := ws.Summary(d.ID, sec.ID, ws.ActiveLens); ok {
				sv.Summary = summaryView{
					Text:      sum.Text,
					KeyPoints: sum.KeyPoints,
					Relevance: sum.Relevance,
				}
				if sv.Summary.KeyPoints == nil {
					sv.Summary.KeyPoints = []string{}
				}
			}
			sv.Provocations = viewProvocations(
				ws.ProvocationsFor(provoke.AnchorSection, workspace.SectionKey(d.ID, sec.ID)))
			dv.Sections = append(dv.Sections, sv)
		}
		out.Documents = append(out.Documents, dv)
	}

	for _, f := range ws.Outline.Flatten() {
		n := f.Node
		nv := nodeView{
			ID:          n.ID,
			Title:       n.Title,
			Notes:       n.Notes,
			Draft:       n.Draft,
			DraftEdited: n.DraftEdited,
			Depth:       f.Depth,
			Grounding:   []groundingView{},
		}
		for _, g := range n.Grounding {
			nv.Grounding = append(nv.Grounding, groundingView{
				DocID: g.DocID, SectionID: g.SectionID, Excerpt: g.Excerpt,
				Stale: s.citationStale(g.DocID, g.SectionID, g.Excerpt),
			})
		}
		nv.Provocations = viewProvocations(ws.ProvocationsFor(provoke.AnchorOutline, n.ID))
		out.Outline = append(out.Outline, nv)
	}

	e := ws.Engagement()
	out.Engagement = engagement{
		SectionsRead:  e.SectionsRead,
		SectionsTotal: e.SectionsTotal,
		Open:          e.ProvocationsOpen,
		Resolved:      e.ProvocationsEngaged + e.ProvocationsDismissed,
		UserWords:     e.UserWords,
		GenWords:      e.GeneratedWords,
		Authorship:    e.AuthorshipFraction(),
		Caveat:        workspace.Caveat,
	}
	return out
}

// citationStale reports whether a citation no longer matches its source.
// Callers hold s.mu.
func (s *Server) citationStale(docID string, sectionID int, excerpt string) bool {
	d, ok := s.ws.Document(docID)
	if !ok {
		return true
	}
	sec, ok := d.Section(sectionID)
	if !ok {
		return true
	}
	return !sec.Contains(excerpt)
}

// viewProvocations orders unresolved first, which is the order attention
// should follow.
func viewProvocations(in []provoke.Provocation) []provocationView {
	open := make([]provocationView, 0, len(in))
	done := make([]provocationView, 0, len(in))
	for _, p := range in {
		v := provocationView{
			ID:       p.ID,
			Kind:     string(p.Kind),
			Label:    p.Kind.Label(),
			Text:     p.Text,
			Status:   string(p.Status),
			Response: p.Response,
		}
		if p.Resolved() {
			done = append(done, v)
		} else {
			open = append(open, v)
		}
	}
	return append(open, done...)
}
