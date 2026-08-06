package doc

import (
	"regexp"
	"strings"
)

// Plain converts markdown into readable prose.
//
// Sections are meant to be read, not decoded: `## Heading`, `**bold**` and
// `[text](url)` on screen make the reader parse syntax instead of argument.
//
// Lossy and one-way. Emphasis and link targets are dropped, not stored for
// re-rendering — Whetstone never writes markdown back out.
func Plain(s string) string {
	if s == "" {
		return ""
	}
	var out []string
	inFence := false

	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)

		// Fenced code: drop the fences, keep the code verbatim.
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}

		if isRule(trimmed) {
			continue
		}
		if isTableSeparator(trimmed) {
			continue
		}

		line = stripLinePrefix(line)
		line = stripTablePipes(line)
		line = plainInline(line)
		out = append(out, strings.TrimRight(line, " \t"))
	}

	// Collapse the runs of blank lines that stripping tends to leave behind.
	return collapseBlank(out)
}

// stripLinePrefix removes block markers that begin a line: heading hashes,
// blockquote arrows, and bullet characters. Ordered list markers ("1.") are
// left alone — they already read as normal text.
func stripLinePrefix(line string) string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	body := strings.TrimLeft(line, " \t")

	// Blockquotes can nest: "> > quoted".
	for strings.HasPrefix(body, ">") {
		body = strings.TrimLeft(strings.TrimPrefix(body, ">"), " ")
	}

	// ATX heading.
	if n := countHashes(body); n > 0 && n <= 6 && len(body) > n && body[n] == ' ' {
		body = strings.TrimSpace(body[n+1:])
		// A heading inside a body is still a heading; keep it visually
		// distinct without markup.
		return indent + body
	}

	// Unordered list markers become a bullet the eye can skim.
	for _, marker := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(body, marker) {
			return indent + "• " + strings.TrimSpace(body[len(marker):])
		}
	}
	return indent + body
}

func countHashes(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	return n
}

// isRule reports a horizontal rule: three or more of -, * or _ and nothing else.
func isRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != c && s[i] != ' ' {
			return false
		}
	}
	return true
}

// isTableSeparator reports the |---|:---:| line under a table header.
func isTableSeparator(s string) bool {
	if !strings.Contains(s, "-") || !strings.Contains(s, "|") {
		return false
	}
	for _, r := range s {
		switch r {
		case '|', '-', ':', ' ':
		default:
			return false
		}
	}
	return true
}

// stripTablePipes turns a table row into spaced columns. Real table layout is
// out of scope; the goal is only that the cells stay readable.
func stripTablePipes(line string) string {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "|") || !strings.HasSuffix(t, "|") {
		return line
	}
	cells := strings.Split(strings.Trim(t, "|"), "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return strings.Join(cells, "   ")
}

var (
	reImage = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reLink  = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reRef   = regexp.MustCompile(`\[([^\]]+)\]\[[^\]]*\]`)
	reBold  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reBoldU = regexp.MustCompile(`__([^_]+)__`)
	reEmA   = regexp.MustCompile(`\*([^*\n]+)\*`)
	// Underscore emphasis only at word boundaries, so snake_case survives.
	reEmU    = regexp.MustCompile(`(^|[^\w])_([^_\n]+)_([^\w]|$)`)
	reCode   = regexp.MustCompile("`([^`\n]+)`")
	reStrike = regexp.MustCompile(`~~([^~\n]+)~~`)
	reAutoLk = regexp.MustCompile(`<(https?://[^>]+)>`)
)

// plainInline removes inline markup, keeping the text it wrapped.
func plainInline(s string) string {
	s = reImage.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reRef.ReplaceAllString(s, "$1")
	s = reAutoLk.ReplaceAllString(s, "$1")
	s = reCode.ReplaceAllString(s, "$1")
	s = reBold.ReplaceAllString(s, "$1")
	s = reBoldU.ReplaceAllString(s, "$1")
	s = reStrike.ReplaceAllString(s, "$1")
	s = reEmA.ReplaceAllString(s, "$1")
	s = reEmU.ReplaceAllString(s, "${1}${2}${3}")
	return s
}

// collapseBlank joins lines, allowing at most one blank line in a row and
// trimming blank lines from both ends.
func collapseBlank(lines []string) string {
	out := make([]string, 0, len(lines))
	blank := false
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			if blank {
				continue
			}
			blank = true
			out = append(out, "")
			continue
		}
		blank = false
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
