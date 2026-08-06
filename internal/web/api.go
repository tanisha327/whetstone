package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/export"
	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/outline"
	"github.com/tanisha327/whetstone/internal/provider"
	"github.com/tanisha327/whetstone/internal/provoke"
	"github.com/tanisha327/whetstone/internal/rewrite"
	"github.com/tanisha327/whetstone/internal/workspace"
)

// requestTimeout bounds any single provider call made on behalf of a request.
const requestTimeout = 2 * time.Minute

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, s.snapshot())
}

func (s *Server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.Question = req.Question
	s.respond(w)
}

func (s *Server) handleSetLens(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LensID string `json:"lensId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if _, ok := lens.ByID(req.LensID); !ok {
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown lens %q", req.LensID))
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.ActiveLens = req.LensID
	s.respond(w)
}

// handleAddDocument parses pasted markdown into sections. This is how documents
// get in: no file picker, just text.
func (s *Server) handleAddDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Text  string `json:"text"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("pasted-%d", len(s.ws.Documents)+1)
	d := doc.Parse(id, req.Title, req.Text)
	if len(d.Sections) == 0 {
		fail(w, http.StatusBadRequest, errors.New("that text did not split into any sections"))
		return
	}
	s.ws.AddDocument(d)
	s.respond(w)
}

func (s *Server) handleDeleteDocument(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID string `json:"docId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.ws.Documents[:0]
	for _, d := range s.ws.Documents {
		if d.ID != req.DocID {
			kept = append(kept, d)
		}
	}
	s.ws.Documents = kept
	s.respond(w)
}

func (s *Server) handleMarkRead(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.MarkRead(req.DocID, req.SectionID)
	s.respond(w)
}

func (s *Server) handleApplyLens(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	// Resolve inputs under the lock, then release it: the provider call takes
	// seconds, and holding the mutex across it would stall every autosave.
	s.mu.Lock()
	d, ok := s.ws.Document(req.DocID)
	if !ok {
		s.mu.Unlock()
		fail(w, http.StatusNotFound, fmt.Errorf("no document %q", req.DocID))
		return
	}
	sec, ok := d.Section(req.SectionID)
	if !ok {
		s.mu.Unlock()
		fail(w, http.StatusNotFound, fmt.Errorf("no section %d", req.SectionID))
		return
	}
	l, ok := lens.ByID(s.ws.ActiveLens)
	s.mu.Unlock()
	if !ok {
		fail(w, http.StatusBadRequest, errors.New("no active lens"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	sum, err := lens.ApplySection(ctx, s.prov, l, sec)
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.PutSummary(req.DocID, sum)
	s.respond(w)
}

// handleProvoke critiques a source passage or the user's own argument,
// depending on the anchor.
func (s *Server) handleProvoke(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
		NodeID    string `json:"nodeId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	in := provoke.Input{Max: 2, Context: s.questionContext()}
	switch {
	case req.NodeID != "":
		n := s.ws.Outline.Find(req.NodeID)
		if n == nil {
			s.mu.Unlock()
			fail(w, http.StatusNotFound, fmt.Errorf("no outline point %q", req.NodeID))
			return
		}
		in.AnchorKind = provoke.AnchorOutline
		in.AnchorID = n.ID
		in.Subject = nodeSubject(n)
	default:
		d, ok := s.ws.Document(req.DocID)
		if !ok {
			s.mu.Unlock()
			fail(w, http.StatusNotFound, fmt.Errorf("no document %q", req.DocID))
			return
		}
		sec, ok := d.Section(req.SectionID)
		if !ok {
			s.mu.Unlock()
			fail(w, http.StatusNotFound, fmt.Errorf("no section %d", req.SectionID))
			return
		}
		in.AnchorKind = provoke.AnchorSection
		in.AnchorID = workspace.SectionKey(d.ID, sec.ID)
		in.Subject = sec.Body
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	items, err := provoke.Generate(ctx, s.prov, in)
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.ws.AddProvocations(items)
	s.respond(w)
}

// handleResolve records engagement or dismissal. The reason is mandatory; the
// provoke package enforces it and this handler surfaces the refusal.
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Engaged  bool   `json:"engaged"`
		Response string `json:"response"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	p := s.ws.Provocation(req.ID)
	if p == nil {
		fail(w, http.StatusNotFound, fmt.Errorf("no provocation %q", req.ID))
		return
	}
	var err error
	if req.Engaged {
		err = p.Engage(req.Response, time.Now())
	} else {
		err = p.Dismiss(req.Response, time.Now())
	}
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.respond(w)
}

// handleAddNode creates an outline point, optionally citing its source passage
// in the same action.
//
// One call, not two: a failure between an add and a cite would leave a point
// whose evidence silently went missing.
func (s *Server) handleAddNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ParentID string `json:"parentId"`
		Title    string `json:"title"`
		// Optional grounding, captured from a text selection.
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
		Excerpt   string `json:"excerpt"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n, err := s.ws.Outline.Add(req.ParentID, req.Title)
	if err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if req.DocID != "" {
		ref, err := s.groundingRef(req.DocID, req.SectionID, req.Excerpt)
		if err != nil {
			fail(w, http.StatusNotFound, err)
			return
		}
		if err := s.ws.Outline.Cite(n.ID, ref); err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
	}
	s.respond(w)
}

