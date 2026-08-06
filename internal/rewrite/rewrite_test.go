package rewrite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/tanisha327/whetstone/internal/provider"
)

// fakeProvider is a test double, not a shipped mock.
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

func TestByID(t *testing.T) {
	if d, ok := ByID("formal"); !ok || d.Name != "More formal" {
		t.Errorf("ByID(formal) = %+v, %v", d, ok)
	}
	if _, ok := ByID("nope"); ok {
		t.Error("ByID(nope) should not be found")
	}
}

func TestBuiltin_AllFieldsPopulated(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range Builtin {
		if d.ID == "" || d.Name == "" || d.Description == "" || d.Focus == "" {
			t.Errorf("dimension %+v has an empty field", d)
		}
		if seen[d.ID] {
			t.Errorf("duplicate dimension ID %q", d.ID)
		}
		seen[d.ID] = true
	}
}

func TestAlternatives(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["First option.","Second option.","Third option."]}`}
	d, _ := ByID("plain")

	got, err := Alternatives(context.Background(), p, d, "The original passage.", 3)
	if err != nil {
		t.Fatalf("Alternatives: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d alternatives, want 3: %+v", len(got), got)
	}

	req := p.last(t)
	if req.Purpose != provider.PurposeRewrite {
		t.Errorf("Purpose = %q", req.Purpose)
	}
	if !req.JSON {
		t.Error("rewrite should ask for JSON")
	}
	if req.Temperature < 0.5 {
		t.Errorf("Temperature = %v; alternatives should differ from each other", req.Temperature)
	}
	body := req.Messages[0].Text
	if !strings.Contains(body, d.Focus) {
		t.Error("prompt should carry the dimension focus")
	}
	if !strings.Contains(body, "The original passage.") {
		t.Error("prompt should carry the author's passage")
	}
}

// An option identical to what the author already wrote is not an option.
func TestAlternatives_DropsEchoesOfTheOriginal(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["The  original   passage.","A real alternative."]}`}
	got, err := Alternatives(context.Background(), p, Builtin[0], "The original passage.", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "A real alternative." {
		t.Errorf("got %+v, want only the genuine alternative", got)
	}
}

func TestAlternatives_DropsDuplicatesAndBlanks(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["One.","one.","   ","Two."]}`}
	got, err := Alternatives(context.Background(), p, Builtin[0], "orig", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %+v, want 2 after dedup", got)
	}
}

func TestAlternatives_RespectsCount(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["a","b","c","d","e"]}`}
	got, err := Alternatives(context.Background(), p, Builtin[0], "orig", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d, want 2", len(got))
	}
}

func TestAlternatives_EmptyTextIsNoOp(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["x"]}`}
	got, err := Alternatives(context.Background(), p, Builtin[0], "   ", 3)
	if err != nil || got != nil {
		t.Errorf("Alternatives = %v, %v; want nil, nil", got, err)
	}
	if len(p.calls) != 0 {
		t.Error("empty text should not cost a provider call")
	}
}

// Re-voicing a whole document in one click is not a thing the author should be
// doing; the limit makes them select a passage.
func TestAlternatives_RejectsOversizedPassage(t *testing.T) {
	p := &fakeProvider{reply: `{"alternatives":["x"]}`}
	_, err := Alternatives(context.Background(), p, Builtin[0], strings.Repeat("word ", 2000), 3)
	if err == nil {
		t.Fatal("expected an error for an oversized passage")
	}
	if !strings.Contains(err.Error(), "select less") {
		t.Errorf("error should tell the author what to do, got: %v", err)
	}
	if len(p.calls) != 0 {
		t.Error("an oversized passage should not reach the provider")
	}
}

func TestAlternatives_NilProvider(t *testing.T) {
	if _, err := Alternatives(context.Background(), nil, Builtin[0], "text", 3); err == nil {
		t.Fatal("expected an error with no provider")
	}
}

func TestAlternatives_PropagatesError(t *testing.T) {
	sentinel := errors.New("upstream down")
	_, err := Alternatives(context.Background(), &fakeProvider{err: sentinel}, Builtin[0], "text", 3)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want it to wrap the provider error", err)
	}
}

func TestAlternatives_MalformedJSON(t *testing.T) {
	if _, err := Alternatives(context.Background(), &fakeProvider{reply: "sorry"}, Builtin[0], "text", 3); err == nil {
		t.Fatal("expected an error for unparseable output")
	}
}

// The system prompt is the whole guarantee that a rewrite stays a rewrite. If
// these clauses go, "re-voice" quietly becomes "re-argue in the author's name".
func TestSystemPrompt_ForbidsInvention(t *testing.T) {
	lowered := strings.ToLower(systemPrompt)
	for _, want := range []string{
		"you do not write for them",
		"preserve every claim",
		"add nothing",
		"remove nothing of substance",
	} {
		if !strings.Contains(lowered, want) {
			t.Errorf("system prompt no longer contains %q", want)
		}
	}
}
