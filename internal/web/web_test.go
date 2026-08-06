package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/provider"
	"github.com/tanisha327/whetstone/internal/workspace"
)

// fakeProvider is a test double, not a shipped mock. It records requests and
// returns whatever the test hands it.
type fakeProvider struct {
	reply string
	err   error
	calls []provider.Request
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return provider.Response{}, f.err
	}
	return provider.Response{Text: f.reply, Model: "fake-1"}, nil
}

func newTestServer(t *testing.T) (*Server, *fakeProvider) {
	t.Helper()
	ws := workspace.New("test", filepath.Join(t.TempDir(), "ws.json"))
	ws.AddDocument(doc.Parse("report", "Report",
		"# One\n\nFirst body text.\n\n# Two\n\nSecond body text.\n"))
	p := &fakeProvider{}
	s, err := NewServer(ws, p)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return s, p
}

// post issues an authenticated request and decodes the returned state.
func post(t *testing.T, s *Server, path string, body any) (state, int) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	method := http.MethodPost
	if body == nil {
		method = http.MethodGet
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	var st state
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	return st, rec.Code
}

func TestState(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/state", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(st.Documents) != 1 || len(st.Documents[0].Sections) != 2 {
		t.Fatalf("documents = %+v", st.Documents)
	}
	if len(st.Lenses) == 0 {
		t.Error("no lenses in state")
	}
	if st.Provider.Name != "fake" {
		t.Errorf("provider = %q", st.Provider.Name)
	}
}

// Loopback is not a security boundary: any local process, and any web page the
// user has open, can reach this server.
func TestAuth_RejectsMissingToken(t *testing.T) {
	s, _ := newTestServer(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/state"},
		{http.MethodPost, "/api/question"},
		{http.MethodPost, "/api/provoke"},
		{http.MethodPost, "/api/outline/add"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s without a token = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
}

func TestAuth_RejectsWrongToken(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Whetstone-Token", "not-the-token")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestIndex_RequiresToken(t *testing.T) {
	s, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Errorf("index without a token = %d, want 403", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?t="+s.Token(), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("index with token = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, s.Token()) {
		t.Error("token was not substituted into the page")
	}
	if strings.Contains(body, "__TOKEN__") {
		t.Error("placeholder survived substitution")
	}
}

func TestStaticAssets(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{"/static/app.css", "/static/app.js"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s served an empty body", path)
		}
	}
}

// Pasting text is how documents get in; there is no file picker.
func TestAddDocument_FromPastedText(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/documents", map[string]string{
		"title": "Pasted",
		"text":  "# Alpha\n\nBody one.\n\n# Beta\n\nBody two.\n",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(st.Documents) != 2 {
		t.Fatalf("documents = %d, want 2", len(st.Documents))
	}
	added := st.Documents[1]
	if len(added.Sections) != 2 {
		t.Errorf("sections = %d, want 2", len(added.Sections))
	}
	if added.Sections[0].Title != "Alpha" {
		t.Errorf("first section = %q", added.Sections[0].Title)
	}
}

func TestAddDocument_EmptyIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	_, code := post(t, s, "/api/documents", map[string]string{"text": "   "})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestMarkRead(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/sections/read", map[string]any{"docId": "report", "sectionId": 1})
	if !st.Documents[0].Sections[0].Read {
		t.Error("section 1 not marked read")
	}
	if st.Engagement.SectionsRead != 1 {
		t.Errorf("SectionsRead = %d, want 1", st.Engagement.SectionsRead)
	}
}

func TestApplyLens(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"text":"Orientation.","key_points":["one","two"],"relevance":7}`

	st, code := post(t, s, "/api/sections/lens", map[string]any{"docId": "report", "sectionId": 1})
	if code != http.StatusOK {
		t.Fatalf("status = %d, body error", code)
	}
	sum := st.Documents[0].Sections[0].Summary
	if sum.Text != "Orientation." || len(sum.KeyPoints) != 2 || sum.Relevance != 7 {
		t.Errorf("summary = %+v", sum)
	}
	if len(p.calls) != 1 || p.calls[0].Purpose != provider.PurposeLens {
		t.Errorf("provider calls = %+v", p.calls)
	}
}

func TestProvoke_Section(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"provocations":[{"kind":"evidence_gap","text":"Where is the control group?"}]}`

	st, code := post(t, s, "/api/provoke", map[string]any{"docId": "report", "sectionId": 1})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	provs := st.Documents[0].Sections[0].Provocations
	if len(provs) != 1 {
		t.Fatalf("provocations = %+v", provs)
	}
	if provs[0].Status != "open" || provs[0].Label != "Evidence gap" {
		t.Errorf("provocation = %+v", provs[0])
	}
	if p.calls[0].Purpose != provider.PurposeProvokeSection {
		t.Errorf("purpose = %q", p.calls[0].Purpose)
	}
}