// handleUpdateNode is the autosave target for the notes and draft textareas.
// Fields are pointers so the client can send only what changed: a nil Notes
// means "leave the notes alone", which is not the same as "set them to empty".
func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID    string  `json:"id"`
		Title *string `json:"title"`
		Notes *string `json:"notes"`
		Draft *string `json:"draft"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	n := s.ws.Outline.Find(req.ID)
	if n == nil {
		fail(w, http.StatusNotFound, fmt.Errorf("no outline point %q", req.ID))
		return
	}
	if req.Title != nil {
		n.Title = *req.Title
	}
	if req.Notes != nil {
		n.Notes = *req.Notes
	}
	if req.Draft != nil && *req.Draft != n.Draft {
		// Editing generated prose makes it co-authored, which the engagement
		// report accounts for. Only a real change counts.
		n.Draft = *req.Draft
		n.DraftEdited = true
	}
	s.respond(w)
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ws.Outline.Remove(req.ID); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	s.respond(w)
}

func (s *Server) handleCite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID    string `json:"nodeId"`
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
		// Excerpt overrides the automatic opening-words excerpt with the exact
		// text the user selected.
		Excerpt string `json:"excerpt"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ref, err := s.groundingRef(req.DocID, req.SectionID, req.Excerpt)
	if err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	if err := s.ws.Outline.Cite(req.NodeID, ref); err != nil {
		fail(w, http.StatusNotFound, err)
		return
	}
	s.respond(w)
}

// maxExcerpt bounds a stored citation. A user can select a whole section; the
// excerpt is a pointer back to the source, not a copy of it.
const maxExcerpt = 600

// groundingRef resolves a citation, preferring the caller's selected text over
// the section's opening words. Callers hold s.mu.
func (s *Server) groundingRef(docID string, sectionID int, excerpt string) (outline.Ref, error) {
	d, ok := s.ws.Document(docID)
	if !ok {
		return outline.Ref{}, fmt.Errorf("no document %q", docID)
	}
	sec, ok := d.Section(sectionID)
	if !ok {
		return outline.Ref{}, fmt.Errorf("no section %d", sectionID)
	}

	text := strings.Join(strings.Fields(excerpt), " ")
	if text == "" {
		text = sec.Excerpt(240)
	}
	if r := []rune(text); len(r) > maxExcerpt {
		text = strings.TrimRight(string(r[:maxExcerpt]), " ") + "…"
	}
	return outline.Ref{DocID: d.ID, SectionID: sec.ID, Excerpt: text}, nil
}

