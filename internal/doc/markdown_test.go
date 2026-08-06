package doc

import (
	"strings"
	"testing"
)

func TestPlain_Inline(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bold stars", "a **bold** word", "a bold word"},
		{"bold underscores", "a __bold__ word", "a bold word"},
		{"italic stars", "an *emphatic* word", "an emphatic word"},
		{"italic underscores", "an _emphatic_ word", "an emphatic word"},
		{"inline code", "use `go test` here", "use go test here"},
		{"strikethrough", "it was ~~wrong~~ right", "it was wrong right"},
		{"link", "see [the report](https://x.com/r) now", "see the report now"},
		{"image", "![a chart](chart.png)", "a chart"},
		{"reference link", "see [the report][1]", "see the report"},
		{"autolink", "at <https://example.com> today", "at https://example.com today"},
		{"combined", "**bold** and *thin* and `code`", "bold and thin and code"},
		{"plain passes through", "just ordinary prose", "just ordinary prose"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Plain(tc.in); got != tc.want {
				t.Errorf("Plain(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Underscores inside identifiers are not emphasis. Mangling snake_case would
// corrupt quoted code and data in a source document.
func TestPlain_LeavesSnakeCaseAlone(t *testing.T) {
	for _, in := range []string{
		"the beta_two variable",
		"openai_responses and bedrock_model_invoke",
		"a_b_c",
	} {
		if got := Plain(in); got != in {
			t.Errorf("Plain(%q) = %q, want it unchanged", in, got)
		}
	}
}

func TestPlain_Headings(t *testing.T) {
	got := Plain("# Title\n\n## Sub heading\n\nBody.")
	if strings.Contains(got, "#") {
		t.Errorf("heading markers survived: %q", got)
	}
	if !strings.Contains(got, "Title") || !strings.Contains(got, "Sub heading") {
		t.Errorf("heading text was lost: %q", got)
	}
}

func TestPlain_Lists(t *testing.T) {
	got := Plain("- first\n* second\n+ third")
	want := "• first\n• second\n• third"
	if got != want {
		t.Errorf("Plain lists = %q, want %q", got, want)
	}
}

func TestPlain_OrderedListsKeptAsIs(t *testing.T) {
	in := "1. first\n2. second"
	if got := Plain(in); got != in {
		t.Errorf("Plain(%q) = %q; ordered lists already read as text", in, got)
	}
}

func TestPlain_Blockquote(t *testing.T) {
	got := Plain("> quoted line\n> > nested")
	if strings.Contains(got, ">") {
		t.Errorf("blockquote markers survived: %q", got)
	}
	if !strings.Contains(got, "quoted line") || !strings.Contains(got, "nested") {
		t.Errorf("quoted text was lost: %q", got)
	}
}

func TestPlain_HorizontalRulesDropped(t *testing.T) {
	got := Plain("above\n\n---\n\nbelow")
	if strings.Contains(got, "---") {
		t.Errorf("rule survived: %q", got)
	}
	if !strings.Contains(got, "above") || !strings.Contains(got, "below") {
		t.Errorf("text around the rule was lost: %q", got)
	}
}

// Code is content: the fence goes, what it wrapped stays exactly as written,
// including any characters that look like markdown.
func TestPlain_CodeFence(t *testing.T) {
	got := Plain("before\n\n```go\nx := *p\n```\n\nafter")
	if strings.Contains(got, "```") {
		t.Errorf("fence survived: %q", got)
	}
	if !strings.Contains(got, "x := *p") {
		t.Errorf("code body was altered: %q", got)
	}
}

func TestPlain_Table(t *testing.T) {
	got := Plain("| Name | Value |\n|------|-------|\n| p99 | 40%   |")
	if strings.Contains(got, "|") {
		t.Errorf("pipes survived: %q", got)
	}
	if strings.Contains(got, "---") {
		t.Errorf("separator row survived: %q", got)
	}
	if !strings.Contains(got, "p99") || !strings.Contains(got, "40%") {
		t.Errorf("cell contents were lost: %q", got)
	}
}

func TestPlain_CollapsesBlankRuns(t *testing.T) {
	got := Plain("a\n\n\n\n\nb")
	if got != "a\n\nb" {
		t.Errorf("Plain = %q, want a\\n\\nb", got)
	}
}

func TestPlain_TrimsEnds(t *testing.T) {
	if got := Plain("\n\n  body  \n\n"); got != "body" {
		t.Errorf("Plain = %q, want %q", got, "body")
	}
}

func TestPlain_Empty(t *testing.T) {
	if got := Plain(""); got != "" {
		t.Errorf("Plain(\"\") = %q", got)
	}
}

// The whole point, end to end: a pasted markdown document reads as prose.
func TestParse_BodiesAreProse(t *testing.T) {
	src := "# Findings\n\n" +
		"The **premium** is real, per [the report](https://x.com).\n\n" +
		"- one point\n" +
		"- another with `code`\n"

	d := Parse("r", "", src)
	if len(d.Sections) != 1 {
		t.Fatalf("sections = %d, want 1", len(d.Sections))
	}
	body := d.Sections[0].Body

	for _, syntax := range []string{"**", "](", "`", "- "} {
		if strings.Contains(body, syntax) {
			t.Errorf("markdown %q survived into the body:\n%s", syntax, body)
		}
	}
	for _, text := range []string{"premium", "the report", "one point", "code"} {
		if !strings.Contains(body, text) {
			t.Errorf("text %q was lost:\n%s", text, body)
		}
	}
	if !strings.Contains(body, "•") {
		t.Errorf("list items should become bullets:\n%s", body)
	}
}

func TestParse_TitlesAreProse(t *testing.T) {
	d := Parse("r", "", "## The **big** `finding`\n\nBody.\n")
	if got := d.Sections[0].Title; got != "The big finding" {
		t.Errorf("Title = %q, want %q", got, "The big finding")
	}
}
