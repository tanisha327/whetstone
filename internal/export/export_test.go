package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/outline"
	"github.com/tanisha327/whetstone/internal/provoke"
	"github.com/tanisha327/whetstone/internal/workspace"
)

func testWorkspace(t *testing.T) *workspace.Workspace {
	t.Helper()
	ws := workspace.New("test", filepath.Join(t.TempDir(), "ws.json"))
	ws.Question = "Should we adopt the new scheduler?"
	ws.AddDocument(doc.Parse("report", "Report", "# One\n\nFirst body text.\n\n# Two\n\nSecond body.\n"))
	ws.MarkRead("report", 1)

	n, err := ws.Outline.Add("", "The latency win is real but narrow")
	if err != nil {
		t.Fatal(err)
	}
	n.Notes = "My own reasoning about the segment."
	n.Draft = "The latency win is concentrated in one workload shape."
	if err := ws.Outline.Cite(n.ID, outline.Ref{
		DocID: "report", SectionID: 1, Excerpt: "First body text.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Outline.Add(n.ID, "A sub-point"); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestFromWorkspace(t *testing.T) {
	d := FromWorkspace(testWorkspace(t), ScopeAll)

	if d.Title != "Should we adopt the new scheduler?" {
		t.Errorf("Title = %q", d.Title)
	}
	if d.Blocks[0].Style != StyleTitle {
		t.Errorf("first block = %+v, want the title", d.Blocks[0])
	}

	var heads, quotes int
	for _, b := range d.Blocks {
		switch b.Style {
		case StyleHeading:
			heads++
		case StyleQuote:
			quotes++
		}
	}
	if heads < 2 {
		t.Errorf("headings = %d, want one per outline point", heads)
	}
	if quotes != 1 {
		t.Errorf("quotes = %d, want 1", quotes)
	}
}

// The prose is the export; notes are the fallback for a point never drafted.
func TestFromWorkspace_FallsBackToNotes(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	n, _ := ws.Outline.Add("", "Undrafted point")
	n.Notes = "Only my notes exist here."

	text := FromWorkspace(ws, ScopeAll).Text()
	if !strings.Contains(text, "Only my notes exist here.") {
		t.Errorf("notes were not exported:\n%s", text)
	}
}

func TestFromWorkspace_UsesDraftOverNotes(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	n, _ := ws.Outline.Add("", "Point")
	n.Notes = "rough notes"
	n.Draft = "the finished prose"

	text := FromWorkspace(ws, ScopeAll).Text()
	if !strings.Contains(text, "the finished prose") {
		t.Error("draft should be exported")
	}
	if strings.Contains(text, "rough notes") {
		t.Error("notes should not be exported alongside a draft")
	}
}

// A document that quietly drops its unanswered objections on the way to the
// printer is the confident artefact this tool exists to discourage.
func TestFromWorkspace_CarriesOpenObjections(t *testing.T) {
	ws := testWorkspace(t)
	ws.AddProvocations([]provoke.Provocation{
		{ID: "a", Kind: provoke.KindEvidenceGap, Text: "Where is the control group?",
			Status: provoke.StatusOpen},
		{ID: "b", Kind: provoke.KindAssumption, Text: "Already handled.",
			Status: provoke.StatusDismissed, Response: "checked"},
	})

	text := FromWorkspace(ws, ScopeAll).Text()
	if !strings.Contains(text, "Objections still open") {
		t.Error("open objections section is missing")
	}
	if !strings.Contains(text, "Where is the control group?") {
		t.Error("the open objection is missing")
	}
	if strings.Contains(text, "Already handled.") {
		t.Error("resolved objections should not be carried into the export")
	}
}

func TestFromWorkspace_ReportsEngagement(t *testing.T) {
	text := FromWorkspace(testWorkspace(t), ScopeAll).Text()
	if !strings.Contains(text, "Sections opened 1 of 2") {
		t.Errorf("engagement line missing or wrong:\n%s", text)
	}
	if !strings.Contains(text, workspace.Caveat) {
		t.Error("the caveat must travel with the numbers")
	}
}

func TestFromWorkspace_EmptyWorkspace(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	d := FromWorkspace(ws, ScopeAll)
	if d.Title == "" {
		t.Error("an untitled workspace still needs a title")
	}
	if !strings.Contains(d.Text(), "is empty") {
		t.Errorf("empty export should say so:\n%s", d.Text())
	}
}

func TestFilename(t *testing.T) {
	tests := []struct{ title, want string }{
		{"Should we adopt the new scheduler?", "should-we-adopt-the-new-scheduler"},
		{"   ", "whetstone"},
		{"!!!", "whetstone"},
		{"A  b__c", "a-b-c"},
		{strings.Repeat("x", 200), strings.Repeat("x", 60)},
	}
	for _, tc := range tests {
		got := Doc{Title: tc.title}.Filename()
		if got != tc.want {
			t.Errorf("Filename(%q) = %q, want %q", clip(tc.title), got, clip(tc.want))
		}
	}
}

func clip(s string) string {
	if len(s) > 30 {
		return s[:30] + "…"
	}
	return s
}

// --- docx ---

// unzip reads a generated .docx back so the test asserts on real archive
// contents rather than on the string that went in.
func unzip(t *testing.T, data []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a valid zip: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("reading %s: %v", f.Name, err)
		}
		out[f.Name] = string(b)
	}
	return out
}

func TestDOCX_HasTheRequiredParts(t *testing.T) {
	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(testWorkspace(t), ScopeAll)); err != nil {
		t.Fatalf("DOCX: %v", err)
	}

	parts := unzip(t, buf.Bytes())
	for _, want := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		if parts[want] == "" {
			t.Errorf("missing or empty part %q (have %v)", want, keys(parts))
		}
	}
}