// handleDraft turns one outline point's own notes and citations into prose. It
// refuses when the point has neither, because a draft with nothing of the
// author's in it is just the model talking.
func (s *Server) handleDraft(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	n := s.ws.Outline.Find(req.ID)
	if n == nil {
		s.mu.Unlock()
		fail(w, http.StatusNotFound, fmt.Errorf("no outline point %q", req.ID))
		return
	}
	if n.Notes == "" && len(n.Grounding) == 0 {
		s.mu.Unlock()
		fail(w, http.StatusBadRequest,
			errors.New("write notes or cite a section first — a draft needs something of yours to work from"))
		return
	}
	prompt := draftPrompt(s.ws.Question, n)
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	resp, err := s.prov.Complete(ctx, provider.Request{
		Purpose: provider.PurposeDraft,
		System: "You turn an author's own outline notes into prose. You add no " +
			"claims, no evidence, and no conclusions that are not already in the " +
			"notes or the cited excerpts. You are a typist with good grammar, not " +
			"a co-author.",
		Messages:    []provider.Message{{Role: provider.RoleUser, Text: prompt}},
		MaxTokens:   600,
		Temperature: 0.4,
	})
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.ws.Outline.Find(req.ID); n != nil {
		n.Draft = resp.Text
		n.DraftEdited = false
	}
	s.respond(w)
}

// handleExportDOCX streams the document as a Word file.
//
// No PDF endpoint: the browser's own print-to-PDF is better than anything worth
// hand-rolling, and costs no dependency.
func (s *Server) handleExportDOCX(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	d := export.FromWorkspace(s.ws, export.ParseScope(r.URL.Query().Get("scope")))
	s.mu.Unlock()

	w.Header().Set("Content-Type",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", d.Filename()+".docx"))
	if err := export.DOCX(w, d); err != nil {
		// The response is already committed, so there is no status left to
		// send; the truncated download is the signal.
		return
	}
}

// handleExportText streams the same document as plain text, for a diff, an
// email, or anywhere Word is not welcome.
func (s *Server) handleExportText(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	d := export.FromWorkspace(s.ws, export.ParseScope(r.URL.Query().Get("scope")))
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", d.Filename()+".txt"))
	_, _ = io.WriteString(w, d.Text())
}

// handleUpdateSection edits a source section in place.
//
// Source is normally read, not written, but a pasted document often arrives
// mangled. The risk is managed rather than forbidden: section IDs never move,
// and a citation whose text no longer appears is reported stale.
func (s *Server) handleUpdateSection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID     string  `json:"docId"`
		SectionID int     `json:"sectionId"`
		Title     *string `json:"title"`
		Body      *string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.ws.Document(req.DocID)
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no document %q", req.DocID))
		return
	}
	sec, ok := d.Section(req.SectionID)
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no section %d", req.SectionID))
		return
	}

	title, body := sec.Title, sec.Body
	if req.Title != nil {
		title = *req.Title
	}
	if req.Body != nil {
		body = *req.Body
	}
	if strings.TrimSpace(body) == "" {
		fail(w, http.StatusBadRequest,
			errors.New("a section cannot be empty — delete it instead"))
		return
	}
	d.SetSection(req.SectionID, title, body)
	s.respond(w)
}

func (s *Server) handleDeleteSection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DocID     string `json:"docId"`
		SectionID int    `json:"sectionId"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d, ok := s.ws.Document(req.DocID)
	if !ok {
		fail(w, http.StatusNotFound, fmt.Errorf("no document %q", req.DocID))
		return
	}
	if !d.RemoveSection(req.SectionID) {
		fail(w, http.StatusNotFound, fmt.Errorf("no section %d", req.SectionID))
		return
	}
	s.respond(w)
}

// handleRewrite returns alternative phrasings of a selected passage.
//
// The one endpoint that does not return workspace state, because it changes
// nothing. Applying a choice is an ordinary update.
func (s *Server) handleRewrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text        string `json:"text"`
		DimensionID string `json:"dimensionId"`
		Count       int    `json:"count"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	d, ok := rewrite.ByID(req.DimensionID)
	if !ok {
		fail(w, http.StatusBadRequest, fmt.Errorf("unknown dimension %q", req.DimensionID))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	alts, err := rewrite.Alternatives(ctx, s.prov, d, req.Text, req.Count)
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}
	if alts == nil {
		alts = []string{}
	}
	writeJSON(w, map[string]any{"alternatives": alts})
}

