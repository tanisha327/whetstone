package export

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// DOCX writes the document as a Word file.
//
// A .docx is a ZIP of XML, so archive/zip and encoding/xml are the whole
// toolchain. Three parts are enough for Word, Pages and LibreOffice to open it:
//
//	[Content_Types].xml   what each part is
//	_rels/.rels           where the main document lives
//	word/document.xml     the content
//
// Formatting is set on each run directly instead of through a styles part: one
// fewer file, and no style inheritance to get wrong.
func DOCX(w io.Writer, d Doc) error {
	zw := zip.NewWriter(w)

	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rootRels},
		{"word/document.xml", documentXML(d)},
	}
	for _, p := range parts {
		f, err := zw.Create(p.name)
		if err != nil {
			return fmt.Errorf("export: creating %s: %w", p.name, err)
		}
		if _, err := io.WriteString(f, p.body); err != nil {
			return fmt.Errorf("export: writing %s: %w", p.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("export: closing docx: %w", err)
	}
	return nil
}

const contentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`

const rootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`

// Half-point font sizes, as OOXML measures them: 28 is 14pt.
const (
	sizeTitle = 40
	sizeH1    = 30
	sizeH2    = 26
	sizeH3    = 24
	sizeBody  = 22
	sizeMeta  = 18
)

func documentXML(d Doc) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n")
	b.WriteString(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`)

	for _, blk := range d.Blocks {
		writeParagraph(&b, blk)
	}

	// A section properties element is required; without it Word repairs the
	// file on open. A4 portrait with 1 inch margins, in twentieths of a point.
	b.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/>` +
		`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr>`)
	b.WriteString(`</w:body></w:document>`)
	return b.String()
}

func writeParagraph(b *strings.Builder, blk Block) {
	var (
		size   = sizeBody
		bold   bool
		italic bool
		indent int // in twentieths of a point
		before = 120
		after  = 120
	)

	switch blk.Style {
	case StyleTitle:
		size, bold, after = sizeTitle, true, 320
	case StyleHeading:
		bold = true
		switch blk.Level {
		case 1:
			size = sizeH1
		case 2:
			size, indent = sizeH2, 240
		default:
			size, indent = sizeH3, 480
		}
		before = 320
	case StyleQuote:
		size, italic, indent = sizeBody, true, 720
	case StyleMeta:
		size, italic = sizeMeta, true
		before = 240
	}

	// Blank lines inside a block become separate paragraphs; a single newline
	// becomes a line break within one.
	for i, para := range strings.Split(blk.Text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		spaceBefore := before
		if i > 0 {
			spaceBefore = 60
		}

		b.WriteString(`<w:p><w:pPr>`)
		fmt.Fprintf(b, `<w:spacing w:before="%d" w:after="%d"/>`, spaceBefore, after)
		if indent > 0 {
			fmt.Fprintf(b, `<w:ind w:left="%d"/>`, indent)
		}
		b.WriteString(`</w:pPr>`)

		b.WriteString(`<w:r><w:rPr>`)
		if bold {
			b.WriteString(`<w:b/>`)
		}
		if italic {
			b.WriteString(`<w:i/>`)
		}
		fmt.Fprintf(b, `<w:sz w:val="%d"/><w:szCs w:val="%d"/>`, size, size)
		b.WriteString(`</w:rPr>`)

		for j, line := range strings.Split(para, "\n") {
			if j > 0 {
				b.WriteString(`<w:br/>`)
			}
			b.WriteString(`<w:t xml:space="preserve">` + escapeXML(line) + `</w:t>`)
		}
		b.WriteString(`</w:r></w:p>`)
	}
}

// escapeXML escapes text and strips the control characters XML forbids — which
// a pasted document can easily contain.
func escapeXML(s string) string {
	var raw strings.Builder
	for _, r := range s {
		if r == '\t' || r >= 0x20 || r == 0x09 {
			raw.WriteRune(r)
		}
	}
	var out strings.Builder
	if err := xml.EscapeText(&out, []byte(raw.String())); err != nil {
		// EscapeText only fails if the writer fails, and strings.Builder
		// cannot. Fall back to the unescaped text rather than losing content.
		return raw.String()
	}
	return out.String()
}
