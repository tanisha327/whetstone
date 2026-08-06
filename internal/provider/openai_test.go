package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestOpenAI(t *testing.T, srv *httptest.Server) *OpenAI {
	t.Helper()
	p, err := NewOpenAI(OpenAIConfig{
		APIKey:     "sk-test-secret",
		BaseURL:    srv.URL,
		Model:      "test-model",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	return p
}

func TestOpenAI_NoCredential(t *testing.T) {
	isolateConfig(t)
	_, err := NewOpenAI(OpenAIConfig{})
	if !errors.Is(err, ErrNoCredential) {
		t.Errorf("err = %v, want ErrNoCredential", err)
	}
}

func TestOpenAI_ReadsKeyFromEnv(t *testing.T) {
	isolateConfig(t)
	t.Setenv(EnvAPIKey, "sk-from-env")
	p, err := NewOpenAI(OpenAIConfig{})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	if p.apiKey != "sk-from-env" {
		t.Errorf("apiKey = %q", p.apiKey)
	}
}

func TestOpenAI_ReadsKeyFromFile(t *testing.T) {
	isolateConfig(t)
	if _, err := SaveKey("sk-from-file"); err != nil {
		t.Fatal(err)
	}
	p, err := NewOpenAI(OpenAIConfig{})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	if p.apiKey != "sk-from-file" {
		t.Errorf("apiKey = %q", p.apiKey)
	}
}

func TestOpenAI_BaseURLTrailingSlashNormalized(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeChatResponse(w, "ok")
	}))
	defer srv.Close()

	p, err := NewOpenAI(OpenAIConfig{
		APIKey:     "sk-test",
		BaseURL:    srv.URL + "/", // the trailing slash a user will paste
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewOpenAI: %v", err)
	}
	if _, err := p.Complete(context.Background(), simpleRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions (no double slash)", gotPath)
	}
}

func TestOpenAI_SendsAuthAndBody(t *testing.T) {
	var (
		gotAuth string
		gotBody chatRequest
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeChatResponse(w, "a reply")
	}))
	defer srv.Close()

	p := newTestOpenAI(t, srv)
	resp, err := p.Complete(context.Background(), Request{
		Purpose:     PurposeLens,
		System:      "be terse",
		Messages:    []Message{{Role: RoleUser, Text: "hello"}},
		MaxTokens:   99,
		Temperature: 0.3,
		JSON:        true,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if gotAuth != "Bearer sk-test-secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.Model != "test-model" {
		t.Errorf("model = %q", gotBody.Model)
	}
	if len(gotBody.Messages) != 2 || gotBody.Messages[0].Role != "system" {
		t.Fatalf("messages = %+v, want system then user", gotBody.Messages)
	}
	if gotBody.Messages[1].Content != "hello" {
		t.Errorf("user content = %q", gotBody.Messages[1].Content)
	}
	if gotBody.MaxTokens != 99 {
		t.Errorf("max_tokens = %d", gotBody.MaxTokens)
	}
	if gotBody.Temperature == nil || *gotBody.Temperature != 0.3 {
		t.Errorf("temperature = %v", gotBody.Temperature)
	}
	if gotBody.ResponseFormat == nil || gotBody.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format = %+v", gotBody.ResponseFormat)
	}
	if resp.Text != "a reply" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Usage.InputTokens != 11 || resp.Usage.OutputTokens != 5 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

func TestOpenAI_OmitsTemperatureWhenZero(t *testing.T) {
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		writeChatResponse(w, "ok")
	}))
	defer srv.Close()

	if _, err := newTestOpenAI(t, srv).Complete(context.Background(), simpleRequest()); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotBody.Temperature != nil {
		t.Errorf("temperature should be omitted when unset, got %v", *gotBody.Temperature)
	}
}

func TestOpenAI_ErrorStatusIncludesAPIMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limit reached","type":"rate_limit"}}`))
	}))
	defer srv.Close()

	_, err := newTestOpenAI(t, srv).Complete(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "rate limit reached") {
		t.Errorf("error should surface the API message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should include the status code, got: %v", err)
	}
}

// The key must never reach an error message, because errors are rendered on
// screen and may be pasted into a bug report.
func TestOpenAI_ErrorDoesNotLeakKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key sk-test-secret supplied"}}`))
	}))
	defer srv.Close()

	_, err := newTestOpenAI(t, srv).Complete(context.Background(), simpleRequest())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-test-secret") {
		t.Fatalf("error leaked the API key: %v", err)
	}
	if !strings.Contains(err.Error(), "***REDACTED***") {
		t.Errorf("expected redaction marker, got: %v", err)
	}
}

func TestOpenAI_NoChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	_, err := newTestOpenAI(t, srv).Complete(context.Background(), simpleRequest())
	if err == nil || !strings.Contains(err.Error(), "no choices") {
		t.Errorf("err = %v, want a no-choices error", err)
	}
}

func TestOpenAI_RespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newTestOpenAI(t, srv).Complete(ctx, simpleRequest())
	if err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	tests := map[string]string{
		"https://api.openai.com/v1":   "https://api.openai.com/v1",
		"https://api.openai.com/v1/":  "https://api.openai.com/v1",
		"https://api.openai.com/v1//": "https://api.openai.com/v1",
		"  https://gw.local/v1/  ":    "https://gw.local/v1",
	}
	for in, want := range tests {
		if got := normalizeBaseURL(in); got != want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func simpleRequest() Request {
	return Request{
		Purpose:  PurposeLens,
		Messages: []Message{{Role: RoleUser, Text: "hello"}},
	}
}

func writeChatResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{
		"model": "test-model",
		"choices": [{"message": {"role": "assistant", "content": ` +
		strconvQuote(content) + `}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 11, "completion_tokens": 5}
	}`))
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
