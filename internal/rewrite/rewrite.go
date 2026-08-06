// Package rewrite offers alternative phrasings of the author's own prose:
// plainer, tighter, more formal, and so on.
//
// Nothing is applied automatically. The caller gets several options, shows them
// side by side, and the author picks one or keeps theirs.
//
// Rewriting only ever operates on text the author already wrote, so there is no
// path here to generate an argument from nothing.
package rewrite

import (
	"context"
	"fmt"
	"strings"

	"github.com/tanisha327/whetstone/internal/provider"
)

// Dimension is an axis along which a passage can be re-voiced.
type Dimension struct {
	// ID is the stable key sent by the client.
	ID string
	// Name is the display label.
	Name string
	// Description tells the author what will change.
	Description string
	// Focus is the instruction fragment given to the model. It is the only
	// part of a Dimension the model sees.
	Focus string
}

// Builtin is the default set, ordered as they appear in the picker.
var Builtin = []Dimension{
	{
		ID:          "practical",
		Name:        "More practical",
		Description: "Concrete, operational, what to do",
		Focus: "Rewrite this to be concrete and operational. Prefer specific " +
			"actions and figures over abstractions. Say what follows from it.",
	},
	{
		ID:          "inspirational",
		Name:        "More inspirational",
		Description: "Motivating, forward-looking",
		Focus: "Rewrite this to be motivating and forward-looking, without " +
			"adding any claim that is not already there. Do not overstate.",
	},
	{
		ID:          "formal",
		Name:        "More formal",
		Description: "Measured, suitable for a board paper",
		Focus: "Rewrite this in measured, professional register suitable for a " +
			"board paper. No contractions, no rhetorical questions.",
	},
	{
		ID:          "plain",
		Name:        "Plainer",
		Description: "Shorter words, no jargon",
		Focus: "Rewrite this in plain language. Remove jargon and abstraction. " +
			"Short words, short sentences, no hedging.",
	},
	{
		ID:          "logical",
		Name:        "More rigorous",
		Description: "Explicit premises and conclusion",
		Focus: "Rewrite this so the reasoning is explicit: state the premises, " +
			"then the conclusion that follows. Do not add new premises.",
	},
	{
		ID:          "concise",
		Name:        "Tighter",
		Description: "Same content, fewer words",
		Focus: "Rewrite this to be substantially shorter while keeping every " +
			"claim and figure. Cut qualifiers and repetition, not content.",
	},
	{
		ID:          "expansive",
		Name:        "Fuller",
		Description: "Same claims, spelled out",
		Focus: "Rewrite this to spell out the reasoning that is currently " +
			"compressed or implied. Add no new claims or evidence.",
	},
}

// ByID returns a builtin dimension.
func ByID(id string) (Dimension, bool) {
	for _, d := range Builtin {
		if d.ID == id {
			return d, true
		}
	}
	return Dimension{}, false
}

// systemPrompt keeps the model re-voicing rather than re-arguing. Every clause
// is load-bearing: without them a "rewrite" quietly becomes a new argument in
// the author's voice, which is the failure this whole tool is built against.
const systemPrompt = `You re-voice an author's own prose. You do not write for them.

Rules:
- Preserve every claim, figure, unit, and hedge exactly. If the original says "up to 12%", the rewrite says "up to 12%".
- Add nothing. No new evidence, no new examples, no new conclusions, no flourishes that assert something the original did not.
- Remove nothing of substance. Register and length may change; content may not.
- If the requested dimension would require distorting the meaning, return the passage close to unchanged rather than distorting it.
- Produce genuinely different options, not one sentence with synonyms swapped.
- Match the original's length unless the dimension asks otherwise.`

type payload struct {
	Alternatives []string `json:"alternatives"`
}

// maxInput bounds what will be sent for rewriting. A whole document is not a
// passage, and re-voicing one is not a thing the author should be doing in a
// single click.
const maxInput = 4000

// Alternatives returns up to n re-voicings of text along dimension d.
//
// Ephemeral: nothing is stored. The caller shows the options and the author
// chooses. Blanks and echoes of the original are dropped.
func Alternatives(ctx context.Context, p provider.Provider, d Dimension, text string, n int) ([]string, error) {
	if p == nil {
		return nil, fmt.Errorf("rewrite: no provider configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	if len([]rune(text)) > maxInput {
		return nil, fmt.Errorf("rewrite: passage is too long (%d characters, limit %d) — select less",
			len([]rune(text)), maxInput)
	}
	if n <= 0 {
		n = 3
	}

	prompt := fmt.Sprintf(
		"DIMENSION: %s\n%s\n\nTHE AUTHOR'S PASSAGE:\n%s\n\n"+
			"Produce %d alternatives. Respond with a JSON object:\n"+
			`{"alternatives": ["<full rewritten passage>", "..."]}`,
		d.Name, d.Focus, text, n)

	resp, err := p.Complete(ctx, provider.Request{
		Purpose:  provider.PurposeRewrite,
		System:   systemPrompt,
		Messages: []provider.Message{{Role: provider.RoleUser, Text: prompt}},
		// Room for n full rewrites of a passage up to maxInput runes.
		MaxTokens: 1800,
		// Warm: the point is that the options differ from each other.
		Temperature: 0.8,
		JSON:        true,
	})
	if err != nil {
		return nil, fmt.Errorf("rewrite %s: %w", d.ID, err)
	}

	var got payload
	if err := provider.ExtractJSON(resp.Text, &got); err != nil {
		return nil, fmt.Errorf("rewrite %s: %w", d.ID, err)
	}

	out := make([]string, 0, len(got.Alternatives))
	seen := map[string]bool{normalize(text): true}
	for _, alt := range got.Alternatives {
		alt = strings.TrimSpace(alt)
		key := normalize(alt)
		if alt == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, alt)
		if len(out) >= n {
			break
		}
	}
	return out, nil
}

// normalize collapses whitespace and case so a rewrite that differs only in
// spacing is treated as a duplicate.
func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
