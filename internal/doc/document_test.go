package doc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_SplitsOnHeadings(t *testing.T) {
	text := `# Report Title

Opening paragraph.

## First Finding

Consumers say they will pay more.

## Second Finding

Costs rose 12%.
`
	d := Parse("r", "", text)
	if len(d.Sections) != 3 {
		t.Fatalf("sections = %d, want 3: %+v", len(d.Sections), titles(d))
	}
	want := []string{"Report Title", "First Finding", "Second Finding"}
	for i, w := range want {
		if d.Sections[i].Title != w {
			t.Errorf("section[%d].Title = %q, want %q", i, d.Sections[i].Title, w)
		}
	}
	if d.Sections[0].Level != 1 || d.Sections[1].Level != 2 {
		t.Errorf("levels = %d, %d; want 1, 2", d.Sections[0].Level, d.Sections[1].Level)
	}
	if !strings.Contains(d.Sections[1].Body, "pay more") {
		t.Errorf("body = %q", d.Sections[1].Body)
	}
	if strings.Contains(d.Sections[1].Body, "## First Finding") {
		t.Error("body should not repeat its own heading")
	}
}

func TestParse_IDsAreSequential(t *testing.T) {
	d := Parse("r", "", "# A\n\nx\n\n# B\n\ny\n")
	for i, s := range d.Sections {
		if s.ID != i+1 {
			t.Errorf("section[%d].ID = %d, want %d", i, s.ID, i+1)
		}
	}
}

func TestParse_PreambleBecomesItsOwnSection(t *testing.T) {
	d := Parse("r", "", "Loose intro text before any heading.\n\n# Heading\n\nBody.\n")
	if len(d.Sections) != 2 {
		t.Fatalf("sections = %d, want 2: %v", len(d.Sections), titles(d))
	}
	if !strings.Contains(d.Sections[0].Body, "Loose intro") {
		t.Errorf("first section body = %q", d.Sections[0].Body)
	}
	if d.Sections[0].Level != 0 {
		t.Errorf("preamble level = %d, want 0", d.Sections[0].Level)
	}
}

// Source material is as often an unstructured transcript as a tidy report, so
// the no-heading path has to produce usable sections rather than one blob.
func TestParse_FallsBackToParagraphs(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString(strings.Repeat("word ", 60))
		b.WriteString("\n\n")
	}
	d := Parse("t", "transcript", b.String())
	if len(d.Sections) < 2 {
		t.Fatalf("sections = %d, want the text chunked", len(d.Sections))
	}
	for _, s := range d.Sections {
		if s.Title == "" {
			t.Error("untitled section should get a synthesised title")
		}
	}
}

func TestParse_SplitsOversizedSection(t *testing.T) {
	body := strings.Repeat(strings.Repeat("word ", 100)+"\n\n", 20) // ~2000 words
	d := Parse("r", "", "# One Huge Heading\n\n"+body)
	if len(d.Sections) < 2 {
		t.Fatalf("sections = %d, want an oversized section to be split", len(d.Sections))
	}
	if d.Sections[0].Title != "One Huge Heading" {
		t.Errorf("first part title = %q", d.Sections[0].Title)
	}
	if !strings.Contains(d.Sections[1].Title, "cont.") {
		t.Errorf("continuation title = %q, want a cont. marker", d.Sections[1].Title)
	}
	for _, s := range d.Sections {
		if got := s.WordCount(); got > maxSectionWords*2 {
			t.Errorf("section %d has %d words, far above the cap", s.ID, got)
		}
	}
}

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line      string
		wantLevel int
		wantText  string
		wantOK    bool
	}{
		{"# Title", 1, "Title", true},
		{"### Deep", 3, "Deep", true},
		{"###### Six", 6, "Six", true},
		{"####### Seven", 0, "", false},
		{"#NoSpace", 0, "", false},
		{"# ", 0, "", false},
		{"not a heading", 0, "", false},
		{"  ## Indented", 2, "Indented", true},
		{"#", 0, "", false},
	}
	for _, tc := range tests {
		level, text, ok := parseHeading(tc.line)
		if ok != tc.wantOK || level != tc.wantLevel || text != tc.wantText {
			t.Errorf("parseHeading(%q) = (%d, %q, %v), want (%d, %q, %v)",
				tc.line, level, text, ok, tc.wantLevel, tc.wantText, tc.wantOK)
		}
	}
}

func TestParse_HandlesCRLF(t *testing.T) {
	d := Parse("r", "", "# A\r\n\r\nBody here.\r\n")
	if len(d.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(d.Sections))
	}
	if strings.Contains(d.Sections[0].Body, "\r") {
		t.Error("carriage returns should be normalised away")
	}
}

func TestParse_Empty(t *testing.T) {
	d := Parse("r", "", "")
	if len(d.Sections) != 0 {
		t.Errorf("sections = %d, want 0", len(d.Sections))
	}
	if d.Title != "Untitled" {
		t.Errorf("Title = %q", d.Title)
	}
}

