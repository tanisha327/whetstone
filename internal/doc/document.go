// Package doc loads source documents and splits them into addressable sections.
//
// Sections are the unit of everything downstream: a lens summarises a section,
// a provocation anchors to one, and an outline node cites excerpts from one. The
// split is deliberately simple and local — no model call is involved in reading
// a document, because the user is meant to read it.
package doc

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Section is one addressable passage of a Document.
type Section struct {
	// ID is the 1-based position of the section within its Document. It is
	// stable for a given parse and is what provocations and outline
	// references anchor to.
	ID int
	// Title is the heading text, or a synthesised label for untitled prose.
	Title string
	// Level is the heading depth (1 for "#", 2 for "##"). Zero means the
	// section came from a paragraph split rather than a heading.
	Level int
	// Body is the section text, excluding its own heading line.
	Body string
	// StartLine is the 1-based line in the source where the section begins.
	StartLine int
}

// WordCount returns the number of whitespace-separated words in the body. It
// feeds the reading-progress metric, so it counts the body only.
func (s Section) WordCount() int { return len(strings.Fields(s.Body)) }

// Excerpt returns at most n runes of the body on a single line, suffixed with
// an ellipsis when truncated. Rune-based so multi-byte text is never split
// mid-character.
func (s Section) Excerpt(n int) string {
	flat := strings.Join(strings.Fields(s.Body), " ")
	r := []rune(flat)
	if len(r) <= n {
		return flat
	}
	if n <= 0 {
		return ""
	}
	return strings.TrimRight(string(r[:n]), " ") + "..."
}

// Document is a loaded source text split into sections.
type Document struct {
	// ID is a stable identifier derived from the filename.
	ID string
	// Title is the document's first heading, or its filename.
	Title string
	// Path is where it was loaded from. Empty for in-memory documents.
	Path string
	// Sections is the ordered split. Always at least one entry for non-empty
	// input.
	Sections []Section
}

// WordCount returns the total across all sections.
func (d *Document) WordCount() int {
	n := 0
	for _, s := range d.Sections {
		n += s.WordCount()
	}
	return n
}

// Section returns the section with the given ID.
func (d *Document) Section(id int) (Section, bool) {
	for _, s := range d.Sections {
		if s.ID == id {
			return s, true
		}
	}
	return Section{}, false
}

// SetSection replaces a section's title and body.
//
// The ID is deliberately left alone. Citations point at IDs, so renumbering
// after an edit would silently re-aim every citation in the workspace at
// different text — the kind of corruption you would not notice until the
// argument was already built on it.
func (d *Document) SetSection(id int, title, body string) bool {
	for i := range d.Sections {
		if d.Sections[i].ID != id {
			continue
		}
		if t := strings.TrimSpace(title); t != "" {
			d.Sections[i].Title = t
		}
		d.Sections[i].Body = strings.TrimRight(body, " \t\n")
		return true
	}
	return false
}

// RemoveSection deletes a section. The remaining IDs are left unchanged, for
// the same reason SetSection keeps them: a citation must never quietly come to
// mean a different passage.
func (d *Document) RemoveSection(id int) bool {
	for i := range d.Sections {
		if d.Sections[i].ID == id {
			d.Sections = append(d.Sections[:i], d.Sections[i+1:]...)
			return true
		}
	}
	return false
}

// Contains reports whether the section still holds the given excerpt, ignoring
// differences in whitespace. Used to mark a citation stale after the source has
// been edited.
func (s Section) Contains(excerpt string) bool {
	needle := strings.Join(strings.Fields(strings.TrimSuffix(excerpt, "…")), " ")
	if needle == "" {
		return true
	}
	hay := strings.Join(strings.Fields(s.Body), " ")
	return strings.Contains(hay, needle)
}

// maxSectionWords bounds a heading-delimited section. A 20,000-word chapter
// under one heading is not a useful unit of attention, so oversized sections
// are split further on paragraph boundaries.
const maxSectionWords = 700

// Load reads a document from disk and parses it.
func Load(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("doc: reading %s: %w", path, err)
	}
	base := filepath.Base(path)
	d := Parse(idFromName(base), strings.TrimSuffix(base, filepath.Ext(base)), string(data))
	d.Path = path
	return d, nil
}

