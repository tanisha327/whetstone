package lens

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tanisha327/whetstone/internal/doc"
	"github.com/tanisha327/whetstone/internal/provider"
)

// fakeProvider is a test double, not a shipped mock. It records requests and
// returns whatever the test hands it, so the package can be tested without a
// network call or an API key.
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

const goodLensReply = `{"text":"Orients the reader.","key_points":["a specific item","another"],"relevance":8}`

func TestByID(t *testing.T) {
	if l, ok := ByID("risk"); !ok || l.Name != "Risk" {
		t.Errorf("ByID(risk) = %+v, %v", l, ok)
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID(nope) should not be found")
	}
}

func TestBuiltin_AllFieldsPopulated(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range Builtin {
		if l.ID == "" || l.Name == "" || l.Description == "" || l.Focus == "" {
			t.Errorf("lens %+v has an empty field", l)
		}
		if seen[l.ID] {
			t.Errorf("duplicate lens ID %q", l.ID)
		}
		seen[l.ID] = true
	}
}

func TestApplySection(t *testing.T) {
	p := &fakeProvider{reply: goodLensReply}
	l, _ := ByID("technical")
	s := doc.Section{ID: 3, Title: "Benchmark", Body: "The scheduler reduced p99 latency by 40%."}

	got, err := ApplySection(context.Background(), p, l, s)
	if err != nil {
		t.Fatalf("ApplySection: %v", err)
	}
	if got.SectionID != 3 {
		t.Errorf("SectionID = %d, want 3", got.SectionID)
	}
	if got.LensID != "technical" {
		t.Errorf("LensID = %q", got.LensID)
	}
	if got.Text != "Orients the reader." {
		t.Errorf("Text = %q", got.Text)
	}
	if len(got.KeyPoints) != 2 {
		t.Errorf("KeyPoints = %v", got.KeyPoints)
	}
	if got.Relevance != 8 {
		t.Errorf("Relevance = %d", got.Relevance)
	}

	req := p.last(t)
	if req.Purpose != provider.PurposeLens {
		t.Errorf("Purpose = %q", req.Purpose)
	}
	if !req.JSON {
		t.Error("lens requests should ask for JSON")
	}
	if req.Temperature > 0.5 {
		t.Errorf("Temperature = %v; orientation should be low-variance", req.Temperature)
	}
	body := req.Messages[0].Text
	if !strings.Contains(body, l.Focus) {
		t.Error("prompt should carry the lens focus")
	}
	if !strings.Contains(body, s.Body) {
		t.Error("prompt should carry the passage")
	}
}

func TestApplySection_ClampsRelevance(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{-4, 0}, {0, 0}, {5, 5}, {10, 10}, {99, 10}} {
		p := &fakeProvider{reply: `{"text":"t","key_points":[],"relevance":` + itoa(tc.in) + `}`}
		got, err := ApplySection(context.Background(), p, Builtin[0], doc.Section{ID: 1, Body: "x"})
		if err != nil {
			t.Fatal(err)
		}
		if got.Relevance != tc.want {
			t.Errorf("relevance %d clamped to %d, want %d", tc.in, got.Relevance, tc.want)
		}
	}
}

func TestApplySection_TolerantOfFencedJSON(t *testing.T) {
	p := &fakeProvider{reply: "Here you go:\n```json\n" + goodLensReply + "\n```"}
	got, err := ApplySection(context.Background(), p, Builtin[0], doc.Section{ID: 1, Body: "x"})
	if err != nil {
		t.Fatalf("ApplySection should tolerate fenced output: %v", err)
	}
	if got.Text != "Orients the reader." {
		t.Errorf("Text = %q", got.Text)
	}
}

func TestApplySection_MalformedJSON(t *testing.T) {
	p := &fakeProvider{reply: "I'd rather not."}
	_, err := ApplySection(context.Background(), p, Builtin[0], doc.Section{ID: 4, Body: "x"})
	if err == nil {
		t.Fatal("expected an error for unparseable output")
	}
	if !strings.Contains(err.Error(), "section 4") {
		t.Errorf("error should name the section, got: %v", err)
	}
}

// The system prompt is a design decision, not incidental wording: it is what
// keeps a lens from becoming a summary that replaces reading.
func TestSystemPrompt_ForbidsConclusions(t *testing.T) {
	lowered := strings.ToLower(systemPrompt)
	for _, want := range []string{"read the source material themselves", "never invent"} {
		if !strings.Contains(lowered, want) {
			t.Errorf("system prompt no longer contains %q", want)
		}
	}
}

func TestApplySection_EmptyBodyShortCircuits(t *testing.T) {
	p := &fakeProvider{reply: goodLensReply}
	got, err := ApplySection(context.Background(), p, Builtin[0], doc.Section{ID: 1, Body: "   "})
	if err != nil {
		t.Fatalf("ApplySection: %v", err)
	}
	if got.Text != "(empty section)" {
		t.Errorf("Text = %q", got.Text)
	}
	if len(p.calls) != 0 {
		t.Error("an empty section should not cost a provider call")
	}
}

func TestApplySection_NilProvider(t *testing.T) {
	_, err := ApplySection(context.Background(), nil, Builtin[0], doc.Section{ID: 1, Body: "x"})
	if err == nil {
		t.Fatal("expected an error with no provider")
	}
}

func TestApplySection_PropagatesProviderError(t *testing.T) {
	sentinel := errors.New("upstream is down")
	p := &fakeProvider{err: sentinel}
	_, err := ApplySection(context.Background(), p, Builtin[0], doc.Section{ID: 7, Body: "x"})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want it to wrap the provider error", err)
	}
	if !strings.Contains(err.Error(), "section 7") {
		t.Errorf("error should name the section, got: %v", err)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct{ in, want int }{{-5, 0}, {0, 0}, {5, 5}, {10, 10}, {99, 10}}
	for _, tc := range tests {
		if got := clamp(tc.in, 0, 10); got != tc.want {
			t.Errorf("clamp(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// itoa avoids pulling strconv in just for the table above.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