func TestSectionExcerpt_IsRuneSafe(t *testing.T) {
	s := Section{Body: "héllo wörld this is a longer passage"}
	got := s.Excerpt(7)
	if strings.Contains(got, "�") {
		t.Errorf("excerpt split a multi-byte rune: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("excerpt = %q, want an ellipsis", got)
	}
	if r := []rune(got); len(r) > 10 {
		t.Errorf("excerpt too long: %q", got)
	}
}

func TestSectionExcerpt_ShortBodyUnchanged(t *testing.T) {
	s := Section{Body: "short"}
	if got := s.Excerpt(100); got != "short" {
		t.Errorf("Excerpt = %q, want %q", got, "short")
	}
}

func TestSectionExcerpt_CollapsesWhitespace(t *testing.T) {
	s := Section{Body: "a\n\nb   c"}
	if got := s.Excerpt(100); got != "a b c" {
		t.Errorf("Excerpt = %q, want %q", got, "a b c")
	}
}

func TestIDFromName(t *testing.T) {
	tests := map[string]string{
		"Industry Report.md":    "industry-report",
		"notes_2026-01.txt":     "notes-2026-01",
		"UPPER.MD":              "upper",
		"!!!.md":                "doc",
		"a b/c.md":              "a-bc",
		"report.final.draft.md": "report-final-draft",
	}
	for in, want := range tests {
		if got := idFromName(in); got != want {
			t.Errorf("idFromName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Industry Report.md")
	if err := os.WriteFile(path, []byte("# Findings\n\nBody text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	d, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if d.ID != "industry-report" {
		t.Errorf("ID = %q", d.ID)
	}
	if d.Path != path {
		t.Errorf("Path = %q", d.Path)
	}
	if len(d.Sections) != 1 || d.Sections[0].Title != "Findings" {
		t.Errorf("sections = %v", titles(d))
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.md"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "nope.md") {
		t.Errorf("error should name the file, got: %v", err)
	}
}

func TestDocument_SectionLookup(t *testing.T) {
	d := Parse("r", "", "# A\n\nx\n\n# B\n\ny\n")
	if s, ok := d.Section(2); !ok || s.Title != "B" {
		t.Errorf("Section(2) = %+v, %v", s, ok)
	}
	if _, ok := d.Section(99); ok {
		t.Error("Section(99) should not be found")
	}
}

func titles(d *Document) []string {
	out := make([]string, 0, len(d.Sections))
	for _, s := range d.Sections {
		out = append(out, s.Title)
	}
	return out
}

func TestSetSection_KeepsIDStable(t *testing.T) {
	d := Parse("r", "", "# A\n\nfirst\n\n# B\n\nsecond\n")
	if !d.SetSection(2, "B renamed", "rewritten body") {
		t.Fatal("SetSection returned false")
	}
	sec, ok := d.Section(2)
	if !ok {
		t.Fatal("section 2 disappeared")
	}
	if sec.Title != "B renamed" || sec.Body != "rewritten body" {
		t.Errorf("section = %+v", sec)
	}
	if d.Sections[0].ID != 1 || d.Sections[1].ID != 2 {
		t.Errorf("IDs moved: %d, %d", d.Sections[0].ID, d.Sections[1].ID)
	}
}

func TestSetSection_EmptyTitleKeepsOld(t *testing.T) {
	d := Parse("r", "", "# Keep\n\nbody\n")
	d.SetSection(1, "  ", "new body")
	if sec, _ := d.Section(1); sec.Title != "Keep" {
		t.Errorf("Title = %q, want the old one kept", sec.Title)
	}
}

func TestSetSection_UnknownID(t *testing.T) {
	d := Parse("r", "", "# A\n\nbody\n")
	if d.SetSection(99, "x", "y") {
		t.Error("SetSection on a missing ID should return false")
	}
}

// Renumbering after a delete would silently re-aim every citation in the
// workspace at different text.
func TestRemoveSection_LeavesOtherIDsAlone(t *testing.T) {
	d := Parse("r", "", "# A\n\none\n\n# B\n\ntwo\n\n# C\n\nthree\n")
	if !d.RemoveSection(2) {
		t.Fatal("RemoveSection returned false")
	}
	if len(d.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(d.Sections))
	}
	if d.Sections[0].ID != 1 || d.Sections[1].ID != 3 {
		t.Errorf("IDs after delete = %d, %d; want 1, 3", d.Sections[0].ID, d.Sections[1].ID)
	}
	if _, ok := d.Section(2); ok {
		t.Error("section 2 is still present")
	}
}

func TestSectionContains(t *testing.T) {
	s := Section{Body: "The latency win is real\nbut not uniform at all."}
	tests := []struct {
		excerpt string
		want    bool
	}{
		{"latency win is real", true},
		{"real but not uniform", true},      // spans a line break
		{"real   but  not   uniform", true}, // whitespace differences ignored
		{"The latency win is re…", true},    // truncation marker stripped
		{"something never written", false},
		{"", true},
	}
	for _, tc := range tests {
		if got := s.Contains(tc.excerpt); got != tc.want {
			t.Errorf("Contains(%q) = %v, want %v", tc.excerpt, got, tc.want)
		}
	}
}