func TestProvoke_Outline(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"provocations":[{"kind":"assumption","text":"Says who?"}]}`

	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "My point"})
	id := st.Outline[0].ID

	st, code := post(t, s, "/api/provoke", map[string]any{"nodeId": id})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(st.Outline[0].Provocations) != 1 {
		t.Fatalf("provocations = %+v", st.Outline[0].Provocations)
	}
	if p.calls[0].Purpose != provider.PurposeProvokeOutline {
		t.Errorf("purpose = %q", p.calls[0].Purpose)
	}
}

// A provider failure must reach the user. "No provocations" and "the provider
// is down" must not look the same.
func TestProvoke_ProviderErrorIsSurfaced(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = ""
	p.err = errTest("upstream exploded")

	req := httptest.NewRequest(http.MethodPost, "/api/provoke",
		strings.NewReader(`{"docId":"report","sectionId":1}`))
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], "upstream exploded") {
		t.Errorf("error = %q", body["error"])
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

// Requiring a reason is the mechanism, so the API must refuse a blank one.
func TestResolve_RequiresReason(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"provocations":[{"kind":"fallacy","text":"Non sequitur."}]}`
	st, _ := post(t, s, "/api/provoke", map[string]any{"docId": "report", "sectionId": 1})
	id := st.Documents[0].Sections[0].Provocations[0].ID

	_, code := post(t, s, "/api/provocations/resolve",
		map[string]any{"id": id, "engaged": false, "response": "   "})
	if code != http.StatusBadRequest {
		t.Errorf("blank reason = %d, want 400", code)
	}

	st, code = post(t, s, "/api/provocations/resolve",
		map[string]any{"id": id, "engaged": false, "response": "sample matches our segment"})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	got := st.Documents[0].Sections[0].Provocations[0]
	if got.Status != "dismissed" || got.Response != "sample matches our segment" {
		t.Errorf("provocation = %+v", got)
	}
	if st.Engagement.Resolved != 1 {
		t.Errorf("Resolved = %d, want 1", st.Engagement.Resolved)
	}
}

func TestOutline_AddUpdateDelete(t *testing.T) {
	s, _ := newTestServer(t)

	st, code := post(t, s, "/api/outline/add", map[string]string{"title": "Market shift"})
	if code != http.StatusOK || len(st.Outline) != 1 {
		t.Fatalf("add: status %d, outline %+v", code, st.Outline)
	}
	id := st.Outline[0].ID

	st, _ = post(t, s, "/api/outline/add", map[string]string{"parentId": id, "title": "Sub"})
	if len(st.Outline) != 2 || st.Outline[1].Depth != 1 {
		t.Fatalf("sub-point = %+v", st.Outline)
	}

	st, _ = post(t, s, "/api/outline/update", map[string]any{"id": id, "notes": "my reasoning"})
	if st.Outline[0].Notes != "my reasoning" {
		t.Errorf("notes = %q", st.Outline[0].Notes)
	}
	if st.Engagement.UserWords == 0 {
		t.Error("notes should count as user words")
	}

	st, _ = post(t, s, "/api/outline/delete", map[string]string{"id": id})
	if len(st.Outline) != 0 {
		t.Errorf("outline after delete = %+v", st.Outline)
	}
}

// A partial update must not blank the fields it did not mention.
func TestOutline_UpdateIsPartial(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID

	post(t, s, "/api/outline/update", map[string]any{"id": id, "notes": "keep me"})
	st, _ = post(t, s, "/api/outline/update", map[string]any{"id": id, "draft": "some prose"})

	if st.Outline[0].Notes != "keep me" {
		t.Errorf("notes = %q, want them preserved", st.Outline[0].Notes)
	}
	if !st.Outline[0].DraftEdited {
		t.Error("editing the draft should mark it edited")
	}
}