// Parse splits text into sections. Markdown ATX headings ("# ", "## ") delimit
// sections when present; otherwise the text is chunked on blank lines. The
// fallback matters because source material arrives as plain transcripts at
// least as often as as structured reports.
func Parse(id, title, text string) *Document {
	d := &Document{ID: id, Title: title}
	text = strings.ReplaceAll(text, "\r\n", "\n")

	blocks := splitOnHeadings(text)
	if len(blocks) == 0 {
		blocks = splitOnParagraphs(text)
	}

	for _, b := range blocks {
		for _, part := range capSize(b) {
			part.ID = len(d.Sections) + 1
			// Markdown is split on above, then converted to prose: sections
			// are for reading, and syntax on screen is syntax the reader has
			// to decode instead of the argument.
			part.Body = Plain(part.Body)
			part.Title = plainInline(part.Title)
			if part.Title == "" {
				part.Title = synthTitle(part.Body, part.ID)
			}
			d.Sections = append(d.Sections, part)
		}
	}

	// Adopt the first heading as the document title when the caller had only a
	// filename to offer.
	if len(d.Sections) > 0 && d.Sections[0].Level > 0 && d.Title == "" {
		d.Title = d.Sections[0].Title
	}
	if d.Title == "" {
		d.Title = "Untitled"
	}
	return d
}

// splitOnHeadings returns one block per ATX heading. Returns nil when the text
// has no headings, so the caller can fall back. Any preamble before the first
// heading becomes its own untitled block.
func splitOnHeadings(text string) []Section {
	var out []Section
	var cur *Section
	var body strings.Builder
	line := 0
	preamble := strings.Builder{}
	preambleStart := 1

	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(body.String())
			out = append(out, *cur)
			body.Reset()
		}
	}

	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line++
		raw := sc.Text()
		if level, heading, ok := parseHeading(raw); ok {
			if cur == nil && strings.TrimSpace(preamble.String()) != "" {
				out = append(out, Section{
					Title:     "",
					Level:     0,
					Body:      strings.TrimSpace(preamble.String()),
					StartLine: preambleStart,
				})
			}
			flush()
			cur = &Section{Title: heading, Level: level, StartLine: line}
			continue
		}
		if cur == nil {
			preamble.WriteString(raw)
			preamble.WriteByte('\n')
			continue
		}
		body.WriteString(raw)
		body.WriteByte('\n')
	}
	flush()

	if len(out) == 0 {
		return nil
	}
	return out
}

// parseHeading recognises an ATX heading line: one to six '#' followed by a
// space and text. Anything else is body.
func parseHeading(line string) (level int, text string, ok bool) {
	trimmed := strings.TrimLeft(line, " ")
	n := 0
	for n < len(trimmed) && trimmed[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(trimmed) || trimmed[n] != ' ' {
		return 0, "", false
	}
	title := strings.TrimSpace(trimmed[n+1:])
	if title == "" {
		return 0, "", false
	}
	return n, title, true
}

// splitOnParagraphs chunks unstructured text on blank lines, accumulating
// paragraphs until the chunk reaches maxSectionWords.
func splitOnParagraphs(text string) []Section {
	paras := strings.Split(text, "\n\n")
	var out []Section
	var cur strings.Builder
	words := 0
	startLine := 1
	lineOf := 1

	flush := func() {
		if strings.TrimSpace(cur.String()) == "" {
			return
		}
		out = append(out, Section{Body: strings.TrimSpace(cur.String()), StartLine: startLine})
		cur.Reset()
		words = 0
	}

	for _, p := range paras {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			if cur.Len() == 0 {
				startLine = lineOf
			}
			cur.WriteString(trimmed)
			cur.WriteString("\n\n")
			words += len(strings.Fields(trimmed))
			if words >= maxSectionWords {
				flush()
			}
		}
		lineOf += strings.Count(p, "\n") + 2
	}
	flush()
	return out
}

// capSize splits an oversized section into parts on paragraph boundaries,
// preserving the heading on the first part and marking continuations.
func capSize(s Section) []Section {
	if len(strings.Fields(s.Body)) <= maxSectionWords {
		return []Section{s}
	}
	parts := splitOnParagraphs(s.Body)
	if len(parts) <= 1 {
		return []Section{s}
	}
	out := make([]Section, 0, len(parts))
	for i, p := range parts {
		p.Level = s.Level
		p.StartLine = s.StartLine
		switch {
		case s.Title == "":
			p.Title = ""
		case i == 0:
			p.Title = s.Title
		default:
			p.Title = fmt.Sprintf("%s (cont. %d)", s.Title, i+1)
		}
		out = append(out, p)
	}
	return out
}

// synthTitle builds a label for a section that had no heading, using its
// opening words.
func synthTitle(body string, id int) string {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return fmt.Sprintf("Section %d", id)
	}
	if len(fields) > 7 {
		fields = fields[:7]
	}
	title := strings.Join(fields, " ")
	title = strings.TrimRight(title, ".,;:")
	return title + " ..."
}

// idFromName derives a filesystem- and JSON-safe document ID from a filename.
func idFromName(name string) string {
	name = strings.TrimSuffix(name, filepath.Ext(name))
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteRune('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "doc"
	}
	return id
}
