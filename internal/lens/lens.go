// Package lens produces task-relevant micro-summaries of document sections.
//
// A lens is not a summary. A summary answers "what does this say?" and replaces
// reading. A lens answers "what does this say that bears on my question?" and
// directs reading instead.
//
// See docs/adr/0001-no-chat-box.md.
package lens

import (
	"context"
	"fmt"
	"strings"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/provider"
)

// Lens is a reading stance applied to a document.
type Lens struct {
	// ID is the stable key persisted in a workspace.
	ID string
	// Name is the display label.
	Name string
	// Description tells the user what this lens will surface.
	Description string
	// Focus is the instruction fragment injected into the prompt. It is the
	// only part of a Lens the model sees.
	Focus string
}

// Builtin is the default set, in picker order.
//
// Kept short on purpose: a long list is a menu to browse rather than a decision
// to make. These four suit any document. Add your own — a lens is data.
var Builtin = []Lens{
	{
		ID:          "claims",
		Name:        "Claims & evidence",
		Description: "What is asserted, and what backs it up",
		Focus: "Identify the concrete claims made in this passage and, for each, " +
			"state what evidence is offered. Name claims that are asserted without support.",
	},
	{
		ID:          "technical",
		Name:        "Technical",
		Description: "Mechanisms, constraints, how it actually works",
		Focus: "Read this passage for technical substance: mechanisms, interfaces, " +
			"dependencies, limits, and stated constraints. Quote versions, numbers, " +
			"and units exactly. Name anything described only in the abstract.",
	},
	{
		ID:          "risk",
		Name:        "Risk",
		Description: "What could go wrong, and what is unstated",
		Focus: "Read this passage for risks, dependencies, and failure modes — " +
			"including ones the text implies but does not name.",
	},
	{
		ID:          "method",
		Name:        "Method",
		Description: "How the findings were produced",
		Focus: "Read this passage for methodology: sample, timeframe, instrument, " +
			"and any limitation that constrains how far the findings generalise.",
	},
}

// ByID returns a builtin lens.
func ByID(id string) (Lens, bool) {
	for _, l := range Builtin {
		if l.ID == id {
			return l, true
		}
	}
	return Lens{}, false
}

// Summary is one section viewed through one lens.
type Summary struct {
	// SectionID is the doc.Section this describes.
	SectionID int
	// LensID is the lens used.
	LensID string
	// Text is a two-to-three sentence orientation to the section.
	Text string
	// KeyPoints are the specific items the lens surfaced.
	KeyPoints []string
	// Relevance is the model's 0-10 judgement of how much this section bears
	// on the lens. It orders the reading queue; it never hides a section.
	Relevance int
}

// systemPrompt keeps the model in the role of an index, not a replacement for
// reading. The last sentence is load-bearing.
const systemPrompt = `You build reading aids for an expert who will read the source material themselves.

You do not summarise to save the reader time. You orient them: you say what is in a passage that bears on their current question, so they can decide how closely to read it.

Rules:
- Be specific. "Discusses pricing" is useless; "claims a 12% premium is acceptable to 68% of respondents" is useful.
- Quote figures and their units exactly as the passage gives them.
- Never invent. If the passage does not address the lens, say so and score relevance low.
- Do not offer conclusions or recommendations. That is the reader's job.`

type summaryPayload struct {
	Text      string   `json:"text"`
	KeyPoints []string `json:"key_points"`
	Relevance int      `json:"relevance"`
}

// ApplySection summarises one section through one lens.
func ApplySection(ctx context.Context, p provider.Provider, l Lens, s doc.Section) (Summary, error) {
	if p == nil {
		return Summary{}, fmt.Errorf("lens: no provider configured")
	}
	if strings.TrimSpace(s.Body) == "" {
		return Summary{SectionID: s.ID, LensID: l.ID, Text: "(empty section)"}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "LENS: %s\n%s\n\n", l.Name, l.Focus)
	fmt.Fprintf(&b, "PASSAGE (section %d, %q):\n%s\n\n", s.ID, s.Title, s.Body)
	b.WriteString(`Respond with a JSON object:
{"text": "<2-3 sentences orienting the reader>",
 "key_points": ["<specific item>", "..."],
 "relevance": <0-10 integer>}`)

	resp, err := p.Complete(ctx, provider.Request{
		Purpose:     provider.PurposeLens,
		System:      systemPrompt,
		Messages:    []provider.Message{{Role: provider.RoleUser, Text: b.String()}},
		MaxTokens:   700,
		Temperature: 0.2, // orientation should be stable across runs
		JSON:        true,
	})
	if err != nil {
		return Summary{}, fmt.Errorf("lens %s on section %d: %w", l.ID, s.ID, err)
	}

	var payload summaryPayload
	if err := provider.ExtractJSON(resp.Text, &payload); err != nil {
		return Summary{}, fmt.Errorf("lens %s on section %d: %w", l.ID, s.ID, err)
	}

	return Summary{
		SectionID: s.ID,
		LensID:    l.ID,
		Text:      strings.TrimSpace(payload.Text),
		KeyPoints: payload.KeyPoints,
		Relevance: clamp(payload.Relevance, 0, 10),
	}, nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