func TestCite(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID

	st, code := post(t, s, "/api/outline/cite",
		map[string]any{"nodeId": id, "docId": "report", "sectionId": 2})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	g := st.Outline[0].Grounding
	if len(g) != 1 || g[0].SectionID != 2 || g[0].Excerpt == "" {
		t.Errorf("grounding = %+v", g)
	}
}

// A draft must have something of the author's to work from.
func TestDraft_RefusesEmptyPoint(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = "prose"
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Bare point"})
	id := st.Outline[0].ID

	_, code := post(t, s, "/api/outline/draft", map[string]string{"id": id})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
	if len(p.calls) != 0 {
		t.Error("an empty point should not cost a provider call")
	}
}

func TestDraft_FromNotes(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = "Generated paragraph."
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID
	post(t, s, "/api/outline/update", map[string]any{"id": id, "notes": "my own reasoning here"})

	st, code := post(t, s, "/api/outline/draft", map[string]string{"id": id})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Outline[0].Draft != "Generated paragraph." {
		t.Errorf("draft = %q", st.Outline[0].Draft)
	}
	if st.Outline[0].DraftEdited {
		t.Error("a fresh draft is not edited by the user")
	}
	if p.calls[0].Purpose != provider.PurposeDraft {
		t.Errorf("purpose = %q", p.calls[0].Purpose)
	}
	if !strings.Contains(p.calls[0].Messages[0].Text, "my own reasoning here") {
		t.Error("the prompt must carry the author's notes")
	}
}

func TestSetLens(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/lens", map[string]string{"lensId": "risk"})
	if code != http.StatusOK || st.ActiveLens != "risk" {
		t.Errorf("status %d, activeLens %q", code, st.ActiveLens)
	}
	_, code = post(t, s, "/api/lens", map[string]string{"lensId": "nope"})
	if code != http.StatusBadRequest {
		t.Errorf("unknown lens = %d, want 400", code)
	}
}

func TestQuestion(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/question", map[string]string{"question": "Should we adopt it?"})
	if st.Question != "Should we adopt it?" {
		t.Errorf("question = %q", st.Question)
	}
}

// Every mutation persists, so closing the tab cannot lose work.
func TestMutationsPersistToDisk(t *testing.T) {
	ws := workspace.New("test", filepath.Join(t.TempDir(), "ws.json"))
	s, err := NewServer(ws, &fakeProvider{})
	if err != nil {
		t.Fatal(err)
	}
	post(t, s, "/api/question", map[string]string{"question": "persisted?"})

	reloaded, err := workspace.Load(ws.Path())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if reloaded.Question != "persisted?" {
		t.Errorf("reloaded question = %q", reloaded.Question)
	}
}

func TestReadJSON_RejectsUnknownFields(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/question",
		strings.NewReader(`{"kwestion":"typo"}`))
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown field", rec.Code)
	}
}

func TestListenAndURL(t *testing.T) {
	s, _ := newTestServer(t)
	ln, err := s.Listen(0)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	url := s.URL(ln)
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Errorf("URL = %q, want a loopback address", url)
	}
	if !strings.Contains(url, s.Token()) {
		t.Error("URL does not carry the session token")
	}
}

func TestTokensAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		s, _ := newTestServer(t)
		if seen[s.Token()] {
			t.Fatal("duplicate session token")
		}
		seen[s.Token()] = true
	}
}

// Selecting a line and turning it into a point must attach the claim to its
// evidence in one action; a failure between the two would leave a point whose
// grounding silently went missing.
func TestAddNode_WithSelection(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/outline/add", map[string]any{
		"title":     "The win is narrow",
		"docId":     "report",
		"sectionId": 2,
		"excerpt":   "Second   body\n text.",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(st.Outline) != 1 {
		t.Fatalf("outline = %+v", st.Outline)
	}
	g := st.Outline[0].Grounding
	if len(g) != 1 {
		t.Fatalf("grounding = %+v, want the selection cited", g)
	}
	if g[0].Excerpt != "Second body text." {
		t.Errorf("excerpt = %q, want whitespace collapsed", g[0].Excerpt)
	}
	if g[0].SectionID != 2 {
		t.Errorf("sectionId = %d, want 2", g[0].SectionID)
	}
}

