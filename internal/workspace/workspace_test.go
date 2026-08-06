package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/lens"
	"github.com/tanisha327/whetstone/internal/provoke"
)

func tempWS(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ws.json")
}

func TestLoad_MissingFileStartsEmpty(t *testing.T) {
	path := tempWS(t)
	w, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if w.Path() != path {
		t.Errorf("Path = %q", w.Path())
	}
	if len(w.Documents) != 0 || w.Summaries == nil || w.Read == nil {
		t.Errorf("expected an initialised empty workspace, got %+v", w)
	}
}

func TestSaveLoad_RoundTrip(t *testing.T) {
	path := tempWS(t)
	w := New("proposal", path)
	w.Question = "Should we adopt the new scheduler?"
	w.ActiveLens = "risk"
	w.AddDocument(doc.Parse("report", "Report", "# A\n\nBody.\n"))
	w.MarkRead("report", 1)
	w.PutSummary("report", lens.Summary{SectionID: 1, LensID: "risk", Text: "orientation", Relevance: 7})
	node, err := w.Outline.Add("", "First point")
	if err != nil {
		t.Fatal(err)
	}
	node.Notes = "my reasoning"
	w.AddProvocations([]provoke.Provocation{{
		ID: "pv-1", Kind: provoke.KindAssumption, Text: "why?",
		AnchorKind: provoke.AnchorOutline, AnchorID: node.ID,
		Status: provoke.StatusOpen, CreatedAt: time.Now(),
	}})

	if err := w.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Question != w.Question || got.ActiveLens != "risk" {
		t.Errorf("scalars did not round-trip: %+v", got)
	}
	if len(got.Documents) != 1 || got.Documents[0].ID != "report" {
		t.Errorf("documents = %+v", got.Documents)
	}
	if !got.IsRead("report", 1) {
		t.Error("read marks did not round-trip")
	}
	if s, ok := got.Summary("report", 1, "risk"); !ok || s.Text != "orientation" {
		t.Errorf("summary = %+v, %v", s, ok)
	}
	if got.Outline.Len() != 1 {
		t.Errorf("outline nodes = %d", got.Outline.Len())
	}
	if len(got.Provocations) != 1 || got.Provocations[0].ID != "pv-1" {
		t.Errorf("provocations = %+v", got.Provocations)
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d", got.Version, Version)
	}
}

