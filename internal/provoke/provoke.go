// Package provoke generates and tracks provocations: critiques that argue with
// the author instead of finishing their sentences.
//
// A provocation is the inverse of a suggestion. Suggestions are meant to be
// accepted; provocations are meant to be answered. A reasoned dismissal counts
// as success, which is why Dismiss requires a reason.
//
// There is therefore no "accept" verb here, and nothing generated is ever
// inserted into the author's text automatically.
package provoke

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/tanisha327/whetstone/internal/provider"
)

// Kind classifies what sort of pressure a provocation applies.
type Kind string

const (
	// KindCounterargument states the strongest case against the passage.
	KindCounterargument Kind = "counterargument"
	// KindAssumption names something treated as settled that is not.
	KindAssumption Kind = "assumption"
	// KindEvidenceGap identifies a claim whose support is missing or weak.
	KindEvidenceGap Kind = "evidence_gap"
	// KindAlternative offers a different reading of the same facts.
	KindAlternative Kind = "alternative"
	// KindFallacy names a specific reasoning error.
	KindFallacy Kind = "fallacy"
)

// Label returns the display name for a Kind.
func (k Kind) Label() string {
	switch k {
	case KindCounterargument:
		return "Counterargument"
	case KindAssumption:
		return "Assumption"
	case KindEvidenceGap:
		return "Evidence gap"
	case KindAlternative:
		return "Alternative"
	case KindFallacy:
		return "Fallacy"
	default:
		return string(k)
	}
}

// parseKind maps model output onto a known Kind, defaulting rather than
// failing: an unrecognised label is not worth discarding a good critique over.
func parseKind(s string) Kind {
	switch Kind(strings.ToLower(strings.TrimSpace(s))) {
	case KindCounterargument:
		return KindCounterargument
	case KindAssumption:
		return KindAssumption
	case KindEvidenceGap:
		return KindEvidenceGap
	case KindAlternative:
		return KindAlternative
	case KindFallacy:
		return KindFallacy
	default:
		return KindCounterargument
	}
}

// AnchorKind says what a provocation is attached to.
type AnchorKind string

const (
	// AnchorSection attaches to a document section being read.
	AnchorSection AnchorKind = "section"
	// AnchorOutline attaches to a node of the user's argument.
	AnchorOutline AnchorKind = "outline"
)

// Status is where a provocation sits in the user's attention.
type Status string

const (
	// StatusOpen means seen but not yet resolved.
	StatusOpen Status = "open"
	// StatusEngaged means the user acted on it and recorded how.
	StatusEngaged Status = "engaged"
	// StatusDismissed means the user judged it inapplicable and said why.
	StatusDismissed Status = "dismissed"
)

// Provocation is one piece of productive resistance.
type Provocation struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Text string `json:"text"`

	AnchorKind AnchorKind `json:"anchorKind"`
	// AnchorID is a section ID rendered as a string, or an outline node ID.
	AnchorID string `json:"anchorId"`

	Status Status `json:"status"`
	// Response is the user's own words: what they did about it, or why it does
	// not apply. Required to leave StatusOpen. Counted as user authorship.
	Response string `json:"response,omitempty"`

	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

// ErrReasonRequired is returned when resolving a provocation without a reason.
var ErrReasonRequired = fmt.Errorf("provoke: a reason is required to resolve a provocation")

// Dismiss marks a provocation inapplicable. The reason is mandatory: an
// unexamined dismissal is indistinguishable from not having read it.
func (p *Provocation) Dismiss(reason string, now time.Time) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ErrReasonRequired
	}
	p.Status = StatusDismissed
	p.Response = reason
	p.ResolvedAt = &now
	return nil
}

// Engage marks a provocation as acted upon, recording what the user did.
func (p *Provocation) Engage(note string, now time.Time) error {
	note = strings.TrimSpace(note)
	if note == "" {
		return ErrReasonRequired
	}
	p.Status = StatusEngaged
	p.Response = note
	p.ResolvedAt = &now
	return nil
}