func TestAddNode_WithoutSelectionHasNoGrounding(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Plain point"})
	if len(st.Outline[0].Grounding) != 0 {
		t.Errorf("grounding = %+v, want none", st.Outline[0].Grounding)
	}
}

func TestAddNode_BadSectionIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	_, code := post(t, s, "/api/outline/add", map[string]any{
		"title": "x", "docId": "nope", "sectionId": 1, "excerpt": "y",
	})
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", code)
	}
}

func TestCite_SelectionOverridesDefaultExcerpt(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID

	st, code := post(t, s, "/api/outline/cite", map[string]any{
		"nodeId": id, "docId": "report", "sectionId": 1, "excerpt": "just this clause",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if got := st.Outline[0].Grounding[0].Excerpt; got != "just this clause" {
		t.Errorf("excerpt = %q, want the selection", got)
	}
}

// A user can select an entire section; the excerpt is a pointer back to the
// source, not a second copy of it.
func TestCite_ExcerptIsBounded(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID

	huge := strings.Repeat("word ", 400)
	st, _ = post(t, s, "/api/outline/cite", map[string]any{
		"nodeId": id, "docId": "report", "sectionId": 1, "excerpt": huge,
	})
	got := st.Outline[0].Grounding[0].Excerpt
	if len([]rune(got)) > maxExcerpt+1 {
		t.Errorf("excerpt is %d runes, want <= %d", len([]rune(got)), maxExcerpt+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated excerpt should be marked: %q", got)
	}
}

// Pasted markdown must arrive as prose, not as syntax to decode.
func TestAddDocument_ConvertsMarkdownToProse(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/documents", map[string]string{
		"text": "# Findings\n\nThe **premium** is real per [the report](https://x).\n\n- one\n- two\n",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	body := st.Documents[1].Sections[0].Body
	for _, syntax := range []string{"**", "](", "# "} {
		if strings.Contains(body, syntax) {
			t.Errorf("markdown %q survived: %q", syntax, body)
		}
	}
	if !strings.Contains(body, "premium") || !strings.Contains(body, "the report") {
		t.Errorf("text was lost: %q", body)
	}
}

// The reader renders sectionView.Body. If that is ever empty the passage shows
// as a title and some buttons with nothing to read.
func TestPastedDocumentHasReadableBody(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/documents", map[string]string{
		"text": "# Benchmark results\n\nThe latency win is **not** uniform.\nIt concentrates in one workload shape.\n\n## Cost\n\nThe scheduler adds 8% CPU overhead.\n",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	doc := st.Documents[1]
	if len(doc.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(doc.Sections))
	}
	for _, sec := range doc.Sections {
		if strings.TrimSpace(sec.Body) == "" {
			t.Errorf("section %d (%q) has an empty body — nothing to read", sec.ID, sec.Title)
		}
		if sec.Words == 0 {
			t.Errorf("section %d reports 0 words", sec.ID)
		}
	}
	if !strings.Contains(doc.Sections[0].Body, "one workload shape") {
		t.Errorf("body text was lost: %q", doc.Sections[0].Body)
	}
}

// A nil Go slice marshals to JSON null, and the client does
// `state.outline.length`. A null there is not an empty list, it is a TypeError
// that kills whatever handler touched it — which is how every selection button
// silently stopped working. Empty workspace, empty arrays.
func TestSnapshot_NeverEmitsNullArrays(t *testing.T) {
	ws := workspace.New("empty", filepath.Join(t.TempDir(), "ws.json"))
	s, err := NewServer(ws, &fakeProvider{})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, field := range []string{"documents", "outline", "lenses"} {
		if strings.Contains(body, `"`+field+`":null`) {
			t.Errorf("%s serialised as null:\n%s", field, body)
		}
	}
}

// Same hazard one level down: a point with no citations, a section with no
// key points.
func TestSnapshot_NestedArraysAreNeverNull(t *testing.T) {
	s, _ := newTestServer(t)
	post(t, s, "/api/outline/add", map[string]string{"title": "No citations yet"})

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, field := range []string{"grounding", "provocations", "keyPoints", "sections"} {
		if strings.Contains(body, `"`+field+`":null`) {
			t.Errorf("%s serialised as null:\n%s", field, body)
		}
	}
}

// The sidebar has to distinguish a real heading from a chunk of prose whose
// title was synthesised from its opening words.
func TestSectionsReportWhetherTheyAreHeadings(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/documents", map[string]string{
		"text": "# Real Heading\n\nBody under it.\n",
	})
	sec := st.Documents[1].Sections[0]
	if !sec.Heading || sec.Level != 1 {
		t.Errorf("heading section = %+v, want Heading true, Level 1", sec)
	}

	st, _ = post(t, s, "/api/documents", map[string]string{
		"text": strings.Repeat("Just loose prose with no headings at all. ", 40),
	})
	sec = st.Documents[2].Sections[0]
	if sec.Heading {
		t.Errorf("prose chunk reported as a heading: %+v", sec)
	}
}

