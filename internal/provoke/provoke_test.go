package provoke

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tanisha327/whetstone/internal/provider"
)

// fakeProvider is a test double, not a shipped mock. It records requests and
// returns whatever the test hands it.
type fakeProvider struct {
	reply string
	err   error
	calls []provider.Request
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Complete(_ context.Context, req provider.Request) (provider.Response, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return provider.Response{}, f.err
	}
	return provider.Response{Text: f.reply, Model: "fake-1"}, nil
}

func (f *fakeProvider) last(t *testing.T) provider.Request {
	t.Helper()
	if len(f.calls) == 0 {
		t.Fatal("no provider call was made")
	}
	return f.calls[len(f.calls)-1]
}

const twoProvocations = `{"provocations":[
  {"kind":"evidence_gap","text":"The the 40% figure is from one benchmark. What would revealed preference look like?"},
  {"kind":"assumption","text":"You treat the panel as representative. Is it?"}
]}`

func TestGenerate(t *testing.T) {
	p := &fakeProvider{reply: twoProvocations}

	got, err := Generate(context.Background(), p, Input{
		AnchorKind: AnchorSection,
		AnchorID:   "report#3",
		Subject:    "The scheduler reduced p99 latency by 40%.",
		Context:    "The author is trying to answer: should we adopt the new scheduler?",
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d provocations, want 2", len(got))
	}
	if got[0].Kind != KindEvidenceGap || got[1].Kind != KindAssumption {
		t.Errorf("kinds = %q, %q", got[0].Kind, got[1].Kind)
	}
	for _, pv := range got {
		if !strings.HasPrefix(pv.ID, "pv-") {
			t.Errorf("bad ID %q", pv.ID)
		}
		if pv.Status != StatusOpen {
			t.Errorf("Status = %q, want open", pv.Status)
		}
		if pv.AnchorID != "report#3" || pv.AnchorKind != AnchorSection {
			t.Errorf("anchor = %s/%s", pv.AnchorKind, pv.AnchorID)
		}
	}

	req := p.last(t)
	if req.Purpose != provider.PurposeProvokeSection {
		t.Errorf("Purpose = %q", req.Purpose)
	}
	if req.Temperature < 0.7 {
		t.Errorf("Temperature = %v; provocations should diverge", req.Temperature)
	}
	if !strings.Contains(req.Messages[0].Text, "new scheduler") {
		t.Error("prompt should carry the question context")
	}
}

func TestGenerate_IDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		got, err := Generate(context.Background(), &fakeProvider{reply: twoProvocations}, Input{
			AnchorKind: AnchorSection,
			AnchorID:   "s",
			Subject:    "a passage",
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, pv := range got {
			if seen[pv.ID] {
				t.Fatalf("duplicate provocation ID %q", pv.ID)
			}
			seen[pv.ID] = true
		}
	}
}

func TestGenerate_OutlineUsesOutlinePurpose(t *testing.T) {
	p := &fakeProvider{reply: twoProvocations}
	if _, err := Generate(context.Background(), p, Input{
		AnchorKind: AnchorOutline,
		AnchorID:   "n-1",
		Subject:    "We should adopt the new scheduler.",
	}); err != nil {
		t.Fatal(err)
	}
	req := p.last(t)
	if req.Purpose != provider.PurposeProvokeOutline {
		t.Errorf("Purpose = %q, want %q", req.Purpose, provider.PurposeProvokeOutline)
	}
	if !strings.Contains(req.Messages[0].Text, "AUTHOR'S OWN ARGUMENT") {
		t.Error("outline provocations should be framed as critiquing the author's own argument")
	}
}

func TestGenerate_RespectsMax(t *testing.T) {
	got, err := Generate(context.Background(), &fakeProvider{reply: twoProvocations}, Input{
		AnchorKind: AnchorSection,
		AnchorID:   "s",
		Subject:    "a passage about pricing",
		Max:        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d provocations, want 1", len(got))
	}
}

func TestGenerate_SkipsEmptyText(t *testing.T) {
	p := &fakeProvider{reply: `{"provocations":[{"kind":"fallacy","text":"  "},{"kind":"fallacy","text":"real one"}]}`}
	got, err := Generate(context.Background(), p, Input{AnchorID: "s", Subject: "text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "real one" {
		t.Errorf("got %+v, want only the non-empty provocation", got)
	}
}

func TestGenerate_EmptySubjectIsNoOp(t *testing.T) {
	p := &fakeProvider{reply: twoProvocations}
	got, err := Generate(context.Background(), p, Input{AnchorID: "s", Subject: "  "})
	if err != nil || got != nil {
		t.Errorf("Generate = %v, %v; want nil, nil", got, err)
	}
	if len(p.calls) != 0 {
		t.Error("an empty subject should not cost a provider call")
	}
}

func TestGenerate_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	_, err := Generate(context.Background(), &fakeProvider{err: sentinel}, Input{
		AnchorID: "s", Subject: "text",
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to wrap the provider error", err)
	}
}

func TestGenerate_MalformedJSON(t *testing.T) {
	_, err := Generate(context.Background(), &fakeProvider{reply: "no."}, Input{
		AnchorID: "s", Subject: "text",
	})
	if err == nil {
		t.Fatal("expected an error for unparseable output")
	}
}

func TestGenerate_NilProvider(t *testing.T) {
	_, err := Generate(context.Background(), nil, Input{AnchorID: "s", Subject: "text"})
	if err == nil {
		t.Fatal("expected an error with no provider")
	}
}

// Requiring a reason is the whole mechanism. If this ever becomes optional,
// dismissing degenerates into swatting away objections without reading them.
func TestDismiss_RequiresReason(t *testing.T) {
	p := &Provocation{Status: StatusOpen}
	for _, blank := range []string{"", "   ", "\t\n"} {
		if err := p.Dismiss(blank, time.Now()); !errors.Is(err, ErrReasonRequired) {
			t.Errorf("Dismiss(%q) = %v, want ErrReasonRequired", blank, err)
		}
		if p.Status != StatusOpen {
			t.Fatalf("Status changed to %q on a rejected dismissal", p.Status)
		}
	}
}

func TestDismiss_RecordsReason(t *testing.T) {
	now := time.Now()
	p := &Provocation{Status: StatusOpen}
	if err := p.Dismiss("  survey covers our segment exactly  ", now); err != nil {
		t.Fatalf("Dismiss: %v", err)
	}
	if p.Status != StatusDismissed {
		t.Errorf("Status = %q", p.Status)
	}
	if p.Response != "survey covers our segment exactly" {
		t.Errorf("Response = %q, want it trimmed", p.Response)
	}
	if p.ResolvedAt == nil || !p.ResolvedAt.Equal(now) {
		t.Errorf("ResolvedAt = %v", p.ResolvedAt)
	}
	if !p.Resolved() {
		t.Error("Resolved() = false")
	}
}

func TestEngage_RequiresNote(t *testing.T) {
	p := &Provocation{Status: StatusOpen}
	if err := p.Engage("", time.Now()); !errors.Is(err, ErrReasonRequired) {
		t.Errorf("Engage = %v, want ErrReasonRequired", err)
	}
}

func TestEngage_RecordsNote(t *testing.T) {
	p := &Provocation{Status: StatusOpen}
	if err := p.Engage("added a caveat about revealed preference", time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Status != StatusEngaged || !p.Resolved() {
		t.Errorf("Status = %q, Resolved = %v", p.Status, p.Resolved())
	}
}

func TestResolved_OpenIsNotResolved(t *testing.T) {
	if (&Provocation{Status: StatusOpen}).Resolved() {
		t.Error("an open provocation is not resolved")
	}
}

func TestParseKind(t *testing.T) {
	tests := map[string]Kind{
		"counterargument": KindCounterargument,
		"ASSUMPTION":      KindAssumption,
		" evidence_gap ":  KindEvidenceGap,
		"alternative":     KindAlternative,
		"fallacy":         KindFallacy,
		"something else":  KindCounterargument, // default rather than discard
		"":                KindCounterargument,
	}
	for in, want := range tests {
		if got := parseKind(in); got != want {
			t.Errorf("parseKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKindLabel(t *testing.T) {
	if got := KindEvidenceGap.Label(); got != "Evidence gap" {
		t.Errorf("Label = %q", got)
	}
	if got := Kind("custom").Label(); got != "custom" {
		t.Errorf("unknown Label = %q, want passthrough", got)
	}
}

func TestSystemPrompt_ForbidsRewriting(t *testing.T) {
	lowered := strings.ToLower(systemPrompt)
	for _, want := range []string{"do not rewrite", "do not praise", "do not agree"} {
		if !strings.Contains(lowered, want) {
			t.Errorf("system prompt no longer contains %q", want)
		}
	}
}