// Resolved reports whether the user has dealt with this provocation.
func (p *Provocation) Resolved() bool {
	return p.Status == StatusEngaged || p.Status == StatusDismissed
}

// Input describes what to critique.
type Input struct {
	// Anchor identifies what the provocations will attach to.
	AnchorKind AnchorKind
	AnchorID   string
	// Subject is the text under scrutiny.
	Subject string
	// Context is surrounding material the critic may use — the user's own
	// notes, sibling outline nodes, cited excerpts. Optional.
	Context string
	// Max bounds how many provocations to generate. Zero means 2.
	Max int
}

// systemPrompt sets the critic's posture. The constraints against agreement and
// against rewriting are what keep this from degenerating into a suggestion box.
const systemPrompt = `You are a rigorous critic embedded in a writing tool. Your job is to make the author think harder, not to do their thinking.

Rules:
- Attack the reasoning, never the author.
- Be specific and grounded in the text you were given. A generic objection that could apply to any argument is worthless.
- Do not rewrite, improve, or complete the author's text. Do not offer replacement wording.
- Do not praise. Do not summarise. Do not agree.
- Prefer the objection the author is least likely to have already considered.
- One or two sharp objections beat five obvious ones.
- If the passage is genuinely sound on a point, find the strongest remaining pressure elsewhere rather than manufacturing a weak one.`

type provocationPayload struct {
	Provocations []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	} `json:"provocations"`
}

// Generate produces provocations for the given input.
func Generate(ctx context.Context, p provider.Provider, in Input) ([]Provocation, error) {
	if p == nil {
		return nil, fmt.Errorf("provoke: no provider configured")
	}
	if strings.TrimSpace(in.Subject) == "" {
		return nil, nil
	}
	max := in.Max
	if max <= 0 {
		max = 2
	}

	purpose := provider.PurposeProvokeSection
	subjectLabel := "PASSAGE THE AUTHOR IS READING"
	if in.AnchorKind == AnchorOutline {
		purpose = provider.PurposeProvokeOutline
		subjectLabel = "THE AUTHOR'S OWN ARGUMENT"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s:\n%s\n\n", subjectLabel, in.Subject)
	if c := strings.TrimSpace(in.Context); c != "" {
		fmt.Fprintf(&b, "SURROUNDING CONTEXT (for grounding only):\n%s\n\n", c)
	}
	fmt.Fprintf(&b, `Produce at most %d objections. Respond with a JSON object:
{"provocations": [{"kind": "<counterargument|assumption|evidence_gap|alternative|fallacy>",
                   "text": "<the objection, 1-3 sentences, ending in a question the author must answer>"}]}`, max)

	resp, err := p.Complete(ctx, provider.Request{
		Purpose:     purpose,
		System:      systemPrompt,
		Messages:    []provider.Message{{Role: provider.RoleUser, Text: b.String()}},
		MaxTokens:   800,
		Temperature: 0.9, // divergence is the product here
		JSON:        true,
	})
	if err != nil {
		return nil, fmt.Errorf("provoke %s: %w", in.AnchorID, err)
	}

	var payload provocationPayload
	if err := provider.ExtractJSON(resp.Text, &payload); err != nil {
		return nil, fmt.Errorf("provoke %s: %w", in.AnchorID, err)
	}

	now := time.Now()
	out := make([]Provocation, 0, len(payload.Provocations))
	for _, item := range payload.Provocations {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		id, err := newID()
		if err != nil {
			return nil, err
		}
		out = append(out, Provocation{
			ID:         id,
			Kind:       parseKind(item.Kind),
			Text:       text,
			AnchorKind: in.AnchorKind,
			AnchorID:   in.AnchorID,
			Status:     StatusOpen,
			CreatedAt:  now,
		})
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

// newID returns a short random identifier. Random rather than sequential so
// provocations from concurrent requests cannot collide.
func newID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("provoke: generating id: %w", err)
	}
	return "pv-" + hex.EncodeToString(b[:]), nil
}