func TestRewrite_ReturnsAlternatives(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"alternatives":["Plainer one.","Plainer two.","Plainer three."]}`

	req := httptest.NewRequest(http.MethodPost, "/api/rewrite",
		strings.NewReader(`{"text":"The original passage.","dimensionId":"plain","count":3}`))
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Alternatives []string `json:"alternatives"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Alternatives) != 3 {
		t.Errorf("alternatives = %+v, want 3", got.Alternatives)
	}
	if p.calls[0].Purpose != provider.PurposeRewrite {
		t.Errorf("purpose = %q", p.calls[0].Purpose)
	}
}

// Rewriting changes nothing until the author picks an option, so the endpoint
// must not touch the workspace.
func TestRewrite_DoesNotMutateWorkspace(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = `{"alternatives":["An option."]}`
	before, _ := post(t, s, "/api/state", nil)

	req := httptest.NewRequest(http.MethodPost, "/api/rewrite",
		strings.NewReader(`{"text":"passage","dimensionId":"formal"}`))
	req.Header.Set("X-Whetstone-Token", s.Token())
	s.ServeHTTP(httptest.NewRecorder(), req)

	after, _ := post(t, s, "/api/state", nil)
	if before.Engagement != after.Engagement || len(before.Outline) != len(after.Outline) {
		t.Error("rewrite mutated the workspace")
	}
}

func TestRewrite_UnknownDimension(t *testing.T) {
	s, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/rewrite",
		strings.NewReader(`{"text":"x","dimensionId":"nope"}`))
	req.Header.Set("X-Whetstone-Token", s.Token())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestState_ExposesDimensions(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/state", nil)
	if len(st.Dimensions) == 0 {
		t.Fatal("no dimensions in state")
	}
	for _, d := range st.Dimensions {
		if d.ID == "" || d.Name == "" {
			t.Errorf("dimension %+v is incomplete", d)
		}
	}
}