// If document.xml is not well-formed, Word offers to repair the file instead
// of opening it.
func TestDOCX_PartsAreWellFormedXML(t *testing.T) {
	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(testWorkspace(t), ScopeAll)); err != nil {
		t.Fatal(err)
	}
	for name, body := range unzip(t, buf.Bytes()) {
		dec := xml.NewDecoder(strings.NewReader(body))
		for {
			_, err := dec.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Errorf("%s is not well-formed XML: %v", name, err)
				break
			}
		}
	}
}

func TestDOCX_CarriesTheContent(t *testing.T) {
	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(testWorkspace(t), ScopeAll)); err != nil {
		t.Fatal(err)
	}
	body := unzip(t, buf.Bytes())["word/document.xml"]

	for _, want := range []string{
		"Should we adopt the new scheduler?",
		"The latency win is real but narrow",
		"concentrated in one workload shape",
		"First body text.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("document.xml is missing %q", want)
		}
	}
	if !strings.Contains(body, "<w:sectPr>") {
		t.Error("missing sectPr; Word will report the file as damaged")
	}
}

// Angle brackets and ampersands in a pasted source would otherwise produce a
// file Word refuses to open.
func TestDOCX_EscapesMarkup(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	ws.Question = "Does A & B <hold>?"
	n, _ := ws.Outline.Add("", "Point <one> & two")
	n.Draft = `He said "5 > 3" & meant it.`

	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(ws, ScopeAll)); err != nil {
		t.Fatal(err)
	}
	body := unzip(t, buf.Bytes())["word/document.xml"]

	if strings.Contains(body, "<hold>") || strings.Contains(body, "Point <one>") {
		t.Error("raw markup leaked into document.xml")
	}
	if !strings.Contains(body, "&amp;") {
		t.Error("ampersand was not escaped")
	}

	dec := xml.NewDecoder(strings.NewReader(body))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("escaping produced invalid XML: %v", err)
		}
	}
}

func TestDOCX_ParagraphsAndLineBreaks(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	n, _ := ws.Outline.Add("", "Point")
	n.Draft = "First para line one\nline two\n\nSecond para"

	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(ws, ScopeAll)); err != nil {
		t.Fatal(err)
	}
	body := unzip(t, buf.Bytes())["word/document.xml"]

	if !strings.Contains(body, "<w:br/>") {
		t.Error("a single newline should become a line break")
	}
	if n := strings.Count(body, "<w:p>"); n < 3 {
		t.Errorf("paragraph count = %d, want the blank line to split them", n)
	}
}

func TestDOCX_EmptyWorkspaceStillOpens(t *testing.T) {
	ws := workspace.New("t", filepath.Join(t.TempDir(), "ws.json"))
	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(ws, ScopeAll)); err != nil {
		t.Fatalf("DOCX on an empty workspace: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("produced an empty file")
	}
	parts := unzip(t, buf.Bytes())
	if parts["word/document.xml"] == "" {
		t.Error("no document part")
	}
}

func TestText_RendersEveryStyle(t *testing.T) {
	text := FromWorkspace(testWorkspace(t), ScopeAll).Text()
	if !strings.Contains(text, "====") {
		t.Error("title underline missing")
	}
	if !strings.Contains(text, "    > ") {
		t.Error("citations should be rendered as indented quotes")
	}
	if !strings.HasSuffix(text, "\n") {
		t.Error("text export should end with a newline")
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Sources travel with the argument by default: a reader who has only the
// conclusions cannot check them.
func TestScope(t *testing.T) {
	ws := testWorkspace(t)

	all := FromWorkspace(ws, ScopeAll).Text()
	if !strings.Contains(all, "The argument") || !strings.Contains(all, "Sources") {
		t.Errorf("ScopeAll should carry both:\n%s", all)
	}
	if !strings.Contains(all, "First body text.") {
		t.Error("ScopeAll is missing the source text")
	}

	arg := FromWorkspace(ws, ScopeArgument).Text()
	if strings.Contains(arg, "First body text.\n") && strings.Contains(arg, "Second body.") {
		t.Errorf("ScopeArgument should omit the source sections:\n%s", arg)
	}
	if !strings.Contains(arg, "The latency win is real but narrow") {
		t.Error("ScopeArgument is missing the argument")
	}

	src := FromWorkspace(ws, ScopeSources).Text()
	if strings.Contains(src, "The argument") {
		t.Errorf("ScopeSources should omit the argument:\n%s", src)
	}
	if !strings.Contains(src, "First body text.") {
		t.Error("ScopeSources is missing the source text")
	}
	if !strings.Contains(src, "Second body.") {
		t.Error("ScopeSources should carry every section, not just cited ones")
	}
}

func TestParseScope(t *testing.T) {
	tests := map[string]Scope{
		"":          ScopeAll,
		"all":       ScopeAll,
		"nonsense":  ScopeAll,
		"argument":  ScopeArgument,
		" SOURCES ": ScopeSources,
	}
	for in, want := range tests {
		if got := ParseScope(in); got != want {
			t.Errorf("ParseScope(%q) = %v, want %v", in, got, want)
		}
	}
}

// The full source text must survive into a Word file, not just the excerpts.
func TestDOCX_CarriesSourceText(t *testing.T) {
	var buf bytes.Buffer
	if err := DOCX(&buf, FromWorkspace(testWorkspace(t), ScopeAll)); err != nil {
		t.Fatal(err)
	}
	body := unzip(t, buf.Bytes())["word/document.xml"]
	for _, want := range []string{"Sources", "First body text.", "Second body."} {
		if !strings.Contains(body, want) {
			t.Errorf("document.xml is missing %q", want)
		}
	}
}