// handleCompose writes a paragraph from the author's instruction, notes, and
// citations.
//
// The instruction steers; the notes and citations are the only source material.
// A steering wheel, not a prompt box — see docs/adr/0001-no-chat-box.md.
func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string `json:"id"`
		Instruction string `json:"instruction"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Instruction) == "" {
		fail(w, http.StatusBadRequest, errors.New("say what you want this paragraph to do"))
		return
	}

	s.mu.Lock()
	n := s.ws.Outline.Find(req.ID)
	if n == nil {
		s.mu.Unlock()
		fail(w, http.StatusNotFound, fmt.Errorf("no outline point %q", req.ID))
		return
	}
	if n.Notes == "" && len(n.Grounding) == 0 {
		s.mu.Unlock()
		fail(w, http.StatusBadRequest,
			errors.New("write notes or cite a section first — an instruction is not source material"))
		return
	}
	prompt := composePrompt(s.ws.Question, n, req.Instruction)
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	resp, err := s.prov.Complete(ctx, provider.Request{
		Purpose: provider.PurposeCompose,
		System: "You write one paragraph for an author, following their instruction, " +
			"using only the notes and cited excerpts they supply. You add no claims, " +
			"no evidence, and no conclusions that are not already in their material. " +
			"If the instruction asks for something the material cannot support, write " +
			"what the material supports and say nothing more.",
		Messages:    []provider.Message{{Role: provider.RoleUser, Text: prompt}},
		MaxTokens:   700,
		Temperature: 0.5,
	})
	if err != nil {
		fail(w, http.StatusBadGateway, err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.ws.Outline.Find(req.ID); n != nil {
		n.Draft = resp.Text
		n.DraftEdited = false
	}
	s.respond(w)
}

func composePrompt(question string, n *outline.Node, instruction string) string {
	s := "POINT: " + n.Title
	if question != "" {
		s += "\n\nOVERALL QUESTION: " + question
	}
	s += "\n\nTHE AUTHOR'S INSTRUCTION FOR THIS PARAGRAPH:\n" + instruction
	if n.Notes != "" {
		s += "\n\nTHE AUTHOR'S NOTES (the substance — use all of it):\n" + n.Notes
	}
	for _, g := range n.Grounding {
		s += "\n\nCITED EXCERPT (" + g.DocID + "):\n" + g.Excerpt
	}
	return s + "\n\nWrite one paragraph. Plain prose, no heading."
}

// respond saves the workspace and returns the new state. Callers hold s.mu.
//
// Saving on every mutation is cheap (an atomic write of a few kilobytes) and
// means closing the tab cannot lose work.
func (s *Server) respond(w http.ResponseWriter) {
	if err := s.save(); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, s.snapshot())
}

// questionContext frames provocations around what the user is actually trying
// to answer. Callers hold s.mu.
func (s *Server) questionContext() string {
	if s.ws.Question == "" {
		return ""
	}
	return "The author is trying to answer: " + s.ws.Question
}

func nodeSubject(n *outline.Node) string {
	s := n.Title
	if n.Notes != "" {
		s += "\n" + n.Notes
	}
	for _, g := range n.Grounding {
		s += "\nCited: " + g.Excerpt
	}
	if n.Draft != "" {
		s += "\n" + n.Draft
	}
	return s
}

func draftPrompt(question string, n *outline.Node) string {
	s := "POINT: " + n.Title
	if question != "" {
		s += "\n\nOVERALL QUESTION: " + question
	}
	if n.Notes != "" {
		s += "\n\nAUTHOR'S NOTES (the substance — use all of it):\n" + n.Notes
	}
	for _, g := range n.Grounding {
		s += "\n\nCITED EXCERPT (" + g.DocID + "):\n" + g.Excerpt
	}
	return s + "\n\nWrite one paragraph making this point, using only the notes and " +
		"excerpts above. Do not add supporting claims of your own. Plain prose, no heading."
}