func TestCompose_UsesInstructionAndNotes(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = "A composed paragraph."
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID
	post(t, s, "/api/outline/update", map[string]any{"id": id, "notes": "my own reasoning"})

	st, code := post(t, s, "/api/outline/compose", map[string]string{
		"id": id, "instruction": "open with the cost objection",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if st.Outline[0].Draft != "A composed paragraph." {
		t.Errorf("draft = %q", st.Outline[0].Draft)
	}

	last := p.calls[len(p.calls)-1]
	if last.Purpose != provider.PurposeCompose {
		t.Errorf("purpose = %q", last.Purpose)
	}
	body := last.Messages[0].Text
	if !strings.Contains(body, "open with the cost objection") {
		t.Error("prompt should carry the author's instruction")
	}
	if !strings.Contains(body, "my own reasoning") {
		t.Error("prompt should carry the author's notes")
	}
}

// An instruction is not source material. Composing from nothing but a prompt is
// the chat box this tool refuses to be.
func TestCompose_RefusesWithoutTheAuthorsMaterial(t *testing.T) {
	s, p := newTestServer(t)
	p.reply = "prose"
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Bare"})
	id := st.Outline[0].ID

	_, code := post(t, s, "/api/outline/compose", map[string]string{
		"id": id, "instruction": "write me something persuasive",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
	if len(p.calls) != 0 {
		t.Error("a bare instruction should not reach the provider")
	}
}

func TestCompose_RequiresInstruction(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]string{"title": "Point"})
	id := st.Outline[0].ID
	post(t, s, "/api/outline/update", map[string]any{"id": id, "notes": "notes"})

	_, code := post(t, s, "/api/outline/compose", map[string]string{"id": id, "instruction": "  "})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestUpdateSection(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/sections/update", map[string]any{
		"docId": "report", "sectionId": 1,
		"title": "Renamed", "body": "Rewritten by hand.",
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	sec := st.Documents[0].Sections[0]
	if sec.Title != "Renamed" || sec.Body != "Rewritten by hand." {
		t.Errorf("section = %+v", sec)
	}
	if sec.ID != 1 {
		t.Errorf("ID = %d, want it unchanged", sec.ID)
	}
}

func TestUpdateSection_RejectsEmptyBody(t *testing.T) {
	s, _ := newTestServer(t)
	_, code := post(t, s, "/api/sections/update", map[string]any{
		"docId": "report", "sectionId": 1, "body": "   ",
	})
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestDeleteSection(t *testing.T) {
	s, _ := newTestServer(t)
	st, code := post(t, s, "/api/sections/delete", map[string]any{
		"docId": "report", "sectionId": 1,
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(st.Documents[0].Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(st.Documents[0].Sections))
	}
	if st.Documents[0].Sections[0].ID != 2 {
		t.Errorf("remaining ID = %d, want 2 (IDs must not renumber)", st.Documents[0].Sections[0].ID)
	}
}

// Editing the source must not silently break a claim's evidence: the citation
// is kept, and flagged.
func TestCitationGoesStaleWhenSourceIsEdited(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]any{
		"title": "A point", "docId": "report", "sectionId": 1,
		"excerpt": "First body text.",
	})
	id := st.Outline[0].ID
	if st.Outline[0].Grounding[0].Stale {
		t.Fatal("a fresh citation should not be stale")
	}

	st, _ = post(t, s, "/api/sections/update", map[string]any{
		"docId": "report", "sectionId": 1, "body": "Something else entirely.",
	})
	node := findNode(st, id)
	if !node.Grounding[0].Stale {
		t.Error("citation should be stale after the source changed")
	}
	if node.Grounding[0].Excerpt != "First body text." {
		t.Errorf("excerpt = %q; what you quoted is still what you read", node.Grounding[0].Excerpt)
	}
}

func TestCitationGoesStaleWhenSectionIsDeleted(t *testing.T) {
	s, _ := newTestServer(t)
	st, _ := post(t, s, "/api/outline/add", map[string]any{
		"title": "A point", "docId": "report", "sectionId": 1,
		"excerpt": "First body text.",
	})
	id := st.Outline[0].ID

	st, _ = post(t, s, "/api/sections/delete", map[string]any{"docId": "report", "sectionId": 1})
	if !findNode(st, id).Grounding[0].Stale {
		t.Error("citation should be stale after its section was deleted")
	}
}

func findNode(st state, id string) nodeView {
	for _, n := range st.Outline {
		if n.ID == id {
			return n
		}
	}
	return nodeView{}
}

func TestExportDOCX(t *testing.T) {
	s, _ := newTestServer(t)
	post(t, s, "/api/question", map[string]string{"question": "Should we adopt it?"})
	post(t, s, "/api/outline/add", map[string]string{"title": "A point"})

	req := httptest.NewRequest(http.MethodGet, "/export.docx?t="+s.Token(), nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "wordprocessingml") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".docx") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	// A .docx is a zip; the local file header magic is the cheapest proof.
	if got := rec.Body.Bytes(); len(got) < 4 || string(got[:2]) != "PK" {
		t.Errorf("body is not a zip archive (%d bytes)", len(got))
	}
}

func TestExportText(t *testing.T) {
	s, _ := newTestServer(t)
	post(t, s, "/api/question", map[string]string{"question": "Should we adopt it?"})

	req := httptest.NewRequest(http.MethodGet, "/export.txt?t="+s.Token(), nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Should we adopt it?") {
		t.Errorf("export is missing the question:\n%s", rec.Body.String())
	}
}

// A download link carries the token in the query, because a navigation cannot
// set a header. It still has to be checked.
func TestExport_RequiresToken(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{"/export.docx", "/export.txt", "/export.docx?t=wrong"} {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d, want 403", path, rec.Code)
		}
	}
}