// A corrupt workspace must never be silently replaced with an empty one: the
// next save would overwrite the user's work.
func TestLoad_CorruptIsFatalAndPreservesFile(t *testing.T) {
	path := tempWS(t)
	original := []byte(`{"version":1,"name":"half-written`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for a corrupt workspace")
	}
	if w != nil {
		t.Error("Load must not return a usable workspace alongside a corruption error")
	}

	var corrupt *ErrCorrupt
	if !errors.As(err, &corrupt) {
		t.Fatalf("err = %T (%v), want *ErrCorrupt", err, err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file, got: %v", err)
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("original file disappeared: %v", readErr)
	}
	if string(after) != string(original) {
		t.Error("the corrupt file was modified; it must be left untouched")
	}
}

func TestLoad_RejectsNewerSchema(t *testing.T) {
	path := tempWS(t)
	if err := os.WriteFile(path, []byte(`{"version":9999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	var corrupt *ErrCorrupt
	if !errors.As(err, &corrupt) {
		t.Fatalf("err = %v, want *ErrCorrupt for a newer schema", err)
	}
}

func TestSave_IsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ws.json")
	w := New("x", path)
	w.Question = "first"
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}
	w.Question = "second"
	if err := w.Save(); err != nil {
		t.Fatal(err)
	}

	// No temp files may survive a successful save.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Question != "second" {
		t.Errorf("Question = %q, want the second write to have won", got.Question)
	}
}

func TestSave_NoPath(t *testing.T) {
	w := New("x", "")
	if err := w.Save(); err == nil {
		t.Error("expected an error saving a workspace with no path")
	}
}

func TestAddDocument_ReplacesSameID(t *testing.T) {
	w := New("x", tempWS(t))
	w.AddDocument(doc.Parse("r", "First", "# A\n\nx\n"))
	w.AddDocument(doc.Parse("r", "Second", "# B\n\ny\n"))
	if len(w.Documents) != 1 {
		t.Fatalf("Documents = %d, want 1", len(w.Documents))
	}
	if w.Documents[0].Title != "Second" {
		t.Errorf("Title = %q, want the reload to win", w.Documents[0].Title)
	}
}

func TestAddProvocations_DeduplicatesByAnchorAndText(t *testing.T) {
	w := New("x", tempWS(t))
	p := provoke.Provocation{
		ID: "pv-1", Text: "Is  the  sample  representative?",
		AnchorKind: provoke.AnchorSection, AnchorID: "r#1", Status: provoke.StatusOpen,
	}
	if n := w.AddProvocations([]provoke.Provocation{p}); n != 1 {
		t.Fatalf("first add returned %d, want 1", n)
	}

	// Same anchor, same text modulo whitespace and case: a duplicate.
	dup := p
	dup.ID = "pv-2"
	dup.Text = "is the sample representative?"
	if n := w.AddProvocations([]provoke.Provocation{dup}); n != 0 {
		t.Errorf("duplicate add returned %d, want 0", n)
	}

	// Same text on a different anchor is not a duplicate.
	other := p
	other.ID = "pv-3"
	other.AnchorID = "r#2"
	if n := w.AddProvocations([]provoke.Provocation{other}); n != 1 {
		t.Errorf("different-anchor add returned %d, want 1", n)
	}
}

func TestProvocation_ReturnsMutablePointer(t *testing.T) {
	w := New("x", tempWS(t))
	w.AddProvocations([]provoke.Provocation{{ID: "pv-1", Status: provoke.StatusOpen}})

	p := w.Provocation("pv-1")
	if p == nil {
		t.Fatal("Provocation returned nil")
	}
	if err := p.Dismiss("not applicable here", time.Now()); err != nil {
		t.Fatal(err)
	}
	if w.Provocations[0].Status != provoke.StatusDismissed {
		t.Error("resolving through the pointer did not affect the stored provocation")
	}
	if w.Provocation("nope") != nil {
		t.Error("Provocation(nope) should be nil")
	}
}

func TestKeys(t *testing.T) {
	if got := SectionKey("report", 3); got != "report#3" {
		t.Errorf("SectionKey = %q", got)
	}
	if got := summaryKey("report", 3, "risk"); got != "report#3#risk" {
		t.Errorf("SummaryKey = %q", got)
	}
}

func TestEngagement(t *testing.T) {
	w := New("x", tempWS(t))
	w.AddDocument(doc.Parse("r", "R", "# A\n\nx\n\n# B\n\ny\n\n# C\n\nz\n"))
	w.MarkRead("r", 1)

	n, _ := w.Outline.Add("", "One two three") // 3 user words
	n.Notes = "four five"                      // 2 user words
	n.Draft = "a b c d e"                      // 5 generated words

	w.AddProvocations([]provoke.Provocation{
		{ID: "a", Text: "one", Status: provoke.StatusOpen, AnchorID: "r#1"},
		{ID: "b", Text: "two", Status: provoke.StatusDismissed, Response: "two words", AnchorID: "r#2"},
	})

	e := w.Engagement()
	if e.SectionsTotal != 3 || e.SectionsRead != 1 {
		t.Errorf("sections = %d/%d, want 1/3", e.SectionsRead, e.SectionsTotal)
	}
	if e.ProvocationsOpen != 1 || e.ProvocationsDismissed != 1 {
		t.Errorf("provocations open/dismissed = %d/%d", e.ProvocationsOpen, e.ProvocationsDismissed)
	}
	if e.OutlineNodes != 1 {
		t.Errorf("OutlineNodes = %d", e.OutlineNodes)
	}
	if e.UserWords != 7 { // 3 title + 2 notes + 2 response
		t.Errorf("UserWords = %d, want 7", e.UserWords)
	}
	if e.GeneratedWords != 5 {
		t.Errorf("GeneratedWords = %d, want 5", e.GeneratedWords)
	}

	if got := e.AuthorshipFraction(); got < 0.58 || got > 0.59 {
		t.Errorf("AuthorshipFraction = %v, want ~0.583", got)
	}
	if e.Summary() == "" {
		t.Error("Summary is empty")
	}
}

func TestEngagement_EmptyWorkspaceHasNoDivideByZero(t *testing.T) {
	e := New("x", tempWS(t)).Engagement()
	if e.AuthorshipFraction() != 0 {
		t.Errorf("empty engagement = %+v", e)
	}
}

func TestWordCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"   ", 0},
		{"one", 1},
		{"one two", 2},
		{"  one   two  ", 2},
		{"one\ntwo\tthree", 3},
		{"trailing space ", 2},
	}
	for _, tc := range tests {
		if got := wordCount(tc.in); got != tc.want {
			t.Errorf("wordCount(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
